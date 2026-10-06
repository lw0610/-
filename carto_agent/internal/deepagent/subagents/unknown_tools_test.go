package subagents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type unknownToolTestTool struct {
	name string
	run  func(context.Context, string) (string, error)
}

func (t *unknownToolTestTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: "offline regression fixture"}, nil
}

func (t *unknownToolTestTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	return t.run(ctx, input)
}

type unknownToolModelStep func(context.Context, []*schema.Message, []*schema.ToolInfo) (*schema.Message, error)

type unknownToolScript struct {
	mu    sync.Mutex
	steps []unknownToolModelStep
	calls int
}

type unknownToolScriptModel struct {
	script *unknownToolScript
	tools  []*schema.ToolInfo
}

func (m *unknownToolScriptModel) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return &unknownToolScriptModel{script: m.script, tools: infos}, nil
}

func (m *unknownToolScriptModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.script.mu.Lock()
	defer m.script.mu.Unlock()
	if m.script.calls >= len(m.script.steps) {
		return nil, fmt.Errorf("unexpected model call after %d scripted steps", m.script.calls)
	}
	step := m.script.steps[m.script.calls]
	m.script.calls++
	infos := model.GetCommonOptions(&model.Options{Tools: m.tools}, opts...).Tools
	return step(ctx, input, infos)
}

func (m *unknownToolScriptModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func unknownToolCall(id, name, args string) *schema.Message {
	return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
		ID: id, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args},
	}}}
}

func unknownToolObservation(input []*schema.Message, callID string) (*schema.Message, error) {
	for _, message := range input {
		if message.Role == schema.Tool && message.ToolCallID == callID {
			return message, nil
		}
	}
	return nil, fmt.Errorf("model did not receive tool observation for %q", callID)
}

func unknownToolGuidance(input []*schema.Message, callID string) (string, error) {
	message, err := unknownToolObservation(input, callID)
	if err != nil {
		return "", err
	}
	var observation struct {
		Success  *bool  `json:"success"`
		Executed *bool  `json:"executed"`
		Guidance string `json:"guidance"`
		Error    struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(message.Content), &observation); err != nil {
		return "", fmt.Errorf("invalid unknown-tool JSON: %w", err)
	}
	if observation.Success == nil || *observation.Success || observation.Executed == nil || *observation.Executed {
		return "", fmt.Errorf("unknown tool was not explicitly reported unsuccessful and unexecuted: %s", message.Content)
	}
	if observation.Error.Code != "TOOL_NOT_AVAILABLE" || observation.Error.Message == "" || observation.Guidance == "" {
		return "", fmt.Errorf("missing actionable unknown-tool error: %s", message.Content)
	}
	return observation.Guidance, nil
}

func runUnknownToolAgent(ctx context.Context, agent adk.Agent, streaming bool) (string, error) {
	iter := agent.Run(ctx, &adk.AgentInput{
		Messages: []adk.Message{schema.UserMessage("Run the offline tool recovery scenario.")}, EnableStreaming: streaming,
	})
	var final string
	var runErr error
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			runErr = errors.Join(runErr, event.Err)
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		message, err := event.Output.MessageOutput.GetMessage()
		if err != nil {
			runErr = errors.Join(runErr, err)
			continue
		}
		if message != nil && message.Role == schema.Assistant && len(message.ToolCalls) == 0 {
			final = message.Content
		}
	}
	return final, runErr
}

