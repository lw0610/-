package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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

type flowModelCapture struct {
	bindings [][]*schema.ToolInfo
	messages []*schema.Message
	options  *model.Options
	calls    int
}

type flowProbeModel struct {
	capture *flowModelCapture
	result  *schema.Message
	err     error
	bindErr error
}

func (m *flowProbeModel) Generate(_ context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.capture.messages = messages
	m.capture.options = model.GetCommonOptions(nil, opts...)
	m.capture.calls++
	return m.result, m.err
}

func (m *flowProbeModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("unexpected streaming call")
}

func (m *flowProbeModel) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	m.capture.bindings = append(m.capture.bindings, infos)
	copy := *m
	return &copy, m.bindErr
}

func newFlowProbe() *flowProbeModel {
	return &flowProbeModel{capture: &flowModelCapture{}, result: schema.AssistantMessage(`{"run":true}`, nil)}
}

func TestFactoryBuildRoleReusesProjectionProviderAndMiddleware(t *testing.T) {
	root, baseDir := t.TempDir(), t.TempDir()
	packageDir := filepath.Join(root, "observation")
	if err := os.MkdirAll(packageDir, 0700); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: observation\ndescription: 观察方法\nkind: category\ntools: [get_layers_info]\n---\n# 方法\n\n" + strings.Repeat("按实际图层核对信息。", 40)
	if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	defaultModel, selected := newFlowProbe(), newFlowProbe()
	governance := control.NewRegistryGovernance(control.RegistryGovernanceDeps{ResolveSkillDirs: func() []string { return []string{root} }})
	factory, err := NewFactory(&FactoryConfig{
		DefaultChatModel: defaultModel, DefaultProviderCfg: &config.LLMProviderConfig{Model: "default"},
		BaseTools:       []tool.BaseTool{&fakeInvokableTool{name: "get_layers_info"}, &fakeInvokableTool{name: "unassigned"}},
		PluginSkillsDir: root, BuiltinSkillsDir: t.TempDir(), UserSkillsDir: t.TempDir(), ConfigBaseDir: baseDir,
		RegistryGovernance: governance, MaxIterations: 3,
		ResolveChatModel: func(provider string) (model.ToolCallingChatModel, error) {
			if provider != "selected" {
				t.Fatalf("unexpected provider %s", provider)
			}
			return selected, nil
		},
		ResolveProviderCfg: func(string) *config.LLMProviderConfig {
			return &config.LLMProviderConfig{Model: "selected", CompressStrategy: config.CompressStrategySummary}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	active := &config.AgentConfigFile{Version: "2.0", Runtime: &config.RuntimeConfig{Agents: map[string]config.AgentDefData{
		"worker": {Kind: "agent", Enabled: true, Description: "核对图层", DisplayName: "图层核对", ToolPackages: []string{"observation"}, LLM: config.AgentLLMConfig{Provider: "selected"}, Prompts: map[string]string{"static_instructions": "保留步骤角色规则"}},
	}}}
	before, _ := json.Marshal(active)
	projection, err := factory.deepAgentConfig("project-profile", active)
	if err != nil || projection.RegistryGovernance != governance || projection.ActiveConfigName != "project-profile" || projection.RuntimeConfigBaseDir != baseDir || projection.MaxIteration != 3 || len(projection.Tools) != 2 {
		t.Fatalf("projection lost runtime dependencies: %+v %v", projection, err)
	}
	agent, err := factory.BuildRole(context.Background(), "project-profile", active, "worker")
	if err != nil || agent.Name(context.Background()) != "worker" {
		t.Fatal(agent, err)
	}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: agent})
	iter := runner.Run(context.Background(), []*schema.Message{schema.UserMessage("核对当前图层")})
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}
	if defaultModel.capture.calls != 0 || selected.capture.calls != 1 {
		t.Fatalf("wrong role model selected: default=%d selected=%d", defaultModel.capture.calls, selected.capture.calls)
	}
	allTools := map[string]bool{}
	for _, binding := range selected.capture.bindings {
		for _, info := range binding {
			allTools[info.Name] = true
		}
	}
	for _, info := range selected.capture.options.Tools {
		allTools[info.Name] = true
	}
	if !allTools["get_layers_info"] || !allTools["skill"] || allTools["unassigned"] {
		t.Fatalf("role authorization or actual middleware lost: %v", allTools)
	}
	if !strings.Contains(selected.capture.messages[0].Content, "保留步骤角色规则") {
		t.Fatal("role prompt lost")
	}
	after, _ := json.Marshal(active)
	if string(before) != string(after) {
		t.Fatal("factory mutated profile")
	}
	if _, err := factory.BuildRole(context.Background(), "project-profile", nil, "worker"); err == nil {
		t.Fatal("missing profile accepted")
	}
	if _, err := factory.BuildRole(context.Background(), "project-profile", active, "missing"); err == nil {
		t.Fatal("missing role accepted")
	}
}

