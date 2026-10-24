package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/resource"
)

func resourceChangePending(err error) bool {
	var changed *flowrun.ResourceChangeError
	return errors.As(err, &changed)
}

func (s *DeepServer) checkFlowPendingResources(ctx context.Context, pending *PendingInterrupt, choice, fingerprint string) (*flowrun.RunState, error) {
	if pending.FlowOptions.flowRunID == "" {
		return nil, nil
	}
	_, controller := s.flowDependencies()
	if controller == nil {
		return nil, nil
	}
	dir, err := config.ResolveSessionDir(pending.ProjectPath, pending.SessionID)
	if err != nil {
		return nil, err
	}
	source := ""
	if pending.FlowOptions.originalSource != nil {
		source = *pending.FlowOptions.originalSource
	}
	return controller.CheckResume(ctx, flowrun.ResumeRequest{SessionID: pending.SessionID, SessionDir: dir, FlowRunID: pending.FlowOptions.flowRunID, OriginalSource: source, ResourceChoice: choice, ResourceFingerprint: fingerprint})
}

func withRunResourceSnapshot(ctx context.Context, state *flowrun.RunState) context.Context {
	if state == nil || state.Plan == nil {
		return ctx
	}
	linked := make([]*resource.ResolvedResource, 0, len(state.Plan.Resources))
	for _, item := range state.Plan.Resources {
		linked = append(linked, item.ResolvedResource())
	}
	return resource.WithScope(ctx, resource.Scope{FlowID: state.FlowID, FlowDir: state.Plan.FlowDir, Lock: state.Plan.Lock, Linked: linked})
}

func (s *DeepServer) discardOldFlowCheckpoint(ctx context.Context, sessionID, projectPath, flowRunID string, previous *PendingInterrupt) {
	if pending, ok := s.getPendingInterrupt(sessionID); ok && pending.FlowOptions.flowRunID == flowRunID {
		s.popPendingInterrupt(nil, sessionID)
	}
	checkpoint := ""
	if previous != nil && previous.FlowOptions.flowRunID == flowRunID {
		checkpoint = previous.CheckpointID
	}
	if checkpoint == "" {
		// After a service restart the resource guard may run before the original
		// pending interrupt is reattached. Its persisted descriptor still owns it.
		if dir, err := config.ResolveSessionDir(projectPath, sessionID); err == nil {
			if runPath, err := flowrun.RunPath(dir, flowRunID); err == nil {
				descriptorPath := filepath.Join(filepath.Dir(runPath), "interrupt.json")
				if info, err := os.Lstat(descriptorPath); err == nil && info.Mode().IsRegular() && info.Size() <= 1<<20 {
					raw, err := os.ReadFile(descriptorPath)
					var descriptor flowInterruptDescriptor
					if err == nil && json.Unmarshal(raw, &descriptor) == nil && descriptor.SessionID == sessionID && descriptor.FlowRunID == flowRunID {
						checkpoint = descriptor.CheckpointID
					}
				}
			}
		}
	}
	if checkpoint != "" {
		_ = s.checkpointStore.Delete(ctx, checkpoint)
	}
}

// A resource decision belongs to the already paused flow. It may use the task
// envelope without admitting unrelated tasks into an occupied session.
func (s *DeepServer) isPausedFlowResume(sessionID, projectPath string, data map[string]any) bool {
	request, err := parseFlowRequest(data)
	if err != nil || request == nil || request.ResumeRunID == "" {
		return false
	}
	state, err := s.flowSnapshot(sessionID, projectPath)
	return err == nil && state != nil && state.ID == request.ResumeRunID && state.SessionID == sessionID && (state.Status == "waiting_user" || state.Status == "interrupted")
}
