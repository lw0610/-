package config

import (
	"fmt"
	"strings"
)

func CreateManagedConfig(manager *AgentConfigManager, name, cloneFrom string, data *AgentConfigData) error {
	if manager == nil {
		return fmt.Errorf("agent config manager is nil")
	}
	configName := strings.TrimSpace(name)
	if configName == "" {
		return fmt.Errorf("config_name 不能为空")
	}
	if source := strings.TrimSpace(cloneFrom); source != "" {
		if err := manager.CloneConfig(source, configName); err != nil {
			return err
		}
	} else {
		if err := manager.CreateConfig(configName); err != nil {
			return err
		}
	}
	if data == nil {
		return nil
	}
	return manager.SaveConfig("", configName, data)
}

func UpdateManagedConfig(manager *AgentConfigManager, name string, data *AgentConfigData) error {
	if manager == nil {
		return fmt.Errorf("agent config manager is nil")
	}
	if data == nil {
		return fmt.Errorf("config_data 不能为空")
	}
	configName := strings.TrimSpace(name)
	if configName == "" {
		return fmt.Errorf("config_name 不能为空")
	}
	return manager.SaveConfig("", configName, data)
}

func AssignManagedSkillPackages(manager *AgentConfigManager, configName, mode, target, agentName string, skillPackages []string) (*AgentConfigData, error) {
	if manager == nil {
		return nil, fmt.Errorf("agent config manager is nil")
	}
	configName = strings.TrimSpace(configName)
	if configName == "" {
		return nil, fmt.Errorf("config_name 不能为空")
	}
	data, err := manager.GetConfigData(configName)
	if err != nil {
		return nil, err
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "runtime"
	}
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		target = "global"
	}
	normalized := normalizeStringSlice(skillPackages)

	switch mode {
	case "runtime":
		if data.Runtime == nil {
			data.Runtime = &RuntimeConfig{Global: GlobalAgentConfig{}, Agents: map[string]AgentDefData{}}
		}
		if target == "global" {
			data.Runtime.Global.SkillPackages = normalized
		} else {
			agentKey := strings.TrimSpace(agentName)
			agent, ok := data.Runtime.Agents[agentKey]
			if !ok {
				return nil, fmt.Errorf("runtime 智能体不存在: %s", agentKey)
			}
			agent.SkillPackages = normalized
			data.Runtime.Agents[agentKey] = agent
		}
	case "plan":
		if data.Plan == nil {
			data.Plan = &PlanConfig{Global: GlobalAgentConfig{}, Agents: map[string]PlanAgentDefData{}}
		}
		if target == "global" {
			data.Plan.Global.SkillPackages = normalized
		} else {
			agentKey := strings.TrimSpace(agentName)
			agent, ok := data.Plan.Agents[agentKey]
			if !ok {
				return nil, fmt.Errorf("plan 智能体不存在: %s", agentKey)
			}
			agent.SkillPackages = normalized
			data.Plan.Agents[agentKey] = agent
		}
	default:
		return nil, fmt.Errorf("不支持的 mode: %s", mode)
	}

	if err := manager.SaveConfig("", configName, data); err != nil {
		return nil, err
	}
	return data, nil
}
