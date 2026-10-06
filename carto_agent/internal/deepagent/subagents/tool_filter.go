package subagents

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/components/tool"
)

// ToolFilter 工具过滤器接口
type ToolFilter interface {
	Filter(ctx context.Context, tools []tool.BaseTool) []tool.BaseTool
}

// WhitelistFilter 白名单过滤器
type WhitelistFilter struct {
	AllowedTools map[string]bool
}

// NewWhitelistFilter 创建白名单过滤器
func NewWhitelistFilter(tools []string) *WhitelistFilter {
	allowed := make(map[string]bool)
	for _, t := range tools {
		name := strings.ToLower(strings.TrimSpace(t))
		if name == "" {
			continue
		}
		allowed[name] = true
	}
	return &WhitelistFilter{AllowedTools: allowed}
}

// Filter 过滤工具
func (f *WhitelistFilter) Filter(ctx context.Context, tools []tool.BaseTool) []tool.BaseTool {
	var filtered []tool.BaseTool
	for _, t := range tools {
		info, err := t.Info(ctx)
		if err != nil {
			continue
		}
		if f.AllowedTools[strings.ToLower(info.Name)] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func CreateRuntimeSkillToolFilter(agentName string, runtimeConfig *RuntimeSkillConfig) ToolFilter {
	if runtimeConfig == nil {
		return NewWhitelistFilter(nil)
	}
	return NewWhitelistFilter(runtimeConfig.GetAgentAllowedTools(agentName))
}
