package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/cloudwego/eino/adk"
)

// RoleOverlay belongs to one step. It never mutates the saved or shared profile.
type RoleOverlay struct {
	FlowRole      *flow.FlowRoleSpec
	RoleID        string
	ExtraPackages []string
}

func configWithRoleOverlay(active *config.AgentConfigFile, overlay RoleOverlay) (*config.AgentConfigFile, string, error) {
	if active == nil || active.Runtime == nil {
		return nil, "", fmt.Errorf("流程角色缺少方案配置")
	}
	encoded, err := json.Marshal(active)
	if err != nil {
		return nil, "", err
	}
	var candidate config.AgentConfigFile
	if err = json.Unmarshal(encoded, &candidate); err != nil {
		return nil, "", err
	}
	id := overlay.RoleID
	if overlay.FlowRole != nil {
		r := overlay.FlowRole
		id = r.ID
		if _, exists := candidate.Runtime.Agents[id]; exists {
			return nil, "", fmt.Errorf("流程自带角色编号与方案冲突：%s", id)
		}
		if candidate.Runtime.Agents == nil {
			candidate.Runtime.Agents = map[string]config.AgentDefData{}
		}
		candidate.Runtime.Agents[id] = config.AgentDefData{Kind: r.Kind, Executor: r.Executor, FlowOnly: true, Enabled: true, Deletable: true, DisplayName: r.DisplayName, Description: r.Description, ToolPackages: append([]string{}, r.ToolPackages...), LLM: config.AgentLLMConfig{Provider: r.Provider}, Prompts: map[string]string{"static_instructions": r.Description}}
	}
	role, exists := candidate.Runtime.Agents[id]
	if !exists || !role.Enabled {
		return nil, "", fmt.Errorf("步骤负责角色不可用：%s", id)
	}
	seen := map[string]bool{}
	for _, name := range role.ToolPackages {
		seen[name] = true
	}
	for _, name := range overlay.ExtraPackages {
		if !seen[name] {
			role.ToolPackages = append(role.ToolPackages, name)
			seen[name] = true
		}
	}
	candidate.Runtime.Agents[id] = role
	return &candidate, id, nil
}
func (f *Factory) BuildRoleWithOverlay(ctx context.Context, scheme string, cfg *config.AgentConfigFile, overlay RoleOverlay) (adk.Agent, error) {
	candidate, id, err := configWithRoleOverlay(cfg, overlay)
	if err != nil {
		return nil, err
	}
	return f.BuildRole(ctx, scheme, candidate, id)
}
