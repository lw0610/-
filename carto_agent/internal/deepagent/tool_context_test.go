package deepagent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/defaults"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/runtimecontrol"
	"github.com/autocarto/carto_agent/internal/toolresult"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestToolContextKeepsRoleVisionAndOnlyAssignedDownloader(t *testing.T) {
	ctx := context.Background()
	downloadCalls := 0
	downloader := utils.NewEnhancedTool(&schema.ToolInfo{Name: "web_download_file", Desc: "fixture"}, func(_ context.Context, args map[string]any) (*schema.ToolResult, error) {
		downloadCalls++
		if args["url"] != "https://example.org/source.pdf" {
			t.Fatal(args)
		}
		return toolresult.Text(`{"path":"/fixture/source.pdf"}`), nil
	})
	cfg := &Config{
		Tools:              []tool.BaseTool{downloader},
		DefaultProviderCfg: &config.LLMProviderConfig{Vision: true},
		Acquisition: func() config.AcquisitionConfig {
			return config.AcquisitionConfig{DocumentPython: "fixture-python", OCREngine: "fixture-ocr", OCRLanguages: "eng"}
		},
	}
	for _, vision := range []bool{false, true} {
		provider := &config.LLMProviderConfig{Vision: vision}
		middleware := newToolContextMiddleware(ctx, cfg, provider, nil, nil)
		if middleware.options.Vision != vision || middleware.options.Config.DocumentPython != "fixture-python" || middleware.options.Config.OCREngine != "fixture-ocr" || middleware.options.Config.OCRLanguages != "eng" {
			t.Fatalf("role document settings changed: %+v", middleware.options)
		}
		if middleware.options.Download != nil {
			t.Fatal("unassigned catalog downloader was made available")
		}
		middleware = newToolContextMiddleware(ctx, cfg, provider, []tool.BaseTool{downloader}, nil)
		result, err := middleware.options.Download(ctx, "https://example.org/source.pdf")
		if err != nil || result["path"] != "/fixture/source.pdf" {
			t.Fatal(result, err)
		}
	}
	if downloadCalls != 2 {
		t.Fatalf("assigned download callback invoked %d times", downloadCalls)
	}
}

type configuredAcquisitionModel struct{ fakeToolCallingChatModel }

func (m *configuredAcquisitionModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}
func (m *configuredAcquisitionModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	completed := map[string]bool{}
	for _, message := range messages {
		if message.Role == schema.Tool {
			completed[message.ToolName] = true
		}
	}
	for _, name := range []string{"data_find", "data_get", "osm_get_features", "gee_run_script", "data_register"} {
		if !completed[name] {
			return schema.AssistantMessage("", []schema.ToolCall{{ID: name, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: `{"template":"dem.py","code":"image.getDownloadURL({})"}`}}}), nil
		}
	}
	return schema.AssistantMessage("completed", nil), nil
}
func (m *configuredAcquisitionModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	result, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{result}), nil
}

func TestConfiguredRolesCanExecuteAssignedAcquisitionTools(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	for _, main := range []bool{false, true} {
		t.Run(fmt.Sprintf("main=%t", main), func(t *testing.T) {
			calls := []string{}
			names := []string{"data_find", "data_get", "osm_get_features", "gee_run_script", "data_register"}
			tools := []tool.BaseTool{}
			for _, name := range names {
				tools = append(tools, utils.NewTool(&schema.ToolInfo{Name: name, Desc: name}, func(_ context.Context, args map[string]any) (string, error) {
					calls = append(calls, name)
					return `{}`, nil
				}))
			}
			role := "researcher"
			if main {
				role = "orchestrator"
			}
			cfg := &Config{Name: role, ChatModel: &configuredAcquisitionModel{}, Tools: tools, Instruction: "执行已分配工具。", MaxIteration: 7,
				SkillRuntimeConfig: &subagents.RuntimeSkillConfig{
					Global: subagents.RuntimeSkillGlobalConfig{PackageTools: map[string][]string{"assigned": names}},
					Agents: map[string]subagents.RuntimeSkillAgentConfig{role: {Kind: "agent", Enabled: true, IsMain: main, ToolPackages: []string{"assigned"}}},
				},
			}
			ctx := context.Background()
			var agent adk.Agent
			var err error
			if main {
				agent, err = New(ctx, cfg)
			} else {
				agent, err = BuildRoleAgent(ctx, cfg, role)
			}
			if err != nil {
				t.Fatal(err)
			}
			iter := agent.Run(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage("获取数据")}})
			for event, ok := iter.Next(); ok; event, ok = iter.Next() {
				if event.Err != nil {
					t.Fatal(event.Err)
				}
			}
			if fmt.Sprint(calls) != fmt.Sprint(names) {
				t.Fatalf("assigned acquisition tools not executed: %v", calls)
			}
		})
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
func TestConfiguredRoleReadsBrowserStatisticsMethodThroughRealMiddleware(t *testing.T) {
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
	runtime.Agents = map[string]subagents.RuntimeSkillAgentConfig{"researcher": {Kind: "agent", Enabled: true, ToolPackages: []string{"browser-data-acquisition", "runtime-control"}, Prompts: subagents.AgentPrompts{StaticInstructions: "读取所分配工具包的参考资料。"}}}
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
	agent, err := BuildRoleAgent(ctx, cfg, "researcher")
	if err != nil {
		t.Fatal(err)
	}
	iter := agent.Run(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage("读取中国统计数据的取数方法")}})
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
		t.Fatal("role construction changed user runtime config")
	}
}