func TestGenericAgentRecoversUnknownBrowserTool(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("streaming=%v", streaming), func(t *testing.T) {
			var listed, executed atomic.Int32
			tools := []tool.BaseTool{
				&unknownToolTestTool{name: "browser_list_tools", run: func(context.Context, string) (string, error) {
					listed.Add(1)
					return `{"tools":[{"name":"browser_navigate","inputSchema":{"type":"object","properties":{"url":{"type":"string"}}}}]}`, nil
				}},
				&unknownToolTestTool{name: "browser_call_tool", run: func(_ context.Context, input string) (string, error) {
					if listed.Load() != 1 {
						return "", errors.New("browser wrapper ran before tool discovery")
					}
					var args struct {
						ToolName string `json:"tool_name"`
						Args     struct {
							URL string `json:"url"`
						} `json:"args"`
					}
					if err := json.Unmarshal([]byte(input), &args); err != nil {
						return "", err
					}
					if args.ToolName != "browser_navigate" || args.Args.URL != "https://example.invalid/offline" {
						return "", fmt.Errorf("wrong wrapper arguments: %s", input)
					}
					executed.Add(1)
					return `{"success":true,"result":"offline navigation completed"}`, nil
				}},
			}
			script := &unknownToolScript{steps: []unknownToolModelStep{
				func(context.Context, []*schema.Message, []*schema.ToolInfo) (*schema.Message, error) {
					return unknownToolCall("wrong-browser-name", "browser_navigate", `{"url":"https://example.invalid/offline"}`), nil
				},
				func(_ context.Context, input []*schema.Message, _ []*schema.ToolInfo) (*schema.Message, error) {
					guidance, err := unknownToolGuidance(input, "wrong-browser-name")
					if err != nil {
						return nil, err
					}
					for _, token := range []string{"browser_list_tools", "inputSchema", "browser_call_tool", "tool_name", "args"} {
						if !strings.Contains(guidance, token) {
							return nil, fmt.Errorf("browser correction omitted %q: %s", token, guidance)
						}
					}
					if listed.Load() != 0 || executed.Load() != 0 {
						return nil, errors.New("unknown tool call executed or forwarded a browser tool")
					}
					return unknownToolCall("discover-browser-tools", "browser_list_tools", `{}`), nil
				},
				func(_ context.Context, input []*schema.Message, _ []*schema.ToolInfo) (*schema.Message, error) {
					if _, err := unknownToolObservation(input, "discover-browser-tools"); err != nil {
						return nil, err
					}
					return unknownToolCall("correct-browser-call", "browser_call_tool", `{"tool_name":"browser_navigate","args":{"url":"https://example.invalid/offline"}}`), nil
				},
				func(_ context.Context, input []*schema.Message, _ []*schema.ToolInfo) (*schema.Message, error) {
					result, err := unknownToolObservation(input, "correct-browser-call")
					if err != nil {
						return nil, err
					}
					if !strings.Contains(result.Content, `"success":true`) {
						return nil, fmt.Errorf("wrapper did not succeed: %s", result.Content)
					}
					return schema.AssistantMessage("browser recovery completed", nil), nil
				},
			}}
			agent, err := NewGenericAgent(context.Background(), &GenericAgentConfig{
				Name: "browser-recovery", ChatModel: &unknownToolScriptModel{script: script}, Tools: tools, MaxIteration: 5,
			})
			if err != nil {
				t.Fatal(err)
			}
			final, err := runUnknownToolAgent(context.Background(), agent, streaming)
			if err != nil {
				t.Fatalf("unknown tool prevented recovery: %v", err)
			}
			if final != "browser recovery completed" || listed.Load() != 1 || executed.Load() != 1 {
				t.Fatalf("incomplete recovery: final=%q discoveries=%d executions=%d", final, listed.Load(), executed.Load())
			}
		})
	}
}

