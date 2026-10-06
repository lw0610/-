package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/compute"
	"github.com/autocarto/carto_agent/internal/hitl"
	"github.com/autocarto/carto_agent/internal/runtimecontrol"
	"github.com/autocarto/carto_agent/internal/runtimepersist"
	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Load the real shipped contract, then use the same registry/engine/prepare
// path as the desktop agent. No model service, user settings or QGIS is needed.
func registeredHITLTools(t *testing.T, prepared bool) []einotool.BaseTool {
	t.Helper()
	root := t.TempDir()
	contract, err := os.ReadFile(filepath.Join("..", "..", "..", "autocarto", "tools", "system", "runtime-control", "hitl_request.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	writeRuntimeFile(t, filepath.Join(root, "tools", "runtime-control", "hitl_request.yaml"), string(contract))
	writeRuntimeFile(t, filepath.Join(root, "skills", "runtime-control", "SKILL.md"), "---\nname: runtime-control\ndescription: Runtime control\nkind: category\ntools:\n  - hitl_request\n---\n")
	toolRegistry := toolpkg.NewRegistry()
	if err := toolRegistry.LoadDirectories(filepath.Join(root, "tools")); err != nil {
		t.Fatal(err)
	}
	registry := skill.NewRegistry(toolRegistry)
	if err := registry.LoadDirectories(filepath.Join(root, "skills")); err != nil {
		t.Fatal(err)
	}
	engine, err := buildRuntimeSkillEngine(registry, skill.BuildOptions{
		ComputeClient:   compute.NewWSClient(noopComputeInvoker{}, "", time.Second),
		InternalInvoker: runtimecontrol.Invoke,
	})
	if err != nil {
		t.Fatal(err)
	}
	tools, err := skill.BuildToolsFromRegistry(registry, engine)
	if err != nil {
		t.Fatal(err)
	}
	if prepared {
		tools = prepareAgentTools(tools)
	}
	return tools
}

func TestRegisteredHITLRetainsStructuredRequestAndResume(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		for _, kind := range []string{"clarification", "decision", "plan_confirmation"} {
			t.Run(fmt.Sprintf("native=%t/%s", prepared, kind), func(t *testing.T) {
				tools := registeredHITLTools(t, prepared)
				request := hitl.RequestInput{
					Type: kind, Title: "具体问题", Description: "选择数据来源", SelectMode: "multiple",
					Options: []hitl.RequestOption{{Label: "公开来源", Value: "public", Reason: "覆盖目标区域"}},
					Default: "public", Defaults: []string{"public"}, Required: true, Category: "source",
					Plan:      &hitl.RequestPlan{Objective: "准备研究区", Steps: []hitl.RequestPlanStep{{Agent: "data_engineer", Action: "核验边界", Impact: "本地输出"}}, Risks: []string{"版本差异"}, Fallback: "报告缺口"},
					Decisions: []hitl.DecisionItem{{ID: "source", Title: "来源", Description: "选择已核实数据", SelectMode: "single", Options: []hitl.RequestOption{{Label: "公开来源", Value: "public", Reason: "已核实"}}, Default: "public", Defaults: []string{"public"}, Required: true, Category: "data"}},
				}
				args, err := json.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				probe := &registeredHITLModel{args: string(args)}
				ctx := context.Background()
				var exposed []*schema.ToolInfo
				for _, item := range tools {
					info, err := item.Info(ctx)
					if err != nil {
						t.Fatal(err)
					}
					exposed = append(exposed, info)
				}
				if _, err := probe.WithTools(exposed); err != nil {
					t.Fatal(err)
				}
				cfg := adk.ToolsConfig{}
				cfg.Tools = tools
				agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{Name: "hitl_probe", Description: "test", Model: probe, ToolsConfig: cfg})
				if err != nil {
					t.Fatal(err)
				}
				runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, CheckPointStore: runtimepersist.NewMemoryCheckpointStore(nil)})
				iter := runner.Query(ctx, "test", adk.WithCheckPointID("registered-hitl"))
				var interruptID string
				for event, ok := iter.Next(); ok; event, ok = iter.Next() {
					if event.Err != nil {
						t.Fatal(event.Err)
					}
					if event.Action == nil || event.Action.Interrupted == nil {
						continue
					}
					interrupt := event.Action.Interrupted.InterruptContexts[0]
					payload, ok := interrupt.Info.(hitl.InterruptPayload)
					if !ok {
						t.Fatalf("unexpected interrupt payload: %T", interrupt.Info)
					}
					if payload.Type != kind || payload.Title != request.Title || payload.Description != request.Description || payload.SelectMode != request.SelectMode || payload.Default != request.Default || payload.Required != request.Required || payload.Category != request.Category {
						t.Fatalf("request fields lost: %#v", payload)
					}
					if !reflect.DeepEqual(payload.Options, request.Options) || !reflect.DeepEqual(payload.Defaults, request.Defaults) || !reflect.DeepEqual(payload.Plan, request.Plan) || !reflect.DeepEqual(payload.Decisions, request.Decisions) {
						t.Fatalf("nested request fields lost: %#v", payload)
					}
					interruptID = interrupt.ID
					break
				}
				if interruptID == "" {
					t.Fatal("expected actual tool interrupt")
				}
				if prepared {
					for _, key := range []string{`"options"`, `"reason"`, `"decisions"`, `"steps"`, `"impact"`} {
						if !strings.Contains(probe.schema, key) {
							t.Fatalf("native nested parameter schema missing %s: %s", key, probe.schema)
						}
					}
				}
				iter, err = runner.ResumeWithParams(ctx, "registered-hitl", &adk.ResumeParams{Targets: map[string]any{interruptID: map[string]any{"decisions": map[string]any{"source": map[string]any{"selected": []string{"public"}, "text": "继续", "confirmed": true}}}}})
				if err != nil {
					t.Fatal(err)
				}
				for event, ok := iter.Next(); ok; event, ok = iter.Next() {
					if event.Err != nil {
						t.Fatal(event.Err)
					}
					if event.Action != nil && event.Action.Interrupted != nil {
						t.Fatal("approved request interrupted again")
					}
				}
				if !probe.resumed {
					t.Fatal("structured decision did not reach the model on resume")
				}
			})
		}
	}
}

