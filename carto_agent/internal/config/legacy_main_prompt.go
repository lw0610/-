package config

import (
	"crypto/sha256"
	"fmt"

	"github.com/autocarto/carto_agent/defaults"
	"gopkg.in/yaml.v3"
)

// Full-byte fingerprints of official prompts from b6adfc3/791bc4c and 6391b2e.
// Their complete retired text lives only in testdata, never in active prompts.
// Do not normalize whitespace: edited or unrelated prompts must stay untouched.
const (
	legacyMainPromptV4SHA256          = "5d2a838fbde8d53f4feb057dc9fda35ef8d17fd7da094140d243b441cd0c608e"
	legacyMainPromptAcquisitionSHA256 = "8edb9af792990c14080734ebe04f6eded8bad9b97cc5c7fec4ff8f08d1d3b5de"
)

// migrateKnownMainFlowPrompts modifies only the independent migration candidate.
// File loading remains read-only; an ordinary SaveConfig persists the candidate.
func migrateKnownMainFlowPrompts(candidate *AgentConfigFile) []MigrationWarning {
	if candidate == nil || candidate.Runtime == nil {
		return nil
	}
	var target *AgentDefData
	warnings := []MigrationWarning{}
	for _, name := range sortedRuntimeAgentNames(candidate.Runtime.Agents) {
		role := candidate.Runtime.Agents[name]
		if !role.IsMain {
			continue
		}
		fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(role.Prompts["static_instructions"])))
		if fingerprint != legacyMainPromptV4SHA256 && fingerprint != legacyMainPromptAcquisitionSHA256 {
			continue
		}
		path := "runtime.agents." + name + ".prompts.static_instructions"
		if target == nil {
			current, err := embeddedMainPromptTarget()
			if err != nil {
				warnings = append(warnings, MigrationWarning{Code: "LEGACY_MAIN_FLOW_PROMPT_UPGRADE_FAILED", Path: path, Message: err.Error()})
				continue
			}
			target = &current
		}
		role.Prompts["static_instructions"] = target.Prompts["static_instructions"]
		role.ToolPackages = normalizeStringSlice(append(role.ToolPackages, target.ToolPackages...))
		candidate.Runtime.Agents[name] = role
		warnings = append(warnings, MigrationWarning{
			Code: "LEGACY_MAIN_FLOW_PROMPT_UPGRADED", Path: path,
			Message: "已识别未编辑的旧官方主智能体提示词，已在运行配置中升级为当前流程读取与制图计划规则，并补齐数据服务工具包；其他自定义配置保持不变，保存方案后持久化。",
		})
	}
	return warnings
}

func embeddedMainPromptTarget() (AgentDefData, error) {
	raw, err := defaults.FS.ReadFile("agent_configs/default/agent.yaml")
	if err != nil {
		return AgentDefData{}, fmt.Errorf("读取当前官方主智能体提示词失败: %w", err)
	}
	var embedded AgentConfigFile
	// Do not normalize here: normalization itself invokes prompt migration.
	if err := yaml.Unmarshal(raw, &embedded); err != nil {
		return AgentDefData{}, fmt.Errorf("解析当前官方主智能体提示词失败: %w", err)
	}
	if embedded.Runtime != nil {
		for _, name := range sortedRuntimeAgentNames(embedded.Runtime.Agents) {
			role := embedded.Runtime.Agents[name]
			if role.IsMain && role.Prompts["static_instructions"] != "" {
				return role, nil
			}
		}
	}
	return AgentDefData{}, fmt.Errorf("当前官方方案缺少主智能体提示词")
}
