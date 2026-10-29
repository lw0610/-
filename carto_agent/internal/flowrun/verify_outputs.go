package flowrun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/flow"
)

func VerifyOutputs(ctx context.Context, step flow.StepPlan, receipt Receipt, root string, tools ToolInvoker, styleTools ...ToolSummary) Verification {
	normalizeReceiptOutputs(step, &receipt)
	result := Verification{OK: true, Errors: []string{}}
	result.Errors = append(result.Errors, verifySymbology(styleTools)...)
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
	case "table":
		path, ok := value.(string)
		if !ok {
			return fmt.Errorf("需要表格文件路径")
		}
		rows, err := acquire.ReadTable(resolvePath(root, path))
		if err != nil {
			return err
		}
		if len(rows) < 2 {
			return fmt.Errorf("表格没有数据行")
		}
		for _, field := range output.Fields {
			found := false
			for _, header := range rows[0] {
				found = found || header == field
			}
			if !found {
				return fmt.Errorf("表格缺少字段 %s", field)
			}
		}
		return nil
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
		if output.Format != "" && flow.CanonicalOutputFormat(filepath.Ext(path)) != flow.CanonicalOutputFormat(output.Format) {
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
			if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
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
		if err := checkToolSuccess(result); err != nil {
			return err
		}
		payload, err := acquire.UnwrapToolResult(result)
		if err != nil {
			return err
		}
		if warnings, ok := payload["warnings"].([]any); ok && len(warnings) > 0 {
			return fmt.Errorf("版面警告：%v", warnings)
		}
		return nil
	case "layers":
		layers, _ := value.([]any)
		if len(layers) == 0 {
			return fmt.Errorf("图层产出为空")
		}
		var available map[string]bool
		var names map[string][]string
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
					if output.Geometry != "" || len(output.Fields) > 0 {
						fields := map[string]string{}
						for _, f := range output.Fields {
							fields[f] = f
						}
						if _, err := dataservice.ValidateLocalData(ctx, resolvePath(root, path), output.Geometry, fields); err != nil {
							return err
						}
					}
					continue
				}
			}
			if ref == "" {
				return fmt.Errorf("图层引用需要有效的 ID、名称、路径或 layer_id/id/name/path/source 字段")
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
				names = map[string][]string{}
				collectLayerReferences(result, available, names)
			}
			if !available[ref] {
				matches := names[ref]
				if len(matches) == 0 {
					return fmt.Errorf("图层未找到：%v", layer)
				}
				if len(matches) != 1 {
					return fmt.Errorf("图层名称「%s」不唯一，请使用图层 ID：%s", ref, strings.Join(matches, "、"))
				}
				ref = matches[0]
			}
			if output.Geometry != "" || len(output.Fields) > 0 {
				result, err := tools.Invoke(ctx, "layer_inspect", map[string]any{"layer_id": ref})
				if err != nil {
					return err
				}
				payload, err := acquire.UnwrapToolResult(result)
				if err != nil {
					return err
				}
				metadata, ok := payload["data"].(map[string]any)
				if !ok {
					return fmt.Errorf("layer_inspect 未返回图层元数据 data")
				}
				if output.Geometry != "" && dataservice.NormalizeGeometry(fmt.Sprint(metadata["geometry"])) != dataservice.NormalizeGeometry(output.Geometry) {
					return fmt.Errorf("图层几何与声明不符")
				}
				raw, _ := json.Marshal(metadata["available_fields"])
				var actual []string
				_ = json.Unmarshal(raw, &actual)
				for _, f := range output.Fields {
					found := false
					for _, a := range actual {
						found = found || a == f
					}
					if !found {
						return fmt.Errorf("图层缺少字段 %s", f)
					}
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("不支持的产出类型 %s", output.Type)
	}
}

// Normalize the equivalent layer references once at the receipt boundary.
// Object descriptors keep their metadata; a name-to-ID collection becomes IDs.
func layerOutputValues(value any) []any {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case []any:
		layers := []any{}
		for _, item := range v {
			layers = append(layers, layerOutputValues(item)...)
		}
		return layers
	case []string:
		layers := make([]any, len(v))
		for i, item := range v {
			layers[i] = item
		}
		return layers
	case map[string]string:
		object := make(map[string]any, len(v))
		for key, item := range v {
			object[key] = item
		}
		value = object
	}
	if object, ok := value.(map[string]any); ok {
		for _, key := range []string{"layers", "layer_ids"} {
			if child, exists := object[key]; exists {
				return layerOutputValues(child)
			}
		}
		for _, key := range []string{"layer_id", "id", "name", "path", "source"} {
			if _, descriptor := object[key]; descriptor {
				return []any{object}
			}
		}
		names := make([]string, 0, len(object))
		for name, id := range object {
			if _, ok := id.(string); !ok {
				return []any{object} // Verification reports the invalid reference.
			}
			names = append(names, name)
		}
		sort.Strings(names)
		layers := make([]any, len(names))
		for i, name := range names {
			layers[i] = object[name]
		}
		return layers
	}
	return []any{value}
}

func normalizeReceiptOutputs(step flow.StepPlan, receipt *Receipt) {
	missing, missingCount := "", 0
	declared := map[string]bool{}
	for _, output := range step.ExpectedOutputs {
		declared[output.Label] = true
		if _, exists := receipt.Outputs[output.Label]; output.Type == "layers" && !exists {
			missing, missingCount = output.Label, missingCount+1
		}
	}
	if missingCount == 1 {
		for _, alias := range []string{"layers", "layer_ids"} {
			if value, exists := receipt.Outputs[alias]; exists && !declared[alias] {
				receipt.Outputs[missing] = value
				delete(receipt.Outputs, alias)
				break
			}
		}
	}
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
func collectLayerReferences(value any, ids map[string]bool, names map[string][]string) {
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
					id, _ := record["id"].(string)
					if id == "" {
						id, _ = record["layer_id"].(string)
					}
					if id == "" {
						continue
					}
					ids[id] = true
					name, _ := record["name"].(string)
					if name == "" {
						name, _ = record["layer_name"].(string)
					}
					if name != "" {
						names[name] = append(names[name], id)
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
