package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/autocarto/carto_agent/internal/agentruntime"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/cloudwego/eino/adk"
)

type flowInterruptDescriptor struct {
	CheckpointID string              `json:"checkpoint_id"`
	SessionID    string              `json:"session_id"`
	FlowRunID    string              `json:"flow_run_id"`
	ProjectPath  string              `json:"project_path"`
	InstanceID   string              `json:"instance_id"`
	Contexts     []*adk.InterruptCtx `json:"contexts"`
	Data         map[string]any      `json:"data"`
}

func (s *DeepServer) persistFlowInterrupt(ctx context.Context, pending *PendingInterrupt, data map[string]any) error {
	if pending.FlowOptions.flowRunID == "" {
		return nil
	}
	dir, err := config.ResolveSessionDir(pending.ProjectPath, pending.SessionID)
	if err != nil {
		return err
	}
	runPath, err := flowrun.RunPath(dir, pending.FlowOptions.flowRunID)
	if err != nil {
		return err
	}
	descriptor := flowInterruptDescriptor{CheckpointID: pending.CheckpointID, SessionID: pending.SessionID, FlowRunID: pending.FlowOptions.flowRunID, ProjectPath: pending.ProjectPath, InstanceID: pending.BoundInstanceID, Contexts: pending.Contexts, Data: data}
	raw, err := json.Marshal(descriptor)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(runPath), ".interrupt-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(filepath.Dir(runPath), "interrupt.json"))
}
func (s *DeepServer) restoreFlowPending(ctx context.Context, sessionID, projectPath, id string, selection ...flowrun.ResumeRequest) (*PendingInterrupt, error) {
	var choice, fingerprint string
	if len(selection) != 0 {
		choice, fingerprint = selection[0].ResourceChoice, selection[0].ResourceFingerprint
	}
	if pending, ok := s.getPendingInterrupt(sessionID); ok && pending.FlowOptions.flowRunID == id {
		if _, err := s.checkFlowPendingResources(ctx, pending, choice, fingerprint); err != nil {
			return nil, err
		}
		return pending, nil
	}
	dir, err := config.ResolveSessionDir(projectPath, sessionID)
	if err != nil {
		return nil, err
	}
	path, err := flowrun.RunPath(dir, id)
	if err != nil {
		return nil, err
	}
	path = filepath.Join(filepath.Dir(path), "interrupt.json")
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, fmt.Errorf("人工确认断点文件无效")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var desc flowInterruptDescriptor
	if err = json.Unmarshal(raw, &desc); err != nil {
		return nil, err
	}
	if desc.SessionID != sessionID || desc.FlowRunID != id || desc.ProjectPath != projectPath || len(desc.Contexts) == 0 {
		return nil, fmt.Errorf("人工确认断点与会话不匹配")
	}
	if _, exists, err := s.checkpointStore.Get(ctx, desc.CheckpointID); err != nil {
		return nil, err
	} else if !exists {
		return nil, fmt.Errorf("原流程断点已失效，不能自动重新执行")
	}
	sess := s.sessionManager.GetOrCreate(sessionID)
	s.hydrateSessionMessages(sess, sessionID, projectPath)
	state, err := (flowrun.FileRunStore{}).Load(ctx, dir, id)
	if err != nil {
		return nil, err
	}
	if err = checkPendingFlowAttempt(state, desc.CheckpointID); err != nil {
		return nil, err
	}
	run := flowSessionContext{Session: sess, SessionID: sessionID, ProjectPath: projectPath, InstanceID: desc.InstanceID, Scheme: state.Scheme, Mode: executionModeBuild, FlowRunID: id}
	source, err := s.findOriginalFlowSource(ctx, run, dir, id)
	if err != nil {
		return nil, err
	}
	run.OriginalSource = source
	s.setFlowContext(run)
	_, controller := s.flowDependencies()
	if controller == nil {
		return nil, fmt.Errorf("流程服务未初始化")
	}
	request, err := controller.RestoreWaiting(ctx, flowrun.ResumeRequest{SessionID: sessionID, SessionDir: dir, FlowRunID: id, OriginalSource: source, ResourceChoice: choice, ResourceFingerprint: fingerprint})
	if err != nil {
		return nil, err
	}
	if request.RunID != desc.CheckpointID {
		return nil, fmt.Errorf("人工确认断点在恢复期间已改变，请重新打开当前计划")
	}
	ctx = withRunResourceSnapshot(ctx, state)
	ctx, err = withRunAcquisitionSnapshot(ctx, state)
	if err != nil {
		return nil, err
	}
	var agent adk.Agent
	if request.Confirmation != nil {
		agent = flowrun.NewConfirmAgent(*request.Confirmation)
	} else {
		builder, cfg, err := s.flowBuilder()
		if err != nil {
			return nil, err
		}
		if request.Overlay != nil {
			agent, err = builder.BuildRoleWithOverlay(ctx, run.Scheme, cfg, agentruntime.RoleOverlay{FlowRole: request.Overlay.FlowRole, RoleID: request.Overlay.RoleID, ExtraPackages: request.Overlay.ExtraPackages})
		} else {
			agent, err = builder.BuildRole(ctx, run.Scheme, cfg, request.Role.ID)
		}
		if err != nil {
			return nil, err
		}
	}
	resumeCtx := knowledge.WithOriginalSource(deepagent.WithKnowledgeReview(withToolSessionContext(ctx, sessionID, projectPath, desc.InstanceID), s.KnowledgeReviewStore()), source)
	options := consumeEventsOptions{flowRunID: id, flowStepID: request.Step.ID, originalSource: &source, persistAssistant: true, modePhase: modeStatePhaseBuilding, executionMode: executionModeBuild, runID: request.RunID}
	pending := &PendingInterrupt{CheckpointID: desc.CheckpointID, Runner: adk.NewRunner(resumeCtx, adk.RunnerConfig{Agent: agent, CheckPointStore: s.checkpointStore}), Contexts: desc.Contexts, SessionID: sessionID, ProjectPath: projectPath, BoundInstanceID: desc.InstanceID, Session: sess, ModePhase: modeStatePhaseBuilding, FlowOptions: options, InterruptData: desc.Data}
	s.setPendingInterrupt(nil, sessionID, pending)
	s.markSessionRuntimeRunning(sessionID, request.RunID, executionModeBuild, modeStatePhaseBuilding, projectPath, desc.InstanceID)
	s.markSessionRuntimeInterrupted(sessionID, request.RunID)
	return pending, nil
}

