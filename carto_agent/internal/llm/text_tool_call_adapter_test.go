package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestParseTextToolCalls_SingleTool(t *testing.T) {
	calls, ok := parseTextToolCalls(
		`{"tool":"layout_create","arguments":{"name":"test"}}`,
		buildTestToolNames("layout_create"),
	)
	if !ok {
		t.Fatal("expected text tool call to parse")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Function.Name != "layout_create" {
		t.Fatalf("unexpected tool name: %s", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"name":"test"}` {
		t.Fatalf("unexpected arguments: %s", calls[0].Function.Arguments)
	}
}

func TestParseTextToolCalls_Steps(t *testing.T) {
	calls, ok := parseTextToolCalls(
		`{"steps":[{"tool":"data_load_vector","arguments":{"file_path":"/tmp/a.shp"}},{"tool":"export_layout_pdf","arguments":{"file_path":"/tmp/out.pdf"}}]}`,
		buildTestToolNames("data_load_vector", "export_layout_pdf"),
	)
	if !ok {
		t.Fatal("expected steps to parse")
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Function.Name != "data_load_vector" || calls[1].Function.Name != "export_layout_pdf" {
		t.Fatalf("unexpected calls: %#v", calls)
	}
}

func TestParseTextToolCalls_OpenAIShapedText(t *testing.T) {
	calls, ok := parseTextToolCalls(
		`{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"layout_delete","arguments":"{\"name\":\"old\"}"}}]}`,
		buildTestToolNames("layout_delete"),
	)
	if !ok {
		t.Fatal("expected openai-shaped text to parse")
	}
	if calls[0].ID != "call_1" {
		t.Fatalf("unexpected id: %s", calls[0].ID)
	}
	if calls[0].Function.Arguments != `{"name":"old"}` {
		t.Fatalf("unexpected arguments: %s", calls[0].Function.Arguments)
	}
}

func TestParseTextToolCalls_FunctionCallXMLWithName(t *testing.T) {
	calls, ok := parseTextToolCalls(
		`<function_call>
{
	"name": "task",
	"arguments": {
		"description": "Query current number of layers and layouts; if layouts count is 0, create a new layout named 'test'",
		"subagent_type": "cartographer"
	}
}
</function_call>`,
		buildTestToolNames("task"),
	)
	if !ok {
		t.Fatal("expected function_call XML text to parse")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Function.Name != "task" {
		t.Fatalf("unexpected tool name: %s", calls[0].Function.Name)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"subagent_type":"cartographer"`) {
		t.Fatalf("unexpected arguments: %s", calls[0].Function.Arguments)
	}
}

func TestParseTextToolCalls_IgnoresUnknownTool(t *testing.T) {
	if calls, ok := parseTextToolCalls(
		`{"tool":"not_a_tool","arguments":{"name":"test"}}`,
		buildTestToolNames("layout_create"),
	); ok || len(calls) > 0 {
		t.Fatalf("expected unknown tool to be ignored, got %#v", calls)
	}
}

func TestTextToolCallModelWrapper_UsesToolsFromModelOptions(t *testing.T) {
	fake := &fakeTextToolCallModel{
		response: schema.AssistantMessage(`<function_call>
{
	"name": "task",
	"arguments": {
		"description": "Query current number of layers and layouts; if layouts count is 0, create a new layout named 'test'",
		"subagent_type": "cartographer"
	}
}
</function_call>`, nil),
	}
	wrapped := NewTextToolCallModelWrapper(fake)

	resp, err := wrapped.Generate(
		context.Background(),
		[]*schema.Message{schema.UserMessage("现在图层中有几个图层，有几个布局，没有布局帮我新建一个test布局")},
		model.WithTools([]*schema.ToolInfo{{
			Name: "task",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"description":   {Type: schema.String, Required: true},
				"subagent_type": {Type: schema.String, Required: true},
			}),
		}}),
	)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if resp.Content != "" {
		t.Fatalf("expected converted message content to be empty, got %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "task" {
		t.Fatalf("expected task tool call from model options, got %#v", resp.ToolCalls)
	}
	if !strings.Contains(resp.ToolCalls[0].Function.Arguments, `"subagent_type":"cartographer"`) {
		t.Fatalf("unexpected arguments: %s", resp.ToolCalls[0].Function.Arguments)
	}
	if len(fake.lastInput) == 0 || fake.lastInput[0].Role != schema.System {
		t.Fatalf("expected wrapper to prepend system tool instruction from model options, got %#v", fake.lastInput)
	}
}

