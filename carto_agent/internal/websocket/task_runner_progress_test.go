package websocket

import (
	"context"
	"fmt"
	"testing"

	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/autocarto/carto_agent/internal/session"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func runtimeProgressTestMessage(attempt int, elapsed int64, status string) *schema.Message {
	p := knowledge.Progress{Stage: "boundary", ID: "c0001/c0002", Attempt: attempt, Status: status, StartedAt: "2026-09-15T08:00:00Z", ElapsedMillis: elapsed}
	msg := schema.AssistantMessage(p.Text(), nil)
	msg.Extra = map[string]any{knowledge.ProgressExtraKey: p.EventData()}
	return msg
}

func TestRuntimeProgressSurvivesToolActivityAndCoalescesHeartbeats(t *testing.T) {
	s := NewDeepServer(0, nil)
	sid := "progress-test"
	s.setSessionAgent(nil, sid, "knowledge_engineer")
	emitted := map[string]struct{}{}
	// This used to discard all custom-agent text after a task/tool call.
	for i := 0; i < 1000; i++ {
		s.emitAssistantWorkflowEvents(nil, sid, runtimeProgressTestMessage(1, int64(i)*20000, "running"), "building", true, false, emitted)
	}
	s.emitAssistantWorkflowEvents(nil, sid, runtimeProgressTestMessage(1, 20000000, "response_received"), "building", true, false, emitted)
	s.emitAssistantWorkflowEvents(nil, sid, runtimeProgressTestMessage(2, 0, "running"), "building", true, false, emitted)
	projection := s.sessionRuntimeProjectionSnapshot(sid, nil)
	if len(projection.WorkflowBlocks) != 1 || len(projection.WorkflowBlocks[0].Entries) != 2 {
		t.Fatalf("heartbeats lost or accumulated: %+v", projection.WorkflowBlocks)
	}
	for i, entry := range projection.WorkflowBlocks[0].Entries {
		if entry.Phase != "progress" || entry.Type != "status" || entry.Data["thinking_kind"] != nil {
			t.Fatalf("progress misrepresented as reasoning: %+v", entry)
		}
		if fmt.Sprint(entry.Data["attempt"]) != fmt.Sprint(i+1) {
			t.Fatalf("attempt identity lost: %+v", entry)
		}
	}
	if projection.WorkflowBlocks[0].Entries[0].Data["status"] != "response_received" {
		t.Fatal("request completion did not replace its running heartbeat")
	}
	// Unmarked assistant prose still follows the pre-existing tool filter.
	s.emitAssistantWorkflowEvents(nil, sid, schema.AssistantMessage("ordinary final prose", nil), "building", true, false, emitted)
	if len(s.sessionRuntimeProjectionSnapshot(sid, nil).WorkflowBlocks[0].Entries) != 2 {
		t.Fatal("ordinary assistant prose bypassed tool filter")
	}
}

func TestRuntimeProgressDoesNotReplaceFinalAnswerOrSessionHistory(t *testing.T) {
	s := NewDeepServer(0, nil)
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Send(adk.EventFromMessage(schema.AssistantMessage("actual answer", nil), nil, schema.Assistant, ""))
	gen.Send(adk.EventFromMessage(runtimeProgressTestMessage(1, 20000, "response_received"), nil, schema.Assistant, ""))
	gen.Close()
	sess := session.NewSession("progress-final-test", 20)
	result := s.consumeEvents(context.Background(), nil, iter, nil, "", sess.ID, "", sess, consumeEventsOptions{persistAssistant: true})
	if result.Err != nil || result.LastContent != "actual answer" || len(sess.Messages) != 1 || sess.Messages[0].Content != "actual answer" {
		t.Fatalf("progress leaked into final result/history: %+v %+v", result, sess.Messages)
	}
}
