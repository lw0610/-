package deepagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"
)

func TestNamedMainKeepsAssignedToolsAndPromptWithoutSelfDelegation(t *testing.T) {
	ctx := context.Background()
	fixture := loadGEESkillIntegrationFixture(t)
	root := filepath.Clean(filepath.Join(fixture.skillsRoot, "..", "..", ".."))
	configBase := filepath.Join(root, "carto_agent", "defaults")
	profilePath := filepath.Join(configBase, "agent_configs", "agent_creator", "agent.yaml")
	raw, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	var profile config.AgentConfigFile
	if err := yaml.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}
	skillDirs := config.ResolveRuntimeSkillDirsWithPlugin(fixture.skillsRoot, fixture.skillsRoot, "", configBase, "agent_creator")
	if err := fixture.registry.skills.LoadDirectories(skillDirs...); err != nil {
		t.Fatal(err)
	}
	runtimeConfig := config.ConvertToRuntimeSkillConfigWithPlugin(&profile, fixture.skillsRoot, fixture.skillsRoot, "", configBase, "agent_creator")
	before, _ := json.Marshal(runtimeConfig)
	var tools []tool.BaseTool
	for _, definition := range fixture.registry.skills.Definitions() {
		tools = append(tools, skill.NewSkillAdapter(definition, nil))
	}
	probe := &namedMainCatalogProbe{instruction: profile.Runtime.Agents["agent_creator"].Prompts["static_instructions"]}
	agent, err := New(ctx, &Config{Name: "orchestrator", ChatModel: probe, Tools: tools, MaxIteration: 2,
		SkillRuntimeConfig: runtimeConfig, BuiltinSkillsDir: fixture.skillsRoot, PluginSkillsDir: fixture.skillsRoot,
		RuntimeConfigBaseDir: configBase, ActiveConfigName: "agent_creator", RegistryGovernance: fixture.registry})
	if err != nil {
		t.Fatal(err)
	}
	iter := agent.Run(ctx, &adk.AgentInput{Messages: []adk.Message{schema.UserMessage("列出已有智能体方案")}})
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}
	if !probe.checked {
		t.Fatal("model never saw the normalized main tool/prompt context")
	}
	after, _ := json.Marshal(runtimeConfig)
	if string(before) != string(after) {
		t.Fatal("main-agent normalization mutated caller configuration")
	}
	if _, exists := runtimeConfig.Agents["orchestrator"]; exists {
		t.Fatal("in-memory alias leaked into the caller's profile")
	}
}

func TestNamedMainRejectsAmbiguousMainAgents(t *testing.T) {
	_, err := New(context.Background(), &Config{SkillRuntimeConfig: &subagents.RuntimeSkillConfig{Agents: map[string]subagents.RuntimeSkillAgentConfig{
		"first": {IsMain: true}, "second": {IsMain: true},
	}}})
	if err == nil || !strings.Contains(err.Error(), "multiple main") {
		t.Fatalf("ambiguous main profile must fail deterministically, got %v", err)
	}
}

type namedMainCatalogProbe struct {
	tools       []*schema.ToolInfo
	instruction string
	checked     bool
	parent      *namedMainCatalogProbe
}

func (m *namedMainCatalogProbe) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return &namedMainCatalogProbe{tools: tools, instruction: m.instruction, parent: m}, nil
}
func (m *namedMainCatalogProbe) Generate(_ context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	available := make(map[string]*schema.ToolInfo)
	for _, info := range model.GetCommonOptions(&model.Options{Tools: m.tools}, opts...).Tools {
		available[info.Name] = info
	}
	for _, required := range []string{"list_agent_configs", "create_agent_config", "update_agent_config", "assign_skill_packages", "hitl_request", "skill"} {
		if available[required] == nil {
			return nil, fmt.Errorf("named main lost assigned tool %s", required)
		}
	}
	if available["task"] != nil {
		return nil, fmt.Errorf("named main was also registered as its own task subagent")
	}
	if available["gee_run_script"] != nil {
		return nil, fmt.Errorf("named main received unassigned specialist tool")
	}
	if !strings.Contains(available["skill"].Desc, "runtime-control") {
		return nil, fmt.Errorf("named main lost its assigned tool package instructions")
	}
	found := false
	for _, message := range input {
		if strings.Contains(message.Content, strings.TrimSpace(m.instruction)) {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("named main lost its configured business prompt")
	}
	m.checked = true
	for parent := m.parent; parent != nil; parent = parent.parent {
		parent.checked = true
	}
	return schema.AssistantMessage("catalog verified", nil), nil
}
func (m *namedMainCatalogProbe) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}
