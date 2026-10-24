package runtimecontrol

import (
	"context"
	"fmt"
	"github.com/autocarto/carto_agent/internal/resource"
	"math"
	"strings"
)

func getResource(ctx context.Context, args map[string]any) (map[string]any, error) {
	resolver := resource.ResolverFromContext(ctx)
	if resolver == nil {
		return nil, fmt.Errorf("资源服务未就绪")
	}
	name := strings.TrimSpace(strings.Trim(stringArg(args, "name"), "「」"))
	if name == "" {
		return nil, fmt.Errorf("资源名称或编号不能为空")
	}
	typ := stringArg(args, "type")
	if typ != "" {
		if _, ok := resource.TypeInfo(typ); !ok {
			return nil, fmt.Errorf("资源类型无效：%s", typ)
		}
	}
	offset, err := resourcePageNumber(args, "offset", 0)
	if err != nil {
		return nil, err
	}
	limit, err := resourcePageNumber(args, "limit", 200)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("limit 必须在 1–1000 之间")
	}
	scope, _ := resource.ScopeFromContext(ctx)
	r := resolver.Resolve(ctx, resource.Ref{Type: typ, Name: name}, scope)
	if r == nil {
		return nil, fmt.Errorf("资源服务没有返回解析结果")
	}
	handle := r.Handle()
	if handle == nil {
		messages := []string{}
		for _, p := range r.Problems {
			if p.Level == "error" {
				messages = append(messages, p.Message)
			}
		}
		return nil, fmt.Errorf("资源「%s」无法使用：%s", name, strings.Join(messages, "；"))
	}
	entries, _ := handle["entries"].([]map[string]any)
	total := len(entries)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	handle["entries"] = entries[start:end]
	handle["total_entries"] = total
	handle["offset"] = offset
	handle["truncated"] = start > 0 || end < total
	return handle, nil
}
func resourcePageNumber(args map[string]any, key string, fallback int) (int, error) {
	value, exists := args[key]
	if !exists || value == nil {
		return fallback, nil
	}
	number := float64(0)
	switch v := value.(type) {
	case int:
		number = float64(v)
	case float64:
		number = v
	case int64:
		number = float64(v)
	default:
		return 0, fmt.Errorf("%s 必须是非负整数", key)
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > 1e9 || math.Trunc(number) != number {
		return 0, fmt.Errorf("%s 必须是非负整数", key)
	}
	return int(number), nil
}