func TestTextToolCallModelWrapper_ConvertsGeneratedText(t *testing.T) {
	fake := &fakeTextToolCallModel{
		response: schema.AssistantMessage(`{"tool":"layout_create","arguments":{"name":"test"}}`, nil),
	}
	wrapped, err := NewTextToolCallModelWrapper(fake).WithTools([]*schema.ToolInfo{{Name: "layout_create"}})
	if err != nil {
		t.Fatalf("WithTools failed: %v", err)
	}

	resp, err := wrapped.Generate(context.Background(), []*schema.Message{schema.UserMessage("新建 test 布局")})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if resp.Content != "" {
		t.Fatalf("expected converted message content to be empty, got %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %#v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Function.Name != "layout_create" {
		t.Fatalf("unexpected tool name: %s", resp.ToolCalls[0].Function.Name)
	}
	if len(fake.lastInput) == 0 || fake.lastInput[0].Role != schema.System {
		t.Fatalf("expected wrapper to prepend system tool instruction, got %#v", fake.lastInput)
	}
}

func TestTextToolCallModelWrapper_IncludesToolDescriptionsInInstruction(t *testing.T) {
	fake := &fakeTextToolCallModel{
		response: schema.AssistantMessage("ok", nil),
	}
	wrapped, err := NewTextToolCallModelWrapper(fake).WithTools([]*schema.ToolInfo{{
		Name: "get_layers_info",
		Desc: "获取当前项目中所有图层的详细信息列表",
	}})
	if err != nil {
		t.Fatalf("WithTools failed: %v", err)
	}

	_, err = wrapped.Generate(context.Background(), []*schema.Message{schema.UserMessage("当前工程中有图层吗")})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(fake.lastInput) == 0 {
		t.Fatal("expected wrapper to prepend system instruction")
	}
	instruction := fake.lastInput[0].Content
	if !strings.Contains(instruction, "get_layers_info: 获取当前项目中所有图层的详细信息列表") {
		t.Fatalf("expected tool description in instruction, got %q", instruction)
	}
	if !strings.Contains(instruction, "不要回答“没有可用技能”") {
		t.Fatalf("expected no-skill guard in instruction, got %q", instruction)
	}
}

func TestTextToolCallModelWrapper_RepairsInvalidNativeToolCall(t *testing.T) {
	fake := &fakeTextToolCallModel{
		responses: []*schema.Message{
			{
				Role: schema.Assistant,
				ToolCalls: []schema.ToolCall{{
					Function: schema.FunctionCall{Name: "skill", Arguments: "{}"},
				}},
			},
			schema.AssistantMessage(`{"tool":"get_layers_info","arguments":{}}`, nil),
		},
	}
	wrapped, err := NewTextToolCallModelWrapper(fake).WithTools([]*schema.ToolInfo{
		{
			Name: "skill",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"skill": {Type: schema.String, Required: true},
			}),
		},
		{Name: "get_layers_info"},
	})
	if err != nil {
		t.Fatalf("WithTools failed: %v", err)
	}

	resp, err := wrapped.Generate(context.Background(), []*schema.Message{schema.UserMessage("当前工程中有几个图层")})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "get_layers_info" {
		t.Fatalf("expected repaired get_layers_info tool call, got %#v", resp.ToolCalls)
	}
	if fake.callCount != 2 {
		t.Fatalf("expected one repair retry, got %d calls", fake.callCount)
	}
	if len(fake.inputs) < 2 || !strings.Contains(fake.inputs[1][1].Content, "缺少必填参数") {
		t.Fatalf("expected repair prompt to include validation problem, got %#v", fake.inputs)
	}
}

