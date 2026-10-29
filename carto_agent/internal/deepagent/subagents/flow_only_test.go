package subagents

import (
	"context"
	"strings"
	"testing"
)

func TestConfiguredRolesRemainInDelegationPrompts(t *testing.T) {
	cfg := &RuntimeSkillConfig{Agents: map[string]RuntimeSkillAgentConfig{
		"orchestrator":  {IsMain: true, Enabled: true},
		"worker":        {Enabled: true, DisplayName: "普通角色"},
		"flow_executor": {Enabled: true, FlowOnly: true, DisplayName: "流程专用角色"},
	}}
	engine := NewTemplateEngine(cfg)
	prompt, err := engine.BuildOrchestratorPrompt(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "flow_executor") || !strings.Contains(prompt, "流程专用角色") || !strings.Contains(prompt, "worker") {
		t.Fatal("prompt omitted a configured delegate")
	}
}
