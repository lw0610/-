package flowrun

import (
	"context"
	"testing"

	"github.com/autocarto/carto_agent/internal/hitl"
	"github.com/autocarto/carto_agent/internal/runtimepersist"
	"github.com/cloudwego/eino/adk"
)

func TestConfirmAgentUsesExistingADKCheckpointAndHITLPayload(t *testing.T) {
	ctx := context.Background()
	agent := NewConfirmAgent(confirmationForStep("请选择下一步", "流程确认", true))
	if agent.Name(ctx) == "" || agent.Description(ctx) == "" {
		t.Fatal("agent metadata missing")
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, CheckPointStore: runtimepersist.NewMemoryCheckpointStore(nil)})
	iter := runner.Query(ctx, "确认", adk.WithCheckPointID("flow-confirm"))
	var paused *adk.AgentEvent
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			paused = event
		}
	}
	if paused == nil || len(paused.Action.Interrupted.InterruptContexts) == 0 {
		t.Fatal("no stateful interrupt")
	}
	interrupt := paused.Action.Interrupted.InterruptContexts[0]
	payload, ok := interrupt.Info.(hitl.InterruptPayload)
	if !ok || payload.Type != "decision" || payload.SelectMode != "single" || len(payload.Options) != 3 {
		t.Fatalf("%#v", interrupt.Info)
	}
	resumed, err := runner.ResumeWithParams(ctx, "flow-confirm", &adk.ResumeParams{Targets: map[string]any{interrupt.ID: map[string]any{"decisions": map[string]any{"flow": map[string]any{"selected": []string{"retry"}, "text": "补充条件"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	last := ""
	for {
		event, ok := resumed.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		if event.Output != nil && event.Output.MessageOutput != nil {
			message, err := event.Output.MessageOutput.GetMessage()
			if err != nil {
				t.Fatal(err)
			}
			last = message.Content
		}
	}
	choice, err := parseChoice(last)
	if err != nil || choice.Choice != "retry" || choice.Guidance != "补充条件" {
		t.Fatal(last, err)
	}
}
