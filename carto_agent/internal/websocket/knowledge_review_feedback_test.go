package websocket

import (
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/session"
)

func TestKnowledgeReviewFeedbackReplacesCardAndPreservesExecutionState(t *testing.T) {
	sess := session.NewSession("review-feedback", 20)
	server := NewDeepServer(0, nil)
	server.markSessionRuntimeRunning(sess.ID, "checkpoint", executionModeBuild, "building", "", "")
	server.applySessionWorkflowMessage(Message{Type: MsgTypeAgentWorkflow, SessionID: sess.ID, Agent: "system_route_knowledge", Phase: "thinking", Content: "抽取原文"})
	states := []struct{ status, block string }{{"awaiting_review", "waiting_user"}, {"compiling", "running"}, {"compile_failed", "error"}, {"compiling", "running"}, {"compiled", "completed"}}
	for _, state := range states {
		info := map[string]any{"type": "knowledge_review", "review_id": "review-1", "session_id": sess.ID, "status": state.status, "revision": 2, "description": "route_style is required"}
		data := knowledgeReviewUpdateData(sess, info, "checkpoint")
		upsertKnowledgeReviewCard(sess, data)
		upsertKnowledgeReviewCard(sess, data) // replay cannot duplicate a card
		kind := "knowledge_review_update"
		if state.status == "awaiting_review" {
			kind = MsgTypeInterruptRequest
		}
		message := Message{Type: kind, SessionID: sess.ID, Data: data}
		server.applySessionWorkflowMessage(message)
		server.applySessionWorkflowMessage(message)
		messages := sess.GetMessages()
		if len(messages) != 1 {
			t.Fatalf("duplicate review cards: %d", len(messages))
		}
		card := messages[0].Extra["knowledge_review_card"].(map[string]any)
		if knowledgeReviewInterruptInfo(card)["status"] != state.status {
			t.Fatalf("stale card: %#v", card)
		}
		projection := server.sessionRuntimeProjectionSnapshot(sess.ID, nil)
		if projection.RuntimeState.Status != sessionRuntimeStatusRunning {
			t.Fatalf("review update stopped overall execution: %+v", projection.RuntimeState)
		}
		if len(projection.WorkflowBlocks) != 1 || projection.WorkflowBlocks[0].Status != state.block {
			t.Fatalf("wrong process state: %#v", projection.WorkflowBlocks)
		}
		if len(projection.WorkflowBlocks[0].Entries) != 2 {
			t.Fatalf("replayed review produced duplicate process rows: %#v", projection.WorkflowBlocks[0].Entries)
		}
		if state.status == "compile_failed" && !strings.Contains(projection.RuntimeSummary, "route_style is required") {
			t.Fatal("failure detail missing")
		}
	}
}
