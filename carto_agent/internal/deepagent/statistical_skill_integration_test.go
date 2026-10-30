package deepagent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// Load the shipped profile and on-disk category packages: declaring a tool in
// workflow prose does not grant permission, and category references can go stale.
func TestStatisticalToolPackagesHaveRealRoleTools(t *testing.T) {
	f := loadGEESkillIntegrationFixture(t)
	for role, workflow := range map[string]string{
		"orchestrator":  "flow-control",
		"data_engineer": "statistical-data-tools",
		"cartographer":  "print-layout",
	} {
		pkg, ok := f.registry.skills.GetPackage(workflow)
		if !ok || pkg.Kind != "category" || !pkg.Enabled {
			t.Fatalf("missing enabled workflow %s", workflow)
		}
		if !slices.Contains(f.runtimeConfig.GetAgentMiddlewareSkills(role), workflow) {
			t.Errorf("%s cannot read %s", role, workflow)
		}
		for _, name := range pkg.ToolRefs {
			if _, ok := f.registry.skills.GetTool(name); !ok {
				t.Errorf("%s requires missing actual tool %s", workflow, name)
			}
			if !slices.Contains(f.runtimeConfig.GetAgentAllowedTools(role), name) {
				t.Errorf("%s lacks category permission for %s required by %s", role, name, workflow)
			}
		}
	}
	for role := range f.runtimeConfig.Agents {
		allowed := slices.Contains(f.runtimeConfig.GetAgentAllowedTools(role), "data_prepare_statistical")
		if allowed != (role == "data_engineer") {
			t.Errorf("statistical preparation permission for %s = %v", role, allowed)
		}
		if readable := slices.Contains(f.runtimeConfig.GetAgentMiddlewareSkills(role), "statistical-data-tools"); readable != allowed {
			t.Errorf("statistical method ownership for %s: readable=%v allowed=%v", role, readable, allowed)
		}
	}
	for _, role := range []string{"data_engineer", "spatial_analyst", "cartographer"} {
		for _, name := range []string{"data_load_vector", "data_load_raster", "data_load_list"} {
			allowed := slices.Contains(f.runtimeConfig.GetAgentAllowedTools(role), name)
			if allowed != (role == "data_engineer") {
				t.Errorf("layer loading permission for %s with %s = %v", role, name, allowed)
			}
		}
	}
}

type statisticalProbeStep struct {
	tool, args, resultMarker string
	result                   map[string]any
}

