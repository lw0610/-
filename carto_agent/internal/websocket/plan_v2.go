package websocket

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/layers"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/planexec"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/taskmaterials"
	"github.com/cloudwego/eino/schema"
)

func (s *DeepServer) preparePlanV2(ctx context.Context, run flowSessionContext, p *plan.Plan, prior *plan.Snapshot) (*preparedCartographicPlan, error) {
	ctx = withToolSessionContext(ctx, run.SessionID, run.ProjectPath, run.InstanceID)
	if s.cartoRuntime == nil {
		return nil, fmt.Errorf("计划服务未就绪")
	}
	env, err := s.cartoRuntime.options.LoadEnvironment(ctx, run.Scheme)
	if err != nil {
		return nil, err
	}
	ctx, err = taskmaterials.Prepare(ctx)
	if err != nil {
		return nil, err
	}
	caps := map[string]bool{"route_knowledge": true}
	for _, role := range env.Roles {
		if role.Enabled && !role.IsMain {
			for _, id := range role.ToolPackages {
				caps[id] = true
			}
		}
	}
	for i := range p.Steps {
		for j, id := range p.Steps[i].Capabilities {
			if resolved, e := flow.ResolvePlanCapabilities([]string{id}, env); e == nil && len(resolved) == 1 {
				p.Steps[i].Capabilities[j] = resolved[0]
			}
		}
		for _, role := range env.Roles {
			if role.ID == strings.TrimSpace(p.Steps[i].RoleHint) && role.Executor == "route_knowledge" {
				p.Steps[i].Capabilities = []string{"route_knowledge"}
				break
			}
		}
	}
	notes := plan.NormalizeV2(p, caps)
	var guide *flow.ResolvedGuide
	if p.Guide != nil && p.Guide.ID != "" {
		guide, err = s.cartoRuntime.options.Guides.Resolve(ctx, p.Guide.ID, s.cartoRuntime.options.Resources)
		if err != nil {
			return nil, err
		}
		if !guide.Package.Enabled {
			return nil, fmt.Errorf("指南已停用")
		}
		p.Guide.Version = guide.Doc.Meta.Version
		digest := sha256.Sum256([]byte(guide.Package.GuideMD + "\n" + guide.LockDigest))
		p.Guide.Fingerprint = "sha256:" + hex.EncodeToString(digest[:])
	}
	var layoutResources []*resource.ResolvedResource
	if guide != nil {
		layoutResources = guide.Scope.Linked
	}
	paper, mapWidth, err := planMapFrame(p, layoutResources)
	if err != nil {
		return nil, err
	}
	serviceNeed := func(d plan.Data) dataservice.Need {
		need := v2ServiceNeed(d, nil)
		need.Paper, need.MapFrameWidthMM = paper, mapWidth
		return need
	}
	original := run.OriginalSource
	if prior != nil {
		original = prior.OriginalSource
	}
	for i := range p.Steps {
		step := &p.Steps[i]
		extract := false
		for _, c := range step.Capabilities {
			extract = extract || c == "route_knowledge"
		}
		if extract && len(step.Inputs) == 0 {
			refs, present, e := taskmaterials.DefaultReferences(ctx)
			if e != nil {
				return nil, e
			}
			if !present {
				refs = []string{"消息原文"}
			}
			for _, ref := range refs {
				id := "source_text"
				for n := 2; ; n++ {
					if _, exists := p.DataByID(id); !exists {
						break
					}
					id = fmt.Sprintf("source_text_%d", n)
				}
				p.Data = append(p.Data, plan.Data{ID: id, Name: "本次提供的原文", Source: "user", Ref: ref, Kind: "file"})
				step.Inputs = append(step.Inputs, id)
			}
			notes = append(notes, step.Title+" 已自动绑定本次原文")
		}
	}
	library, err := layers.NewDefaultLibrary()
	if err != nil {
		return nil, err
	}
	localLayers, err := library.List()
	if err != nil {
		return nil, err
	}
	bindings := map[string]any{}
	var service *dataservice.Service
	getService := func() (*dataservice.Service, error) {
		if service == nil {
			service, err = s.dataService(ctx, run.SessionID)
		}
		return service, err
	}
	var projectLayers []dataservice.Binding
	projectRead := false
	precheck := plan.CheckData(p, func(d *plan.Data) plan.Precheck {
		bad := func(reason, suggestion string) plan.Precheck {
			return plan.Precheck{Status: "unavailable", Reason: reason, Suggestion: suggestion}
		}
		good := func(reason string) plan.Precheck { return plan.Precheck{Status: "available", Reason: reason} }
		if d.Source == "" {
			if _, e := layers.ResolveName(localLayers, d.Name); e == nil {
				d.Source = "builtin"
				d.Ref = d.Name
			} else {
				d.Source = "retrieve"
			}
			notes = append(notes, d.Name+" 自动确定来源为 "+d.Source)
		}
		if d.Need == nil && d.Source == "retrieve" {
			d.Need = &plan.Need{Category: d.Name}
		}
		category := ""
		params := map[string]string{}
		fields := []string{}
		if d.Need != nil {
			category = d.Need.Category
			fields = d.Need.Fields
			for k, v := range d.Need.Params {
				params[k] = fmt.Sprint(v)
			}
		}
		if category == "" {
			category = d.Name
		}
		if env.DataSources != nil {
			if c, pp, e := env.DataSources.ResolveAcquisitionCategory(category, params); e == nil && c != nil {
				category, params = c.ID, pp
				if d.Geometry == "" && c.Geometry != "table" {
					d.Geometry = c.Geometry
				}
				if d.Kind == "" {
					if c.Geometry == "table" {
						d.Kind = "table"
					} else {
						d.Kind = "layer"
					}
				}
				if d.Need != nil {
					d.Need.Category = c.ID
					if d.Need.Params == nil {
						d.Need.Params = map[string]any{}
					}
					for k, v := range pp {
						d.Need.Params[k] = v
					}
				}
			}
		}
		if d.Kind == "" {
			d.Kind = "file"
		}
		switch d.Source {
		case "user":
			if d.Ref == "消息原文" || d.Ref == "current_user_message" {
				d.Ref = "消息原文"
				bindings[d.ID] = map[string]any{"source": "user", "text": original}
				return good("本次消息原文")
			}
			physical, sublayer, hasSublayer := strings.Cut(d.Ref, "|layername=")
			material, e := taskmaterials.ResolveReference(ctx, physical)
			if e != nil {
				return bad(e.Error(), "从本次材料清单选择文件或文件夹中的具体文件")
			}
			physical = material.Path
			ref := physical
			if hasSublayer {
				ref += "|layername=" + sublayer
			}
			isDir := material.Kind == "folder"
			if isDir && d.Kind == "file" {
				d.Kind = "dataset"
			}
			if isDir && d.Kind != "dataset" {
				svc, e := getService()
				if e != nil {
					return bad(e.Error(), "检查数据目录")
				}
				need := serviceNeed(*d)
				if need.What.Category == "" {
					need.What.Category = category
				}
				result, e := svc.Find(ctx, dataservice.FindRequest{Need: need, Provided: []string{physical}})
				if e != nil {
					return bad(e.Error(), "选择文件夹中的具体文件")
				}
				for _, row := range result.Rows {
					for _, c := range row.Candidates {
						if c.ID == row.Recommended && c.Available && c.Path != "" {
							d.Ref = c.Path
							if c.Sublayer != "" {
								d.Ref += "|layername=" + c.Sublayer
							}
							d.Geometry = c.Geometry
							bindings[d.ID] = d.Ref
							status := "available"
							if c.RequiresConfirmation {
								status = "confirm"
							}
							return plan.Precheck{Status: status, Reason: c.MatchReason}
						}
					}
				}
				return bad("文件夹中没有匹配名称、几何和字段的数据", "选择具体图层或改用内置图层")
			}
			if isDir {
				d.Kind = "dataset"
			} else if d.Kind == "layer" || d.Kind == "table" || d.Geometry != "" || strings.Contains("|.shp|.gpkg|.geojson|.csv|.xlsx|.tif|.tiff|", "|"+strings.ToLower(filepath.Ext(physical))+"|") {
				required := map[string]string{}
				for _, f := range fields {
					required[f] = f
				}
				b, e := dataservice.ValidateLocalData(ctx, ref, d.Geometry, required)
				if e != nil {
					return bad(e.Error(), "选择几何和字段符合要求的文件")
				}
				d.Geometry = b.Geometry
				if b.Geometry == "table" {
					d.Kind = "table"
				} else {
					d.Kind = "layer"
				}
			}
			d.Ref = ref
			bindings[d.ID] = ref
			return good("用户文件存在，数据类型已核对")
		case "project":
			if !projectRead {
				projectRead = true
				value, e := (flowRuntimeBinding{server: s}).Invoke(ctx, "get_layers_info", map[string]any{"include_invisible": true})
				if e != nil {
					return bad(e.Error(), "连接当前工程")
				}
				projectLayers, e = dataLayerBindings(value)
				if e != nil {
					return bad(e.Error(), "重新选择工程图层")
				}
			}
			for _, l := range projectLayers {
				if l.LayerID == d.Ref || l.Name == d.Ref {
					if d.Geometry != "" && dataservice.NormalizeGeometry(d.Geometry) != dataservice.NormalizeGeometry(l.Geometry) {
						return bad("工程图层几何不符", "选择正确几何类型的图层")
					}
					for _, f := range fields {
						found := false
						for _, actual := range l.Fields {
							found = found || actual == f
						}
						if !found {
							return bad("工程图层缺少字段："+f, "补充字段或更换图层")
						}
					}
					d.Ref = l.LayerID
					d.Kind = "layer"
					d.Geometry = l.Geometry
					bindings[d.ID] = l.LayerID
					return good("当前工程图层存在，几何与字段符合需求")
				}
			}
			return bad("工程中不存在图层："+d.Ref, "选择工程中的图层或其他来源")
		case "builtin", "retrieve":
			// The local discovery service ranks explicitly provided files and project
			// layers ahead of built-ins; never replace a user's matching dataset.
			var found *dataservice.FindResult
			useLocal := d.Method == "" || d.Method == "builtin"
			if svc, e := getService(); e == nil {
				probe := serviceNeed(*d)
				if probe.What.Category == "" {
					probe.What.Category = category
				}
				if candidateResult, e := svc.Find(ctx, dataservice.FindRequest{Need: probe, IncludeFallback: true}); e == nil {
					if d.Extent != nil && d.Extent.Region != "" && candidateResult.Resolved.Extent != nil && candidateResult.Resolved.Extent.Pending {
						return bad("无法解析指定地区："+d.Extent.Region, "指定单个可识别的地区、bbox，或用 extent.from 引用上游产出")
					}
					found = &candidateResult
					for _, row := range candidateResult.Rows {
						for _, c := range row.Candidates {
							if !useLocal || !c.Available || !((c.Kind == "user" || c.Kind == "user_memory" || c.Kind == "session") && c.Path != "" || c.Kind == "project_layer") {
								continue
							}
							d.Source = "user"
							d.Ref = c.Path
							if c.Sublayer != "" {
								d.Ref += "|layername=" + c.Sublayer
							}
							if c.Kind == "project_layer" {
								d.Source = "project"
								d.Ref = c.LayerID
							}
							d.Geometry = c.Geometry
							bindings[d.ID] = d.Ref
							status := "available"
							if c.RequiresConfirmation {
								status = "confirm"
							}
							return plan.Precheck{Status: status, Reason: c.MatchReason}
						}
					}
				}
			}
			req := layers.Requirement{Name: d.Name, Category: category, Params: params, Geometry: d.Geometry, Fields: fields}
			if d.Source == "builtin" {
				req.ExplicitName = d.Ref
			}
			if d.Extent != nil {
				req.Extent = d.Extent.BBox
				if d.Extent.Region != "" && env.DataSources != nil {
					if region, e := env.DataSources.ResolveRegion(d.Extent.Region); e == nil {
						req.Extent = region.BBox
					}
				}
			}
			candidates := layers.Match(localLayers, req)
			if useLocal && len(candidates) > 0 {
				c := candidates[0]
				d.Source = "builtin"
				d.Ref = c.Layer.Name
				d.Kind = "layer"
				d.Geometry = c.Layer.Geometry
				if d.Style == "" {
					d.Style = c.Layer.DefaultStyle
				}
				if d.LabelField == "" {
					d.LabelField = c.Layer.LabelField
				}
				status := "available"
				if c.NeedsConfirmation {
					status = "confirm"
				}
				return plan.Precheck{Status: status, Reason: strings.Join(c.Reasons, "；"), Pack: c.Layer.Pack}
			}
			explicit := d.Source == "builtin"
			oldRef := d.Ref
			if d.Need == nil {
				d.Need = &plan.Need{Category: category, Params: map[string]any{}}
				for k, v := range params {
					d.Need.Params[k] = v
				}
			}
			if d.Extent == nil && d.Need.Region != "" {
				d.Extent = &plan.Extent{Region: d.Need.Region}
			}
			if d.Extent == nil {
				return bad("没有可用内置图层，检索范围尚未指定", "指定地区、bbox 或前序产出 extent.from")
			}
			svc, e := getService()
			if e != nil {
				return bad(e.Error(), "检查数据获取设置")
			}
			var result dataservice.FindResult
			if found != nil {
				result = *found
			} else {
				result, e = svc.Find(ctx, dataservice.FindRequest{Need: serviceNeed(*d), IncludeFallback: true})
				if e == nil && d.Extent.Region != "" && result.Resolved.Extent != nil && result.Resolved.Extent.Pending {
					return bad("无法解析指定地区："+d.Extent.Region, "指定单个可识别的地区、bbox，或用 extent.from 引用上游产出")
				}
			}
			if e != nil {
				return bad(e.Error(), "检查数据类别、范围与获取设置")
			}
			methods := []string{}
			selection := ""
			estimatedSeconds := 0
			if env.DataSources != nil && result.Resolved.ScaleDenominator > 0 {
				if category := env.DataSources.Category(result.Resolved.Category); category != nil {
					rule := category.Routing[datasource.ScaleClass(result.Resolved.ScaleDenominator)]
					selection = rule.Filter
					if rule.Note != "" {
						selection += "；" + rule.Note
					}
				}
			}
			for _, row := range result.Rows {
				for _, c := range row.Candidates {
					if c.Available {
						method := c.Method
						if method == "" {
							method = c.Kind
						}
						methods = append(methods, method)
						if len(methods) == 1 && env.DataSources != nil {
							if source := env.DataSources.Source(c.SourceID); source != nil {
								estimatedSeconds = source.TypicalSeconds
							}
						}
					}
				}
			}
			for _, c := range result.Fallback {
				if c.Available {
					methods = append(methods, c.Kind)
				}
			}
			if len(methods) == 0 {
				reasons := []string{}
				for _, diagnostic := range result.Diagnostics {
					reasons = append(reasons, diagnostic.Message)
				}
				return bad(strings.Join(reasons, "；"), "启用可用取数方式或提供数据")
			}
			d.Source = "retrieve"
			d.Ref = ""
			status, reason := "available", "按需求范围和比例尺自动检索"
			if explicit {
				status = "confirm"
				reason = "点名的内置图层「" + oldRef + "」不可用，建议自动检索"
			}
			if d.Extent.From != "" {
				reason += "；范围在上游数据就绪后计算"
			}
			return plan.Precheck{Status: status, Reason: reason, Methods: methods, Selection: strings.Trim(selection, "；"), EstimatedSeconds: estimatedSeconds}
		default:
			return bad("未知数据来源："+d.Source, "选择用户文件、工程图层、内置图层或自动检索")
		}
	})
	diagnostics := plan.ValidateV2(p)
	if flow.HasErrors(diagnostics) {
		return nil, plan.NewValidationError(diagnostics, plan.Environment{Capabilities: caps})
	}
	options := planexec.BuildOptions{Environment: env, Bindings: bindings, OriginalSource: original}
	if guide != nil {
		options.Guide = guide.Doc
		options.Scope = guide.Scope
	}
	executable, inputs, err := planexec.Build(p, options)
	if err != nil {
		return nil, err
	}
	policy := "when_needed"
	networkPolicy := "direct"
	if s.configService != nil {
		policy = s.configService.GetCartographicPlanConfig().Confirm
		networkPolicy = s.configService.GetAcquisitionConfig().NetworkPolicy
	}
	network := false
	for _, d := range p.Data {
		network = network || d.Source == "retrieve"
	}
	confirm := planexec.ConfirmationFor(p, policy, networkPolicy, network)
	unavailable := false
	for _, item := range precheck {
		unavailable = unavailable || item.Status == "unavailable"
		if item.Status == "confirm" && policy != "never" {
			confirm.Required = true
			confirm.Reasons = append(confirm.Reasons, item.Reason)
		}
	}
	snapshot := &plan.Snapshot{Plan: p, PlanID: p.ID, PlanVersion: p.Version, SessionID: run.SessionID, Status: "ready", NeedsConfirmation: confirm.Required, ConfirmationReasons: confirm.Reasons, ExecutionMode: string(run.Mode), OriginalSource: original, Precheck: precheck, Notes: notes}
	if confirm.Required {
		snapshot.Status = "awaiting_confirmation"
	}
	if run.Mode == executionModePlan {
		snapshot.Status = "preview"
		snapshot.NeedsConfirmation = false
	}
	if unavailable {
		snapshot.Status = "needs_data"
		snapshot.NeedsConfirmation = false
	}
	if prior != nil {
		snapshot.RunID = prior.RunID
	}
	if guide != nil {
		snapshot.GuideSnapshot, _ = json.Marshal(guide)
	}
	return &preparedCartographicPlan{Snapshot: snapshot, Executable: executable, Inputs: inputs, Guide: options.Guide}, nil
}

