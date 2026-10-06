package toolutils

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParsedToolDefinition 表示通用的工具定义载荷。
type ParsedToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Category    string         `json:"category,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ParseToolDefinitions 解析插件上报的工具定义数组。
func ParseToolDefinitions(raw any) []ParsedToolDefinition {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	tools := make([]ParsedToolDefinition, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(ToString(entry["name"]))
		if name == "" {
			continue
		}
		tools = append(tools, ParsedToolDefinition{
			Name:        name,
			Description: strings.TrimSpace(ToString(entry["description"])),
			Category:    strings.TrimSpace(ToString(entry["category"])),
			InputSchema: ToMap(entry["inputSchema"]),
		})
	}
	return tools
}

// ToMap 将任意值转换为对象 schema，失败时返回空对象 schema。
func ToMap(raw any) map[string]any {
	m, ok := raw.(map[string]any)
	if !ok || m == nil {
		return map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}
	return DeepCopyMap(m)
}

// ToString 将值转换为字符串。
func ToString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// ToInt 将值转换为 int。
func ToInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}

// ToInt64 将值转换为 int64。
func ToInt64(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	default:
		return 0
	}
}

// ToBool 将值转换为 bool。
func ToBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

// InjectInstanceIDParam 向 schema 中注入 instance_id 参数。
func InjectInstanceIDParam(original map[string]any) map[string]any {
	return injectOptionalStringParam(original, "instance_id", "目标 QGIS 实例 ID，不指定则使用当前活跃实例")
}

// InjectTargetInstanceIDParam 向 schema 中注入 target_instance_id 参数。
func InjectTargetInstanceIDParam(original map[string]any) map[string]any {
	return injectOptionalStringParam(original, "target_instance_id", "自动加载输出图层时指定目标 QGIS 实例 ID，不指定则使用当前活跃实例")
}

func injectOptionalStringParam(original map[string]any, key string, description string) map[string]any {
	copied := DeepCopyMap(original)
	props, ok := copied["properties"].(map[string]any)
	if !ok || props == nil {
		props = make(map[string]any)
		copied["properties"] = props
	}
	props[key] = map[string]any{
		"type":        "string",
		"description": strings.TrimSpace(description),
	}
	return copied
}

// DeepCopyMap 深拷贝 map。
func DeepCopyMap(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(src)
	if err != nil {
		dst := make(map[string]any, len(src))
		for k, v := range src {
			dst[k] = v
		}
		return dst
	}
	dst := map[string]any{}
	if err := json.Unmarshal(raw, &dst); err != nil {
		dst = make(map[string]any, len(src))
		for k, v := range src {
			dst[k] = v
		}
	}
	return dst
}

// IsRasterPath 判断路径是否为常见栅格数据。
func IsRasterPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".tif") || strings.HasSuffix(lower, ".tiff")
}

// ExtractToolInvokeError 提取工具调用错误信息。
func ExtractToolInvokeError(rawError string, output any) string {
	if msg := strings.TrimSpace(rawError); msg != "" {
		return msg
	}

	if payload, ok := output.(map[string]any); ok {
		for _, key := range []string{"error", "info", "output"} {
			value, exists := payload[key]
			if !exists {
				continue
			}
			switch typed := value.(type) {
			case string:
				if msg := strings.TrimSpace(typed); msg != "" {
					return msg
				}
			case map[string]any:
				if key != "output" && len(typed) > 0 {
					return fmt.Sprintf("%v", typed)
				}
			case []any:
				if key != "output" && len(typed) > 0 {
					return fmt.Sprintf("%v", typed)
				}
			default:
				if key != "output" && typed != nil {
					return fmt.Sprintf("%v", typed)
				}
			}
		}
	}

	if text, ok := output.(string); ok {
		if msg := strings.TrimSpace(text); msg != "" {
			return msg
		}
	}

	return "工具执行失败"
}
