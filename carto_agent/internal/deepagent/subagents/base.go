package subagents

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// GenericAgentConfig 通用智能体配置
type GenericAgentConfig struct {
	Name         string
	DisplayName  string
	Description  string
	Instruction  string
	ChatModel    model.ToolCallingChatModel
	Tools        []tool.BaseTool
	MaxIteration int
	Middlewares  []adk.AgentMiddleware
	Handlers     []adk.ChatModelAgentMiddleware
}

// NewGenericAgent 创建通用智能体（完全由配置文件驱动）
func NewGenericAgent(ctx context.Context, cfg *GenericAgentConfig) (adk.Agent, error) {
	// 设置默认最大迭代次数
	maxIter := cfg.MaxIteration
	if maxIter <= 0 {
		maxIter = 20
	}

	// 防御性处理：确保 Description 非空（eino adk 要求必填）
	description := cfg.Description
	if description == "" {
		if cfg.DisplayName != "" {
			description = cfg.DisplayName
		} else {
			description = cfg.Name
		}
	}

	// 创建 ADK ChatModelAgent
	toolsConfig := adk.ToolsConfig{}
	toolsConfig.Tools = cfg.Tools
	toolsConfig.UnknownToolsHandler = NewUnknownToolsHandler(ctx, toolsConfig.Tools)

	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          cfg.Name,
		Description:   description,
		Instruction:   cfg.Instruction,
		Model:         cfg.ChatModel,
		ToolsConfig:   toolsConfig,
		MaxIterations: maxIter,
		Middlewares:   cfg.Middlewares,
		Handlers:      cfg.Handlers,
	})
}

// SubAgentConfig 子智能体配置
type SubAgentConfig struct {
	Name         string
	Description  string
	Instruction  string
	ChatModel    model.ToolCallingChatModel
	Tools        []tool.BaseTool
	ToolFilter   ToolFilter // 新增：工具过滤器
	MaxIteration int
}

// NewSubAgent 创建子智能体
func NewSubAgent(ctx context.Context, cfg *SubAgentConfig) (adk.Agent, error) {
	var filteredTools []tool.BaseTool

	// 优先使用新的过滤器
	if cfg.ToolFilter != nil {
		filteredTools = cfg.ToolFilter.Filter(ctx, cfg.Tools)
	} else {
		// 默认使用所有工具
		filteredTools = cfg.Tools
	}

	// 如果没有匹配的工具，使用所有工具
	if len(filteredTools) == 0 {
		filteredTools = cfg.Tools
	}

	// 设置默认最大迭代次数
	maxIter := cfg.MaxIteration
	if maxIter <= 0 {
		maxIter = 20
	}

	// 防御性处理：确保 Description 非空（eino adk 要求必填）
	description := cfg.Description
	if description == "" {
		description = cfg.Name
	}

	// 创建 ADK ChatModelAgent
	toolsConfig := adk.ToolsConfig{}
	toolsConfig.Tools = filteredTools
	toolsConfig.UnknownToolsHandler = NewUnknownToolsHandler(ctx, toolsConfig.Tools)

	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          cfg.Name,
		Description:   description,
		Instruction:   cfg.Instruction,
		Model:         cfg.ChatModel,
		ToolsConfig:   toolsConfig,
		MaxIterations: maxIter,
	})
}

// ToolInfo 工具信息辅助结构
type ToolInfo struct {
	Name        string          `json:"name"`
	DisplayName string          `json:"display_name,omitempty"`
	Desc        string          `json:"desc"`
	Description string          `json:"description,omitempty"` // 别名，与 Desc 相同
	Category    string          `json:"category,omitempty"`
	Parameters  []ParameterInfo `json:"parameters,omitempty"`
}

// ParameterInfo 参数信息
type ParameterInfo struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Default     any    `json:"default,omitempty"`
	Description string `json:"description"`
}

// GetToolInfos 获取工具信息列表
func GetToolInfos(ctx context.Context, tools []tool.BaseTool) []ToolInfo {
	infos := make([]ToolInfo, 0, len(tools))
	for _, t := range tools {
		info, err := t.Info(ctx)
		if err != nil {
			continue
		}
		infos = append(infos, ToolInfo{
			Name: info.Name,
			Desc: info.Desc,
		})
	}
	return infos
}

// BuildToolListPrompt 构建工具列表提示词
func BuildToolListPrompt(ctx context.Context, tools []tool.BaseTool) string {
	var sb strings.Builder
	sb.WriteString("\n## 可用工具\n")

	for _, t := range tools {
		info, err := t.Info(ctx)
		if err != nil {
			continue
		}
		sb.WriteString("- **")
		sb.WriteString(info.Name)
		sb.WriteString("**: ")
		sb.WriteString(info.Desc)
		sb.WriteString("\n")
	}

	return sb.String()
}

// InvokableToBaseTool 将 InvokableTool 转换为 BaseTool
func InvokableToBaseTool(tools []tool.InvokableTool) []tool.BaseTool {
	baseTools := make([]tool.BaseTool, len(tools))
	for i, t := range tools {
		baseTools[i] = t
	}
	return baseTools
}

// StateKey 状态键类型
type StateKey string

const (
	// SessionIDKey 会话ID键
	SessionIDKey StateKey = "session_id"
	// ProjectPathKey 项目路径键
	ProjectPathKey StateKey = "project_path"
)

// GetSessionValue 从上下文获取会话值
func GetSessionValue[T any](ctx context.Context, key StateKey) (T, bool) {
	val, exists := adk.GetSessionValue(ctx, string(key))
	if !exists || val == nil {
		var zero T
		return zero, false
	}
	typed, ok := val.(T)
	return typed, ok
}

// SetSessionValue 设置会话值
func SetSessionValue(ctx context.Context, key StateKey, value any) {
	adk.AddSessionValue(ctx, string(key), value)
}

// MessageToString 将消息转换为字符串
func MessageToString(msg *schema.Message) string {
	if msg == nil {
		return ""
	}
	return msg.Content
}
