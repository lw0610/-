package agentruntime

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"

	"github.com/autocarto/carto_agent/defaults"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/plantools"
)

type fakeInvokableTool struct {
	name string
}

type fakePlanToolCallingChatModel struct{}

func TestPlanAgentUnmarshalIgnoresLegacyToolPolicyMode(t *testing.T) {
	var planAgent config.PlanAgentDefData
	raw := []byte(`display_name: Planner
tool_policy:
  mode: readonly_only
  private:
    exclusive: [pyqgis_get_context]
  exclude_tools: [hitl_request]
prompts:
  user_prompt_template: "{{ user_input }}"
`)
	if err := yaml.Unmarshal(raw, &planAgent); err != nil {
		t.Fatalf("unmarshal plan agent failed: %v", err)
	}
	if planAgent.Mode != "" {
		t.Fatalf("expected legacy tool_policy.mode ignored, got: %s", planAgent.Mode)
	}
	if err := validatePlanConfigRuntime(context.Background(), &planAgent, &config.PlanConfig{}); err != nil {
		t.Fatalf("validatePlanConfigRuntime returned error: %v", err)
	}
}

func TestValidatePlanConfigRuntimeRejectsMissingUserInputPlaceholder(t *testing.T) {
	planAgent := &config.PlanAgentDefData{
		Prompts: map[string]string{
			"user_prompt_template": "no placeholder",
		},
	}
	planCfg := &config.PlanConfig{}

	err := validatePlanConfigRuntime(context.Background(), planAgent, planCfg)
	if err == nil {
		t.Fatal("expected error when user_prompt_template misses placeholder")
	}
	if !strings.Contains(err.Error(), "必须包含 {{ user_input }}") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func (t *fakeInvokableTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: "fake"}, nil
}

func (t *fakeInvokableTool) InvokableRun(_ context.Context, _ string, _ ...einotool.Option) (string, error) {
	return `{"ok":true}`, nil
}

func (f *fakePlanToolCallingChatModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return schema.AssistantMessage("ok", nil), nil
}

func (f *fakePlanToolCallingChatModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

func (f *fakePlanToolCallingChatModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}

func mustToolNames(t *testing.T, tools []einotool.BaseTool) []string {
	t.Helper()
	result := make([]string, 0, len(tools))
	for _, item := range tools {
		info, err := item.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info failed: %v", err)
		}
		result = append(result, strings.ToLower(strings.TrimSpace(info.Name)))
	}
	return result
}

func loadEmbeddedDefaultAgentConfig(t *testing.T) *config.AgentConfigFile {
	t.Helper()
	raw, err := defaults.FS.ReadFile("agent_configs/default/agent.yaml")
	if err != nil {
		t.Fatalf("read embedded default agent config failed: %v", err)
	}
	var cfg config.AgentConfigFile
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal embedded default agent config failed: %v", err)
	}
	return &cfg
}

func TestSelectPlanToolsReadonlyPlus(t *testing.T) {
	invokables := []einotool.InvokableTool{
		&fakeInvokableTool{name: "get_layers_info"},
		&fakeInvokableTool{name: "check_validity"},
		&fakeInvokableTool{name: "hitl_request"},
		&fakeInvokableTool{name: "buffer"},
		&fakeInvokableTool{name: "layout_info"},
	}
	baseTools := subagents.InvokableToBaseTool(invokables)

	planAgent := &config.PlanAgentDefData{
		Mode:          config.PlanToolStrategyReadonlyPlus,
		SkillPackages: []string{"validation", "vector-write", "layout-read"},
	}
	planCfg := &config.PlanConfig{
		Global: config.GlobalAgentConfig{
			SkillPackages: []string{"project-state"},
		},
	}

	packageTools := map[string][]string{
		"project-state": {"get_layers_info"},
		"validation":    {"check_validity"},
		"vector-write":  {"buffer"},
		"layout-read":   {"layout_info"},
	}

	selected := selectPlanTools(context.Background(), baseTools, planCfg, planAgent, packageTools)
	names := strings.Join(mustToolNames(t, selected), ",")

	if !strings.Contains(names, "get_layers_info") {
		t.Fatalf("expected get_layers_info in selected tools: %s", names)
	}
	if !strings.Contains(names, "check_validity") {
		t.Fatalf("expected check_validity in selected tools: %s", names)
	}
	if !strings.Contains(names, "layout_info") {
		t.Fatalf("expected layout_info in selected tools: %s", names)
	}
	if strings.Contains(names, "hitl_request") {
		t.Fatalf("unexpected tool outside configured category skills: %s", names)
	}
	if strings.Contains(names, "buffer") {
		t.Fatalf("unexpected write tool buffer in selected tools: %s", names)
	}
}

