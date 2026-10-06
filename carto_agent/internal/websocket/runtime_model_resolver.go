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

func (s *DeepServer) resolvePlanChatModel(providerName string) (model.ToolCallingChatModel, error) {
	candidates, err := s.resolvePlanProviderCandidates(providerName)
	if err != nil {
		return nil, err
	}

	var failureMessages []string
	for _, provider := range candidates {
		chatModel, resolveErr := buildPlanChatModelFromProvider(provider, providerName)
		if resolveErr == nil {
			return chatModel, nil
		}
		failureMessages = append(failureMessages, resolveErr.Error())
	}

	return nil, fmt.Errorf("%s", strings.Join(failureMessages, "; "))
}

func (s *DeepServer) resolvePlanProviderCandidates(providerName string) ([]*config.LLMProviderConfig, error) {
	if s.agentRebuildConfigSnapshot() == nil {
		return nil, fmt.Errorf("agentRebuildConfig 未设置")
	}

	trimmedProvider := strings.TrimSpace(providerName)
	if trimmedProvider != "" {
		provider, err := s.resolveProviderByName(trimmedProvider)
		if err != nil {
			return nil, err
		}
		return []*config.LLMProviderConfig{provider}, nil
	}

	candidates := make([]*config.LLMProviderConfig, 0, 2)
	seen := make(map[string]struct{}, 2)
	appendCandidate := func(provider *config.LLMProviderConfig) {
		if provider == nil {
			return
		}
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			name = fmt.Sprintf("%s::%s", strings.TrimSpace(provider.Provider), strings.TrimSpace(provider.Model))
		}
		if name == "" {
			return
		}
		if _, exists := seen[name]; exists {
			return
		}
		seen[name] = struct{}{}
		candidates = append(candidates, provider)
	}

	if s.configService != nil {
		if quickProvider, err := s.configService.GetQuickProvider(); err == nil {
			appendCandidate(quickProvider)
		}
	}
	if quickProvider := s.resolveRuntimeQuickProviderConfig(); quickProvider != nil {
		appendCandidate(quickProvider)
	}

	if activeProvider := s.resolveActiveRuntimeProviderConfig(); activeProvider != nil {
		appendCandidate(activeProvider)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("全局 provider 未初始化")
	}

	return candidates, nil
}

func (s *DeepServer) resolveRuntimeQuickProviderConfig() *config.LLMProviderConfig {
	rebuildConfig := s.agentRebuildConfigSnapshot()
	if rebuildConfig == nil || rebuildConfig.BaseConfig == nil {
		return nil
	}
	quickName := strings.TrimSpace(rebuildConfig.BaseConfig.LLM.QuickModel)
	if quickName == "" {
		return nil
	}
	if s.configService != nil {
		provider, err := s.configService.GetProviderByName(quickName)
		if err == nil && provider != nil {
			return provider
		}
	}
	return cloneProviderConfig(rebuildConfig.BaseConfig.GetProviderByName(quickName))
}

func (s *DeepServer) resolveActiveRuntimeProviderConfig() *config.LLMProviderConfig {
	defaultProvider := s.defaultRuntimeProviderConfig()
	if defaultProvider == nil {
		return nil
	}
	if s.configService == nil {
		return defaultProvider
	}
	provider, err := s.configService.GetProviderByName(defaultProvider.Name)
	if err != nil || provider == nil {
		return defaultProvider
	}
	return provider
}

func buildPlanChatModelFromProvider(provider *config.LLMProviderConfig, providerName string) (model.ToolCallingChatModel, error) {
	if provider == nil {
		return nil, fmt.Errorf("创建 Plan 模式独立模型失败（provider=%s）: provider 未初始化", strings.TrimSpace(providerName))
	}

	normalized, err := llm.PrepareConfigForCapability(config.ProviderToLLMConfig(provider), llm.CapabilityPlanExecuteForcedToolChoice)
	if err != nil {
		targetName := strings.TrimSpace(provider.Name)
		if targetName == "" {
			targetName = strings.TrimSpace(providerName)
		}
		return nil, fmt.Errorf("创建 Plan 模式独立模型失败（provider=%s backend=%s model=%s）: %w", targetName, provider.Provider, provider.Model, err)
	}

	chatModel, err := llm.NewChatModel(normalized)
	if err != nil {
		targetName := strings.TrimSpace(provider.Name)
		if targetName == "" {
			targetName = strings.TrimSpace(providerName)
		}
		return nil, fmt.Errorf("创建 Plan 模式独立模型失败（provider=%s backend=%s model=%s）: %w", targetName, provider.Provider, provider.Model, err)
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
