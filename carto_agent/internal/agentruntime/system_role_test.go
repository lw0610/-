package agentruntime

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type systemRoleProbe struct {
	tools           map[string]bool
	taskDescription string
	target          string
	args            string
	result          string
	calls           int
}

func (p *systemRoleProbe) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	p.capture(infos)
	return p, nil
}
func (p *systemRoleProbe) capture(infos []*schema.ToolInfo) {
	for _, info := range infos {
		p.tools[info.Name] = true
		if info.Name == "task" {
			p.taskDescription = info.Desc
			if info.ParamsOneOf != nil {
				schema, _ := info.ParamsOneOf.ToJSONSchema()
				raw, _ := json.Marshal(schema)
				p.taskDescription += string(raw)
			}
		}
	}
}
func (p *systemRoleProbe) Generate(_ context.Context, messages []*schema.Message, options ...model.Option) (*schema.Message, error) {
	p.capture(model.GetCommonOptions(nil, options...).Tools)
	p.calls++
	if p.target != "" && p.calls == 1 {
		return schema.AssistantMessage("", []schema.ToolCall{{ID: "role-probe", Type: "function", Function: schema.FunctionCall{Name: p.target, Arguments: p.args}}}), nil
	}
	for _, message := range messages {
		if message.Role == schema.Tool && message.ToolCallID == "role-probe" {
			p.result = message.Content
		}
	}
	return schema.AssistantMessage("complete", nil), nil
}
func (p *systemRoleProbe) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	result, err := p.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{result}), nil
}

func systemRoleFactory(t *testing.T, probe *systemRoleProbe) (*Factory, *config.AgentConfigFile) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	assets, home := filepath.Join(root, "autocarto"), t.TempDir()
	skills := filepath.Join(assets, "skills", "system")
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", assets)
	t.Setenv("AUTOCARTO_HOME", home)
	tools := []tool.BaseTool{}
	for _, name := range []string{"data_find", "data_get", "data_register", "web_search", "reference_read", "get_layers_info", "plan_propose", "plan_revise", "marching_route_get_extraction_contract", "marching_route_compile_dataset", "unassigned"} {
		tools = append(tools, &fakeInvokableTool{name: name})
	}
	factory, err := NewFactory(&FactoryConfig{
		DefaultChatModel: probe, BaseTools: tools, MaxIterations: 3,
		BuiltinSkillsDir: skills, PluginSkillsDir: skills, UserSkillsDir: filepath.Join(home, "skills"), ConfigBaseDir: filepath.Join(home, "config"),
		RegistryGovernance: control.NewRegistryGovernance(control.RegistryGovernanceDeps{ResolveSkillDirs: func() []string { return []string{skills} }}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return factory, loadEmbeddedDefaultAgentConfig(t)
}

func runSystemRoleProbe(agent adk.Agent) error {
	ctx := context.Background()
	iter := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent}).Run(ctx, []*schema.Message{schema.UserMessage("检查当前步骤角色")})
	for {
		event, ok := iter.Next()
		if !ok {
			return nil
		}
		if event.Err != nil {
			return event.Err
		}
	}
}

func TestFactoryBuildRoleUsesConfiguredDataEngineer(t *testing.T) {
	probe := &systemRoleProbe{tools: map[string]bool{}, target: "skill", args: `{"skill":"browser-data-acquisition"}`}
	factory, profile := systemRoleFactory(t, probe)
	before, _ := json.Marshal(profile)
	if _, exists := profile.Runtime.Agents["system_data_acquisition"]; exists {
		t.Fatal("profile contains obsolete hidden acquisition role")
	}
	agent, err := factory.BuildRole(context.Background(), "default", profile, "data_engineer")
	if err != nil || agent.Name(context.Background()) != "data_engineer" {
		t.Fatalf("configured data engineer was not built through the production factory: %v", err)
	}
	if err := runSystemRoleProbe(agent); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"data_find", "data_get", "data_register", "web_search", "reference_read", "skill"} {
		if !probe.tools[required] {
			t.Fatalf("configured role lost authorized tool %s: %+v", required, probe.tools)
		}
	}
	if probe.tools["unassigned"] || !strings.Contains(probe.result, "中国统计数据") {
		t.Fatal("configured role permissions or real manual loading changed", probe.tools)
	}
	after, _ := json.Marshal(profile)
	if string(before) != string(after) {
		t.Fatal("role creation mutated the user's profile")
	}
	if _, err := factory.BuildRole(context.Background(), "default", profile, "system_data_acquisition"); err == nil {
		t.Fatal("implicit acquisition role accepted")
	}
}

