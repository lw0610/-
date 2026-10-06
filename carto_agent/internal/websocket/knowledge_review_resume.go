package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent"
	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// A single store (and its CAS lock) is shared by HTTP, extraction and Resume.
func (s *DeepServer) KnowledgeReviewStore() *knowledge.ReviewStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.knowledgeReviews == nil {
		data, err := config.ResolvePluginDataDir()
		if err != nil || !filepath.IsAbs(data) {
			return nil
		}
		s.knowledgeReviews = knowledge.NewReviewStore(filepath.Join(data, "knowledge_reviews"))
	}
	return s.knowledgeReviews
}

// A process can stop after claiming a compilation. Do not leave a persisted
// draft permanently read-only, and do not silently repeat external work.
func (s *DeepServer) GetKnowledgeReview(_ context.Context, sid, rid string) (*knowledge.Review, error) {
	s.knowledgeConfirmMu.Lock()
	defer s.knowledgeConfirmMu.Unlock()
	store := s.KnowledgeReviewStore()
	if store == nil {
		return nil, fmt.Errorf("知识核对存储不可用")
	}
	review, err := store.Get(sid, rid)
	if err != nil {
		return nil, err
	}
	return s.reconcileKnowledgeReview(store, review)
}

func (s *DeepServer) reconcileKnowledgeReview(store *knowledge.ReviewStore, review *knowledge.Review) (*knowledge.Review, error) {
	if review.Status != knowledge.ReviewCompiling {
		return review, nil
	}
	state, active := s.activeSessionRuntime(review.SessionID)
	if active && state.Status == sessionRuntimeStatusRunning && review.Resume != nil && state.RunID == review.Resume.CheckpointID {
		return review, nil
	}
	return store.Fail(review.SessionID, review.ReviewID, review.Revision, []knowledge.ReviewError{{Message: "上次编译已中断，修改稿已保留。请先检查已有制图结果，再明确重新确认；系统不会自动重复执行。"}})
}

func knowledgeReviewRef(contexts []*adk.InterruptCtx) (reviewID, interruptID string) {
	for _, ic := range contexts {
		if !ic.IsRootCause {
			continue
		}
		raw, _ := json.Marshal(ic.Info)
		var info struct {
			Type     string `json:"type"`
			ReviewID string `json:"review_id"`
		}
		if json.Unmarshal(raw, &info) == nil && info.Type == "knowledge_review" && info.ReviewID != "" {
			return info.ReviewID, ic.ID
		}
	}
	return "", ""
}

func (s *DeepServer) persistKnowledgeReviewInterrupt(pending *PendingInterrupt, data map[string]any, mode executionMode) error {
	rid, root := knowledgeReviewRef(pending.Contexts)
	if rid == "" {
		return nil
	}
	store := s.KnowledgeReviewStore()
	if store == nil {
		return fmt.Errorf("知识核对存储不可用")
	}
	if mode == "" {
		mode = executionModeBuild
	}
	_, err := store.SetResume(pending.SessionID, rid, &knowledge.ReviewResumeDescriptor{
		CheckpointID: pending.CheckpointID, InterruptID: root, ExecutionMode: string(mode),
		ModePhase: pending.ModePhase, InstanceID: pending.BoundInstanceID, ProjectPath: pending.ProjectPath,
		RuntimeRevision: s.currentRuntimeMetadata().RuntimeRevision,
	})
	if err != nil {
		return err
	}
	if pending.Session != nil {
		for _, msg := range pending.Session.GetMessages() {
			if card, ok := msg.Extra["knowledge_review_card"].(map[string]any); ok && card["checkpoint_id"] == pending.CheckpointID {
				return nil
			}
		}
		message := schema.AssistantMessage("知识抽取结果已保存，可查看并修改。", nil)
		message.Extra = map[string]any{"knowledge_review_card": data}
		pending.Session.AddMessage(message)
	}
	return nil
}

func (s *DeepServer) knowledgeReviewCompiler() tool.BaseTool {
	cfg := s.agentRebuildConfigSnapshot()
	if cfg == nil {
		return nil
	}
	for _, candidate := range cfg.Tools {
		info, err := candidate.Info(context.Background())
		if err == nil && info != nil && info.Name == "marching_route_compile_dataset" {
			return candidate
		}
	}
	return nil
}

