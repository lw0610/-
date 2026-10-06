package deepagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

var thematicRequiredTools = []string{
	"cartography_graduated_style", "cartography_add_labels", "layer_get_style_info",
	"layout_set_map_extent", "layout_set_map_layers",
}

// Real shipped profile, registry, schemas, Eino task delegation and skill read;
// only the QGIS backend is replaced, so no user project or network is touched.
func TestThematicSkillToolsReachDefaultCartographer(t *testing.T) {
	ctx := context.Background()
	fixture := loadGEESkillIntegrationFixture(t)
	runtimeConfig := *fixture.runtimeConfig
	runtimeConfig.Agents = map[string]subagents.RuntimeSkillAgentConfig{
		"orchestrator": fixture.runtimeConfig.Agents["orchestrator"],
		"cartographer": fixture.runtimeConfig.Agents["cartographer"],
	}
	path := filepath.Join(fixture.skillsRoot, "thematic-map-styling", "SKILL.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	probe := &thematicProbe{skillBody: strings.TrimSpace(strings.SplitN(string(body), "---", 3)[2])}
	backend := skill.NewQGISBackend(nil, func(ctx context.Context, _ string, args map[string]any) (any, error) {
		definition := skill.SkillDefinitionFromContext(ctx)
		if definition == nil || definition.Metadata.Name != "cartography_graduated_style" {
			return nil, fmt.Errorf("unexpected QGIS tool invocation")
		}
		if args["field"] != "population" || args["method"] != "jenks" || args["color_ramp"] != "YlOrRd" {
			return nil, fmt.Errorf("real tool schema/mapping lost thematic parameters: %v", args)
		}
		probe.mu.Lock()
		probe.toolInvoked = true
		probe.mu.Unlock()
		return map[string]any{"success": true, "data": map[string]any{"renderer_type": "graduatedSymbol", "renderer_field": "population", "class_count": 5}}, nil
	}, nil)
	tools, err := skill.BuildToolsFromRegistry(fixture.registry.skills, skill.NewEngine(fixture.registry.skills, backend))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := New(ctx, &Config{Name: "orchestrator", ChatModel: &thematicProbeModel{state: probe}, Tools: tools, MaxIteration: 8,
		SkillRuntimeConfig: &runtimeConfig, BuiltinSkillsDir: fixture.skillsRoot, RuntimeConfigBaseDir: t.TempDir(), RegistryGovernance: fixture.registry})
	if err != nil {
		t.Fatal(err)
	}
	iter := agent.Run(ctx, &adk.AgentInput{Messages: []adk.Message{schema.UserMessage("根据已准备好的人口数据制作各市人口分布图")}})
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}
	if !probe.delegated || !probe.skillRead || !probe.toolInvoked || !probe.returned {
		t.Fatalf("incomplete task -> thematic skill -> actual tool adapter chain: %+v", probe)
	}
}

type thematicProbe struct {
	mu                                          sync.Mutex
	skillBody                                   string
	delegated, skillRead, toolInvoked, returned bool
}

type thematicProbeModel struct {
	state *thematicProbe
	tools []*schema.ToolInfo
}

func (m *thematicProbeModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return &thematicProbeModel{state: m.state, tools: tools}, nil
}

func (m *thematicProbeModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (m *thematicProbeModel) Generate(_ context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	available := make(map[string]*schema.ToolInfo)
	for _, info := range model.GetCommonOptions(&model.Options{Tools: m.tools}, opts...).Tools {
		available[info.Name] = info
	}
	if available["task"] != nil {
		for _, name := range thematicRequiredTools {
			if available[name] != nil {
				return nil, fmt.Errorf("specialist tool %s leaked to orchestrator", name)
			}
		}
		if !m.state.delegated {
			m.state.delegated = true
			return geeProbeToolCall("thematic-task", "task", `{"subagent_type":"cartographer","description":"根据已准备好的人口数据制作各市人口分布图"}`), nil
		}
		for _, message := range input {
			if message.Role == schema.Tool && strings.Contains(message.Content, "thematic-tool-chain-verified") {
				m.state.returned = true
				return schema.AssistantMessage("verified", nil), nil
			}
		}
		return nil, fmt.Errorf("cartographer result not returned through task")
	}
	for _, name := range thematicRequiredTools {
		if available[name] == nil {
			return nil, fmt.Errorf("cartographer missing required thematic tool %s", name)
		}
	}
	if available["skill"] == nil || !strings.Contains(available["skill"].Desc, "thematic-map-styling") {
		return nil, fmt.Errorf("cartographer cannot discover its thematic workflow")
	}
	for _, message := range input {
		toolText := message.Content
		for _, part := range message.UserInputMultiContent {
			toolText += part.Text
		}
		if message.Role == schema.Tool && message.ToolCallID == "thematic-skill" {
			if !strings.Contains(toolText, m.state.skillBody) {
				return nil, fmt.Errorf("actual thematic skill body was not returned")
			}
			m.state.skillRead = true
		}
		if message.Role == schema.Tool && message.ToolCallID == "thematic-style" {
			if !strings.Contains(toolText, "graduatedSymbol") || !m.state.toolInvoked {
				return nil, fmt.Errorf("graduated tool was not executed through actual adapter/backend: invoked=%v result=%s", m.state.toolInvoked, toolText)
			}
			return schema.AssistantMessage("thematic-tool-chain-verified", nil), nil
		}
	}
	if !m.state.skillRead {
		return geeProbeToolCall("thematic-skill", "skill", `{"skill":"thematic-map-styling"}`), nil
	}
	return geeProbeToolCall("thematic-style", "cartography_graduated_style", `{"layer":"population_layer","field":"population","classes":5,"method":"jenks","color_ramp":"YlOrRd"}`), nil
}