func TestFactoryMainKeepsOnlyItsAssignedPackages(t *testing.T) {
	probe := &systemRoleProbe{tools: map[string]bool{}}
	factory, profile := systemRoleFactory(t, probe)
	agent, err := factory.BuildRole(context.Background(), "default", profile, "orchestrator")
	if err != nil {
		t.Fatal(err)
	}
	if err := runSystemRoleProbe(agent); err != nil {
		t.Fatal(err)
	}
	for _, assigned := range []string{"data_find", "data_get", "data_register"} {
		if !probe.tools[assigned] {
			t.Fatalf("main lost assigned data-service tool %s", assigned)
		}
	}
	for _, forbidden := range []string{"web_search", "unassigned"} {
		if probe.tools[forbidden] {
			t.Fatalf("ordinary main received system capability %s", forbidden)
		}
	}
}

func TestFactoryConfiguredRoleExecutesAssignedAcquisitionTool(t *testing.T) {
	probe := &systemRoleProbe{tools: map[string]bool{}, target: "web_search", args: `{"query":"fixture"}`}
	factory, profile := systemRoleFactory(t, probe)
	agent, err := factory.BuildRole(context.Background(), "default", profile, "data_engineer")
	if err != nil {
		t.Fatal(err)
	}
	if err := runSystemRoleProbe(agent); err != nil {
		t.Fatalf("configured acquisition tool was blocked: %v", err)
	}
	if probe.calls != 2 || probe.result == "" {
		t.Fatal("assigned web_search was not executed", probe.calls, probe.result)
	}
}

func TestFactoryBuildMainDelegatesWithDataServiceWithoutPlanTools(t *testing.T) {
	probe := &systemRoleProbe{tools: map[string]bool{}}
	factory, profile := systemRoleFactory(t, probe)
	agent, err := factory.buildDeepAgent(context.Background(), "default", profile)
	if err != nil {
		t.Fatal(err)
	}
	probe.tools = map[string]bool{}
	if err := runSystemRoleProbe(agent); err != nil {
		t.Fatal(err)
	}
	if probe.taskDescription == "" {
		t.Fatal("ordinary task delegation disappeared")
	}
	for _, name := range []string{"cartographer", "knowledge_engineer", "data_engineer"} {
		if !strings.Contains(probe.taskDescription, name) {
			t.Fatalf("Build task lost delegate %s: %s", name, probe.taskDescription)
		}
	}
	if strings.Contains(probe.taskDescription, "system_data_acquisition") {
		t.Fatal("hidden role remained in task delegation")
	}
	for _, name := range []string{"data_find", "data_get", "data_register"} {
		if !probe.tools[name] {
			t.Fatalf("Build main lost %s", name)
		}
	}
	for _, name := range []string{"plan_propose", "plan_revise", "web_search"} {
		if probe.tools[name] {
			t.Fatalf("Build main received %s instead of its task handoff", name)
		}
	}
}

func TestFactoryPlanMainKeepsPlanToolsWithoutExecution(t *testing.T) {
	probe := &systemRoleProbe{tools: map[string]bool{}}
	factory, profile := systemRoleFactory(t, probe)
	agent, err := factory.buildPlanComponents(context.Background(), "default", profile)
	if err != nil {
		t.Fatal(err)
	}
	probe.tools = map[string]bool{}
	if err := runSystemRoleProbe(agent); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"plan_propose", "plan_revise"} {
		if !probe.tools[name] {
			t.Fatalf("Plan lost %s", name)
		}
	}
	for _, name := range []string{"data_get", "data_register", "web_search"} {
		if probe.tools[name] {
			t.Fatalf("Plan received execution tool %s", name)
		}
	}
	if strings.Contains(probe.taskDescription, "cartographer") || strings.Contains(probe.taskDescription, "data_engineer") {
		t.Fatal("Plan exposes execution delegates", probe.taskDescription)
	}
}