func TestSelectPlanToolsFromDefaultProfile(t *testing.T) {
	defaultCfg := loadEmbeddedDefaultAgentConfig(t)
	if defaultCfg.Plan == nil {
		t.Fatal("expected embedded default plan config")
	}
	planner, ok := defaultCfg.Plan.Agents["planner"]
	if !ok {
		t.Fatal("expected planner in embedded default config")
	}

	invokables := []einotool.InvokableTool{
		&fakeInvokableTool{name: "hitl_request"},
		&fakeInvokableTool{name: "query_build_capabilities"},
		&fakeInvokableTool{name: "buffer"},
		&fakeInvokableTool{name: "pyqgis_execute_script"},
	}
	baseTools := subagents.InvokableToBaseTool(invokables)
	packageTools := map[string][]string{
		"runtime-control":            {"hitl_request"},
		"build-capability-awareness": {"query_build_capabilities"},
		"pyqgis-scripting":           {"pyqgis_execute_script"},
	}

	selected := selectPlanTools(context.Background(), baseTools, defaultCfg.Plan, &planner, packageTools)
	names := strings.Join(mustToolNames(t, selected), ",")

	if !strings.Contains(names, "hitl_request") {
		t.Fatalf("expected hitl_request in selected tools: %s", names)
	}
	if !strings.Contains(names, "query_build_capabilities") {
		t.Fatalf("expected query_build_capabilities in selected tools: %s", names)
	}
	if strings.Contains(names, "buffer") {
		t.Fatalf("unexpected write tool buffer in selected tools: %s", names)
	}
	if strings.Contains(names, "pyqgis_execute_script") {
		t.Fatalf("unexpected scripting tool in selected tools: %s", names)
	}
}

func TestResolvePlanCategorySkillToolsMatchesRuntimeSkillProjection(t *testing.T) {
	planAgent := &config.PlanAgentDefData{
		SkillPackages: []string{"validation", "shared"},
	}
	planCfg := &config.PlanConfig{
		Global: config.GlobalAgentConfig{SkillPackages: []string{"project-state", "shared"}},
	}
	packageTools := map[string][]string{
		"project-state": {"get_layers_info"},
		"validation":    {"check_validity"},
		"shared":        {"layout_info", "check_validity"},
	}

	resolved := resolvePlanCategorySkillTools(planCfg, planAgent, packageTools)
	runtimeConfig := buildPlanRuntimeSkillConfig(planCfg, planAgent, packageTools)
	if runtimeConfig == nil {
		t.Fatal("expected runtime skill projection for planner")
	}
	expected := strings.Join(runtimeConfig.GetAgentAllowedTools(planRuntimeAgentName), ",")
	if got := strings.Join(resolved, ","); got != expected {
		t.Fatalf("expected planner tools to follow runtime projection, got=%s want=%s", got, expected)
	}
}

