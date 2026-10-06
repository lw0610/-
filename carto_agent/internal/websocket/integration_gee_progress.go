package websocket

import (
	"context"

	"github.com/autocarto/carto_agent/internal/geeruntime"
	"github.com/gorilla/websocket"
)

func (s *DeepServer) withGEEProgress(ctx context.Context, conn *websocket.Conn, sessionID, phase string) context.Context {
	return geeruntime.WithProgressCallback(ctx, func(p geeruntime.Progress) {
		s.sendGEEProgress(ctx, conn, sessionID, phase, p)
	})
}

func (s *DeepServer) sendGEEProgress(ctx context.Context, conn *websocket.Conn, sessionID, phase string, p geeruntime.Progress) {
	if ctx.Err() != nil {
		return
	}
	data := map[string]any{
		"source": "gee", "tool": "gee_run_script", "filename": p.Filename,
		"request_key": p.RequestKey, "download_id": p.DownloadID, "download_phase": p.Phase,
		"completed_bytes": p.CompletedBytes, "total_bytes": p.TotalBytes,
	}
	if p.Percent != nil {
		data["percent"] = *p.Percent
	}
	if p.Type == "workflow_progress" {
		data["stage_id"], data["stage"] = p.StageID, p.Stage
		data["elapsed_seconds"] = p.ElapsedSeconds
		data["workflow_phase"] = p.Phase
		delete(data, "download_phase")
	}
	s.sendAgentWorkflowPhase(conn, sessionID, "progress", p.Text(), withModePhaseData(data, phase))
}
