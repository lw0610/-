package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/capability"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"testing"
)

type l30BadModel struct{ calls int }

func (m *l30BadModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.calls++
	return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: fmt.Sprint(m.calls), Type: "function", Function: schema.FunctionCall{Name: "data_find", Arguments: `{"need":{"what":"description"}}`}}}}, nil
}
func (m *l30BadModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("not streaming")
}
func (m *l30BadModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}
func TestL30ModelStopsAfterThirdMalformedDataArgument(t *testing.T) {
	s := dataservice.New(dataservice.Options{})
	m := &l30BadModel{}
	invoke := &fakeEnhancedTool{info: &schema.ToolInfo{Name: "data_find"}, runFn: func(ctx context.Context, a *schema.ToolArgument, _ ...tool.Option) (*schema.ToolResult, error) {
		var args map[string]any
		json.Unmarshal([]byte(a.Text), &args)
		_, e := s.Invoke(ctx, "data_find", args)
		return nil, e
	}}
	observer := &recordingObserver{}
	wrapped := wrapToolWithErrorHandler(invoke, &capability.ControlPlane{Observer: observer})
	a, e := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{Name: "guard", Description: "guard", Model: m, MaxIterations: 10, ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{wrapped}}}})
	if e != nil {
		t.Fatal(e)
	}
	iter := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: a}).Run(context.Background(), []*schema.Message{schema.UserMessage("find")})
	failed := false
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			failed = true
		}
	}
	if len(observer.ends) != 3 || observer.ends[2].Success {
		t.Fatal("terminal invocation has no failed end observation", observer.ends)
	}
	if !failed || m.calls != 3 {
		t.Fatalf("failed=%v model requests=%d", failed, m.calls)
	}
}