func TestPrepareAgentToolsDoesNotGrantHITL(t *testing.T) {
	for _, tools := range [][]einotool.BaseTool{nil, {&fakeAgentTool{name: "get_project_info"}}} {
		for _, item := range prepareAgentTools(tools) {
			info, err := item.Info(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if info.Name == runtimecontrol.ToolHITLRequest {
				t.Fatal("prepare added unregistered HITL capability")
			}
		}
	}
}

type registeredHITLModel struct {
	args    string
	schema  string
	called  bool
	resumed bool
}

func (m *registeredHITLModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	for _, info := range tools {
		if info.Name == runtimecontrol.ToolHITLRequest {
			value, err := info.ParamsOneOf.ToJSONSchema()
			if err != nil {
				return nil, err
			}
			data, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			m.schema = string(data)
		}
	}
	return m, nil
}

func (m *registeredHITLModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	if !m.called {
		m.called = true
		return schema.AssistantMessage("", []schema.ToolCall{{ID: "request-1", Function: schema.FunctionCall{Name: runtimecontrol.ToolHITLRequest, Arguments: m.args}}}), nil
	}
	for _, msg := range messages {
		text := msg.Content
		for _, part := range msg.UserInputMultiContent {
			text += part.Text
		}
		if msg.Role == schema.Tool && strings.Contains(text, `"approved":true`) && strings.Contains(text, `"selected":["public"]`) && strings.Contains(text, `"confirmed":true`) {
			m.resumed = true
			return schema.AssistantMessage("完成", nil), nil
		}
	}
	return nil, fmt.Errorf("resumed decision payload missing")
}

func (m *registeredHITLModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("stream not used in HITL test")
}
