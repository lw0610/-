package flowrun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/flow"
)

func VerifyOutputs(ctx context.Context, step flow.StepPlan, receipt Receipt, root string, tools ToolInvoker) Verification {
	result := Verification{OK: true, Errors: []string{}}
	for _, output := range step.ExpectedOutputs {
		value, exists := receipt.Outputs[output.Label]
		if !exists || !provided(value) {
			if output.Required {
				result.Errors = append(result.Errors, "缺少必需产出「"+output.Label+"」")
			}
			continue
		}
		if err := verifyOutput(ctx, output, value, root, tools); err != nil {
			result.Errors = append(result.Errors, output.Label+"："+err.Error())
		}
	}
	result.OK = len(result.Errors) == 0
	return result
}
func verifyOutput(ctx context.Context, output flow.OutputDeclaration, value any, root string, tools ToolInvoker) error {
	switch output.Type {
	case "confirmation", "any":
		return nil
	case "text":
		if !provided(value) {
			return fmt.Errorf("文本为空")
		}
		return nil
	case "review_initial":
		return VerifyInitialReview(value, root)
	case "review_final":
		return VerifyFinalReview(value)
	case "file":
		path, ok := value.(string)
		if !ok {
			return fmt.Errorf("需要文件路径")
		}
		info, err := os.Stat(resolvePath(root, path))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("文件为空或不是普通文件")
		}
		if output.Format != "" && !strings.EqualFold(strings.TrimPrefix(filepath.Ext(path), "."), strings.TrimPrefix(output.Format, ".")) {
			return fmt.Errorf("文件格式应为 %s", output.Format)
		}
		return nil
	case "dataset":
		path, ok := value.(string)
		if !ok {
			return fmt.Errorf("需要数据集目录")
		}
		path = resolvePath(root, path)
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("数据集不是目录")
		}
		names := []string{}
		switch output.Format {
		case "route_dataset":
			names = []string{"places.csv", "routes.csv", "route_paths.csv", "points.csv", "symbol_mapping.csv", "callouts.csv"}
		case "statistical_dataset":
			names = []string{"manifest.json"}
		}
		for _, name := range names {
			info, err := os.Stat(filepath.Join(path, name))
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("缺少数据集文件 %s", name)
			}
		}
		return nil
	case "layout":
		name, ok := value.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return fmt.Errorf("需要版面名")
		}
		if tools == nil {
			return fmt.Errorf("只读工具通道未配置")
		}
		result, err := tools.Invoke(ctx, "layout_info", map[string]any{"name": name})
		if err != nil {
			return err
		}
		return checkToolSuccess(result)
	case "layers":
		layers := layerOutputValues(value)
		if len(layers) == 0 {
			return fmt.Errorf("图层产出为空")
		}
		var available map[string]bool
		for _, layer := range layers {
			ref, path := "", ""
			switch v := layer.(type) {
			case string:
				ref, path = v, v
			case map[string]any:
				ref, _ = v["layer_id"].(string)
				if ref == "" {
					ref, _ = v["id"].(string)
				}
				if ref == "" {
					ref, _ = v["name"].(string)
				}
				path, _ = v["path"].(string)
				if path == "" {
					path, _ = v["source"].(string)
				}
			}
			// Exported layers are usable even when no longer loaded in QGIS.
			// Keep provider suffixes (e.g. |layername=...) in the receipt itself.
			if path != "" {
				file := strings.SplitN(path, "|", 2)[0]
				if info, err := os.Stat(resolvePath(root, file)); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
					continue
				}
			}
			if ref == "" {
				return fmt.Errorf("图层未找到：%v", layer)
			}
			if available == nil {
				if tools == nil {
					return fmt.Errorf("只读工具通道未配置")
				}
				result, err := tools.Invoke(ctx, "get_layers_info", map[string]any{})
				if err != nil {
					return err
				}
				if err = checkToolSuccess(result); err != nil {
					return err
				}
				available = map[string]bool{}
				collectLayerIDs(result, available)
			}
			if !available[ref] {
				return fmt.Errorf("图层未找到：%v", layer)
			}
		}
		return nil
	default:
		return fmt.Errorf("不支持的产出类型 %s", output.Type)
	}
}

// A single layer and a list are equivalent at the handoff boundary.
func layerOutputValues(value any) []any {
	raw, _ := json.Marshal(value)
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil || decoded == nil {
		return nil
	}
	if layers, ok := decoded.([]any); ok {
		return layers
	}
	return []any{decoded}
}

func normalizeReceiptOutputs(step flow.StepPlan, receipt *Receipt) {
	for _, output := range step.ExpectedOutputs {
		if value, ok := receipt.Outputs[output.Label]; ok && output.Type == "layers" {
			receipt.Outputs[output.Label] = layerOutputValues(value)
		}
	}
}
func checkToolSuccess(result any) error {
	if text, ok := result.(string); ok {
		var decoded any
		if json.Unmarshal([]byte(text), &decoded) != nil {
			return fmt.Errorf("只读工具返回无效JSON")
		}
		result = decoded
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) == nil {
		if success, exists := object["success"]; exists && success == false {
			return fmt.Errorf("只读工具返回失败：%v", object["error"])
		}
	}
	if result == nil {
		return fmt.Errorf("只读工具返回空结果")
	}
	return nil
}
func collectLayerIDs(value any, ids map[string]bool) {
	if text, ok := value.(string); ok {
		var decoded any
		if json.Unmarshal([]byte(text), &decoded) != nil {
			return
		}
		value = decoded
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return
	}
	var records func(any)
	records = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, child := range x {
				if record, ok := child.(map[string]any); ok {
					for _, key := range []string{"id", "layer_id", "name", "layer_name"} {
						if value, ok := record[key].(string); ok && value != "" {
							ids[value] = true
						}
					}
				}
			}
		case map[string]any:
			for _, key := range []string{"layers", "items"} {
				if child, ok := x[key]; ok {
					records(child)
					return
				}
			}
			for _, key := range []string{"data", "result", "info"} {
				if child, ok := x[key]; ok {
					records(child)
				}
			}
		}
	}
	records(decoded)
}
