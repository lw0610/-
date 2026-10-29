package flowrun

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/acquire"
)

// IsStyleTool limits full result capture to the tools whose QGIS readback is
// part of step verification. Other tool outputs keep their existing summaries.
func IsStyleTool(name string) bool {
	return name == "layer_set_symbology" || name == "layer_load_style" || name == "layer_load_styles"
}

func verifySymbology(calls []ToolSummary) []string {
	type layerCheck struct {
		name     string
		expected map[string]any
		actual   map[string]any
		err      string
	}
	checks := map[string]*layerCheck{}
	aliases := map[string]string{}
	var order, errors []string
	for _, call := range calls {
		if !IsStyleTool(call.Name) {
			continue
		}
		var args, response map[string]any
		if json.Unmarshal(call.Arguments, &args) != nil || json.Unmarshal([]byte(call.Result), &response) != nil {
			errors = append(errors, call.Name+"：缺少可核验的原始请求或完整工具读回")
			continue
		}
		if unwrapped, err := acquire.UnwrapToolResult(response); err == nil {
			response = unwrapped
		} else if output, ok := response["output"].(map[string]any); ok {
			// DefaultErrorHandler retains the Python result here, including the
			// successful items of a partially failed batch.
			response = output
		}
		scale := 1.0
		if value, ok := args["scale"].(float64); ok {
			scale = value
		}
		requests, _ := args["items"].([]any)
		if call.Name == "layer_load_style" {
			requests = []any{args}
		}
		data, _ := response["data"].(map[string]any)
		items, _ := data["items"].([]any)
		if call.Name == "layer_load_style" {
			items = []any{response}
		}
		for i, request := range requests {
			raw, _ := request.(map[string]any)
			name, _ := raw["layer"].(string)
			row := response
			if i < len(items) {
				row, _ = items[i].(map[string]any)
			}
			payload := row
			if nested, ok := row["data"].(map[string]any); ok {
				payload = nested
			}
			id, _ := payload["layer_id"].(string)
			if id == "" {
				id = name
			}
			if existing := aliases[id]; existing != "" {
				id = existing
			} else if existing := aliases[name]; existing != "" {
				id = existing
			}
			aliases[name], aliases[id] = id, id
			if actualID, ok := payload["layer_id"].(string); ok {
				aliases[actualID] = id
			}
			check := checks[id]
			if check == nil {
				check = &layerCheck{name: name, expected: map[string]any{}}
				checks[id] = check
				order = append(order, id)
			}
			requested, _ := payload["requested"].(map[string]any)
			actual, _ := payload["actual"].(map[string]any)
			check.err = ""
			if requested == nil || actual == nil {
				check.err = "工具未返回写入设置与 QGIS 读回设置"
				if info, ok := row["info"].(string); ok && info != "" {
					check.err += "：" + info
				}
				continue
			}
			// The tool resolves auto fields, fonts and style defaults. Explicit
			// user parameters still come from the recorded request, not its echo.
			expected := requested
			if call.Name == "layer_set_symbology" {
				expected = requestedSymbology(raw, requested, scale)
			} else if field, ok := raw["label_field"].(string); ok && field != "" {
				labels, _ := expected["labels"].(map[string]any)
				if labels == nil {
					labels = map[string]any{}
					expected["labels"] = labels
				}
				if field == "无" {
					labels["enabled"] = false
				} else {
					labels["field"] = field
				}
			}
			for key, value := range expected {
				check.expected[key] = value
			}
			check.actual = actual
		}
	}
	for _, id := range order {
		check := checks[id]
		if check.err != "" {
			errors = append(errors, "图层「"+check.name+"」："+check.err)
			continue
		}
		for _, mismatch := range compareStyleValues("", check.expected, check.actual) {
			errors = append(errors, "图层「"+check.name+"」符号化读回不一致："+mismatch)
		}
	}
	return errors
}

