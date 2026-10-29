package agentruntime

import (
	"context"
	"github.com/cloudwego/eino/adk"
	"os"
	"path/filepath"
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

func TestSelectPlanToolsFromDefaultProfile(t *testing.T) {
	defaultCfg := loadEmbeddedDefaultAgentConfig(t)
	if defaultCfg.Plan != nil || defaultCfg.Runtime == nil {
		t.Fatal("Plan must use the common runtime profile")
	}
	names := []string{"hitl_request", "query_build_capabilities", "data_find", "plan_propose", "buffer", "osm_get_features", "web_search", "data_get", "pyqgis_execute_script"}
	tools := []einotool.BaseTool{}
	for _, name := range names {
		tools = append(tools, &fakeInvokableTool{name: name})
	}
	selected := selectCartographicPlanTools(context.Background(), tools)
	actual := strings.Join(mustToolNames(t, selected), ",")
	for _, required := range []string{"hitl_request", "query_build_capabilities", "plan_propose"} {
		if !strings.Contains(actual, required) {
			t.Fatal("missing readonly capability", required)
		}
	}
	if len(selected) != 3 {
		t.Fatalf("execution or network tools leaked: %s", actual)
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
	if !strings.Contains(err.Error(), "runtime config projection failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPlanFactoryKeepsMainPromptWithoutRestoringExecutionToolsOrDelegates(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "all-capabilities")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	names := []string{"data_find", "data_get", "data_register", "plan_propose", "buffer", "osm_get_features", "web_search", "browser_open", "pyqgis_execute_script"}
	body := "---\nname: all-capabilities\ndescription: test\nkind: category\ntools: [" + strings.Join(names, ",") + "]\n---\nfixture"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	probe := newFlowProbe()
	base := []einotool.BaseTool{}
	for _, name := range names {
		base = append(base, &fakeInvokableTool{name: name})
	}
	factory, err := NewFactory(&FactoryConfig{DefaultChatModel: probe, BaseTools: base, PluginSkillsDir: root, BuiltinSkillsDir: t.TempDir(), UserSkillsDir: t.TempDir(), ConfigBaseDir: t.TempDir(), MaxIterations: 2})
	if err != nil {
		t.Fatal(err)
	}
	profile := &config.AgentConfigFile{Runtime: &config.RuntimeConfig{Agents: map[string]config.AgentDefData{"main": {Kind: "agent", Enabled: true, IsMain: true, ToolPackages: []string{"all-capabilities"}, Prompts: map[string]string{"static_instructions": "shared-main-marker"}}, "worker": {Kind: "agent", Enabled: true, ToolPackages: []string{"all-capabilities"}}}}}
	agent, err := factory.buildPlanComponents(context.Background(), "fixture", profile)
	if err != nil {
		t.Fatal(err)
	}
	iterator := agent.Run(context.Background(), &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage("制定计划")}})
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}
	bound := map[string]bool{}
	for _, infos := range probe.capture.bindings {
		for _, info := range infos {
			bound[info.Name] = true
		}
	}
	if probe.capture.options != nil {
		for _, info := range probe.capture.options.Tools {
			bound[info.Name] = true
		}
	}
	for _, forbidden := range []string{"data_get", "data_register", "buffer", "osm_get_features", "web_search", "browser_open", "pyqgis_execute_script", "task"} {
		if bound[forbidden] {
			t.Fatalf("Plan regained execution: %s %+v", forbidden, bound)
		}
	}
	if bound["data_find"] || !bound["plan_propose"] {
		t.Fatal("missing plan tools", bound)
	}
	found := false
	for _, message := range probe.capture.messages {
		found = found || strings.Contains(message.Content, "shared-main-marker")
	}
	if !found {
		t.Fatal("Plan lost common main prompt")
	}
}