// Restore uses the original checkpoint and the current compatible runtime, not a
// fresh model task. Missing/incompatible checkpoints never trigger re-extraction.
func (s *DeepServer) restoreKnowledgeReviewPending(ctx context.Context, review *knowledge.Review) (*PendingInterrupt, error) {
	desc := review.Resume
	if desc == nil || desc.CheckpointID == "" || desc.InterruptID == "" {
		return nil, fmt.Errorf("核对断点尚未准备好，请稍后重试；草稿已保存")
	}
	state, active := s.activeSessionRuntime(review.SessionID)
	if !active {
		// A restart is not permission to revive a task explicitly cancelled earlier.
		saved, _ := s.LoadSession(review.SessionID, review.ProjectPath)
		state = s.sessionRuntimeProjectionSnapshot(review.SessionID, saved).RuntimeState
	}
	if state.RunID != "" && state.RunID != desc.CheckpointID {
		return nil, fmt.Errorf("当前会话已有更新的任务，不能恢复旧核对任务；核对稿仍可查看")
	}
	if state.Status == sessionRuntimeStatusCancelled || state.Status == sessionRuntimeStatusCompleted {
		return nil, fmt.Errorf("原制图任务已结束，不能继续确认；核对稿和已保存的修改仍保留")
	}
	if _, exists, err := s.checkpointStore.Get(ctx, desc.CheckpointID); err != nil {
		return nil, fmt.Errorf("原制图任务配置已变化，不能恢复旧断点；核对稿仍可查看: %w", err)
	} else if !exists {
		return nil, fmt.Errorf("原制图任务的恢复断点已失效；核对稿仍保留，不能静默重新抽取")
	}
	mode := executionMode(desc.ExecutionMode)
	if mode != executionModePlan {
		mode = executionModeBuild
	}
	agent := s.GetAgent(mode)
	if agent == nil {
		return nil, fmt.Errorf("当前模式智能体未初始化")
	}
	sess := s.sessionManager.GetOrCreate(review.SessionID)
	s.hydrateSessionMessages(sess, review.SessionID, review.ProjectPath)
	resumeCtx := deepagent.WithKnowledgeReview(withToolSessionContext(ctx, review.SessionID, review.ProjectPath, review.InstanceID), s.KnowledgeReviewStore())
	pending := &PendingInterrupt{
		CheckpointID: desc.CheckpointID, SessionID: review.SessionID, ProjectPath: review.ProjectPath,
		BoundInstanceID: review.InstanceID, Session: sess, ModePhase: desc.ModePhase,
		Runner:   adk.NewRunner(resumeCtx, adk.RunnerConfig{Agent: agent, CheckPointStore: s.checkpointStore}),
		Contexts: []*adk.InterruptCtx{{ID: desc.InterruptID, IsRootCause: true, Info: deepagent.KnowledgeReviewInfo(review)}},
	}
	s.setPendingInterrupt(nil, review.SessionID, pending)
	s.markSessionRuntimeRunning(review.SessionID, desc.CheckpointID, mode, desc.ModePhase, review.ProjectPath, review.InstanceID)
	s.markSessionRuntimeInterrupted(review.SessionID, desc.CheckpointID)
	return pending, nil
}

