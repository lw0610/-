package agentruntime

import (
	"context"
	"strings"

	"github.com/autocarto/carto_agent/internal/capability"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
)

// ModeIssue 描述某个执行模式当前不可用的原因。
type ModeIssue struct {
	Code    string
	Message string
}

// Runtime 统一封装按模式构建出的 agent 集合。
type Runtime struct {
	ActiveConfigName string
	ActiveConfig     *config.AgentConfigFile
	Revision         string

	PlanAgent  adk.Agent
	BuildAgent adk.Agent
	ModeIssues map[string]ModeIssue
}

// Agent 返回指定模式下的 agent，并在必要时执行降级。
func (r *Runtime) Agent(mode string) adk.Agent {
	agent, _ := r.ResolveAgent(mode)
	return agent
}

// ResolveAgent 返回指定模式下的 agent，以及是否发生了降级。
func (r *Runtime) ResolveAgent(mode string) (adk.Agent, bool) {
	normalized := config.NormalizeSessionExecutionMode(mode)
	switch normalized {
	case "plan":
		if r.PlanAgent != nil {
			return r.PlanAgent, false
		}
		if r.BuildAgent != nil {
			return r.BuildAgent, true
		}
	default:
		if r.BuildAgent != nil {
			return r.BuildAgent, false
		}
		if r.PlanAgent != nil {
			return r.PlanAgent, true
		}
	}

	return nil, false
}

// ModeIssue 返回指定模式的不可用信息。
func (r *Runtime) ModeIssue(mode string) (ModeIssue, bool) {
	if r == nil || len(r.ModeIssues) == 0 {
		return ModeIssue{}, false
	}
	issue, ok := r.ModeIssues[config.NormalizeSessionExecutionMode(mode)]
	if !ok {
		return ModeIssue{}, false
	}
	if strings.TrimSpace(issue.Code) == "" && strings.TrimSpace(issue.Message) == "" {
		return ModeIssue{}, false
	}
	return issue, true
}

// FactoryConfig 统一运行时工厂输入。
type FactoryConfig struct {
	DefaultChatModel     model.ToolCallingChatModel
	DefaultProviderCfg   *config.LLMProviderConfig
	ResolveChatModel     func(provider string) (model.ToolCallingChatModel, error)
	ResolvePlanChatModel func(provider string) (model.ToolCallingChatModel, error)
	ResolveProviderCfg   func(provider string) *config.LLMProviderConfig
	BaseTools            []tool.BaseTool
	MaxIterations        int
	BuiltinSkillsDir     string
	UserSkillsDir        string
	PluginSkillsDir      string
	ConfigBaseDir        string
	RegistryGovernance   *control.RegistryGovernance
	CapabilityStore      *capability.SnapshotStore
	CapabilityMode       string
	ControlPlane         *capability.ControlPlane
}

// Builder 工厂接口，便于后续注入测试桩。
type Builder interface {
	Build(ctx context.Context, activeConfigName string, activeConfig *config.AgentConfigFile) (*Runtime, error)
	BuildActive(ctx context.Context, manager *config.AgentConfigManager) (*Runtime, error)
}