func flowMainProfile() *config.AgentConfigFile {
	return &config.AgentConfigFile{Version: "2.0", Runtime: &config.RuntimeConfig{Agents: map[string]config.AgentDefData{
		"renamed-main": {Kind: "agent", IsMain: true, Enabled: true, LLM: config.AgentLLMConfig{Provider: "main-model"}},
		"specialist":   {Kind: "agent", Enabled: true, LLM: config.AgentLLMConfig{Provider: "other-model"}},
	}}}
}

func TestFactoryCallFlowJSONUsesMainModelZeroTemperatureAndNoTools(t *testing.T) {
	defaultModel, mainModel := newFlowProbe(), newFlowProbe()
	factory, err := NewFactory(&FactoryConfig{DefaultChatModel: defaultModel, ResolveChatModel: func(provider string) (model.ToolCallingChatModel, error) {
		if provider != "main-model" {
			t.Fatal(provider)
		}
		return mainModel, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := factory.CallFlowJSON(context.Background(), flowMainProfile(), "判断条件并返回JSON")
	if err != nil || result != `{"run":true}` || mainModel.capture.calls != 1 || defaultModel.capture.calls != 0 {
		t.Fatal(result, err)
	}
	opts := mainModel.capture.options
	if opts.Temperature == nil || *opts.Temperature != 0 || opts.Tools == nil || len(opts.Tools) != 0 || opts.ToolChoice == nil || *opts.ToolChoice != schema.ToolChoiceForbidden {
		t.Fatalf("JSON model call is not deterministic/tool-free: %+v", opts)
	}
	if len(mainModel.capture.bindings) != 1 || len(mainModel.capture.bindings[0]) != 0 || mainModel.capture.messages[1].Content != "判断条件并返回JSON" {
		t.Fatal("tools bound or prompt altered")
	}
	profile := flowMainProfile()
	role := profile.Runtime.Agents["renamed-main"]
	role.LLM.Provider = ""
	profile.Runtime.Agents["renamed-main"] = role
	if _, err := factory.CallFlowJSON(context.Background(), profile, "{}"); err != nil || defaultModel.capture.calls != 1 {
		t.Fatal("default main model not used", err)
	}
}

func TestFactoryCallFlowJSONRejectsInvalidConfigurationAndModelOutput(t *testing.T) {
	probe := newFlowProbe()
	factory, _ := NewFactory(&FactoryConfig{DefaultChatModel: probe, ResolveChatModel: func(string) (model.ToolCallingChatModel, error) { return probe, nil }})
	for _, profile := range []*config.AgentConfigFile{nil, {}, {Runtime: &config.RuntimeConfig{Agents: map[string]config.AgentDefData{}}}, {Runtime: &config.RuntimeConfig{Agents: map[string]config.AgentDefData{"one": {IsMain: true, Enabled: true}, "two": {IsMain: true, Enabled: true}}}}} {
		if _, err := factory.CallFlowJSON(context.Background(), profile, "{}"); err == nil {
			t.Fatal("invalid profile accepted")
		}
	}
	if _, err := factory.CallFlowJSON(context.Background(), flowMainProfile(), " "); err == nil {
		t.Fatal("empty prompt accepted")
	}
	for _, result := range []*schema.Message{nil, schema.AssistantMessage("```json\n{}\n```", nil), schema.AssistantMessage("not JSON", nil), {Content: `{}`, ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: "not_allowed"}}}}} {
		probe.result = result
		if _, err := factory.CallFlowJSON(context.Background(), flowMainProfile(), "{}"); err == nil {
			t.Fatal("invalid model output accepted", result)
		}
	}
	probe.bindErr = errors.New("binding failed")
	if _, err := factory.CallFlowJSON(context.Background(), flowMainProfile(), "{}"); err == nil || !strings.Contains(err.Error(), "binding failed") {
		t.Fatal(err)
	}
	probe.bindErr, probe.err = nil, errors.New("model failed")
	if _, err := factory.CallFlowJSON(context.Background(), flowMainProfile(), "{}"); err == nil || !strings.Contains(err.Error(), "model failed") {
		t.Fatal(err)
	}
	factory.config.ResolveChatModel = func(string) (model.ToolCallingChatModel, error) { return nil, errors.New("provider missing") }
	if _, err := factory.CallFlowJSON(context.Background(), flowMainProfile(), "{}"); err == nil || !strings.Contains(err.Error(), "provider missing") {
		t.Fatal(err)
	}
}

