package settings

import (
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
)

func TestAgentProfileViewRoundTrip(t *testing.T) {
	profile := AgentProfileView{
		Meta: config.AgentConfigMeta{
			DefaultExecutionMode: "plan",
			Description:          "测试方案",
			PlanEnabled:          true,
		},
		RuntimeShared: AgentProfileGlobalView{
			SkillPackages: []string{"prompt-refinement"},
		},
		RuntimeAgents: []AgentProfileRuntimeAgentView{
			{
				Name: "orchestrator",
				Agent: AgentProfileRuntimeAgentData{
					DisplayName:   "运行主控",
					Description:   "runtime main",
					IsMain:        true,
					Enabled:       true,
					SkillPackages: []string{"vector-selection"},
					Prompts:       map[string]string{"static_instructions": "test"},
				},
			},
		},
		HasPlan: true,
		Plan: AgentProfilePlanView{
			Enabled:       true,
			MaxIterations: 42,
			Shared: AgentProfileGlobalView{
				SkillPackages: []string{"pyqgis-workflow"},
			},
			Agents: []AgentProfilePlanAgentView{
				{
					Name: "planner",
					Agent: AgentProfilePlanAgentData{
						DisplayName:   "规划主控",
						IsMain:        true,
						Mode:          "readonly_plus",
						SkillPackages: []string{"project-state"},
						Prompts:       map[string]string{"static_instructions": "plan"},
					},
				},
			},
		},
	}

	expanded := ExpandAgentProfileView(profile)
	if expanded == nil {
		t.Fatal("expected expanded config data")
	}
	if expanded.Runtime == nil || expanded.Plan == nil {
		t.Fatalf("expected runtime and plan config after expansion: %#v", expanded)
	}
	if expanded.Plan.MaxIterations != 42 {
		t.Fatalf("unexpected plan max_iterations: %d", expanded.Plan.MaxIterations)
	}

	rebuilt := BuildAgentProfileView("default", expanded, nil)
	if rebuilt.Meta.Description != "测试方案" {
		t.Fatalf("unexpected description after round trip: %q", rebuilt.Meta.Description)
	}
	if len(rebuilt.RuntimeAgents) != 1 || rebuilt.RuntimeAgents[0].Name != "orchestrator" {
		t.Fatalf("unexpected runtime agents after round trip: %#v", rebuilt.RuntimeAgents)
	}
	if len(rebuilt.Plan.Agents) != 1 || rebuilt.Plan.Agents[0].Name != "planner" {
		t.Fatalf("unexpected plan agents after round trip: %#v", rebuilt.Plan.Agents)
	}
	if !rebuilt.HasPlan {
		t.Fatal("expected plan presence to survive round trip")
	}
	if rebuilt.Plan.Agents[0].Agent.Mode != "readonly_plus" {
		t.Fatalf("unexpected plan tool mode after round trip: %#v", rebuilt.Plan.Agents[0].Agent)
	}
}

func TestExpandAgentProfileViewPreservesMissingPlan(t *testing.T) {
	profile := AgentProfileView{
		Meta:    config.AgentConfigMeta{DefaultExecutionMode: "build"},
		HasPlan: false,
	}

	expanded := ExpandAgentProfileView(profile)
	if expanded == nil {
		t.Fatal("expected expanded config data")
	}
	if expanded.Plan != nil {
		t.Fatalf("expected missing plan to remain absent, got %#v", expanded.Plan)
	}
}
