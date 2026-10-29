package deepagent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/defaults"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/runtimecontrol"
	"github.com/autocarto/carto_agent/internal/testutil"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

type acquisitionSequenceModel struct{ tools []*schema.ToolInfo }

func (m *acquisitionSequenceModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return &acquisitionSequenceModel{tools: tools}, nil
}
func (m *acquisitionSequenceModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	seenFind, seenGet := false, false
	for _, message := range messages {
		if message.Role == schema.Tool {
			if message.ToolName == "data_find" {
				seenFind = true
			}
			if message.ToolName == "data_get" {
				seenGet = true
			}
		}
	}
	if !seenFind {
		return schema.AssistantMessage("", []schema.ToolCall{{ID: "find-1", Type: "function", Function: schema.FunctionCall{Name: "data_find", Arguments: `{"need":{"name":"河流","what":{"category":"河流"}}}`}}}), nil
	}
	if !seenGet {
		return schema.AssistantMessage("", []schema.ToolCall{{ID: "get-1", Type: "function", Function: schema.FunctionCall{Name: "data_get", Arguments: `{"find_id":"df-test","choice":{"*":"recommended"}}`}}}), nil
	}
	return schema.AssistantMessage("完成", nil), nil
}
func (m *acquisitionSequenceModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	result, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{result}), nil
}
func TestSystemDataExpertAvailableWithoutEditingProfiles(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	for _, name := range []string{"default", "zttzt", "minimal"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "agent.yaml")
			original := []byte(testutil.V4MinimalProfileYAML)
			if name == "zttzt" {
				original = []byte(testutil.V4ZttztProfileYAML)
			}
			if name == "default" {
				original, _ = defaults.FS.ReadFile("agent_configs/default/agent.yaml")
			}
			os.WriteFile(path, original, 0600)
			var profile config.AgentConfigFile
			if err := yaml.Unmarshal(original, &profile); err != nil {
				t.Fatal(err)
			}
			calls := []string{}
			tools := []tool.BaseTool{}
			for _, toolName := range []string{"data_find", "data_get", "data_register"} {
				n := toolName
				tools = append(tools, utils.NewTool(&schema.ToolInfo{Name: n, Desc: n}, func(ctx context.Context, args map[string]any) (string, error) {
					calls = append(calls, n)
					if n == "data_get" && args["find_id"] != "df-test" {
						return "", fmt.Errorf("unverified query")
					}
					return `{"find_id":"df-test","rows":[]}`, nil
				}))
			}
			cfg := &Config{ChatModel: &acquisitionSequenceModel{}, Tools: tools, MaxIteration: 5, SkillRuntimeConfig: config.ConvertToRuntimeSkillConfig(&profile, "", "", root, name)}
			experts, err := createSystemExperts(context.Background(), cfg)
			if err != nil || len(experts) != 1 {
				t.Fatalf("%v %v", experts, err)
			}
			iterator := experts[0].Run(context.Background(), &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage("获取河流数据")}})
			for {
				event, ok := iterator.Next()
				if !ok {
					break
				}
				if event.Err != nil {
					t.Fatal(event.Err)
				}
			}
			if !reflect.DeepEqual(calls, []string{"data_find", "data_get"}) {
				t.Fatal(calls)
			}
			saved, _ := os.ReadFile(path)
			if string(saved) != string(original) {
				t.Fatal("profile modified")
			}
			cfg.SkipSubAgents = true
			if agents, err := createSystemExperts(context.Background(), cfg); err != nil || len(agents) != 0 {
				t.Fatal("Plan mode received executor")
			}
		})
	}
}
func TestOrdinaryRolesCannotUseDownloadToolsAndGEEAnalysisSurvives(t *testing.T) {
	calls := 0
	tools := []tool.BaseTool{}
	for _, name := range []string{"osm_get_features", "browser_call", "web_download_file", "admin_boundary_extract", "file_downloader", "data_get", "data_register", "data_find", "buffer", "gee_run_script"} {
		n := name
		tools = append(tools, utils.NewTool(&schema.ToolInfo{Name: n, Desc: n}, func(context.Context, map[string]any) (string, error) { calls++; return `{}`, nil }))
	}
	cfg := &Config{}
	filtered := filterAcquisitionTools(context.Background(), cfg, tools, false)
	names := []string{}
	for _, candidate := range filtered {
		info, _ := candidate.Info(context.Background())
		names = append(names, info.Name)
		if info.Name == "gee_run_script" {
			run := candidate.(tool.InvokableTool)
			if _, err := run.InvokableRun(context.Background(), `{"code":"result=image.normalizedDifference(['B8','B4'])"}`); err != nil {
				t.Fatal(err)
			}
			if _, err := run.InvokableRun(context.Background(), `{"template":"dem.py"}`); err == nil {
				t.Fatal("download template escaped")
			}
			if _, err := run.InvokableRun(context.Background(), `{"code":"image.getDownloadURL({})"}`); err == nil {
				t.Fatal("download primitive escaped")
			}
		}
	}
	if strings.Join(names, ",") != "data_find,buffer,gee_run_script" || calls != 1 {
		t.Fatal(names, calls)
	}
	disabled := false
	cfg.Acquisition = func() config.AcquisitionConfig { return config.AcquisitionConfig{ExclusiveAgent: &disabled} }
	raw, _ := json.Marshal(cfg.SkillRuntimeConfig)
	filterAcquisitionTools(context.Background(), cfg, tools, false)
	after, _ := json.Marshal(cfg.SkillRuntimeConfig)
	if string(raw) != string(after) {
		t.Fatal("runtime config mutated")
	}
}

