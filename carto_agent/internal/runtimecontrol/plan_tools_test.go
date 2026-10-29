package runtimecontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"testing"
)

type submitPlanFixture struct {
	calls       int
	rejectFirst bool
}

func (s *submitPlanFixture) ProposePlan(_ context.Context, raw json.RawMessage) (map[string]any, error) {
	s.calls++
	if s.rejectFirst && s.calls == 1 {
		return nil, fmt.Errorf("PLAN_SCHEMA_INVALID: fix plan")
	}
	return map[string]any{"accepted": true}, nil
}
func (s *submitPlanFixture) RevisePlan(c context.Context, r json.RawMessage) (map[string]any, error) {
	return s.ProposePlan(c, r)
}

type submitPlanModel struct {
	calls int
	name  string
}

func (m *submitPlanModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}
func (m *submitPlanModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.calls++
	if m.calls > 2 {
		return nil, fmt.Errorf("planning continued after acceptance")
	}
	return schema.AssistantMessage("", []schema.ToolCall{{ID: fmt.Sprint(m.calls), Type: "function", Function: schema.FunctionCall{Name: m.name, Arguments: `{"plan":{"plan_id":"test"}}`}}}), nil
}
func (m *submitPlanModel) Stream(c context.Context, msg []*schema.Message, o ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	r, e := m.Generate(c, msg, o...)
	if e != nil {
		return nil, e
	}
	return schema.StreamReaderFromArray([]*schema.Message{r}), nil
}
func TestPlanSubmissionEndsRoundOnlyAfterAcceptance(t *testing.T) {
	for _, name := range []string{ToolPlanPropose, ToolPlanRevise} {
		for _, reject := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reject=%v", name, reject), func(t *testing.T) {
				service := &submitPlanFixture{rejectFirst: reject}
				cm := &submitPlanModel{name: name}
				ctx := WithPlanService(context.Background(), service)
				submit := utils.NewTool(&schema.ToolInfo{Name: name, Desc: "submit plan"}, func(c context.Context, a map[string]any) (string, error) {
					r, e := Invoke(c, name, a)
					if e != nil {
						return `{"error":"invalid plan"}`, nil
					}
					b, _ := json.Marshal(r)
					return string(b), nil
				})
				agent, e := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{Name: "planner", Description: "planner", Model: WithPlanSubmissionBoundary(cm), ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{submit}}}, MaxIterations: 4})
				if e != nil {
					t.Fatal(e)
				}
				iter := agent.Run(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage("prepare")}})
				exited := false
				for {
					ev, ok := iter.Next()
					if !ok {
						break
					}
					if ev.Err != nil {
						t.Fatal(ev.Err)
					}
					if ev.Action != nil && ev.Action.Exit {
						exited = true
					}
				}
				want := 1
				if reject {
					want = 2
				}
				if cm.calls != want || service.calls != want || !exited {
					t.Fatalf("model=%d submit=%d exit=%v", cm.calls, service.calls, exited)
				}
			})
		}
	}
}
