package websocket

import (
	"context"
	"testing"

	"github.com/autocarto/carto_agent/internal/geeruntime"
)

func TestGEEProgressCoalescesPerDownloadAndHonorsCancellation(t *testing.T) {
	s := NewDeepServer(0, nil)
	sid := "gee-progress-test"
	s.setSessionAgent(nil, sid, "data_engineer")
	p := geeruntime.Progress{Type: "download_progress", RequestKey: "gee:run-1:1", DownloadID: 1, Filename: "image.tif", Phase: "downloading"}
	for i := 0; i < 100; i++ {
		pct := float64(i)
		p.Percent = &pct
		s.sendGEEProgress(context.Background(), nil, sid, "building", p)
	}
	p.Phase = "completed"
	s.sendGEEProgress(context.Background(), nil, sid, "building", p)
	p.RequestKey, p.Phase = "gee:run-2:1", "preparing"
	s.sendGEEProgress(context.Background(), nil, sid, "building", p)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.RequestKey = "gee:run-cancelled:1"
	s.sendGEEProgress(ctx, nil, sid, "building", p)
	projection := s.sessionRuntimeProjectionSnapshot(sid, nil)
	if len(projection.WorkflowBlocks) != 1 || len(projection.WorkflowBlocks[0].Entries) != 2 {
		t.Fatalf("progress accumulated, lost separate runs, or survived cancellation: %+v", projection.WorkflowBlocks)
	}
	entries := projection.WorkflowBlocks[0].Entries
	if entries[0].Data["download_phase"] != "completed" || entries[1].Data["download_phase"] != "preparing" {
		t.Fatalf("latest progress not preserved: %+v", entries)
	}
	for _, entry := range entries {
		if entry.Type != "status" || entry.Phase != "progress" || entry.Data["thinking_kind"] != nil {
			t.Fatalf("progress misrepresented as model reasoning: %+v", entry)
		}
	}
}
