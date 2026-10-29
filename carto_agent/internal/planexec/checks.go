package planexec

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
)

type ToolInvoker interface {
	Invoke(context.Context, string, map[string]any) (any, error)
}
type CheckResult struct {
	OK     bool     `json:"ok"`
	Errors []string `json:"errors"`
	Manual []string `json:"manual"`
}
type CheckInput struct {
	Plan        *plan.Plan
	Guide       *flow.GuideDoc
	Outputs     map[string]any
	SourceNotes []string
}

func Check(ctx context.Context, input CheckInput, tools ToolInvoker) CheckResult {
	result := CheckResult{OK: true, Errors: []string{}, Manual: []string{}}
	if input.Plan == nil {
		return result
	}
	var layout any
	var layoutErr error
	loadedLayout := false
	readLayout := func() (any, error) {
		if loadedLayout {
			return layout, layoutErr
		}
		loadedLayout = true
		name := ""
		for _, s := range input.Plan.Steps {
			for _, o := range s.Outputs {
				if o.Type == "layout" {
					if v, ok := input.Outputs[o.Label].(string); ok && v != "" {
						name = v
					}
				}
			}
		}
		if name == "" {
			layoutErr = fmt.Errorf("缺少可核验的版面产出")
			return nil, layoutErr
		}
		if tools == nil {
			layoutErr = fmt.Errorf("只读工具通道不可用")
			return nil, layoutErr
		}
		raw, err := tools.Invoke(ctx, "layout_info", map[string]any{"name": name})
		if err == nil {
			layout, err = decodeObservation(raw)
		}
		layoutErr = err
		return layout, err
	}
	for _, check := range input.Plan.Checks {
		switch {
		case strings.HasPrefix(check, "版面元素包含"):
			observed, err := readLayout()
			if err != nil {
				result.Errors = append(result.Errors, err.Error())
				continue
			}
			items := layoutItems(observed)
			required := splitValues(strings.TrimSpace(strings.TrimPrefix(check, "版面元素包含")))
			for _, name := range required {
				if !containsElement(items, name) {
					result.Errors = append(result.Errors, "版面缺少："+name)
				}
			}
		case check == "计划中的图层都已在工程中":

			if tools == nil {
				result.Errors = append(result.Errors, "只读工具通道不可用")
				continue
			}
			raw, err := tools.Invoke(ctx, "get_layers_info", map[string]any{})
			if err != nil {
				result.Errors = append(result.Errors, err.Error())
				continue
			}
			observed, err := decodeObservation(raw)
			if err != nil {
				result.Errors = append(result.Errors, err.Error())
				continue
			}
			names := map[string]bool{}
			walkObjects(observed, func(o map[string]any) {
				if name, ok := o["name"].(string); ok {
					names[name] = true
				}
			})
			for _, d := range input.Plan.Data {
				if d.LayerName != "" && !names[d.LayerName] {
					result.Errors = append(result.Errors, "工程缺少图层："+d.LayerName)
				}
			}
		case strings.HasPrefix(check, "说明栏包含"):
			observed, err := readLayout()
			if err != nil {
				result.Errors = append(result.Errors, err.Error())
				continue
			}
			needle := strings.Trim(strings.TrimSpace(strings.TrimPrefix(check, "说明栏包含")), "「」“”\"：: ")
			notes := []string{needle}
			if needle == "数据来源说明" {
				notes = input.SourceNotes
				if len(notes) == 0 {
					result.Errors = append(result.Errors, "缺少数据来源说明记录")
					continue
				}
			}
			texts := []string{}
			for _, item := range layoutItems(observed) {
				if text, ok := item["text"].(string); ok {
					texts = append(texts, text)
				}
			}
			actual := compactText(strings.Join(texts, "\n"))
			for _, note := range notes {
				if !strings.Contains(actual, compactText(note)) {
					result.Errors = append(result.Errors, "版面说明没有包含："+note)
				}
			}
		default:
			result.Manual = append(result.Manual, check)
		}
	}
	result.OK = len(result.Errors) == 0
	return result
}
func decodeObservation(value any) (any, error) {
	if text, ok := value.(string); ok {
		if err := json.Unmarshal([]byte(text), &value); err != nil {
			return nil, fmt.Errorf("只读工具返回无效JSON")
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var obj any
	if err = json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	if m, ok := obj.(map[string]any); ok {
		if success, exists := m["success"]; exists && success == false {
			return nil, fmt.Errorf("只读工具失败：%v", m["error"])
		}
	}
	if obj == nil {
		return nil, fmt.Errorf("只读工具未返回结果")
	}
	return obj, nil
}
func walkObjects(value any, visit func(map[string]any)) {
	switch v := value.(type) {
	case map[string]any:
		visit(v)
		for _, x := range v {
			walkObjects(x, visit)
		}
	case []any:
		for _, x := range v {
			walkObjects(x, visit)
		}
	}
}
func layoutItems(value any) []map[string]any {
	out := []map[string]any{}
	walkObjects(value, func(o map[string]any) {
		if _, ok := o["type"]; ok {
			if _, id := o["item_id"]; id {
				out = append(out, o)
			}
		}
	})
	return out
}
func splitValues(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return strings.ContainsRune("、,，；;：:\n", r) || unicode.IsSpace(r) })
}
func compactText(s string) string { return strings.Join(strings.Fields(s), "") }
func containsElement(items []map[string]any, name string) bool {
	aliases := map[string][]string{"主图": {"main_map", "主图"}, "标题": {"title", "标题"}, "综述": {"summary", "overview", "overview_text", "综述"}, "装饰边框": {"decorative_border", "border", "装饰边框"}, "指北针": {"north_arrow", "north", "指北针"}, "图例": {"legend", "图例"}, "比例尺": {"scalebar", "scale_bar", "比例尺"}}
	for _, item := range items {
		if name == "网格" {
			if grids, ok := item["grids"].([]any); ok {
				for _, raw := range grids {
					if grid, ok := raw.(map[string]any); ok && grid["enabled"] == true {
						return true
					}
				}
			}
			continue
		}
		role, _ := item["role"].(string)
		id, _ := item["item_id"].(string)
		typ, _ := item["type"].(string)
		if (name == "主图" && typ == "QgsLayoutItemMap") || (name == "图例" && typ == "QgsLayoutItemLegend") || (name == "比例尺" && typ == "QgsLayoutItemScaleBar") {
			return true
		}
		names := append([]string{name}, aliases[name]...)
		for _, label := range names {
			if role == label || id == label || strings.HasSuffix(id, "_"+label) || strings.HasPrefix(id, label+"_") {
				return true
			}
		}
	}
	return false
}