func v2ServiceNeed(d plan.Data, resolved *datasource.ExtentPlan) dataservice.Need {
	n := dataservice.Need{Name: d.Name, Geometry: d.Geometry, Method: d.Method, Fields: map[string]string{}, Params: map[string]string{}, ExtentPlan: resolved}
	if d.Need != nil {
		n.What = dataservice.What{Category: d.Need.Category, Description: d.Need.Description}
		for k, v := range d.Need.Params {
			n.Params[k] = fmt.Sprint(v)
		}
		for _, f := range d.Need.Fields {
			n.Fields[f] = f
		}
		for k, v := range map[string]string{"region": d.Need.Region, "level": d.Need.Level, "indicator": d.Need.Indicator, "caliber": d.Need.Caliber, "unit": d.Need.Unit} {
			if v != "" {
				n.Params[k] = v
			}
		}
		if d.Need.Year > 0 {
			n.Params["year"] = strconv.Itoa(d.Need.Year)
		}
	}
	if n.ExtentPlan == nil && d.Extent != nil {
		switch {
		case len(d.Extent.BBox) == 4:
			n.ExtentPlan = &datasource.ExtentPlan{BBox: d.Extent.BBox}
		case d.Extent.Region != "":
			n.Extent = d.Extent.Region
		case d.Extent.From != "":
			n.ExtentPlan = &datasource.ExtentPlan{Source: d.Extent.From, Pending: true}
		}
	}
	return n
}