// Real Eino delegation, middleware skill read, schemas, engine and parameter
// mapping run offline. Only QGIS execution is stubbed; Python/QGIS tests cover
// joins and renderer behavior separately without making this test open a project.
func TestStatisticalTaskSkillAndToolAdapters(t *testing.T) {
	f := loadGEESkillIntegrationFixture(t)
	preparedPath := filepath.Join(t.TempDir(), "polygons.geojson")
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	encode := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	prepareArgs := func(validate bool) string {
		return encode(map[string]any{"geojson_path": "/input/regions.geojson", "csv_path": "/input/annual.csv", "year": 2024, "validate_only": validate})
	}
	prepared := map[string]any{"success": true, "output": manifestPath, "data": map[string]any{
		"paths":           map[string]any{"polygons_geojson": preparedPath, "manifest": manifestPath},
		"metadata":        map[string]any{"indicator": "常住人口", "unit": "万人", "year": 2024},
		"missing_regions": []string{"缺测区"},
	}}
	cases := []struct {
		role, workflow string
		steps          []statisticalProbeStep
	}{
		{"data_engineer", "statistical-data-tools", []statisticalProbeStep{
			{"data_prepare_statistical", prepareArgs(true), "validation-only", map[string]any{"success": true, "data": map[string]any{"validation": "validation-only"}}},
			{"data_prepare_statistical", prepareArgs(false), preparedPath, prepared},
			{"data_load_vector", encode(map[string]any{"path": preparedPath, "layer_name": "statistical_theme"}), "theme-id", map[string]any{"success": true, "data": map[string]any{"layer_id": "theme-id"}}},
			{"data_load_vector", encode(map[string]any{"path": preparedPath, "layer_name": "statistical_base"}), "base-id", map[string]any{"success": true, "data": map[string]any{"layer_id": "base-id"}}},
		}},
		{"cartographer", "print-layout", []statisticalProbeStep{
			{"cartography_set_style", `{"layer":"statistical_base","style_type":"simple","color":"#e5e7eb"}`, "gray-base", map[string]any{"success": true, "data": "gray-base"}},
			{"cartography_graduated_style", `{"layer":"statistical_theme","field":"stat_value","classes":5,"method":"jenks","color_ramp":"Blues"}`, "graduatedSymbol", map[string]any{"success": true, "data": map[string]any{"renderer_type": "graduatedSymbol", "renderer_field": "stat_value"}}},
			{"layout_set_map_layers", `{"layout":"statistics","item_id":"map","layers":["statistical_theme","statistical_base"]}`, "locked-layers", map[string]any{"success": true, "data": "locked-layers"}},
			{"layout_set_map_extent", `{"layout":"statistics","item_id":"map","layer":"statistical_theme","padding":0.03,"preserve_frame_size":true}`, "extent-transformed", map[string]any{"success": true, "data": "extent-transformed"}},
			{"layout_set_legend_style", `{"layout":"statistics","item_id":"legend","legend_layers":["statistical_theme"],"legend_extra_items":[],"title":"常住人口（万人）"}`, "native-legend", map[string]any{"success": true, "data": "native-legend"}},
			{"export_print_layout", `{"layout":"statistics","output":"/output/statistics.png","dpi":200}`, "/output/statistics.png", map[string]any{"success": true, "output": "/output/statistics.png"}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			ctx := context.Background()
			runtimeConfig := *f.runtimeConfig
			runtimeConfig.Agents = map[string]subagents.RuntimeSkillAgentConfig{
				"orchestrator": f.runtimeConfig.Agents["orchestrator"], tc.role: f.runtimeConfig.Agents[tc.role],
			}
			pkg, _ := f.registry.skills.GetPackage(tc.workflow)
			probe := &statisticalProbe{role: tc.role, workflow: tc.workflow, body: strings.TrimSpace(pkg.MarkdownBody), steps: tc.steps}
			backend := skill.NewQGISBackend(nil, func(ctx context.Context, _ string, args map[string]any) (any, error) {
				probe.mu.Lock()
				defer probe.mu.Unlock()
				if probe.executed >= len(probe.steps) {
					return nil, fmt.Errorf("unexpected extra tool execution")
				}
				expected := probe.steps[probe.executed]
				definition := skill.SkillDefinitionFromContext(ctx)
				if definition == nil || definition.Metadata.Name != expected.tool {
					return nil, fmt.Errorf("expected real definition for %s, got %v", expected.tool, definition)
				}
				var submitted map[string]any
				if err := json.Unmarshal([]byte(expected.args), &submitted); err != nil {
					return nil, err
				}
				for key, want := range submitted {
					if encode(args[key]) != encode(want) {
						return nil, fmt.Errorf("%s mapping lost %s: got %v want %v", expected.tool, key, args[key], want)
					}
				}
				if expected.tool == "data_prepare_statistical" && args["allow_unmatched"] != false {
					return nil, fmt.Errorf("strict unmatched default lost: %v", args)
				}
				probe.executed++
				return expected.result, nil
			}, nil)
			engine := skill.NewEngine(f.registry.skills, backend)
			engine.SetResourceResolver(resource.NewLibrary("", "", bindings.New(filepath.Join(t.TempDir(), "bindings.yaml"))))
			tools, err := skill.BuildToolsFromRegistry(f.registry.skills, engine)
			if err != nil {
				t.Fatal(err)
			}
			agent, err := New(ctx, &Config{Name: "orchestrator", ChatModel: &statisticalProbeModel{state: probe}, Tools: tools, MaxIteration: 16,
				SkillRuntimeConfig: &runtimeConfig, BuiltinSkillsDir: f.skillsRoot, RuntimeConfigBaseDir: t.TempDir(), RegistryGovernance: f.registry})
			if err != nil {
				t.Fatal(err)
			}
			iter := agent.Run(ctx, &adk.AgentInput{Messages: []adk.Message{schema.UserMessage("根据年度统计表和行政区制作人口统计专题图，缺测区域不能当成零")}})
			for event, ok := iter.Next(); ok; event, ok = iter.Next() {
				if event.Err != nil {
					t.Fatal(event.Err)
				}
			}
			if !probe.delegated || !probe.read || !probe.returned || probe.executed != len(tc.steps) {
				t.Fatalf("incomplete task -> skill -> actual adapters -> task result: %+v", probe)
			}
		})
	}
}

type statisticalProbe struct {
	mu                        sync.Mutex
	role, workflow, body      string
	steps                     []statisticalProbeStep
	executed, acknowledged    int
	delegated, read, returned bool
}

type statisticalProbeModel struct {
	state *statisticalProbe
	tools []*schema.ToolInfo
}

func (m *statisticalProbeModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return &statisticalProbeModel{state: m.state, tools: tools}, nil
}

func (m *statisticalProbeModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (m *statisticalProbeModel) Generate(_ context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	s := m.state
	s.mu.Lock()
	defer s.mu.Unlock()
	available := map[string]*schema.ToolInfo{}
	for _, info := range model.GetCommonOptions(&model.Options{Tools: m.tools}, opts...).Tools {
		available[info.Name] = info
	}
	if available["task"] != nil {
		if available["data_prepare_statistical"] != nil || available["cartography_graduated_style"] != nil {
			return nil, fmt.Errorf("statistical specialist tool leaked to orchestrator")
		}
		if available["skill"] == nil || !strings.Contains(available["skill"].Desc, "flow-control") {
			return nil, fmt.Errorf("orchestrator cannot discover flow tools")
		}
		if !s.delegated {
			s.delegated = true
			return geeProbeToolCall("statistics-task", "task", fmt.Sprintf(`{"subagent_type":%q,"description":"按统计技能执行任务并返回真实产物"}`, s.role)), nil
		}
		for _, message := range input {
			if message.Role == schema.Tool && strings.Contains(message.Content, "statistical-chain-verified") {
				s.returned = true
				return schema.AssistantMessage("verified", nil), nil
			}
		}
		return nil, fmt.Errorf("specialist result did not return through task")
	}
	if s.role != "data_engineer" && available["data_prepare_statistical"] != nil {
		return nil, fmt.Errorf("data preparation tools leaked to %s", s.role)
	}
	for _, step := range s.steps {
		if available[step.tool] == nil {
			return nil, fmt.Errorf("%s lacks actual tool %s", s.role, step.tool)
		}
	}
	if available["skill"] == nil || !strings.Contains(available["skill"].Desc, s.workflow) {
		return nil, fmt.Errorf("%s cannot discover %s", s.role, s.workflow)
	}
	for _, message := range input {
		if message.Role != schema.Tool {
			continue
		}
		text := message.Content
		for _, part := range message.UserInputMultiContent {
			text += part.Text
		}
		if message.ToolCallID == "statistics-skill" {
			if s.body == "" || !strings.Contains(text, s.body) {
				return nil, fmt.Errorf("actual %s body was not returned", s.workflow)
			}
			s.read = true
		}
		if s.acknowledged < len(s.steps) && message.ToolCallID == fmt.Sprintf("statistics-tool-%d", s.acknowledged) {
			if s.executed <= s.acknowledged || !strings.Contains(text, s.steps[s.acknowledged].resultMarker) {
				return nil, fmt.Errorf("tool did not run through actual adapter: %s", text)
			}
			s.acknowledged++
		}
	}
	if !s.read {
		return geeProbeToolCall("statistics-skill", "skill", fmt.Sprintf(`{"skill":%q}`, s.workflow)), nil
	}
	if s.acknowledged == len(s.steps) {
		return schema.AssistantMessage("statistical-chain-verified", nil), nil
	}
	step := s.steps[s.acknowledged]
	return geeProbeToolCall(fmt.Sprintf("statistics-tool-%d", s.acknowledged), step.tool, step.args), nil
}