type flowInvokable struct {
	name, result string
	calls        int
}

func (t *flowInvokable) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name}, nil
}
func (t *flowInvokable) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	t.calls++
	return t.result, nil
}

func TestFactoryInvokeFlowToolCallsReadonlyBaseToolsAndDecodesResults(t *testing.T) {
	var enhancedCalls int
	layers := &fakeEnhancedTool{info: &schema.ToolInfo{Name: "get_layers_info"}, runFn: func(_ context.Context, args *schema.ToolArgument, _ ...tool.Option) (*schema.ToolResult, error) {
		enhancedCalls++
		if args.Text != `{"include_invisible":false}` {
			t.Fatal(args.Text)
		}
		return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: `{"success":true,"data":{"count":2}}`}}}, nil
	}}
	layout := &flowInvokable{name: "layout_info", result: `{"success":true,"data":{"name":"layout"}}`}
	writeTool := &flowInvokable{name: "data_load_vector", result: `{}`}
	factory, err := NewFactory(&FactoryConfig{DefaultChatModel: newFlowProbe(), BaseTools: []tool.BaseTool{layers, layout, writeTool}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := factory.InvokeFlowTool(context.Background(), "get_layers_info", map[string]any{"include_invisible": false})
	if err != nil || result.(map[string]any)["data"].(map[string]any)["count"] != float64(2) || enhancedCalls != 1 {
		t.Fatal(result, err)
	}
	result, err = factory.InvokeFlowTool(context.Background(), "layout_info", nil)
	if err != nil || result.(map[string]any)["data"].(map[string]any)["name"] != "layout" || layout.calls != 1 {
		t.Fatal(result, err)
	}
	if _, err := factory.InvokeFlowTool(context.Background(), "data_load_vector", nil); err == nil || writeTool.calls != 0 {
		t.Fatal("write tool escaped read-only allowlist", err)
	}
	if _, err := factory.InvokeFlowTool(context.Background(), "get_layers_info", map[string]any{"invalid": func() {}}); err == nil || enhancedCalls != 1 {
		t.Fatal("invalid arguments invoked tool", err)
	}
}

func TestFactoryInvokeFlowToolRejectsMissingDuplicateMalformedAndFailedResults(t *testing.T) {
	ctx := context.Background()
	for _, result := range []string{"", "null", "not JSON", `{"success":"true"}`, `{"success":false,"info":"找不到布局"}`} {
		factory, _ := NewFactory(&FactoryConfig{DefaultChatModel: newFlowProbe(), BaseTools: []tool.BaseTool{&flowInvokable{name: "layout_info", result: result}}})
		if _, err := factory.InvokeFlowTool(ctx, "layout_info", nil); err == nil {
			t.Fatal("invalid/failed result accepted", result)
		}
	}
	for _, tools := range [][]tool.BaseTool{nil, {&flowInvokable{name: "layout_info"}, &flowInvokable{name: "layout_info"}}} {
		factory, _ := NewFactory(&FactoryConfig{DefaultChatModel: newFlowProbe(), BaseTools: tools})
		if _, err := factory.InvokeFlowTool(ctx, "layout_info", nil); err == nil {
			t.Fatal("missing/duplicate tool accepted")
		}
	}
	failing := &fakeEnhancedTool{info: &schema.ToolInfo{Name: "layout_info"}, runFn: func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.ToolResult, error) {
		return nil, errors.New("bridge unavailable")
	}}
	factory, _ := NewFactory(&FactoryConfig{DefaultChatModel: newFlowProbe(), BaseTools: []tool.BaseTool{failing}})
	if _, err := factory.InvokeFlowTool(ctx, "layout_info", nil); err == nil || !strings.Contains(err.Error(), "bridge unavailable") {
		t.Fatal("wrapped tool transport error accepted", err)
	}
}

