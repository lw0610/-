package datasource

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

// Acquisition models live with catalogs so both flow preview and runtime can use
// the same deterministic ranking rules without a flow/acquire import cycle.
type AcquisitionPlan struct {
	Items []AcquisitionItem `json:"items"`
}
type ExtentPlan struct {
	BBox             []float64 `json:"bbox,omitempty"`
	Path             string    `json:"path,omitempty"`
	Source           string    `json:"source"`
	Region           string    `json:"region,omitempty"`
	Pending          bool      `json:"pending"`
	DependencyStep   string    `json:"dependency_step,omitempty"`
	DependencyOutput string    `json:"dependency_output,omitempty"`
	ExpandPercent    float64   `json:"expand_percent,omitempty"`
}
type AcquisitionItem struct {
	Method             string            `json:"method,omitempty"`
	Key                string            `json:"key"`
	Requirement        string            `json:"requirement"`
	Entry              string            `json:"entry,omitempty"`
	Category           string            `json:"category"`
	Params             map[string]string `json:"params"`
	Fields             map[string]string `json:"fields"`
	Extent             *ExtentPlan       `json:"extent"`
	Scale              int               `json:"scale_denominator"`
	Mode               string            `json:"mode"`
	Chosen             *SourceChoice     `json:"chosen,omitempty"`
	Alternatives       []SourceChoice    `json:"alternatives"`
	Excluded           []SourceExclusion `json:"excluded"`
	Available          bool              `json:"available"`
	Reasons            []string          `json:"reasons"`
	Notes              []string          `json:"notes"`
	Input              string            `json:"input"`
	InputType          string            `json:"input_type,omitempty"`
	Resource           string            `json:"resource,omitempty"`
	Filename           string            `json:"filename,omitempty"`
	Required           bool              `json:"required"`
	Acquirable         bool              `json:"acquirable"`
	Geometry           string            `json:"geometry,omitempty"`
	ExtentExpression   string            `json:"extent_expression,omitempty"`
	ScaleExpression    string            `json:"scale_expression,omitempty"`
	Acquisition        string            `json:"acquisition,omitempty"`
	RecommendedSources []string          `json:"recommended_sources,omitempty"`
	ExcludedSources    []string          `json:"excluded_sources,omitempty"`
	UserPath           string            `json:"user_path,omitempty"`
}
type SourceChoice struct {
	SourceID    string      `json:"source_id"`
	Name        string      `json:"name"`
	Tier        int         `json:"tier"`
	Derived     string      `json:"derived_from,omitempty"`
	Derivation  *Derivation `json:"derivation,omitempty"`
	Provide     Provide     `json:"provide"`
	CacheHit    bool        `json:"cache_hit"`
	StoreKey    string      `json:"store_key,omitempty"`
	EstimatedMB float64     `json:"estimated_mb,omitempty"`
	License     string      `json:"license"`
	Attribution string      `json:"attribution"`
	NeedsLogin  bool        `json:"needs_login,omitempty"`
}
type SourceExclusion struct {
	SourceID string   `json:"source_id"`
	Name     string   `json:"name"`
	Reasons  []string `json:"reasons"`
}
type PlanningEnvironment struct {
	CacheLookup   func(AcquisitionItem, SourceChoice) (string, bool)
	Catalog       *Catalog
	Integrations  map[string]bool
	Secrets       map[string]bool
	AutoAcquire   string
	WebSearch     bool
	MaxDownloadMB int

	portalExploration bool
}

// RankPortalExploration preserves all ordinary source prerequisites, while
// allowing a browser expert to explore portals without a deterministic adapter.
// Experimental download/query adapters remain unavailable in both modes.
func (e PlanningEnvironment) RankPortalExploration(item AcquisitionItem) AcquisitionItem {
	e.portalExploration = true
	e.CacheLookup = nil // A cached file cannot authorize a fresh portal operation.
	return e.Rank(item)
}