// Disk restoration is scoped to this configured runtime's known session directory.
func (s *DeepServer) flowSnapshot(sessionID, projectPath string) (*flowrun.RunState, error) {
	_, controller := s.flowDependencies()
	if controller == nil {
		return nil, nil
	}
	if state, err := controller.Snapshot(sessionID); err != nil || state != nil {
		return state, err
	}
	dir, err := config.ResolveSessionDir(projectPath, sessionID)
	if err != nil {
		return nil, err
	}
	_, err = (flowrun.FileRunStore{}).MarkInterrupted(context.Background(), dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(dir, "flows"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var latest *flowrun.RunState
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		state, err := (flowrun.FileRunStore{}).Load(context.Background(), dir, entry.Name())
		if err != nil {
			return nil, err
		}
		if state.SessionID != sessionID {
			continue
		}
		if latest == nil || state.StartedAt.After(latest.StartedAt) {
			latest = state
		}
	}
	if latest != nil {
		s.mu.Lock()
		if s.sessionRunEventSeq[sessionID] < uint64(latest.EventSeq) {
			s.sessionRunEventSeq[sessionID] = uint64(latest.EventSeq)
		}
		s.mu.Unlock()
	}
	return latest, nil
}
func pendingInterruptPayload(pending *PendingInterrupt) map[string]any {
	data := copyMessageData(pending.InterruptData)
	data["checkpoint_id"] = pending.CheckpointID
	if pending.FlowOptions.flowRunID != "" {
		data["flow_run_id"] = pending.FlowOptions.flowRunID
		data["step_id"] = pending.FlowOptions.flowStepID
	}
	return data
}

func (s *DeepServer) markStoredFlowsInterrupted(ctx context.Context) error {
	root, err := config.ResolvePluginDataDir()
	if err != nil {
		return err
	}
	projects, err := os.ReadDir(filepath.Join(root, "sessions"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		projectDir := filepath.Join(root, "sessions", project.Name())
		sessions, err := os.ReadDir(projectDir)
		if err != nil {
			return err
		}
		for _, entry := range sessions {
			if !entry.IsDir() {
				continue
			}
			if _, err = (flowrun.FileRunStore{}).MarkInterrupted(ctx, filepath.Join(projectDir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