func TestFactoryFlowControllerCanInspectAndCleanTrackedLayers(t *testing.T) {
	for _, name := range []string{"layer_inspect", "layer_remove_created"} {
		target := &flowInvokable{name: name, result: `{"success":true,"data":{}}`}
		factory, err := NewFactory(&FactoryConfig{DefaultChatModel: newFlowProbe(), BaseTools: []tool.BaseTool{target}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = factory.InvokeFlowTool(context.Background(), name, map[string]any{"layer_ids": []string{"created-by-attempt"}}); err != nil || target.calls != 1 {
			t.Fatal(name, err, target.calls)
		}
	}
}

func TestFactoryBuildsConfiguredRoleWithSharedAndAutomaticTools(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"declared", "shared", "foundation"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		raw := "---\nname: " + name + "\ndescription: fixture\nkind: category\ntools: [" + name + "_tool]\n"
		if name == "foundation" {
			raw += "system: true\n"
		}
		raw += "---\n# fixture\nFixture instructions.\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
	probe := newFlowProbe()
	factory, err := NewFactory(&FactoryConfig{DefaultChatModel: probe, DefaultProviderCfg: &config.LLMProviderConfig{Model: "fixture"}, BaseTools: []tool.BaseTool{&fakeInvokableTool{name: "declared_tool"}, &fakeInvokableTool{name: "shared_tool"}, &fakeInvokableTool{name: "foundation_tool"}, &fakeInvokableTool{name: "unassigned"}}, PluginSkillsDir: root, BuiltinSkillsDir: t.TempDir(), UserSkillsDir: t.TempDir(), ConfigBaseDir: t.TempDir(), RegistryGovernance: control.NewRegistryGovernance(control.RegistryGovernanceDeps{ResolveSkillDirs: func() []string { return []string{root} }})})
	if err != nil {
		t.Fatal(err)
	}
	active := &config.AgentConfigFile{Runtime: &config.RuntimeConfig{SharedToolPackages: []string{"shared"}, Agents: map[string]config.AgentDefData{
		"main":   {Kind: "agent", Enabled: true, IsMain: true},
		"worker": {Kind: "agent", Enabled: true, DisplayName: "处理", ToolPackages: []string{"declared"}, Prompts: map[string]string{"static_instructions": "按方案完成任务"}},
	}}}
	agent, err := factory.BuildRole(context.Background(), "fixture", active, "worker")
	if err != nil {
		t.Fatal(err)
	}
	if probe.capture.calls != 0 {
		t.Fatal("construction called model")
	}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: agent})
	iter := runner.Run(context.Background(), []*schema.Message{schema.UserMessage("执行测试")})
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}
	names := map[string]bool{}
	for _, binding := range probe.capture.bindings {
		for _, info := range binding {
			names[info.Name] = true
		}
	}
	for _, info := range probe.capture.options.Tools {
		names[info.Name] = true
	}
	for _, name := range []string{"declared_tool", "shared_tool", "foundation_tool"} {
		if !names[name] {
			t.Fatalf("missing %s in %+v", name, names)
		}
	}
	if names["unassigned"] || len(active.Runtime.Agents) != 2 || probe.capture.calls != 1 {
		t.Fatal("execution changed profile or unexpected model count", names)
	}
}

func TestFactoryBuildsConfiguredKnowledgeExecutor(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "knowledge")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw := "---\nname: knowledge\ndescription: fixture\nkind: category\nhidden: true\nauto_mount: executor:route_knowledge\ntools: [marching_route_get_extraction_contract,marching_route_compile_dataset]\n---\n# fixture\nExtraction instructions.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	probe := newFlowProbe()
	factory, err := NewFactory(&FactoryConfig{DefaultChatModel: probe, DefaultProviderCfg: &config.LLMProviderConfig{Model: "fixture"}, BaseTools: []tool.BaseTool{&fakeInvokableTool{name: "marching_route_get_extraction_contract"}, &fakeInvokableTool{name: "marching_route_compile_dataset"}}, PluginSkillsDir: root, BuiltinSkillsDir: t.TempDir(), UserSkillsDir: t.TempDir(), ConfigBaseDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	active := &config.AgentConfigFile{Runtime: &config.RuntimeConfig{Agents: map[string]config.AgentDefData{
		"main":         {Kind: "agent", Enabled: true, IsMain: true},
		"route_reader": {Kind: "executor", Executor: "route_knowledge", Enabled: true, DisplayName: "抽取", Prompts: map[string]string{"static_instructions": "根据原文抽取路线"}},
	}}}
	agent, err := factory.BuildRole(context.Background(), "fixture", active, "route_reader")
	if err != nil || agent == nil {
		t.Fatal(agent, err)
	}
	if agent.Name(context.Background()) != "route_reader" || probe.capture.calls != 0 {
		t.Fatal("unexpected executor construction")
	}
}