// ConfirmKnowledgeReview is used by the HTTP editor and by legacy WS clients.
// Ownership checks and CAS precede consuming the pending interrupt. Socket send
// success is not a confirmation acknowledgement.
func (s *DeepServer) ConfirmKnowledgeReview(ctx context.Context, sid, rid string, revision int64, requestID string) (*knowledge.Review, error) {
	s.knowledgeConfirmMu.Lock()
	defer s.knowledgeConfirmMu.Unlock()
	store := s.KnowledgeReviewStore()
	if store == nil {
		return nil, fmt.Errorf("知识核对存储不可用")
	}
	review, err := store.Get(sid, rid)
	if err != nil {
		return nil, err
	}
	if review.Revision != revision {
		return nil, knowledge.ErrReviewConflict
	}
	if review.Status == knowledge.ReviewCompiling {
		updated, err := s.reconcileKnowledgeReview(store, review)
		if err != nil {
			return nil, err
		}
		if updated.Status != knowledge.ReviewCompiling {
			return updated, fmt.Errorf("%w: 上次编译中断，请先查看保存的修改稿与已有输出后再确认", knowledge.ErrReviewConflict)
		}
	}
	if review.Status == knowledge.ReviewCompiled || review.Status == knowledge.ReviewCompiling {
		return review, nil // Same version already accepted; never launch twice.
	}
	pending, exists := s.getPendingInterrupt(sid)
	if !exists {
		pending, err = s.restoreKnowledgeReviewPending(ctx, review)
		if err != nil {
			return nil, err
		}
	}
	pendingID, rootID := knowledgeReviewRef(pending.Contexts)
	if pendingID != rid || pending.SessionID != sid || pending.ProjectPath != review.ProjectPath || pending.BoundInstanceID != review.InstanceID {
		return nil, fmt.Errorf("核对结果与当前制图任务或实例不一致")
	}
	if _, exists, err := s.checkpointStore.Get(ctx, pending.CheckpointID); err != nil {
		return nil, fmt.Errorf("原制图任务配置已变化，核对稿已保留，不能恢复旧断点: %w", err)
	} else if !exists {
		return nil, fmt.Errorf("原制图任务恢复断点已失效，核对稿已保留")
	}
	if review.InstanceID != "" {
		if _, err := s.resolveSessionBinding(review.InstanceID, review.ProjectPath); err != nil {
			return nil, fmt.Errorf("请先重新连接原制图实例: %w", err)
		}
	}
	// Start Mapping accepts this exact saved revision. Semantic/content checks
	// remain available as diagnostics, but never veto the user's confirmation.
	// Compilation and its operational failures are handled by the resumed task.
	if strings.TrimSpace(requestID) == "" {
		requestID = fmt.Sprintf("%s:%d", rid, revision)
	}
	claimed, fresh, err := store.Claim(sid, rid, revision, requestID)
	if err != nil || !fresh {
		return claimed, err
	}
	actual, ok := s.popPendingInterrupt(nil, sid)
	if !ok || actual != pending {
		if ok {
			s.setPendingInterrupt(nil, sid, actual)
		}
		_, _ = store.Fail(sid, rid, revision, []knowledge.ReviewError{{Message: "制图任务状态已变化，请重新打开核对结果"}})
		return nil, knowledge.ErrReviewConflict
	}
	runCtx, cancel := context.WithCancel(context.Background())
	if !s.startSessionTask(runCtx, nil, sid, cancel, func() {
		s.markSessionRuntimeResumed(sid)
		s.resumeTask(runCtx, nil, pending, map[string]any{rootID: map[string]any{"review_id": rid, "revision": revision}})
	}) {
		return nil, fmt.Errorf("会话已删除，不能恢复执行")
	}
	return claimed, nil
}

func (s *DeepServer) failKnowledgeReviewResume(pending *PendingInterrupt, failure error) bool {
	rid, _ := knowledgeReviewRef(pending.Contexts)
	if rid == "" {
		return false
	}
	store := s.KnowledgeReviewStore()
	if store == nil {
		return false
	}
	review, err := store.Get(pending.SessionID, rid)
	if err != nil {
		return false
	}
	if review.Status == knowledge.ReviewCompiling {
		_, _ = store.Fail(review.SessionID, rid, review.Revision, []knowledge.ReviewError{{Message: "恢复制图失败，修改稿已保留：" + failure.Error()}})
	}
	s.setPendingInterrupt(nil, pending.SessionID, pending)
	s.markSessionRuntimeInterrupted(pending.SessionID, pending.CheckpointID)
	s.persistManagedSessionState(pending.SessionID, pending.ProjectPath, executionModeBuild)
	return true
}

// persistKnowledgeReviewCard translates the schema.Message extra back to the
// chat DTO, so server snapshots and frontend saves both retain the same card.
func persistKnowledgeReviewCard(payload map[string]any, msg *schema.Message) {
	if msg == nil {
		return
	}
	if data, ok := msg.Extra["knowledge_review_card"].(map[string]any); ok {
		payload["msg_type"], payload["data"] = "interrupt", data
	}
}
