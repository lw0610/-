package websocket

import (
	"context"
	"strings"
	"sync"

	"github.com/autocarto/carto_agent/internal/osmdata"
	"github.com/gorilla/websocket"
)

func (s *DeepServer) withOSMProgress(ctx context.Context, conn *websocket.Conn, sessionID, phase string) context.Context {
	// Parallel downloads share the callback, but never a request identity. Keep
	// their terminal events ordered even when a heartbeat was already in flight.
	var mu sync.Mutex
	finished := map[string]bool{}
	return osmdata.WithProgressCallback(ctx, func(p osmdata.Progress) {
		mu.Lock()
		defer mu.Unlock()
		if finished[p.RequestKey] {
			return
		}
		if terminalOSMProgress(p.Phase) {
			finished[p.RequestKey] = true
		}
		s.sendOSMProgress(ctx, conn, sessionID, phase, p)
	})
}

func terminalOSMProgress(phase string) bool {
	switch phase {
	case "completed", "partial", "failed", "cancelled":
		return true
	}
	return false
}

func (s *DeepServer) sendOSMProgress(ctx context.Context, conn *websocket.Conn, sessionID, phase string, p osmdata.Progress) {
	// Cancellation must still close the progress row; a cancelled context only
	// suppresses late nonterminal heartbeats.
	if strings.TrimSpace(p.RequestKey) == "" || p.Text() == "" || (ctx.Err() != nil && !terminalOSMProgress(p.Phase)) {
		return
	}
	data := map[string]any{
		"source": "osm", "tool": p.Tool, "filename": p.Filename,
		"request_key": p.RequestKey, "download_phase": p.Phase,
		"endpoint": p.Endpoint, "attempt": p.Attempt, "max_attempts": p.MaxAttempts,
		"extent_index": p.ExtentIndex, "extent_count": p.ExtentCount,
		"completed_bytes": p.CompletedBytes, "total_bytes": p.TotalBytes,
		"bytes_per_second": p.BytesPerSecond,
		"elapsed_seconds":  p.ElapsedSeconds, "feature_count": p.FeatureCount,
		"artifact_paths": append([]string(nil), p.ArtifactPaths...),
	}
	// Unknown content length is not converted into a made-up percentage.
	// The producer only sends 100 after the output has been saved successfully.
	if p.Percent != nil {
		data["percent"] = *p.Percent
	}
	s.sendAgentWorkflowPhase(conn, sessionID, "progress", p.Text(), withModePhaseData(data, phase))
}