func TestBuildPlanRuntimeSkillConfigExposesMiddlewareSkillsSeparately(t *testing.T) {
	planAgent := &config.PlanAgentDefData{SkillPackages: []string{"validation", "planning-knowledge"}}
	planCfg := &config.PlanConfig{Global: config.GlobalAgentConfig{SkillPackages: []string{"project-state"}}}
	packageTools := map[string][]string{
		"project-state": {"get_layers_info"},
		"validation":    {"check_validity"},
	}

	runtimeConfig := buildPlanRuntimeSkillConfig(planCfg, planAgent, packageTools)
	if runtimeConfig == nil {
		t.Fatal("expected runtime config")
	}
	if got := strings.Join(runtimeConfig.GetAgentAllowedTools(planRuntimeAgentName), ","); got != "get_layers_info,check_validity" {
		t.Fatalf("unexpected planner allowed tools: %s", got)
	}
	if got := strings.Join(runtimeConfig.GetAgentMiddlewareSkills(planRuntimeAgentName), ","); got != "planning-knowledge" {
		t.Fatalf("expected non-category planner skill to remain middleware-visible, got %s", got)
	}
}

func TestBuildPlanComponentsRejectsEmptySelectedTools(t *testing.T) {
	factory, err := NewFactory(&FactoryConfig{
		DefaultChatModel: &fakePlanToolCallingChatModel{},
		BaseTools: subagents.InvokableToBaseTool([]einotool.InvokableTool{
			&fakeInvokableTool{name: plantools.ToolQueryBuildCapabilities},
		}),
		PluginSkillsDir:  t.TempDir(),
		BuiltinSkillsDir: t.TempDir(),
		UserSkillsDir:    t.TempDir(),
		ConfigBaseDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewFactory error: %v", err)
	}

	_, err = factory.buildPlanComponents(context.Background(), "default", &config.AgentConfigFile{
		Plan: &config.PlanConfig{
			Agents: map[string]config.PlanAgentDefData{
				"planner": {
					Prompts: map[string]string{
						"user_prompt_template": "{{ user_input }}",
					},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected clear error when plan selected tools are empty")
	}
	if !strings.Contains(err.Error(), "plan mode 未选择到任何可用工具") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAppendToolIfMissingReplacesDuplicateName(t *testing.T) {
	ctx := context.Background()
	existing := subagents.InvokableToBaseTool([]einotool.InvokableTool{
		&fakeInvokableTool{name: plantools.ToolQueryBuildCapabilities},
	})
	replacement := &fakeInvokableTool{name: plantools.ToolQueryBuildCapabilities}

	result := appendToolIfMissing(ctx, existing, replacement)
	if len(result) != 1 {
		t.Fatalf("expected duplicate tool to be replaced in place, got %d tools", len(result))
	}

	names := mustToolNames(t, result)
	if len(names) != 1 || names[0] != plantools.ToolQueryBuildCapabilities {
		t.Fatalf("unexpected tool names after replacement: %v", names)
	}
	if result[0] != replacement {
		t.Fatalf("expected canonical replacement tool to win duplicate name")
	}
}

func TestAppendToolIfMissingAddsNewTool(t *testing.T) {
	ctx := context.Background()
	existing := subagents.InvokableToBaseTool([]einotool.InvokableTool{
		&fakeInvokableTool{name: "hitl_request"},
	})

	result := appendToolIfMissing(ctx, existing, &fakeInvokableTool{name: plantools.ToolQueryBuildCapabilities})
	if len(result) != 2 {
		t.Fatalf("expected new tool to be appended, got %d tools", len(result))
	}

	names := strings.Join(mustToolNames(t, result), ",")
	if !strings.Contains(names, "hitl_request") || !strings.Contains(names, plantools.ToolQueryBuildCapabilities) {
		t.Fatalf("expected both tools after append, got %s", names)
	}
}

func TestDedupeToolsByNameRemovesLaterDuplicates(t *testing.T) {
	ctx := context.Background()
	tools := subagents.InvokableToBaseTool([]einotool.InvokableTool{
		&fakeInvokableTool{name: "hitl_request"},
		&fakeInvokableTool{name: plantools.ToolQueryBuildCapabilities},
		&fakeInvokableTool{name: "hitl_request"},
	})

	result := dedupeToolsByName(ctx, tools)
	names := mustToolNames(t, result)
	if len(names) != 2 {
		t.Fatalf("expected duplicates removed, got %v", names)
	}
	if names[0] != "hitl_request" || names[1] != plantools.ToolQueryBuildCapabilities {
		t.Fatalf("unexpected tool order after dedupe: %v", names)
	}
}