func TestTextToolCallModelWrapper_NormalizesTaskPromptIntoDescription(t *testing.T) {
	fake := &fakeTextToolCallModel{
		response: &schema.Message{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{{
				Function: schema.FunctionCall{
					Name:      "task",
					Arguments: `{"description":"知识抽取：河南行军路线","prompt":"这是完整路线叙事，包含三门峡、洛阳和郑州。","subagent_type":"knowledge_engineer"}`,
				},
			}},
		},
	}
	wrapped, err := NewTextToolCallModelWrapper(fake).WithTools([]*schema.ToolInfo{{
		Name: "task",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"description":   {Type: schema.String},
			"subagent_type": {Type: schema.String},
		}),
	}})
	if err != nil {
		t.Fatalf("WithTools failed: %v", err)
	}

	resp, err := wrapped.Generate(context.Background(), []*schema.Message{schema.UserMessage("制作行军路线图")})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one task call, got %#v", resp.ToolCalls)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(resp.ToolCalls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("invalid normalized arguments: %v", err)
	}
	if _, exists := args["prompt"]; exists {
		t.Fatalf("prompt alias should be removed, got %#v", args)
	}
	description, _ := args["description"].(string)
	if !strings.Contains(description, "这是完整路线叙事") {
		t.Fatalf("full prompt was not preserved in description: %#v", args)
	}
	if args["subagent_type"] != "knowledge_engineer" {
		t.Fatalf("unexpected subagent type: %#v", args)
	}
}

func TestTextToolCallModelWrapper_RepairsTaskMissingSubagentType(t *testing.T) {
	fake := &fakeTextToolCallModel{
		responses: []*schema.Message{
			{
				Role: schema.Assistant,
				ToolCalls: []schema.ToolCall{{
					Function: schema.FunctionCall{
						Name:      "task",
						Arguments: `{"description":"知识抽取","prompt":"完整的河南行军路线叙事"}`,
					},
				}},
			},
			schema.AssistantMessage(`{"tool":"task","arguments":{"description":"完整的河南行军路线叙事","subagent_type":"knowledge_engineer"}}`, nil),
		},
	}
	wrapped, err := NewTextToolCallModelWrapper(fake).WithTools([]*schema.ToolInfo{{
		Name: "task",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"description":   {Type: schema.String},
			"subagent_type": {Type: schema.String},
		}),
	}})
	if err != nil {
		t.Fatalf("WithTools failed: %v", err)
	}

	resp, err := wrapped.Generate(context.Background(), []*schema.Message{schema.UserMessage("制作行军路线图")})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if fake.callCount != 2 {
		t.Fatalf("expected one repair retry, got %d calls", fake.callCount)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "task" {
		t.Fatalf("expected repaired task call, got %#v", resp.ToolCalls)
	}
	if !strings.Contains(resp.ToolCalls[0].Function.Arguments, `"subagent_type":"knowledge_engineer"`) {
		t.Fatalf("missing repaired subagent type: %s", resp.ToolCalls[0].Function.Arguments)
	}
	if len(fake.inputs) < 2 || !strings.Contains(fake.inputs[1][1].Content, "subagent_type") {
		t.Fatalf("repair prompt did not identify missing subagent_type: %#v", fake.inputs)
	}
}

func TestTextToolCallModelWrapper_RepairsLowRelevanceToolCall(t *testing.T) {
	fake := &fakeTextToolCallModel{
		responses: []*schema.Message{
			schema.AssistantMessage(`{"tool":"output_get_info","arguments":{}}`, nil),
			schema.AssistantMessage(`{"tool":"get_layers_info","arguments":{}}`, nil),
		},
	}
	wrapped := NewTextToolCallModelWrapper(fake)

	resp, err := wrapped.Generate(
		context.Background(),
		[]*schema.Message{schema.UserMessage("当前工程名是什么，有几个图层")},
		model.WithTools([]*schema.ToolInfo{
			{
				Name: "output_get_info",
				Desc: "获取当前输出信息与执行产物摘要",
			},
			{
				Name: "get_project_info",
				Desc: "获取当前QGIS项目的基本信息，包括CRS、文件路径、图层数量等",
			},
			{
				Name: "get_layers_info",
				Desc: "获取当前项目中所有图层的详细信息列表",
			},
		}),
	)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "get_layers_info" {
		t.Fatalf("expected low-relevance tool call to be repaired, got %#v", resp.ToolCalls)
	}
	if fake.callCount != 2 {
		t.Fatalf("expected one repair retry, got %d calls", fake.callCount)
	}
	if len(fake.inputs) < 2 || !strings.Contains(fake.inputs[1][1].Content, "相关性较低") {
		t.Fatalf("expected repair prompt to include relevance problem, got %#v", fake.inputs)
	}
}

