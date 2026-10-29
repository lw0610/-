package websocket

import (
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/llm"
)

func (s *DeepServer) resolveRuntimeChatModel(providerName string) (model.ToolCallingChatModel, error) {
	rebuildConfig := s.agentRebuildConfigSnapshot()
	if rebuildConfig == nil {
		return nil, fmt.Errorf("agentRebuildConfig 未设置")
	}

	trimmedProvider := strings.TrimSpace(providerName)
	if trimmedProvider == "" {
		if rebuildConfig.ChatModel == nil {
			return nil, fmt.Errorf("全局 ChatModel 未初始化")
		}
		return rebuildConfig.ChatModel, nil
	}

	provider, err := s.resolveProviderByName(trimmedProvider)
	if err != nil {
		return nil, err
	}

	chatModel, err := llm.NewChatModel(config.ProviderToLLMConfig(provider))
	if err != nil {
		return nil, fmt.Errorf("创建运行时独立模型失败（provider=%s）: %w", trimmedProvider, err)
	}
	return chatModel, nil
}

func (s *DeepServer) resolveProviderByName(providerName string) (*config.LLMProviderConfig, error) {
	trimmed := strings.TrimSpace(providerName)
	if trimmed == "" {
		return nil, fmt.Errorf("provider 不能为空")
	}

	if s.configService != nil {
		provider, err := s.configService.GetProviderByName(trimmed)
		if err == nil && provider != nil {
			return provider, nil
		}
	}

	if rebuildConfig := s.agentRebuildConfigSnapshot(); rebuildConfig != nil && rebuildConfig.BaseConfig != nil {
		provider := rebuildConfig.BaseConfig.GetProviderByName(trimmed)
		if provider != nil {
			cp := *provider
			return &cp, nil
		}
	}

	return nil, fmt.Errorf("未找到 provider: %s", trimmed)
}

func (s *DeepServer) defaultRuntimeProviderConfig() *config.LLMProviderConfig {
	rebuildConfig := s.agentRebuildConfigSnapshot()
	if rebuildConfig == nil || rebuildConfig.BaseConfig == nil {
		return nil
	}
	return cloneProviderConfig(rebuildConfig.BaseConfig.GetActiveProvider())
}

func (s *DeepServer) resolveRuntimeProviderConfig(providerName string) *config.LLMProviderConfig {
	trimmed := strings.TrimSpace(providerName)
	if trimmed == "" {
		return s.defaultRuntimeProviderConfig()
	}
	// Use the same lookup order as resolveRuntimeChatModel so updated model
	// settings and the identity/budgets attached to that model stay together.
	provider, err := s.resolveProviderByName(trimmed)
	if err != nil {
		return nil
	}
	return cloneProviderConfig(provider)
}

func cloneProviderConfig(cfg *config.LLMProviderConfig) *config.LLMProviderConfig {
	if cfg == nil {
		return nil
	}
	cp := *cfg
	return &cp
}