type acquisitionManualModel struct{ capture *manualCapture }
type manualCapture struct {
	tools     map[string]bool
	reference string
	text      string
}

func (m *acquisitionManualModel) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	for _, info := range infos {
		m.capture.tools[info.Name] = true
	}
	return m, nil
}
func (m *acquisitionManualModel) Generate(_ context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	for _, info := range model.GetCommonOptions(nil, opts...).Tools {
		m.capture.tools[info.Name] = true
	}
	loaded := false
	for _, message := range messages {
		if message.Role != schema.Tool {
			continue
		}
		if message.ToolName == "reference_read" {
			m.capture.text = message.Content
			return schema.AssistantMessage("已读取统计取数方法", nil), nil
		}
		if message.ToolName == "skill" {
			loaded = true
			m.capture.reference = regexp.MustCompile(`browser-data-acquisition/references/[^"\x60\s]+\.md`).FindString(message.Content)
		}
	}
	name, args := "skill", `{"skill":"browser-data-acquisition"}`
	if loaded {
		if m.capture.reference == "" {
			return nil, fmt.Errorf("skill missing statistics method reference")
		}
		name = "reference_read"
		raw, _ := json.Marshal(map[string]string{"path": m.capture.reference})
		args = string(raw)
	}
	return schema.AssistantMessage("", []schema.ToolCall{{ID: name, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}}), nil
}
func (m *acquisitionManualModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	result, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{result}), nil
}
func TestSystemDataExpertReadsBrowserStatisticsMethodThroughRealMiddleware(t *testing.T) {
	skillsRoot, _ := filepath.Abs("../../../autocarto/skills/system")
	assets, _ := filepath.Abs("../../../autocarto")
	t.Setenv("AUTOCARTO_ASSETS_ROOT", assets)
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	raw, _ := defaults.FS.ReadFile("agent_configs/default/agent.yaml")
	var profile config.AgentConfigFile
	if err := yaml.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}
	runtime := config.ConvertToRuntimeSkillConfig(&profile, skillsRoot, "", t.TempDir(), "default")
	before, _ := json.Marshal(runtime)
	capture := &manualCapture{tools: map[string]bool{}}
	tools := []tool.BaseTool{}
	for _, name := range []string{"data_find", "data_get", "data_register", "browser_list_tools", "browser_call_tool", "web_search", "reference_read"} {
		name := name
		tools = append(tools, utils.NewTool(&schema.ToolInfo{Name: name, Desc: name}, func(ctx context.Context, args map[string]any) (string, error) {
			if name != "reference_read" {
				return "", fmt.Errorf("manual lookup unexpectedly executed %s", name)
			}
			result, err := runtimecontrol.Invoke(ctx, name, args)
			if err != nil {
				return "", err
			}
			raw, err := json.Marshal(result)
			return string(raw), err
		}))
	}
	cfg := &Config{ChatModel: &acquisitionManualModel{capture: capture}, Tools: tools, SkillRuntimeConfig: runtime, PluginSkillsDir: skillsRoot, MaxIteration: 4, RegistryGovernance: control.NewRegistryGovernance(control.RegistryGovernanceDeps{ResolveSkillDirs: func() []string { return []string{skillsRoot} }})}
	ctx := context.Background()
	agents, err := createSystemExperts(ctx, cfg)
	if err != nil || len(agents) != 1 {
		t.Fatal(agents, err)
	}
	iter := agents[0].Run(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage("读取中国统计数据的取数方法")}})
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}
	if !capture.tools["skill"] || !capture.tools["reference_read"] || capture.reference == "" {
		t.Fatal(capture)
	}
	expected, err := os.ReadFile(filepath.Join(skillsRoot, capture.reference))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if json.Unmarshal([]byte(capture.text), &result) != nil || result["text"] != string(expected) || result["truncated"] != false {
		t.Fatal("expert did not receive full manual", capture.text)
	}
	after, _ := json.Marshal(runtime)
	if string(before) != string(after) {
		t.Fatal("system expert changed user runtime config")
	}
}
