package flow

import (
	"context"
	"fmt"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/resource"
	"regexp"
	"strconv"
	"strings"
)

func (e Environment) AcquisitionEnvironment() datasource.PlanningEnvironment {
	return datasource.PlanningEnvironment{Catalog: e.DataSources, Integrations: e.Integrations, Secrets: e.Secrets, AutoAcquire: e.AutoAcquire, WebSearch: e.WebSearchEnabled, MaxDownloadMB: e.MaxDownloadMB}
}
func (c *Compiler) AcquisitionEnvironment(ctx context.Context, scheme string) (datasource.PlanningEnvironment, error) {
	e, err := c.options.LoadEnvironment(ctx, scheme)
	return e.AcquisitionEnvironment(), err
}

var extentExpression = regexp.MustCompile(`^「([^」]+)」(?:\s*外扩\s*([0-9]+(?:\.[0-9]+)?)\s*[%％])?$`)

func buildRequirements(doc *FlowDoc, plan *FlowPlan, resources []*resource.ResolvedResource, env Environment) {
	if len(doc.Requirements) == 0 {
		return
	}
	plan.Requirements = append([]RequirementDoc{}, doc.Requirements...)
	plan.Acquisition = &AcquisitionPlan{Items: []AcquisitionItem{}}
	plan.AutoAcquire = env.AutoAcquire
	if plan.AutoAcquire == "" {
		plan.AutoAcquire = "ask"
	}
	diagnose := func(req RequirementDoc, code, level, message string) {
		plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Code: code, Level: level, Message: req.Name + "：" + message, Line: req.Line})
	}
	for index, req := range doc.Requirements {
		input := -1
		resourceKey := ""
		entries := []resource.ResourceEntry{}
		if req.List != "" {
			for _, r := range resources {
				if r.Label == strings.Trim(req.List, "「」") && r.Type == "data_list" {
					resourceKey = r.Key
					entries = r.Entries
					for i, p := range plan.Inputs {
						if p.Key == r.BaseDirFromInput && p.Type == "folder" {
							input = i
							break
						}
					}
					break
				}
			}
		} else {
			for i, p := range plan.Inputs {
				if p.Label == req.Name {
					input = i
					break
				}
			}
		}
		if input < 0 {
			diagnose(req, "FLOW_REQ_BIND_MISSING", "error", "数据需求未关联文件、图层输入或带文件夹输入的数据清单")
			continue
		}
		in := &plan.Inputs[input]
		if req.List == "" && in.Type == "folder" {
			diagnose(req, "FLOW_REQ_FOLDER_NEEDS_LIST", "error", "文件夹输入必须通过数据清单声明需求")
			continue
		}
		if req.List == "" && in.Type != "file" && in.Type != "layer" {
			diagnose(req, "FLOW_REQ_BIND_MISSING", "error", "单项数据需求须关联文件或图层输入")
			continue
		}
		if req.List == "" {
			entries = []resource.ResourceEntry{{Name: "", Kind: "", Fields: map[string]any{"category": req.Category, "acquire": true, "required": in.Required}}}
		}
		extent, err := RequirementExtent(req.Extent, plan, env.DataSources)
		if err != nil {
			diagnose(req, "FLOW_REQ_EXTENT_INVALID", "error", err.Error())
		}
		firstConsumer := len(plan.Steps)
		for i, step := range plan.Steps {
			if step.Kind == "acquire" {
				continue
			}
			if hasString(step.Refs.Inputs, in.Key) || resourceKey != "" && hasString(step.Refs.Resources, resourceKey) {
				firstConsumer = i
				break
			}
		}
		if req.Acquisition != "" && req.Acquisition != "系统" {
			stepID := strings.Trim(req.Acquisition, "「」")
			found := -1
			for i, step := range plan.Steps {
				if step.ID == stepID || step.Title == stepID || stepID == fmt.Sprintf("第%d步", step.Index) {
					for _, o := range step.ExpectedOutputs {
						if o.Label == req.Name {
							found = i
							break
						}
					}
				}
			}
			if found < 0 || found >= firstConsumer {
				diagnose(req, "FLOW_REQ_STEP_INVALID", "error", "自定义获取步骤必须位于消费步骤之前且声明同名产出")
			} else {
				firstConsumer = found + 1
			}
		}
		for _, entry := range entries {
			key := req.Name
			if entry.Name != "" {
				key += "/" + entry.Name
			}
			categoryName := req.Category
			if req.List != "" {
				categoryName, _ = entry.Fields["category"].(string)
			}
			allowed := categoryName != ""
			if value, ok := entry.Fields["acquire"].(bool); ok && !value {
				allowed = false
			}
			required := true
			if value, ok := entry.Fields["required"].(bool); ok {
				required = value
			}
			params := map[string]string{}
			for k, v := range req.Params {
				params[k] = v
			}
			if req.Level != "" {
				params["level"] = req.Level
			}
			fields := map[string]string{}
			for k, v := range req.Fields {
				fields[k] = strings.ToLower(v)
			}
			if raw, ok := entry.Fields["fields"].(string); ok {
				fields = RequirementFields(raw)
			}
			item := AcquisitionItem{Key: key, Requirement: req.Name, Entry: entry.Name, Category: categoryName, Params: params, Fields: fields, Extent: extent, ExtentExpression: req.Extent, ScaleExpression: req.Scale, Acquisition: req.Acquisition, Mode: "user", Input: in.Key, InputType: in.Type, Resource: resourceKey, Filename: entry.Extra["path"], Required: required, Acquirable: allowed, Reasons: []string{}, Notes: []string{}, Alternatives: []SourceChoice{}, Excluded: []SourceExclusion{}, RecommendedSources: sourceIDs(env.DataSources, req.RecommendedSources), ExcludedSources: sourceIDs(env.DataSources, req.ExcludedSources)}
			if item.Filename == "" {
				item.Filename = entry.Extra["relative_path"]
			}
			if item.Filename == "" {
				item.Filename = entry.Path
			}
			switch hints := entry.Fields["source_hint"].(type) {
			case []string:
				item.RecommendedSources = append(sourceIDs(env.DataSources, hints), item.RecommendedSources...)
			case []any:
				for _, hint := range hints {
					if s, ok := hint.(string); ok {
						item.RecommendedSources = append(sourceIDs(env.DataSources, []string{s}), item.RecommendedSources...)
					}
				}
			}
			if env.DataSources == nil {
				item.Acquirable = false
				item.Reasons = append(item.Reasons, "数据源目录尚未就绪")
			} else if allowed {
				// Referenced parameters are validated against the real value during input checks.
				staticParams := map[string]string{}
				for k, v := range params {
					if !strings.Contains(v, "「") {
						staticParams[k] = v
					}
				}
				category, merged, err := env.DataSources.ResolveAcquisitionCategory(categoryName, staticParams)
				if category == nil {
					level := "error"
					if req.List != "" {
						level = "warning"
					}
					diagnose(req, "FLOW_REQ_CATEGORY_UNKNOWN", level, "未知数据类别："+categoryName)
					item.Acquirable = false
				} else {
					item.Category = category.ID
					item.Geometry = category.Geometry
					for k, v := range params {
						merged[k] = v
					}
					item.Params = merged
					if err != nil && !hasDynamicParameter(params) {
						diagnose(req, "FLOW_REQ_PARAM_INVALID", "error", err.Error())
					}
					for _, standard := range fields {
						if _, ok := category.Fields[standard]; !ok {
							diagnose(req, "FLOW_REQ_FIELD_INVALID", "error", "类别 "+category.Name+" 没有标准字段 "+standard)
						}
					}
					if entry.Kind == "raster" && category.Geometry != "raster" || entry.Kind == "vector" && (category.Geometry == "raster" || category.Geometry == "table" || category.Geometry == "tiles") {
						diagnose(req, "FLOW_REQ_GEOMETRY_MISMATCH", "warning", "数据清单类型与类别几何不一致："+entry.Name)
					}
				}
			} else {
				diagnose(req, "FLOW_REQ_ROW_NOT_ACQUIRABLE", "info", "该条目需要用户提供："+entry.Name)
			}
			if scale, err := ParseScale(req.Scale); err == nil {
				item.Scale = scale
			}
			item = env.AcquisitionEnvironment().Rank(item)
			plan.Acquisition.Items = append(plan.Acquisition.Items, item)
			in.Acquirable = in.Acquirable || item.Acquirable
			in.AcquireAvailable = in.AcquireAvailable || item.Available
		}
		if in.Acquirable {
			if in.Type == "layer" {
				for _, decl := range doc.Inputs {
					if decl.Label == in.Label {
						in.Remember = !decl.NoRemember
						break
					}
				}
			}
			in.Modes = []string{"user", "auto"}
			if in.Type == "folder" {
				in.Modes = append(in.Modes, "fill")
			}
		}
		if in.Acquirable && firstConsumer < len(plan.Steps) {
			system := StepPlan{ID: fmt.Sprintf("acquire-%d", index+1), Index: 0, Title: "获取数据：" + req.Name, Kind: "acquire", Executor: "data_acquisition", RoleLabel: "系统数据获取", Instructions: "按已确认的数据来源获取、规范化并核验数据。", AcquisitionRequirement: req.Name, Refs: PlanRefs{Inputs: []string{in.Key}, Resources: []string{}, Outputs: []OutputRef{}}, ExpectedOutputs: []OutputDeclaration{}, SuggestedTools: []string{}, MissingCapabilities: []string{}, OnFailure: "ask"}
			if resourceKey != "" {
				system.Refs.Resources = append(system.Refs.Resources, resourceKey)
			}
			plan.Steps = append(plan.Steps, nilStep())
			copy(plan.Steps[firstConsumer+1:], plan.Steps[firstConsumer:])
			plan.Steps[firstConsumer] = system
		}
	}
}
func nilStep() StepPlan { return StepPlan{} }
func hasString(items []string, value string) bool {
	for _, v := range items {
		if v == value {
			return true
		}
	}
	return false
}
func hasDynamicParameter(params map[string]string) bool {
	for _, v := range params {
		if strings.Contains(v, "「") {
			return true
		}
	}
	return false
}
func sourceIDs(c *datasource.Catalog, names []string) []string {
	result := []string{}
	for _, name := range names {
		id := name
		if c != nil {
			for _, s := range c.Sources {
				if s.Name == name {
					id = s.ID
					break
				}
			}
		}
		result = append(result, id)
	}
	return result
}
func RequirementFields(raw string) map[string]string {
	result := map[string]string{}
	for _, field := range strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == '；' || r == '、' || r == ',' }) {
		parts := strings.SplitN(strings.TrimSpace(field), "=", 2)
		target := strings.TrimSpace(parts[0])
		if target == "" {
			continue
		}
		standard := target
		if len(parts) == 2 {
			standard = strings.TrimSpace(parts[1])
		}
		result[target] = strings.ToLower(standard)
	}
	return result
}
func ParseScale(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "自动" {
		return 0, nil
	}
	value = strings.TrimPrefix(strings.TrimPrefix(value, "1:"), "1：")
	value = strings.ReplaceAll(value, ",", "")
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("比例尺须为自动或 1:N")
	}
	return n, nil
}
func RequirementExtent(expression string, plan *FlowPlan, catalog *datasource.Catalog) (*ExtentPlan, error) {
	if strings.TrimSpace(expression) == "全国" {
		if catalog == nil {
			return &ExtentPlan{Source: expression, Region: "全国", Pending: true}, nil
		}
		region, err := catalog.ResolveRegion("全国")
		return &ExtentPlan{BBox: region.BBox, Source: expression, Region: "全国"}, err
	}
	matches := extentExpression.FindStringSubmatch(strings.TrimSpace(expression))
	if matches == nil {
		return nil, fmt.Errorf("范围须为全国或「输入或产出」，可附外扩百分比")
	}
	extent := &ExtentPlan{Source: matches[1], Pending: true}
	if matches[2] != "" {
		extent.ExpandPercent, _ = strconv.ParseFloat(matches[2], 64)
		if extent.ExpandPercent > 1000 {
			return nil, fmt.Errorf("外扩比例不能大于 1000%%")
		}
	}
	inputFound := false
	for _, input := range plan.Inputs {
		if input.Key == extent.Source {
			inputFound = true
		}
	}
	for _, step := range plan.Steps {
		for _, output := range step.ExpectedOutputs {
			if output.Label == extent.Source {
				extent.DependencyStep = step.ID
				extent.DependencyOutput = output.Label
				return extent, nil
			}
		}
	}
	if inputFound {
		return extent, nil
	}
	return nil, fmt.Errorf("范围引用不存在：%s", extent.Source)
}
