package skill

import (
	"context"
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/resource"
)

func (e *Engine) SetResourceResolver(resolver resource.Resolver) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.resourceResolver = resolver
}

func (e *Engine) expandResourceArgs(ctx context.Context, definition SkillDefinition, args map[string]any) error {
	e.mu.RLock()
	resolver := e.resourceResolver
	e.mu.RUnlock()
	if ctx == nil {
		ctx = context.Background()
	}
	scope, _ := resource.ScopeFromContext(ctx)
	for _, parameter := range definition.Spec.Parameters {
		if parameter.Type != "resource" {
			if len(parameter.Schema) > 0 {
				value, err := e.expandNestedResourceArgs(ctx, definition, parameter.Schema, args[parameter.Name])
				if err != nil {
					return err
				}
				if value != nil {
					args[parameter.Name] = value
				}
			}
			continue
		}
		invalid := func(message string) error {
			return &ExecutionValidationError{SkillName: definition.Metadata.Name, Message: message}
		}
		if !resource.IsPublicType(strings.SplitN(parameter.ResourceType, ":", 2)[0]) {
			return invalid("运行时资源只支持样式和版面；数据使用 items，工具预设使用名称")
		}
		if resolver == nil {
			return invalid("资源服务未就绪")
		}
		ref := resource.Ref{Type: parameter.ResourceType}
		value := args[parameter.Name]
		if value == nil || (func() bool { raw, ok := value.(string); return ok && strings.TrimSpace(raw) == "" })() {
			if parameter.DefaultResource != "" {
				ref.ID = parameter.DefaultResource
			} else if parameter.Required {
				return invalid("缺少资源：" + parameter.Name)
			} else {
				delete(args, parameter.Name)
				continue
			}
		} else {
			switch raw := value.(type) {
			case string:
				name := strings.Trim(strings.TrimSpace(raw), "「」")
				if strings.TrimSpace(name) == "" {
					return invalid("资源名称不能为空：" + parameter.Name)
				}
				baseType := strings.SplitN(parameter.ResourceType, ":", 2)[0]
				switch {
				case strings.HasPrefix(name, baseType+".") || strings.HasPrefix(name, "legacy."):
					ref.ID = name
				case strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, "~") || resourcePathSuffix(name, baseType):
					ref.Location = name
				default:
					ref.Name = name
				}
			case map[string]any:
				if !resource.InternalHandlesAllowed(ctx) || raw["api_version"] != "resource-handle/v1" {
					return invalid("资源参数只接受名称、编号或路径")
				}
				ref.ID, _ = raw["id"].(string)
				if ref.ID == "" {
					return invalid("内部资源句柄缺少编号")
				}
			default:
				return invalid("资源参数只接受名称、编号或路径")
			}
		}

		resolved := resolver.Resolve(ctx, ref, scope)
		if resolved == nil {
			return invalid("资源服务没有返回解析结果")
		}
		handle := resolved.Handle()
		if handle == nil {
			messages := []string{}
			for _, problem := range resolved.Problems {
				if problem.Level == "error" {
					messages = append(messages, problem.Message)
				}
			}
			if len(messages) == 0 {
				messages = append(messages, "资源句柄不可用")
			}
			name := ref.Name
			if name == "" {
				name = ref.ID
			}
			if name == "" {
				name = ref.Location
			}
			message := fmt.Sprintf("资源「%s」无法使用：%s", name, strings.Join(messages, "；"))
			if names := availableResourceNames(resolver, strings.SplitN(parameter.ResourceType, ":", 2)[0]); len(names) > 0 {
				message += "。可用的同类资源：" + strings.Join(names, "、")
			}
			return invalid(message)
		}
		args[parameter.Name] = handle
	}
	return nil
}

func resourcePathSuffix(value, typ string) bool {
	format, ok := resource.TypeInfo(typ)
	if !ok {
		return false
	}
	lower := strings.ToLower(value)
	for _, suffix := range format.EntryExtensions {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func availableResourceNames(resolver resource.Resolver, typ string) []string {
	lister, ok := resolver.(interface {
		List(string) ([]resource.LibraryItem, error)
	})
	if !ok {
		return nil
	}
	items, err := lister.List("all")
	if err != nil {
		return nil
	}
	seen, names := map[string]bool{}, []string{}
	for _, item := range items {
		if item.Type == typ && !seen[item.Name] {
			names = append(names, item.Name)
			seen[item.Name] = true
		}
		if len(names) == 10 {
			break
		}
	}
	return names
}

// Do not persist expanded entries and paths into the conversation/tool trace.
func resourceTraceParams(value any) any {
	switch v := value.(type) {
	case map[string]any:
		if v["api_version"] == "resource-handle/v1" {
			return v["name"]
		}
		copy := make(map[string]any, len(v))
		for key, item := range v {
			copy[key] = resourceTraceParams(item)
		}
		return copy
	case []any:
		copy := make([]any, len(v))
		for i, item := range v {
			copy[i] = resourceTraceParams(item)
		}
		return copy
	default:
		return value
	}
}

// Preserve the caller's resource reference in diagnostic traces, while the
// backend alone receives the resolved handle.
func resourceTraceParamsForCall(def SkillDefinition, params, original map[string]any) map[string]any {
	result, _ := resourceTraceParams(params).(map[string]any)
	for _, parameter := range def.Spec.Parameters {
		if parameter.Type != "resource" {
			continue
		}
		if _, exists := result[parameter.Name]; !exists {
			continue
		}
		if raw, ok := original[parameter.Name]; ok {
			result[parameter.Name] = resourceTraceParams(raw)
		} else if parameter.DefaultResource != "" {
			result[parameter.Name] = parameter.DefaultResource
		}
	}
	return result
}

// Nested resources use a JSON Schema extension while exposing a string to the
// model. All resolution still passes through the same scoped Go authority.
func (e *Engine) expandNestedResourceArgs(ctx context.Context, def SkillDefinition, schema map[string]any, value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	if typ, ok := schema["x-resource-type"].(string); ok && typ != "" {
		nested := def
		nested.Spec.Parameters = []SkillParameter{{Name: "value", Type: "resource", ResourceType: typ, Required: true}}
		args := map[string]any{"value": value}
		if err := e.expandResourceArgs(ctx, nested, args); err != nil {
			return nil, err
		}
		return args["value"], nil
	}
	switch v := value.(type) {
	case []any:
		itemSchema, _ := schema["items"].(map[string]any)
		if itemSchema == nil {
			return value, nil
		}
		out := make([]any, len(v))
		for i, item := range v {
			next, err := e.expandNestedResourceArgs(ctx, def, itemSchema, item)
			if err != nil {
				return nil, fmt.Errorf("items[%d]: %w", i, err)
			}
			out[i] = next
		}
		return out, nil
	case map[string]any:
		properties, _ := schema["properties"].(map[string]any)
		out := make(map[string]any, len(v))
		for key, item := range v {
			child, _ := properties[key].(map[string]any)
			next, err := e.expandNestedResourceArgs(ctx, def, child, item)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			out[key] = next
		}
		return out, nil
	}
	return value, nil
}
