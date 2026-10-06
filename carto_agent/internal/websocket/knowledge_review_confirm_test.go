package websocket

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/cloudwego/eino/adk"
)

func TestStartMappingAcceptsSavedRevisionWithoutContentPrevalidation(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	ctx := context.Background()
	agent := &testExecutionModeAgent{name: "accepted-review"}
	server := NewDeepServer(0, agent)
	store := knowledge.NewReviewStore(t.TempDir())
	server.knowledgeReviews = store
	source := "已抽取原文"
	hash := sha256.Sum256([]byte(source))
	// Deliberately old protocol and incomplete content would fail prevalidation.
	// The click still accepts the user's saved version; operational failures in
	// the resumed task are a separate state transition, never a confirmation 422.
	graph := knowledge.Graph{"routes": []any{}}
	review, err := store.Create(knowledge.ReviewCreateInput{SessionID: "accept-content", SourceText: source, Contract: knowledge.Contract{ContractID: "legacy", SourceSHA256: hex.EncodeToString(hash[:]), JSONSchema: json.RawMessage(`{}`)}, OriginalResult: &knowledge.Result{Graph: graph}, Graph: graph})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := "accepted-checkpoint"
	if err = server.checkpointStore.Set(ctx, checkpoint, []byte(`invalid checkpoint payload`)); err != nil {
		t.Fatal(err)
	}
	_, err = store.SetResume(review.SessionID, review.ReviewID, &knowledge.ReviewResumeDescriptor{CheckpointID: checkpoint, InterruptID: "root"})
	if err != nil {
		t.Fatal(err)
	}
	pending := &PendingInterrupt{CheckpointID: checkpoint, SessionID: review.SessionID, ProjectPath: review.ProjectPath,
		Runner:   adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, CheckPointStore: server.checkpointStore}),
		Session:  server.sessionManager.GetOrCreate(review.SessionID),
		Contexts: []*adk.InterruptCtx{{ID: "root", IsRootCause: true, Info: map[string]any{"type": "knowledge_review", "review_id": review.ReviewID}}},
	}
	server.setPendingInterrupt(nil, review.SessionID, pending)
	server.markSessionRuntimeRunning(review.SessionID, checkpoint, executionModeBuild, "", review.ProjectPath, "")
	server.markSessionRuntimeInterrupted(review.SessionID, checkpoint)
	accepted, err := server.ConfirmKnowledgeReview(ctx, review.SessionID, review.ReviewID, review.Revision, "start")
	if err != nil || accepted.Status != knowledge.ReviewCompiling || accepted.ClaimedRevision != review.Revision {
		t.Fatalf("Start Mapping rejected content: %+v %v", accepted, err)
	}
	// Wait for the deliberately broken checkpoint's background failure before
	// temp-directory cleanup; this also proves a resume was actually attempted.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		latest, err := store.Get(review.SessionID, review.ReviewID)
		if err == nil && latest.Status == knowledge.ReviewCompileFailed {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("accepted task was not resumed")
}
