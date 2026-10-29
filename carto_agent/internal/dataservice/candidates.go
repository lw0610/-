package dataservice

import (
	"context"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/layers"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

func (s *Service) candidates(ctx context.Context, request FindRequest, rowName string, item datasource.AcquisitionItem) FindRow {
	row := FindRow{Row: rowName, Category: item.Category, Candidates: []Candidate{}, Excluded: []Exclusion{}}
	seen := map[string]bool{}
	addBinding := func(b Binding, kind string, tier int, reason string) {
		if b.LayerID != "" {
			if !b.Explicit && b.Category != item.Category && !s.matchName(b.Name, request.Need, item.Category) {
				return
			}
			if NormalizeGeometry(b.Geometry) != NormalizeGeometry(item.Geometry) {
				row.Excluded = append(row.Excluded, Exclusion{Reason: "工程图层几何类型不符：" + b.Name})
				return
			}
			for required := range item.Fields {
				found := false
				for _, field := range b.Fields {
					if field == required {
						found = true
						break
					}
				}
				if !found {
					row.Excluded = append(row.Excluded, Exclusion{Reason: "工程图层缺少所需字段：" + b.Name + " / " + required})
					return
				}
			}
			identity := "layer:" + b.LayerID
			if seen[identity] {
				return
			}
			seen[identity] = true
			if b.Explicit {
				tier = 0
				reason = "用户明确引用的工程图层"
			}
			row.Candidates = append(row.Candidates, Candidate{ID: candidateID(rowName, kind, identity), Kind: kind, Name: b.Name, LayerID: b.LayerID, Available: true, Tier: tier, Geometry: b.Geometry, Confidence: "high", Reasons: []string{reason}, MatchReason: "工程图层名称与几何类型匹配"})
			return
		}
		if b.Path == "" {
			return
		}
		physical, selector, _ := strings.Cut(b.Path, "|layername=")
		b.Path = physical
		if b.Sublayer == "" {
			b.Sublayer = selector
		}
		paths, err := localPaths(ctx, b.Path)
		if err != nil {
			row.Excluded = append(row.Excluded, Exclusion{Path: b.Path, Reason: err.Error()})
			return
		}
		info, _ := os.Stat(b.Path)
		folder := info != nil && info.IsDir()
		for _, path := range paths {
			layers, err := InspectProvided(ctx, path)
			if err != nil {
				row.Excluded = append(row.Excluded, Exclusion{Path: path, Reason: err.Error()})
				continue
			}
			for _, local := range layers {
				if b.Sublayer != "" && local.Sublayer != b.Sublayer {
					continue
				}
				named := b.Category == item.Category || s.matchName(local.Name, request.Need, item.Category) || (!folder && s.matchName(b.Name, request.Need, item.Category))
				explicit := b.Explicit && !folder
				if local.Geometry != NormalizeGeometry(item.Geometry) {
					if named || explicit {
						row.Excluded = append(row.Excluded, Exclusion{Path: path, Reason: "几何类型不符：需要" + item.Geometry + "，实际" + local.Geometry})
					}
					continue
				}
				if !named && !explicit && (kind != "user" || !s.relatedName(local.Name, item.Category)) {
					continue
				}
				identity := path + "|" + local.Sublayer
				if seen[identity] {
					continue
				}
				seen[identity] = true
				confidence, why := "high", "名称与几何类型匹配"
				if explicit {
					why = "用户明确提供，几何类型匹配"
				}
				if !named && !explicit {
					confidence = "confirm"
					why = "几何类型匹配，但名称关联较弱，请确认用途"
				}
				missing := false
				for field := range item.Fields {
					has := false
					for _, actual := range local.Fields {
						if field == actual {
							has = true
						}
					}
					if !has {
						missing = true
						row.Excluded = append(row.Excluded, Exclusion{Path: path, Reason: "缺少所需字段：" + field})
					}
				}
				if missing {
					continue
				}
				row.Candidates = append(row.Candidates, Candidate{ID: candidateID(rowName, kind, identity), Kind: kind, Name: local.Name, Available: true, Reasons: []string{reason, why}, Path: path, Sublayer: local.Sublayer, Tier: tier, Geometry: local.Geometry, Confidence: confidence, RequiresConfirmation: confidence == "confirm", MatchReason: why})
			}
		}
	}
	for _, path := range request.Provided {
		addBinding(Binding{Path: path, Name: filepath.Base(path), Explicit: len(request.Provided) == 1}, "user", 0, "用户本次提供")
	}
	for _, b := range s.options.UserData {
		// A historical attachment is not an explicit selection for every new need.
		b.Explicit = false
		addBinding(b, "user", 1, "用户提供")
	}
	if s.options.SessionDir != "" && s.options.SessionID != "" {
		if store, err := sessionfiles.NewAt(s.options.SessionID, s.options.SessionDir); err == nil {
			if files, err := store.List(false); err == nil {
				for _, file := range files {
					if RequiresRegistration(file.Origin.Tool) && !IsRegistered(s.options.SessionDir, file.Path) {
						continue
					}
					if file.Kind == "directory" {
						continue
					}
					name := strings.TrimSuffix(file.Name, filepath.Ext(file.Name))
					b := Binding{Name: name, Path: file.Path}
					if file.Origin.Label == rowName {
						b.Name = rowName
					}
					addBinding(b, "session", 2, "本会话已有")
				}
			}
		}
	}
	if s.options.Layers != nil {
		layers, err := s.options.Layers(ctx)
		if err == nil {
			for _, b := range layers {
				addBinding(b, "project_layer", 3, "工程已有图层")
			}
		} else {
			row.Excluded = append(row.Excluded, Exclusion{Reason: "工程图层不可检查：" + err.Error()})
		}
	} else {
		row.Excluded = append(row.Excluded, Exclusion{Reason: "未连接 QGIS，跳过工程图层"})
	}
	if request.Need.Method == "" || request.Need.Method == "builtin" {
		library, err := layers.NewDefaultLibrary()
		if err == nil {
			required := []string{}
			for field := range item.Fields {
				required = append(required, field)
			}
			var extent []float64
			if item.Extent != nil {
				extent = item.Extent.BBox
			}
			matches, matchErr := library.Match(layers.Requirement{Name: request.Need.Name, Category: item.Category, Params: item.Params, Geometry: item.Geometry, Fields: required, Extent: extent, Scale: item.Scale})
			if matchErr != nil {
				row.Excluded = append(row.Excluded, Exclusion{Reason: "内置图层库读取失败：" + matchErr.Error()})
			}
			for _, match := range matches {
				layer := match.Layer
				row.Candidates = append(row.Candidates, Candidate{ID: candidateID(rowName, "builtin_layer", layer.ID), Kind: "builtin_layer", Method: "builtin", Name: layer.Name, SourceID: layer.ID, Available: true, Tier: 4, Path: layer.Path(), Sublayer: layer.Sublayer, Geometry: layer.Geometry, Reasons: match.Reasons, RequiresConfirmation: match.NeedsConfirmation})
			}
		}
	}
	if request.Need.Method == "" && s.options.Engine != nil && s.options.Engine.Store != nil && item.Extent != nil && !item.Extent.Pending {
		regionCode := ""
		if region, err := s.options.Engine.Catalog.ResolveRegion(item.Extent.Region); err == nil {
			regionCode = region.Code
		}
		cached, err := s.options.Engine.Store.FindSemantic(ctx, item.Category, item.Params, datasource.ScaleClass(item.Scale), item.Extent.BBox, regionCode)
		if err != nil {
			row.Excluded = append(row.Excluded, Exclusion{Reason: "数据仓读取失败：" + err.Error()})
		}
		for _, record := range cached {
			notes := append([]string{}, record.Notes...)
			if record.Expired {
				notes = append(notes, "已过更新期限，可刷新来源")
			}
			row.Candidates = append(row.Candidates, Candidate{ID: candidateID(rowName, "data_store", record.Key), Kind: "data_store", Method: "store", Name: request.Need.Name, SourceID: record.Spec.SourceID, StoreKey: record.Key, Available: true, Tier: 5, Reasons: []string{"同类别、参数、比例尺档的数据仓覆盖本次范围"}, Notes: notes})
		}
	}
	preferred := map[string]bool{}
	if s.options.Memory != nil {
		entries, err := s.options.Memory.List()
		if err == nil {
			for _, e := range entries {
				match := request.Guide != "" && e.Guide == request.Guide && (e.Name == request.Need.Name || e.Name == rowName)
				if request.Guide == "" {
					match = e.Guide == "" && e.Category == item.Category && e.Extent == request.Need.Extent
				}
				if match {
					if e.SourceID != "" {
						preferred[e.SourceID] = true
					}
					if e.Path != "" {
						addBinding(Binding{Name: rowName, Path: e.Path}, "user_memory", 4, "上次使用")
					}
				}
			}
		}
	}
	choices := append([]datasource.SourceChoice{}, item.Alternatives...)
	if item.Chosen != nil {
		choices = append([]datasource.SourceChoice{*item.Chosen}, choices...)
	}
	for _, choice := range choices {
		source := s.options.Environment.Catalog.Source(choice.SourceID)
		if source == nil {
			continue
		}
		// This adapter returns a connection definition, including on cache hits.
		// A network-approved plan does not grant permission to store a key in QGIS.
		if source.Kind == "tianditu" && item.Geometry == "tiles" {
			row.Excluded = append(row.Excluded, Exclusion{SourceID: source.ID, Reason: onlineTilesUnsupported})
			continue
		}
		kind, tier, network := sourceKind(*source, choice.CacheHit)
		if tier >= 11 {
			continue
		}
		if network && s.options.Config.NetworkPolicy == "deny" {
			row.Excluded = append(row.Excluded, Exclusion{SourceID: source.ID, Reason: "设置禁止联网获取"})
			continue
		}
		reasons := []string{"来源提供所需类别并满足目录筛选条件"}
		if choice.CacheHit {
			reasons = append(reasons, "数据仓中有未过期且摘要有效的结果")
		}
		if preferred[source.ID] {
			reasons = append(reasons, "用户上次明确选择的来源")
		}
		if item.Extent != nil && !item.Extent.Pending {
			reasons = append(reasons, "覆盖所需范围")
		}
		notes := append([]string{}, source.Notes...)
		if source.License != "" {
			notes = append(notes, source.License)
		}
		available := true
		if item.Extent == nil || item.Extent.Pending {
			notes = append(notes, "范围待定，执行前须补齐")
		}
		copy := choice
		row.Candidates = append(row.Candidates, Candidate{ID: candidateID(rowName, kind, source.ID+digest(choice.Provide)), Kind: kind, Method: source.RoutingMethod(), Name: source.Name, SourceID: source.ID, Available: available, Reasons: reasons, Notes: notes, Tier: tier, Network: network, Choice: &copy, Handbook: source.Handbook.Summary()})
	}
	for _, ex := range item.Excluded {
		row.Excluded = append(row.Excluded, Exclusion{SourceID: ex.SourceID, Reason: strings.Join(ex.Reasons, "；")})
	}
	sort.SliceStable(row.Candidates, func(i, j int) bool {
		a, b := row.Candidates[i], row.Candidates[j]
		if a.Tier == b.Tier && a.RequiresConfirmation != b.RequiresConfirmation {
			return !a.RequiresConfirmation
		}
		if a.Tier != b.Tier {
			return a.Tier < b.Tier
		}
		return preferred[a.SourceID] && !preferred[b.SourceID]
	})
	for _, c := range row.Candidates {
		if c.Available {
			row.Recommended = c.ID
			break
		}
	}
	return row
}
func sourceKind(source datasource.Source, cached bool) (string, int, bool) {
	if cached {
		return "data_store", 5, false
	}
	switch source.Kind {
	case "local_folder":
		return "local_source", 4, false
	case "offline_asset":
		return "builtin_offline", 4, false
	case "tianditu", "ogc_wfs", "arcgis_rest":
		return "official_api", 8, true
	case "http_file":
		return "open_download", 8, true
	case "overpass", "gee":
		return "online_query", 8, true
	case "portal":
		return "portal", 11, true
	default:
		return "web_search", 12, true
	}
}
func (s *Service) fallbackCandidates(item datasource.AcquisitionItem, row string) []Candidate {
	out := []Candidate{}
	category := s.options.Environment.Catalog.Category(item.Category)
	if category == nil {
		return out
	}
	if category.Sensitivity == "cn_territory" {
		return out
	}
	// Browser exploration can handle portals without a deterministic adapter;
	// it must still satisfy the same configuration, parameters and coverage.
	item = s.options.Environment.RankPortalExploration(item)
	ready := map[string]bool{}
	if item.Chosen != nil {
		ready[item.Chosen.SourceID] = true
	}
	for _, choice := range item.Alternatives {
		ready[choice.SourceID] = true
	}
	excluded := map[string][]string{}
	for _, exclusion := range item.Excluded {
		excluded[exclusion.SourceID] = append(excluded[exclusion.SourceID], exclusion.Reasons...)
	}
	for _, source := range s.options.Environment.Catalog.Sources {
		if source.Kind != "portal" && source.Kind != "web_search" {
			continue
		}
		matches := false
		for _, p := range source.Provides {
			if p.Category == item.Category {
				matches = true
			}
		}
		if !matches || source.Status == "disabled" {
			continue
		}
		kind, tier, _ := sourceKind(source, false)
		reasons := append([]string{}, excluded[source.ID]...)
		available := ready[source.ID]
		if !available && len(reasons) == 0 {
			reasons = append(reasons, "来源不满足本次数据需求的类别或参数")
		}
		// These are runtime requirements of the kind itself, even when a
		// user-supplied catalog forgot to declare integration:* in requires.
		integration, label := "browser", "浏览器"
		if kind == "web_search" {
			integration, label = "web", "网页取数"
		}
		if !s.options.Environment.Integrations[integration] {
			available = false
			reason := "需要在设置 → 扩展服务启用" + label
			if !slices.Contains(reasons, reason) {
				reasons = append(reasons, reason)
			}
		}
		if available {
			reasons = append(reasons, "没有可用的确定性来源，可由方案中的数据角色探索并登记真实数据")
		}
		out = append(out, Candidate{ID: candidateID(row, kind, source.ID), Kind: kind, Method: source.RoutingMethod(), Name: source.Name, SourceID: source.ID, Available: available, Reasons: reasons, Notes: append([]string{}, source.Notes...), Tier: tier, Network: true, Handbook: source.Handbook.Summary()})
	}
	if s.options.Environment.Integrations["osm"] && category.Geometry != "table" && category.Geometry != "raster" && category.Geometry != "tiles" {
		out = append(out, Candidate{ID: candidateID(row, "custom_osm", item.Category), Kind: "custom_osm", Name: "自定义 OSM 查询", Available: true, Reasons: []string{"目录无可用来源，可按已知标签查询后登记"}, Notes: []string{"必须符合 OSM 服务限制并标注署名"}, Tier: 11, Network: true})
	}
	if s.options.Environment.Integrations["web"] && s.options.Environment.WebSearch {
		out = append(out, Candidate{ID: candidateID(row, "web_search", item.Category), Kind: "web_search", Name: "公开来源搜索", Available: true, Reasons: []string{"确定性来源不可用，可查找真实公开数据后登记"}, Notes: []string{"不得编造数据；必须保留来源网址与许可"}, Tier: 12, Network: true})
	}
	return out
}
