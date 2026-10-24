package agentruntime

import (
	"context"
	"fmt"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
)

func (f *Factory) buildDeepAgent(ctx context.Context, activeConfigName string, activeConfig *config.AgentConfigFile) (adk.ResumableAgent, error) {
	cfg, err := f.deepAgentConfig(activeConfigName, activeConfig)
	if err != nil {
		return nil, err
	}
	return deepagent.New(ctx, cfg)
}

// deepAgentConfig keeps delegated and flow roles on the same runtime projection.
func (f *Factory) deepAgentConfig(activeConfigName string, activeConfig *config.AgentConfigFile) (*deepagent.Config, error) {
	if activeConfig == nil {
		return nil, fmt.Errorf("active config is required")
	}
	runtimeSkillConfig := config.ConvertToRuntimeSkillConfigWithPlugin(activeConfig, f.config.PluginSkillsDir, f.config.BuiltinSkillsDir, f.config.UserSkillsDir, f.config.ConfigBaseDir, activeConfigName)
	if runtimeSkillConfig == nil {
		return nil, fmt.Errorf("runtime config projection failed")
	}

	return &deepagent.Config{
		Name:                 "orchestrator",
		Description:          "AutoCarto AI 智能GIS助手",
		ChatModel:            f.config.DefaultChatModel,
		Tools:                cloneBaseTools(f.config.BaseTools),
		MaxIteration:         f.maxIterations(),
		SkillRuntimeConfig:   runtimeSkillConfig,
		BuiltinSkillsDir:     f.config.BuiltinSkillsDir,
		UserSkillsDir:        f.config.UserSkillsDir,
		PluginSkillsDir:      f.config.PluginSkillsDir,
		RegistryGovernance:   f.config.RegistryGovernance,
		DefaultProviderCfg:   cloneProviderConfig(f.config.DefaultProviderCfg),
		ActiveConfigName:     activeConfigName,
		RuntimeConfigBaseDir: f.config.ConfigBaseDir,
		AgentLLMs:            collectBuildAgentLLMs(activeConfig),
		ResolveAgentChatModel: func(provider string) (model.ToolCallingChatModel, error) {
			return f.resolveChatModel(provider)
		},
		ResolveProviderCfg: func(provider string) *config.LLMProviderConfig {
			return f.resolveProviderConfig(provider)
		},
	}, nil
}