func requestedSymbology(raw, resolved map[string]any, scale float64) map[string]any {
	// JSON decoding above gives this invocation its own mutable request tree.
	expected := resolved
	if symbol, ok := raw["symbol"].(map[string]any); ok {
		// This tool replaces the renderer with a single symbol, including when
		// an earlier call loaded a categorized QML. Labels-only leaves it intact.
		expected["renderer"] = "QgsSingleSymbolRenderer"
		out, _ := expected["symbol"].(map[string]any)
		if out == nil {
			out = map[string]any{}
			expected["symbol"] = out
		}
		out["type"] = symbol["type"]
		layers, _ := symbol["layers"].([]any)
		resolvedLayers, _ := out["layers"].([]any)
		result := make([]any, len(layers))
		for i, value := range layers {
			layer, _ := value.(map[string]any)
			item := map[string]any{}
			if i < len(resolvedLayers) {
				if defaults, ok := resolvedLayers[i].(map[string]any); ok {
					item = defaults
				}
			}
			for key, value := range layer {
				switch key {
				case "width", "outline_width", "size", "offset":
					if number, ok := value.(float64); ok {
						number *= scale
						if key == "width" || key == "outline_width" {
							number = math.Max(.1, number)
						}
						item[key+"_mm"] = number
					}
				case "dash":
					values, _ := value.([]any)
					dash := make([]any, len(values))
					for j, v := range values {
						if number, ok := v.(float64); ok {
							dash[j] = number * scale
						}
					}
					item["dash_mm"] = dash
				case "color", "fill", "outline":
					if value == nil {
						item[key] = nil
					} else if color, ok := value.(string); ok && strings.HasPrefix(color, "#") {
						item[key] = canonicalStyleColor(color)
					}
				default:
					item[key] = value
				}
			}
			result[i] = item
		}
		out["layers"] = result
	}
	if labels, exists := raw["labels"]; exists {
		if labels == nil {
			expected["labels"] = map[string]any{"enabled": false}
		} else if input, ok := labels.(map[string]any); ok {
			out, _ := expected["labels"].(map[string]any)
			if out == nil {
				out = map[string]any{}
				expected["labels"] = out
			}
			out["enabled"] = true
			for key, value := range input {
				switch key {
				case "field":
					if value != "auto" && value != "" {
						out[key] = value
					}
				case "color":
					if color, ok := value.(string); ok && strings.HasPrefix(color, "#") {
						out[key] = canonicalStyleColor(color)
					}
				case "size":
					if size, ok := value.(float64); ok {
						if input["unit"] == "pt" {
							size *= 25.4 / 72
						}
						out["size_mm"] = math.Max(1.8, size*scale)
					}
				case "repeat":
					if repeat, ok := value.(float64); ok {
						out["repeat_mm"] = repeat * scale
					}
				case "buffer":
					if buffer, ok := value.(map[string]any); ok {
						resolvedBuffer, _ := out["buffer"].(map[string]any)
						if resolvedBuffer == nil {
							resolvedBuffer = map[string]any{}
							out["buffer"] = resolvedBuffer
						}
						for key, value := range buffer {
							if key == "size" {
								if size, ok := value.(float64); ok {
									resolvedBuffer["size_mm"] = size * scale
								}
							} else if key == "color" {
								if color, ok := value.(string); ok && strings.HasPrefix(color, "#") {
									resolvedBuffer[key] = canonicalStyleColor(color)
								}
							} else {
								resolvedBuffer[key] = value
							}
						}
					}
				case "unit", "font": // Font selection may legitimately fall back with a warning.
				default:
					out[key] = value
				}
			}
		}
	}
	return expected
}

func canonicalStyleColor(color string) string {
	color = strings.ToLower(color)
	if len(color) == 4 {
		return fmt.Sprintf("#%c%c%c%c%c%c", color[1], color[1], color[2], color[2], color[3], color[3])
	}
	return color
}

func compareStyleValues(path string, expected, actual any) []string {
	var errors []string
	switch want := expected.(type) {
	case map[string]any:
		got, ok := actual.(map[string]any)
		if !ok {
			return []string{path + " 缺少读回设置"}
		}
		keys := make([]string, 0, len(want))
		for key := range want {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			errors = append(errors, compareStyleValues(strings.TrimPrefix(path+"."+key, "."), want[key], got[key])...)
		}
	case []any:
		got, ok := actual.([]any)
		if !ok || len(want) != len(got) {
			return []string{fmt.Sprintf("%s 数量应为 %d，读回 %d", path, len(want), len(got))}
		}
		for i := range want {
			errors = append(errors, compareStyleValues(fmt.Sprintf("%s[%d]", path, i), want[i], got[i])...)
		}
	case float64:
		got, ok := actual.(float64)
		tolerance := 0.00001
		if strings.Contains(path, "_mm") {
			tolerance = .01
		}
		if !ok || math.Abs(want-got) > tolerance+1e-9 {
			errors = append(errors, fmt.Sprintf("%s 应为 %v，读回 %v", path, want, actual))
		}
	default:
		if !reflect.DeepEqual(expected, actual) {
			errors = append(errors, fmt.Sprintf("%s 应为 %v，读回 %v", path, expected, actual))
		}
	}
	return errors
}
