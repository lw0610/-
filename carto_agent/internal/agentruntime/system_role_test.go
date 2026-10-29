package agentruntime

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/deepagent"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type systemRoleProbe struct {
	tools  map[string]bool
	target string
	args   string
	result string
	calls  int
}

func (p *systemRoleProbe) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	p.capture(infos)
	return p, nil
}
func (p *systemRoleProbe) capture(infos []*schema.ToolInfo) {
	for _, info := range infos {
		p.tools[info.Name] = true
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
	for _, name := range []string{"data_find", "data_get", "data_register", "web_search", "reference_read", "get_layers_info", "unassigned"} {
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

func TestFactoryBuildRoleConstructsRealSystemDataExpert(t *testing.T) {
	probe := &systemRoleProbe{tools: map[string]bool{}, target: "skill", args: `{"skill":"browser-data-acquisition"}`}
	factory, profile := systemRoleFactory(t, probe)
	before, _ := json.Marshal(profile)
	if _, exists := profile.Runtime.Agents[deepagent.DataAcquisitionExpertName]; exists {
		t.Fatal("test must start without a user-owned system expert role")
	}
	agent, err := factory.BuildRole(context.Background(), "default", profile, deepagent.DataAcquisitionExpertName)
	if err != nil || agent.Name(context.Background()) != deepagent.DataAcquisitionExpertName {
		t.Fatalf("system expert was not built through the production factory: %v", err)
	}
	if err := runSystemRoleProbe(agent); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"data_find", "data_get", "data_register", "web_search", "reference_read", "skill"} {
		if !probe.tools[required] {
			t.Fatalf("system expert lost authorized tool %s: %+v", required, probe.tools)
		}
	}
	if probe.tools["unassigned"] || !strings.Contains(probe.result, "中国统计数据") {
		t.Fatal("system expert permissions or real manual loading changed", probe.tools)
	}
	after, _ := json.Marshal(profile)
	if string(before) != string(after) {
		t.Fatal("system expert creation mutated the user's profile")
	}
	if _, err := factory.BuildRole(context.Background(), "default", profile, "system_unknown"); err == nil {
		t.Fatal("unknown system name accepted")
	}
}

func TestFactorySystemRoleDoesNotGrantAcquisitionToolsToOrdinaryMain(t *testing.T) {
	probe := &systemRoleProbe{tools: map[string]bool{}}
	factory, profile := systemRoleFactory(t, probe)
	agent, err := factory.BuildRole(context.Background(), "default", profile, "orchestrator")
	if err != nil {
		t.Fatal(err)
	}
	if err := runSystemRoleProbe(agent); err != nil {
		t.Fatal(err)
	}
	if !probe.tools["data_find"] {
		t.Fatal("ordinary main lost discovery tool")
	}
	for _, forbidden := range []string{"data_get", "data_register", "web_search", "unassigned"} {
		if probe.tools[forbidden] {
			t.Fatalf("ordinary main received system capability %s", forbidden)
		}
	}
}

func TestFactorySystemRoleRetainsFallbackAuthorization(t *testing.T) {
	probe := &systemRoleProbe{tools: map[string]bool{}, target: "web_search", args: `{"query":"fixture"}`}
	factory, profile := systemRoleFactory(t, probe)
	agent, err := factory.BuildRole(context.Background(), "default", profile, deepagent.DataAcquisitionExpertName)
	if err != nil {
		t.Fatal(err)
	}
	if err := runSystemRoleProbe(agent); err == nil || !strings.Contains(err.Error(), "数据服务") {
		t.Fatalf("planned system role bypassed ordinary acquisition guard: %v", err)
	}
}
