package subagents

import (
	"context"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type fakeRuntimeInvokableTool struct {
	name string
}

func (t *fakeRuntimeInvokableTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: "fake"}, nil
}

func (t *fakeRuntimeInvokableTool) InvokableRun(_ context.Context, _ string, _ ...einotool.Option) (string, error) {
	return `{"ok":true}`, nil
}

func TestCreateRuntimeSkillToolFilterUsesCategorySkillsOnly(t *testing.T) {
	runtimeConfig := &RuntimeSkillConfig{
		Global: RuntimeSkillGlobalConfig{
			SkillPackages: []string{"project-state"},
			PackageTools: map[string][]string{
				"project-state": {"get_project_info"},
				"vector-write":  {"buffer"},
				"validation":    {"check_validity"},
			},
		},
		Agents: map[string]RuntimeSkillAgentConfig{
			"analyst": {
				Enabled:       true,
				SkillPackages: []string{"vector-write"},
			},
			"reviewer": {
				Enabled:       true,
				SkillPackages: []string{"validation"},
			},
		},
	}

	baseTools := InvokableToBaseTool([]einotool.InvokableTool{
		&fakeRuntimeInvokableTool{name: "get_project_info"},
		&fakeRuntimeInvokableTool{name: "buffer"},
		&fakeRuntimeInvokableTool{name: "check_validity"},
		&fakeRuntimeInvokableTool{name: "hitl_request"},
	})

	filter := CreateRuntimeSkillToolFilter("analyst", runtimeConfig)
	filtered := filter.Filter(context.Background(), baseTools)
	names := make([]string, 0, len(filtered))
	for _, item := range filtered {
		info, err := item.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info failed: %v", err)
		}
		names = append(names, info.Name)
	}
	joined := strings.Join(names, ",")

	for _, expected := range []string{"get_project_info", "buffer"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected %s in filtered tools: %s", expected, joined)
		}
	}
	if strings.Contains(joined, "check_validity") {
		t.Fatalf("unexpected agent-external skill package tool in filtered tools: %s", joined)
	}
	if strings.Contains(joined, "hitl_request") {
		t.Fatalf("unexpected tool without category skill binding: %s", joined)
	}
}

func TestGetAgentMiddlewareSkillsSkipsShortCategoryPackages(t *testing.T) {
	runtimeConfig := &RuntimeSkillConfig{
		Global: RuntimeSkillGlobalConfig{
			SkillPackages: []string{"project-state", "prompt-refinement"},
			PackageTools: map[string][]string{
				"project-state": {"get_project_info"},
				"vector-write":  {"buffer"},
			},
		},
		Agents: map[string]RuntimeSkillAgentConfig{
			"analyst": {
				Enabled:       true,
				SkillPackages: []string{"vector-write", "data-to-map-full-pipeline"},
			},
		},
	}

	skills := runtimeConfig.GetAgentMiddlewareSkills("analyst")
	joined := strings.Join(skills, ",")

	for _, expected := range []string{"prompt-refinement", "data-to-map-full-pipeline"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected middleware skill %s in %s", expected, joined)
		}
	}
	for _, unexpected := range []string{"project-state", "vector-write"} {
		if strings.Contains(joined, unexpected) {
			t.Fatalf("unexpected category package %s in middleware skills: %s", unexpected, joined)
		}
	}
}
