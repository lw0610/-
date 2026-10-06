package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/capability"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
)

// Factory 从活动配置构建 plan/build 两类 agent。
type Factory struct {
	config FactoryConfig
}

// NewFactory 创建统一运行时工厂。
func NewFactory(cfg *FactoryConfig) (*Factory, error) {
	if cfg == nil {
		return nil, fmt.Errorf("factory config is required")
	}
	if cfg.DefaultChatModel == nil {
		return nil, fmt.Errorf("default chat model is required")
	}

	controlPlane := capability.NormalizeControlPlane(cfg.ControlPlane)
	baseTools := make([]tool.BaseTool, len(cfg.BaseTools))
	for idx, item := range cfg.BaseTools {
		if item == nil {
			return nil, fmt.Errorf("base tool[%d] is nil", idx)
		}
		baseTools[idx] = wrapToolWithErrorHandler(item, controlPlane)
	}

	return &Factory{
		config: FactoryConfig{
			DefaultChatModel:     cfg.DefaultChatModel,
			DefaultProviderCfg:   cloneProviderConfig(cfg.DefaultProviderCfg),
			ResolveChatModel:     cfg.ResolveChatModel,
			ResolvePlanChatModel: cfg.ResolvePlanChatModel,
			ResolveProviderCfg:   cfg.ResolveProviderCfg,
			BaseTools:            baseTools,
			MaxIterations:        cfg.MaxIterations,
			BuiltinSkillsDir:     cfg.BuiltinSkillsDir,
			UserSkillsDir:        cfg.UserSkillsDir,
			PluginSkillsDir:      cfg.PluginSkillsDir,
			ConfigBaseDir:        cfg.ConfigBaseDir,
			RegistryGovernance:   cfg.RegistryGovernance,
			CapabilityStore:      cfg.CapabilityStore,
			CapabilityMode:       cfg.CapabilityMode,
			ControlPlane:         controlPlane,
		},
	}, nil
}

// BuildActive 从 AgentConfigManager 当前活动配置构建 Runtime。
func (f *Factory) BuildActive(ctx context.Context, manager *config.AgentConfigManager) (*Runtime, error) {
	if manager == nil {
		return nil, fmt.Errorf("agent config manager is required")
	}

	activeConfig := manager.GetActiveConfig()
	if activeConfig == nil {
		return nil, fmt.Errorf("active agent config not found")
	}

	return f.Build(ctx, manager.GetActiveConfigName(), activeConfig)
}

// Build 从指定活动配置构建 Runtime。
func (f *Factory) Build(ctx context.Context, activeConfigName string, activeConfig *config.AgentConfigFile) (*Runtime, error) {
	if activeConfig == nil {
		return nil, fmt.Errorf("active config is required")
	}

	buildAgent, err := f.buildDeepAgent(ctx, activeConfigName, activeConfig)
	if err != nil {
		return nil, err
	}

	runtime := &Runtime{
		ActiveConfigName: strings.TrimSpace(activeConfigName),
		ActiveConfig:     activeConfig,
		Revision:         buildRuntimeRevision(activeConfigName, activeConfig),
		BuildAgent:       buildAgent,
		ModeIssues:       map[string]ModeIssue{},
	}

	if !activeConfig.Meta.PlanEnabled {
		runtime.PlanAgent = buildAgent
		return runtime, nil
	}

	planAgent, err := f.buildPlanComponents(ctx, activeConfigName, activeConfig)
	if err != nil {
		runtime.ModeIssues["plan"] = ModeIssue{
			Code:    "PLAN_PROTOCOL_VIOLATION",
			Message: fmt.Sprintf("Plan 模式不可用：%v", err),
		}
		return runtime, nil
	}

	runtime.PlanAgent = planAgent
	return runtime, nil
}

func (f *Factory) maxIterations() int {
	if f.config.MaxIterations > 0 {
		return f.config.MaxIterations
	}
	return 50
}

func (f *Factory) resolveChatModel(provider string) (model.ToolCallingChatModel, error) {
	trimmed := strings.TrimSpace(provider)
	if trimmed == "" {
		return f.config.DefaultChatModel, nil
	}

	if f.config.ResolveChatModel == nil {
		return nil, fmt.Errorf("resolve chat model callback is not configured")
	}

	return f.config.ResolveChatModel(trimmed)
}

func (f *Factory) resolvePlanChatModel(provider string) (model.ToolCallingChatModel, error) {
	if f.config.ResolvePlanChatModel != nil {
		return f.config.ResolvePlanChatModel(strings.TrimSpace(provider))
	}
	return f.resolveChatModel(provider)
}

func (f *Factory) resolveProviderConfig(provider string) *config.LLMProviderConfig {
	trimmed := strings.TrimSpace(provider)
	if trimmed == "" {
		return cloneProviderConfig(f.config.DefaultProviderCfg)
	}
	if f.config.ResolveProviderCfg == nil {
		return nil
	}
	return cloneProviderConfig(f.config.ResolveProviderCfg(trimmed))
}

func cloneBaseTools(items []tool.BaseTool) []tool.BaseTool {
	if len(items) == 0 {
		return nil
	}
	out := make([]tool.BaseTool, len(items))
	copy(out, items)
	return out
}

func buildRuntimeRevision(activeConfigName string, activeConfig *config.AgentConfigFile) string {
	payload := map[string]any{
		"active_config_name": strings.TrimSpace(activeConfigName),
		"config":             activeConfig,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return strings.TrimSpace(activeConfigName)
	}

	sum := sha256.Sum256(data)
	return fmt.Sprintf("%s:%s", strings.TrimSpace(activeConfigName), hex.EncodeToString(sum[:8]))
}

func collectBuildAgentLLMs(activeConfig *config.AgentConfigFile) map[string]config.AgentLLMConfig {
	if activeConfig == nil || activeConfig.Runtime == nil || len(activeConfig.Runtime.Agents) == 0 {
		return nil
	}

	out := make(map[string]config.AgentLLMConfig)
	for agentName, agentDef := range activeConfig.Runtime.Agents {
		provider := strings.TrimSpace(agentDef.LLM.Provider)
		if provider == "" {
			continue
		}
		out[agentName] = config.AgentLLMConfig{Provider: provider}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneProviderConfig(cfg *config.LLMProviderConfig) *config.LLMProviderConfig {
	if cfg == nil {
		return nil
	}
	cp := *cfg
	return &cp
}

func selectPlanMainAgent(activeConfig *config.AgentConfigFile) *config.PlanAgentDefData {
	if activeConfig == nil || activeConfig.Plan == nil || len(activeConfig.Plan.Agents) == 0 {
		return nil
	}

	if agentDef, ok := activeConfig.Plan.Agents["planner"]; ok {
		cp := agentDef
		return &cp
	}

	names := make([]string, 0, len(activeConfig.Plan.Agents))
	for name, agentDef := range activeConfig.Plan.Agents {
		if agentDef.IsMain {
			cp := agentDef
			return &cp
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil
	}

	cp := activeConfig.Plan.Agents[names[0]]
	return &cp
}

func wrapSingleAgent(ctx context.Context, name, description string, agent adk.Agent) (adk.ResumableAgent, error) {
	if agent == nil {
		return nil, fmt.Errorf("agent is required")
	}
	return adk.NewSequentialAgent(ctx, &adk.SequentialAgentConfig{
		Name:        name,
		Description: description,
		SubAgents:   []adk.Agent{agent},
	})
}