func TestTextToolCallModelWrapper_RetriesNoAvailableSkillText(t *testing.T) {
	fake := &fakeTextToolCallModel{
		responses: []*schema.Message{
			schema.AssistantMessage("没有可用的技能来检查当前工程中是否存在图层。", nil),
			schema.AssistantMessage(`{"tool":"get_layers_info","arguments":{}}`, nil),
		},
	}
	wrapped, err := NewTextToolCallModelWrapper(fake).WithTools([]*schema.ToolInfo{{
		Name: "get_layers_info",
		Desc: "获取当前项目中所有图层的详细信息列表",
	}})
	if err != nil {
		t.Fatalf("WithTools failed: %v", err)
	}

	resp, err := wrapped.Generate(context.Background(), []*schema.Message{schema.UserMessage("现在我的工程里面有图层吗")})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "get_layers_info" {
		t.Fatalf("expected get_layers_info tool call after retry, got %#v", resp.ToolCalls)
	}
	if resp.Content != "" {
		t.Fatalf("expected converted content to be empty, got %q", resp.Content)
	}
}

func TestTextToolCallModelWrapper_StreamUsesToolsFromModelOptions(t *testing.T) {
	fake := &fakeTextToolCallModel{
		response: schema.AssistantMessage(`{"tool":"layout_create","arguments":{"name":"test"}}`, nil),
	}
	wrapped := NewTextToolCallModelWrapper(fake)

	stream, err := wrapped.Stream(
		context.Background(),
		[]*schema.Message{schema.UserMessage("新建 test 布局")},
		model.WithTools([]*schema.ToolInfo{{Name: "layout_create"}}),
	)
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "layout_create" {
		t.Fatalf("expected layout_create tool call from stream options, got %#v", resp.ToolCalls)
	}
	if fake.streamCount != 0 {
		t.Fatalf("expected Stream to use Generate adaptation path, got %d raw stream calls", fake.streamCount)
	}
}

func TestTextToolCallModelWrapper_StreamUsesToolCallAdapter(t *testing.T) {
	fake := &fakeTextToolCallModel{
		response: schema.AssistantMessage("没有可用的技能来检查当前工程中是否存在图层。", nil),
		responses: []*schema.Message{
			schema.AssistantMessage("没有可用的技能来检查当前工程中是否存在图层。", nil),
			schema.AssistantMessage(`{"tool":"get_layers_info","arguments":{}}`, nil),
		},
	}
	wrapped, err := NewTextToolCallModelWrapper(fake).WithTools([]*schema.ToolInfo{{
		Name: "get_layers_info",
		Desc: "获取当前项目中所有图层的详细信息列表",
	}})
	if err != nil {
		t.Fatalf("WithTools failed: %v", err)
	}

	stream, err := wrapped.Stream(context.Background(), []*schema.Message{schema.UserMessage("现在我的工程里面有图层吗")})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "get_layers_info" {
		t.Fatalf("expected get_layers_info tool call from stream adapter, got %#v", resp.ToolCalls)
	}
	if fake.streamCount != 0 {
		t.Fatalf("expected Stream to use Generate adaptation path, got %d raw stream calls", fake.streamCount)
	}
}

func buildTestToolNames(names ...string) map[string]string {
	tools := make([]*schema.ToolInfo, 0, len(names))
	for _, name := range names {
		tools = append(tools, &schema.ToolInfo{Name: name})
	}
	return buildToolNameLookup(tools)
}

type fakeTextToolCallModel struct {
	response    *schema.Message
	responses   []*schema.Message
	callCount   int
	streamCount int
	lastInput   []*schema.Message
	inputs      [][]*schema.Message
}

func (f *fakeTextToolCallModel) Generate(_ context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	f.lastInput = input
	f.inputs = append(f.inputs, input)
	f.callCount++
	if len(f.responses) > 0 {
		idx := f.callCount - 1
		if idx >= len(f.responses) {
			idx = len(f.responses) - 1
		}
		return f.responses[idx], nil
	}
	return f.response, nil
}

func (f *fakeTextToolCallModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	f.streamCount++
	return nil, errors.New("not implemented")
}

func (f *fakeTextToolCallModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}
