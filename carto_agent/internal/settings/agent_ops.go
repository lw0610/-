package settings

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
)

// DecodeAgentConfigData 将 map 形式配置安全反序列化为 AgentConfigData。
func DecodeAgentConfigData(payload map[string]any) (*config.AgentConfigData, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化智能体配置失败: %w", err)
	}
	var result config.AgentConfigData
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("解析智能体配置失败: %w", err)
	}
	return &result, nil
}

// AddAgentToConfig 向配置方案中新增智能体。
func AddAgentToConfig(manager *config.AgentConfigManager, runtime RuntimeController, configName, agentName string, agent config.AgentDefData) (map[string]any, error) {
	if manager == nil {
		return nil, fmt.Errorf("智能体配置管理器未初始化")
	}
	if err := manager.AddAgent(configName, agentName, agent); err != nil {
		return nil, err
	}
	builder := NewMutationBuilder(runtime)
	mutation := builder.New("agent_config")
	ApplyAgentMutation(builder, runtime, &mutation, configName, "智能体已创建，但运行时重建失败: ")
	return MutationEnvelope(map[string]any{
		"agent_name": strings.TrimSpace(agentName),
	}, mutation), nil
}

// UpdateAgentInConfig 更新配置方案中的智能体。
func UpdateAgentInConfig(manager *config.AgentConfigManager, runtime RuntimeController, configName string, agentName string, agent config.AgentDefData) (SettingsMutationResult, error) {
	if manager == nil {
		return SettingsMutationResult{}, fmt.Errorf("智能体配置管理器未初始化")
	}
	if err := manager.UpdateAgent(configName, agentName, agent); err != nil {
		return SettingsMutationResult{}, err
	}
	builder := NewMutationBuilder(runtime)
	mutation := builder.New("agent_config")
	ApplyAgentMutation(builder, runtime, &mutation, configName, "智能体已更新，但运行时重建失败: ")
	return mutation, nil
}

// RemoveAgentFromConfig 删除配置方案中的智能体。
func RemoveAgentFromConfig(manager *config.AgentConfigManager, runtime RuntimeController, configName, agentName string) (SettingsMutationResult, error) {
	if manager == nil {
		return SettingsMutationResult{}, fmt.Errorf("智能体配置管理器未初始化")
	}
	if err := manager.RemoveAgent(configName, agentName); err != nil {
		return SettingsMutationResult{}, err
	}
	builder := NewMutationBuilder(runtime)
	mutation := builder.New("agent_config")
	ApplyAgentMutation(builder, runtime, &mutation, configName, "智能体已删除，但运行时重建失败: ")
	return mutation, nil
}

// CreateAgentConfig 创建或从现有配置克隆一个新方案。
func CreateAgentConfig(manager *config.AgentConfigManager, runtime RuntimeDiagnosticsProvider, name, cloneFrom string) (map[string]any, error) {
	if manager == nil {
		return nil, fmt.Errorf("智能体配置管理器未初始化")
	}
	targetName := strings.TrimSpace(name)
	sourceName := strings.TrimSpace(cloneFrom)
	var err error
	if sourceName != "" {
		err = manager.CloneConfig(sourceName, targetName)
	} else {
		err = manager.CreateConfig(targetName)
	}
	if err != nil {
		return nil, err
	}
	mutation := NewMutationBuilder(runtime).New("agent_config")
	return MutationEnvelope(map[string]any{
		"config_name": targetName,
		"cloned_from": sourceName,
	}, mutation), nil
}

// CloneAgentConfig 克隆配置方案。
func CloneAgentConfig(manager *config.AgentConfigManager, runtime RuntimeDiagnosticsProvider, sourceName, targetName string) (map[string]any, error) {
	return CreateAgentConfig(manager, runtime, targetName, sourceName)
}

// SaveAgentConfig 保存完整配置方案。
func SaveAgentConfig(manager *config.AgentConfigManager, runtime RuntimeController, userID, name string, data *config.AgentConfigData) (SettingsMutationResult, error) {
	if manager == nil {
		return SettingsMutationResult{}, fmt.Errorf("智能体配置管理器未初始化")
	}
	if err := manager.SaveConfig(userID, name, data); err != nil {
		return SettingsMutationResult{}, err
	}
	builder := NewMutationBuilder(runtime)
	mutation := builder.New("agent_config")
	ApplyAgentMutation(builder, runtime, &mutation, name, "配置已保存，但运行时重建失败: ")
	return mutation, nil
}

// DeleteAgentConfig 删除配置方案，并尝试重建当前激活运行态。
func DeleteAgentConfig(manager *config.AgentConfigManager, runtime RuntimeController, userID, name string) (map[string]any, error) {
	if manager == nil {
		return nil, fmt.Errorf("智能体配置管理器未初始化")
	}
	targetName := strings.TrimSpace(name)
	wasActive := strings.TrimSpace(manager.GetActiveConfigName()) == targetName
	if err := manager.DeleteConfig(userID, targetName); err != nil {
		return nil, err
	}
	builder := NewMutationBuilder(runtime)
	mutation := builder.New("agent_config_delete")
	if wasActive {
		ApplyActiveRuntimeMutation(builder, runtime, &mutation, "配置方案已删除，但运行时重建失败: ")
	} else {
		mutation.RequiredAction = MutationRequiredActionNone
		mutation.NextStep = ""
		mutation = builder.Attach(mutation)
	}
	return MutationEnvelope(map[string]any{
		"config_name": targetName,
	}, mutation), nil
}

// ActivateAgentConfig 激活配置方案并同步运行态。
func ActivateAgentConfig(manager *config.AgentConfigManager, runtime RuntimeController, userID, name string) (SettingsMutationResult, error) {
	if manager == nil {
		return SettingsMutationResult{}, fmt.Errorf("智能体配置管理器未初始化")
	}
	if err := manager.SwitchConfig(userID, name); err != nil {
		return SettingsMutationResult{}, err
	}
	builder := NewMutationBuilder(runtime)
	mutation := builder.NewTransient("agent_config_switch")
	ApplyAgentMutation(builder, runtime, &mutation, name, "配置方案已切换，但运行时重建失败: ")
	return mutation, nil
}