func (e PlanningEnvironment) Rank(item AcquisitionItem) AcquisitionItem {
	item.Chosen = nil
	item.Alternatives = []SourceChoice{}
	item.Excluded = []SourceExclusion{}
	item.Reasons = []string{}
	if e.Catalog == nil {
		item.Available = false
		item.Reasons = append(item.Reasons, "数据源目录尚未就绪")
		return item
	}
	category := e.Catalog.Category(item.Category)
	if category == nil || !item.Acquirable {
		item.Available = false
		item.Reasons = append(item.Reasons, "此条目未声明可自动获取的类别")
		return item
	}
	candidates := []SourceChoice{}
	for _, source := range e.Catalog.Sources {
		for _, provide := range source.Provides {
			var derive *Derivation
			if provide.Category != item.Category {
				for _, d := range category.Derive {
					if d.From == provide.Category {
						copy := d
						derive = &copy
						break
					}
				}
				if derive == nil {
					continue
				}
			}
			params := provide.EffectiveParams(item.Params, derive)
			matches := true
			for k, v := range provide.Params {
				if params[k] != v {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
			if _, _, err := e.Catalog.ResolveCategory(item.Category, params); err != nil {
				item.Excluded = append(item.Excluded, SourceExclusion{SourceID: source.ID, Name: source.Name, Reasons: []string{err.Error()}})
				continue
			}
			// A cache hit only removes download prerequisites, never catalog policy.
			if reasons := e.sourceReasonsForCache(source, category, item, true); len(reasons) > 0 {
				item.Excluded = append(item.Excluded, SourceExclusion{SourceID: source.ID, Name: source.Name, Reasons: reasons})
				continue
			}
			choice := SourceChoice{SourceID: source.ID, Name: source.Name, Tier: sourceTier(source), Provide: provide, EstimatedMB: source.SizeMB, License: source.License, Attribution: source.Attribution, NeedsLogin: contains(source.Requires, "login:user"), Derivation: derive}
			if derive != nil {
				choice.Derived = derive.From
			}
			if e.CacheLookup != nil {
				if key, ok := e.CacheLookup(item, choice); ok {
					choice.Tier = 1
					choice.CacheHit = true
					choice.StoreKey = key
				}
			}
			reasons := e.sourceReasonsForCache(source, category, item, choice.CacheHit)
			if len(reasons) > 0 {
				item.Excluded = append(item.Excluded, SourceExclusion{SourceID: source.ID, Name: source.Name, Reasons: reasons})
				continue
			}
			candidates = append(candidates, choice)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		sa, sb := e.Catalog.Source(a.SourceID), e.Catalog.Source(b.SourceID)
		if a.Tier != b.Tier {
			return a.Tier < b.Tier
		}
		if len(category.Routing) > 0 {
			order := category.Routing[ScaleClass(item.Scale)].Methods
			ar, br := preference(order, sa.RoutingMethod()), preference(order, sb.RoutingMethod())
			if ar != br {
				return ar < br
			}
		}
		if (a.Derived == "") != (b.Derived == "") {
			return a.Derived == ""
		}
		ar, br := preference(item.RecommendedSources, a.SourceID), preference(item.RecommendedSources, b.SourceID)
		if ar != br {
			return ar < br
		}
		if sa.Priority != sb.Priority {
			return sa.Priority < sb.Priority
		}
		if a.EstimatedMB != b.EstimatedMB {
			return a.EstimatedMB < b.EstimatedMB
		}
		return a.SourceID < b.SourceID
	})
	item.Available = len(candidates) > 0
	if item.Available {
		item.Chosen = &candidates[0]
		item.Alternatives = candidates[1:]
	} else {
		for _, ex := range item.Excluded {
			for _, r := range ex.Reasons {
				if !contains(item.Reasons, r) {
					item.Reasons = append(item.Reasons, r)
				}
			}
		}
		if len(item.Reasons) == 0 {
			item.Reasons = append(item.Reasons, "没有提供此类别及参数的数据源")
		}
	}
	return item
}
func (e PlanningEnvironment) sourceReasons(s Source, c *Category, item AcquisitionItem) []string {
	return e.sourceReasonsForCache(s, c, item, false)
}
func (e PlanningEnvironment) sourceReasonsForCache(s Source, c *Category, item AcquisitionItem, cached bool) []string {
	reasons := []string{}
	method := s.RoutingMethod()
	if item.Method != "" && method != item.Method && s.ID != item.Method {
		reasons = append(reasons, "用户指定检索方式："+item.Method)
	}
	if s.Kind != "local_folder" && s.Kind != "offline_asset" && c != nil && len(c.Routing) > 0 {
		rule := c.Routing[ScaleClass(item.Scale)]
		if !contains(rule.Methods, method) && !(e.portalExploration && (method == "portal" || method == "search") && contains(rule.Methods, "agent")) {
			reasons = append(reasons, "此类别在当前比例尺档不使用"+method)
		}
	}
	if len(s.ScaleClasses) > 0 && item.Scale > 0 && !contains(s.ScaleClasses, ScaleClass(item.Scale)) {
		reasons = append(reasons, "来源不适用当前比例尺档")
	}
	if s.ChineseNames != nil && !*s.ChineseNames && (item.Extent == nil || len(item.Extent.BBox) != 4 || Intersects(item.Extent.BBox, []float64{73, 3, 136, 54})) {
		for field, standard := range item.Fields {
			if field == "name" || standard == "name" {
				reasons = append(reasons, "需要中文名称，此来源不提供中文名")
				break
			}
		}
	}
	if s.Status == "experimental" && !(e.portalExploration && s.Kind == "portal") {
		reasons = append(reasons, "该来源尚未完成验证")
	}
	if s.Status == "disabled" {
		reasons = append(reasons, "该来源已停用")
	}
	if e.AutoAcquire == "deny" {
		reasons = append(reasons, "当前方案已关闭自动获取数据")
	}
	if contains(item.ExcludedSources, s.ID) {
		reasons = append(reasons, "流程排除了该来源")
	}
	if s.Kind == "web_search" && !e.WebSearch {
		reasons = append(reasons, "尚未允许联网搜索数据")
	}
	for _, required := range s.Requires {
		if cached {
			continue
		}
		if strings.HasPrefix(required, "integration:") {
			name := strings.TrimPrefix(required, "integration:")
			if !e.Integrations[name] {
				label := map[string]string{"osm": "OpenStreetMap", "web": "网页取数", "browser": "浏览器", "gee": "Google Earth Engine"}[name]
				if label == "" {
					label = name
				}
				reasons = append(reasons, "需要在设置 → 扩展服务启用"+label)
			}
		}
		if strings.HasPrefix(required, "secret:") {
			name := strings.TrimPrefix(required, "secret:")
			if !e.Secrets[name] {
				reasons = append(reasons, "需要配置 "+name+" 密钥")
			}
		}
	}
	if item.Scale > 0 {
		if s.Scale.MinDenominator > 0 && item.Scale < s.Scale.MinDenominator {
			reasons = append(reasons, fmt.Sprintf("该来源过于概略，不适合 1:%d", item.Scale))
		}
		if s.Scale.MaxDenominator > 0 && item.Scale > s.Scale.MaxDenominator {
			reasons = append(reasons, "该来源适合较小范围，请选择区域提取包")
		}
	}
	if !cached && e.MaxDownloadMB > 0 && s.SizeMB > float64(e.MaxDownloadMB) {
		reasons = append(reasons, fmt.Sprintf("预计 %.0f MB，超过下载上限 %d MB", s.SizeMB, e.MaxDownloadMB))
	}
	if s.Kind == "local_folder" {
		if info, err := os.Stat(s.Root); err != nil || !info.IsDir() {
			reasons = append(reasons, "本机数据源目录不存在")
		}
	}
	china := true
	if item.Extent != nil && len(item.Extent.BBox) == 4 {
		box := item.Extent.BBox
		china = Intersects(box, []float64{73, 3, 136, 54})
		if len(s.Coverage.BBox) == 4 && !Contains(s.Coverage.BBox, box) {
			reasons = append(reasons, "获取范围超出了该来源的覆盖范围")
		}
		if contains(s.Coverage.Regions, "CN") && !Contains([]float64{72, 0, 137, 55}, box) {
			reasons = append(reasons, "该来源只覆盖中国")
		}
		area := (box[2] - box[0]) * 111.32 * math.Cos((box[1]+box[3])*math.Pi/360) * (box[3] - box[1]) * 111.32
		if s.Limits.MinAreaKM2 > 0 && area < s.Limits.MinAreaKM2 {
			reasons = append(reasons, "此大型提取包不适合当前小范围，优先区域查询")
		}
		if s.Limits.MaxAreaKM2 > 0 && area > s.Limits.MaxAreaKM2 {
			reasons = append(reasons, "范围超出该来源单次查询限制，请使用区域提取包")
		}
	}
	if reason := TerritoryReason(c, &s, china); reason != "" {
		reasons = append(reasons, reason)
	}
	return reasons
}

// ScaleClass is shared by routing, normalization and semantic cache keys.
func ScaleClass(denominator int) string {
	if denominator <= 500000 {
		return "detailed"
	}
	if denominator <= 3000000 {
		return "regional"
	}
	return "overview"
}
func (s Source) RoutingMethod() string {
	if s.Method != "" {
		return s.Method
	}
	switch s.Kind {
	case "web_search":
		return "search"
	case "offline_asset", "local_folder":
		return "builtin"
	}
	return s.Kind
}
func sourceTier(s Source) int {
	if s.Kind == "local_folder" || s.Kind == "offline_asset" {
		return 0
	}
	return 2
}
func preference(values []string, id string) int {
	for i, v := range values {
		if v == id {
			return i
		}
	}
	return len(values)
}
func Intersects(a, b []float64) bool {
	return len(a) == 4 && len(b) == 4 && a[0] <= b[2] && a[2] >= b[0] && a[1] <= b[3] && a[3] >= b[1]
}
func Contains(a, b []float64) bool {
	return len(a) == 4 && len(b) == 4 && a[0] <= b[0] && a[1] <= b[1] && a[2] >= b[2] && a[3] >= b[3]
}

// EffectiveParams merges execution defaults without changing caller-owned maps.
// Params remain source constraints; Defaults may be overridden by the request.
// Derivation parameters describe the source data needed for the transformation.
func (p Provide) EffectiveParams(params map[string]string, derivation *Derivation) map[string]string {
	merged := map[string]string{}
	for k, v := range p.Defaults {
		merged[k] = v
	}
	for k, v := range params {
		merged[k] = v
	}
	if derivation != nil {
		for k, v := range derivation.Params {
			merged[k] = v
		}
	}
	return merged
}

// ResolveAcquisitionCategory validates declared values while allowing a required
// value to come from a source default. Defaults stay with each source choice, so
// switching/falling back to another source cannot reuse the first source's defaults.
// Rank validates the effective parameters again for each eligible source.
func (c *Catalog) ResolveAcquisitionCategory(name string, params map[string]string) (*Category, map[string]string, error) {
	category, merged, err := c.ResolveCategory(name, params)
	if err == nil || category == nil {
		return category, merged, err
	}
	for _, source := range c.Sources {
		for _, provide := range source.Provides {
			if provide.Category != category.ID || len(provide.Defaults) == 0 {
				continue
			}
			effective := provide.EffectiveParams(merged, nil)
			matches := true
			for k, v := range provide.Params {
				if effective[k] != v {
					matches = false
					break
				}
			}
			if _, _, candidateErr := c.ResolveCategory(category.ID, effective); matches && candidateErr == nil {
				return category, merged, nil
			}
		}
	}
	return category, merged, err
}
