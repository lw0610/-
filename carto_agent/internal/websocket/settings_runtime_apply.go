package websocket

import (
	"fmt"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/api"
)

// ApplyPlanModeConfig 重新加载 plan_mode 相关配置并尝试重建运行时。
func (s *DeepServer) ApplyPlanModeConfig() error {
	s.setPendingRuntimeRebuildReason("plan_mode_config")
	return s.reloadPlanModeConfig()
}

// ApplyLLMConfig 使用当前激活 provider 重建 ChatModel 与运行时。
func (s *DeepServer) ApplyLLMConfig() error {
	s.setPendingRuntimeRebuildReason("llm_config")
	if s == nil || s.configService == nil {
		return fmt.Errorf("configService 未初始化")
	}
	fullCfg, err := s.configService.GetFullConfig()
	if err != nil {
		return err
	}
	if fullCfg == nil {
		return fmt.Errorf("完整配置为空")
	}
	provider := fullCfg.GetActiveProvider()
	if provider == nil {
		return fmt.Errorf("激活模型不存在")
	}
	if apiKey, keyErr := s.configService.GetAPIKey(provider.Name); keyErr == nil && strings.TrimSpace(apiKey) != "" {
		provider.APIKey = apiKey
	}
	return s.rebuildChatModelAndAgent(provider)
}

// ApplyAgentConfig 在目标配置处于激活态时重建运行时。
func (s *DeepServer) ApplyAgentConfig(configName string) (bool, error) {
	if s == nil || s.agentConfigManager == nil {
		return false, fmt.Errorf("agentConfigManager 未初始化")
	}
	target := strings.TrimSpace(configName)
	active := strings.TrimSpace(s.agentConfigManager.GetActiveConfigName())
	if target == "" || active == "" || !strings.EqualFold(target, active) {
		return false, nil
	}
	s.setPendingRuntimeRebuildReason(fmt.Sprintf("agent_config:%s", target))
	if err := s.rebuildAgent(); err != nil {
		return true, err
	}
	s.requestCapabilityRefresh(fmt.Sprintf("agent_config:%s", target))
	return true, nil
}

// ApplyActiveRuntime 针对当前激活配置执行一次运行时重建。
func (s *DeepServer) ApplyActiveRuntime() error {
	if s == nil || s.agentConfigManager == nil {
		return fmt.Errorf("agentConfigManager 未初始化")
	}
	if strings.TrimSpace(s.agentConfigManager.GetActiveConfigName()) == "" {
		return nil
	}
	s.setPendingRuntimeRebuildReason("active_runtime_refresh")
	if err := s.rebuildAgent(); err != nil {
		return err
	}
	s.requestCapabilityRefresh("active_runtime_refresh")
	return nil
}

// GetRuntimeRevision 返回当前运行时修订号。
func (s *DeepServer) GetRuntimeRevision() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	runtime := s.runtime
	s.mu.Unlock()
	if runtime == nil {
		return ""
	}
	return strings.TrimSpace(runtime.Revision)
}

// GetRuntimeActiveConfigName 返回当前运行时绑定的配置名。
func (s *DeepServer) GetRuntimeActiveConfigName() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	runtime := s.runtime
	s.mu.Unlock()
	if runtime != nil && strings.TrimSpace(runtime.ActiveConfigName) != "" {
		return strings.TrimSpace(runtime.ActiveConfigName)
	}
	if s.agentConfigManager == nil {
		return ""
	}
	return strings.TrimSpace(s.agentConfigManager.GetActiveConfigName())
}

// GetRuntimeModeIssues 返回当前运行时记录的模式可用性问题。
func (s *DeepServer) GetRuntimeModeIssues() map[string]api.SettingsModeIssue {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	runtime := s.runtime
	s.mu.Unlock()
	if runtime == nil || len(runtime.ModeIssues) == 0 {
		return nil
	}
	result := make(map[string]api.SettingsModeIssue, len(runtime.ModeIssues))
	for mode, issue := range runtime.ModeIssues {
		if strings.TrimSpace(mode) == "" {
			continue
		}
		result[mode] = api.SettingsModeIssue{
			Code:    strings.TrimSpace(issue.Code),
			Message: strings.TrimSpace(issue.Message),
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

var _ interface {
	ApplyPlanModeConfig() error
	ApplyLLMConfig() error
	ApplyAgentConfig(configName string) (bool, error)
	ApplyActiveRuntime() error
	GetRuntimeRevision() string
	GetRuntimeActiveConfigName() string
	GetRuntimeModeIssues() map[string]api.SettingsModeIssue
	GetToolSchemaRevision() string
	GetToolAvailabilityRevision() string
	GetLastRuntimeRebuildReason() string
	GetLastRuntimeRebuildAt() time.Time
	GetPlannerReadyReason() string
	GetActiveInstanceID() string
} = (*DeepServer)(nil)