func (s *DeepServer) editPlanV2(ctx context.Context, run flowSessionContext, prior *plan.Snapshot, edit plan.EditRequest) (*plan.Snapshot, error) {
	if prior.Plan.Version != edit.Version {
		return nil, plan.ErrVersionConflict
	}
	if prior.Status == "running" {
		return nil, fmt.Errorf("请先暂停当前运行")
	}
	next, err := plan.Clone(prior.Plan)
	if err != nil {
		return nil, err
	}
	found := false
	for i := range next.Data {
		d := &next.Data[i]
		if d.ID != edit.DataID {
			continue
		}
		found = true
		d.Source, d.Ref, d.Method = edit.Source, edit.Ref, edit.Method
		if d.Source == "user" && d.Ref != "" && d.Ref != "消息原文" && d.Ref != "current_user_message" {
			// Source edits come from the user's file picker and use the same entry
			// point as chat attachments, including directory grants and retention.
			physical := strings.SplitN(d.Ref, "|layername=", 2)[0]
			msg := Message{Data: map[string]any{"attachments": []plan.Attachment{{Path: physical}}}}
			if e := s.bindTaskReferences(ctx, run.SessionID, run.ProjectPath, msg, schema.UserMessage("")); e != nil {
				return nil, e
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("计划中没有数据项 %s", edit.DataID)
	}
	next.Version++
	if prior.RunID != "" {
		state, e := s.planRunSnapshot(run, prior.RunID)
		if e != nil {
			return nil, e
		}
		flowrun.PreserveCompleted(state, next)
	}
	prepared, err := s.preparePlanV2(ctx, run, next, prior)
	if err != nil {
		return nil, err
	}
	store, err := planStore(run)
	if err != nil {
		return nil, err
	}
	if err = store.Save(prepared.Snapshot, prior.Plan.Version); err != nil {
		return nil, err
	}
	s.publishPlan(prepared.Snapshot)
	return prepared.Snapshot, nil
}