func TestGenericAgentUnknownBrowserToolCannotEscalateRole(t *testing.T) {
	var safeCalls atomic.Int32
	safeTool := &unknownToolTestTool{name: "get_project_info", run: func(context.Context, string) (string, error) {
		safeCalls.Add(1)
		return `{"success":true}`, nil
	}}
	script := &unknownToolScript{steps: []unknownToolModelStep{
		func(context.Context, []*schema.Message, []*schema.ToolInfo) (*schema.Message, error) {
			return unknownToolCall("disallowed-browser-name", "browser_navigate", `{}`), nil
		},
		func(_ context.Context, input []*schema.Message, infos []*schema.ToolInfo) (*schema.Message, error) {
			guidance, err := unknownToolGuidance(input, "disallowed-browser-name")
			if err != nil {
				return nil, err
			}
			if strings.Contains(guidance, "browser_call_tool") || strings.Contains(guidance, "browser_list_tools") {
				return nil, fmt.Errorf("role without browser permission received a wrapper shortcut: %s", guidance)
			}
			if len(infos) != 1 || infos[0].Name != "get_project_info" {
				return nil, fmt.Errorf("unknown tool altered the role catalog: %#v", infos)
			}
			return unknownToolCall("disallowed-wrapper", "browser_call_tool", `{"tool_name":"browser_navigate","args":{}}`), nil
		},
		func(_ context.Context, input []*schema.Message, _ []*schema.ToolInfo) (*schema.Message, error) {
			if _, err := unknownToolGuidance(input, "disallowed-wrapper"); err != nil {
				return nil, err
			}
			return schema.AssistantMessage("browser capability unavailable", nil), nil
		},
	}}
	agent, err := NewGenericAgent(context.Background(), &GenericAgentConfig{
		Name: "restricted-role", ChatModel: &unknownToolScriptModel{script: script}, Tools: []tool.BaseTool{safeTool}, MaxIteration: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	final, err := runUnknownToolAgent(context.Background(), agent, false)
	if err != nil || final != "browser capability unavailable" || safeCalls.Load() != 0 {
		t.Fatalf("restricted role recovery: final=%q tool calls=%d error=%v", final, safeCalls.Load(), err)
	}
}

func TestGenericAgentRegisteredToolErrorRemainsFatal(t *testing.T) {
	sentinel := errors.New("registered browser wrapper failed")
	var calls atomic.Int32
	script := &unknownToolScript{steps: []unknownToolModelStep{
		func(context.Context, []*schema.Message, []*schema.ToolInfo) (*schema.Message, error) {
			return unknownToolCall("registered-failure", "browser_call_tool", `{}`), nil
		},
	}}
	agent, err := NewGenericAgent(context.Background(), &GenericAgentConfig{
		Name: "registered-error", ChatModel: &unknownToolScriptModel{script: script},
		Tools: []tool.BaseTool{&unknownToolTestTool{name: "browser_call_tool", run: func(context.Context, string) (string, error) {
			calls.Add(1)
			return "", sentinel
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runUnknownToolAgent(context.Background(), agent, false)
	if !errors.Is(err, sentinel) || calls.Load() != 1 {
		t.Fatalf("registered tool error was swallowed: calls=%d error=%v", calls.Load(), err)
	}
}

func TestUnknownToolsHandlerPreservesContextCancellation(t *testing.T) {
	handler := NewUnknownToolsHandler(context.Background(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	observation, err := handler(ctx, "browser_navigate", `{}`)
	if !errors.Is(err, context.Canceled) || observation != "" {
		t.Fatalf("cancellation became an ordinary observation: output=%q error=%v", observation, err)
	}
}

func TestGenericAgentUnknownToolPreservesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	script := &unknownToolScript{steps: []unknownToolModelStep{
		func(context.Context, []*schema.Message, []*schema.ToolInfo) (*schema.Message, error) {
			cancel()
			return unknownToolCall("cancelled-unknown", "browser_navigate", `{}`), nil
		},
	}}
	agent, err := NewGenericAgent(context.Background(), &GenericAgentConfig{
		Name: "cancelled-recovery", ChatModel: &unknownToolScriptModel{script: script},
		Tools: []tool.BaseTool{&unknownToolTestTool{name: "browser_call_tool", run: func(context.Context, string) (string, error) {
			calls.Add(1)
			return `{"success":true}`, nil
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runUnknownToolAgent(ctx, agent, false)
	if !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("agent did not preserve cancellation: calls=%d error=%v", calls.Load(), err)
	}
}
