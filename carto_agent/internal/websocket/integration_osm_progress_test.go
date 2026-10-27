package websocket

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/osmdata"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func osmProgressEntries(s *DeepServer, sid string) []workflowEntry {
	var out []workflowEntry
	for _, block := range s.sessionRuntimeProjectionSnapshot(sid, nil).WorkflowBlocks {
		for _, entry := range block.Entries {
			if entry.Phase == "progress" && stringValue(entry.Data["source"]) == "osm" {
				out = append(out, entry)
			}
		}
	}
	return out
}

func TestOSMProgressSpeedAndPercentSurviveProjectionWithoutStaleValues(t *testing.T) {
	s := NewDeepServer(0, nil)
	const sid = "osm-byte-metrics"
	percent := 25.0
	p := osmdata.Progress{RequestKey: "osm:metrics", Filename: "river.geojson", Phase: "downloading", CompletedBytes: 2500000, TotalBytes: 10000000, BytesPerSecond: 12345, Percent: &percent}
	s.sendOSMProgress(context.Background(), nil, sid, "building", p)
	entries := osmProgressEntries(s, sid)
	if len(entries) != 1 || entries[0].Data["percent"] != 25.0 || entries[0].Data["bytes_per_second"] != 12345.0 || !strings.Contains(entries[0].Content, "12.3 KB/s") {
		t.Fatalf("byte metrics not projected: %+v", entries)
	}
	// A new node with unknown length must remove the previous node's percent.
	p.Endpoint, p.TotalBytes, p.CompletedBytes, p.BytesPerSecond, p.Percent = "next.example", 0, 0, 0, nil
	s.sendOSMProgress(context.Background(), nil, sid, "building", p)
	entries = osmProgressEntries(s, sid)
	if len(entries) != 1 || entries[0].Data["percent"] != nil || entries[0].Data["bytes_per_second"] != 0.0 || !strings.Contains(entries[0].Content, "总大小未知") {
		t.Fatalf("stale response metrics survived: %+v", entries)
	}
	p.Phase, p.ArtifactPaths = "completed", []string{"river.geojson"}
	percent = 100
	p.Percent = &percent
	s.sendOSMProgress(context.Background(), nil, sid, "building", p)
	p.Phase, p.Percent = "downloading", nil
	s.sendOSMProgress(context.Background(), nil, sid, "building", p)
	entries = osmProgressEntries(s, sid)
	if entries[0].Data["percent"] != 100.0 || entries[0].Data["download_phase"] != "completed" {
		t.Fatalf("late heartbeat replaced saved completion: %+v", entries)
	}
}

func TestOSMParallelProgressKeepsRowsAndTerminalStates(t *testing.T) {
	s := NewDeepServer(0, nil)
	const sid = "osm-parallel-progress"
	s.setSessionAgent(nil, sid, "data_engineer")
	var wg sync.WaitGroup
	for _, name := range []string{"rivers", "railways"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			p := osmdata.Progress{RequestKey: "osm:" + name, Tool: "osm_get_features", Filename: name + ".geojson", Phase: "downloading"}
			for i := 0; i < 20; i++ {
				p.CompletedBytes = int64(i * 1024)
				s.sendOSMProgress(context.Background(), nil, sid, "building", p)
				_ = s.sessionRuntimeProjectionSnapshot(sid, nil)
			}
			p.Phase, p.FeatureCount = "completed", 3
			p.ArtifactPaths = []string{"/tmp/" + name + ".geojson"}
			s.sendOSMProgress(context.Background(), nil, sid, "building", p)
		}(name)
	}
	wg.Wait()
	// Delegate back to the orchestrator before a late heartbeat arrives.
	s.setSessionAgent(nil, sid, "orchestrator")
	s.sendAgentWorkflowPhase(nil, sid, "decision", "准备交付", nil)
	s.sendOSMProgress(context.Background(), nil, sid, "building", osmdata.Progress{RequestKey: "osm:rivers", Filename: "rivers.geojson", Phase: "downloading"})
	entries := osmProgressEntries(s, sid)
	if len(entries) != 2 {
		t.Fatalf("parallel requests merged or appended across blocks: %+v", entries)
	}
	for _, e := range entries {
		if e.Type != "status" || e.Data["thinking_kind"] != nil || e.Data["percent"] != nil || e.Data["download_phase"] != "completed" || !strings.Contains(e.Content, "3 个要素") {
			t.Fatalf("invalid final progress: %+v", e)
		}
	}
}

func TestOSMProgressPublishesCancellationAndDropsLateHeartbeat(t *testing.T) {
	s := NewDeepServer(0, nil)
	ctx, cancel := context.WithCancel(context.Background())
	p := osmdata.Progress{RequestKey: "osm:cancel", Filename: "rivers.geojson", Phase: "waiting"}
	s.sendOSMProgress(ctx, nil, "osm-cancel", "building", p)
	cancel()
	p.Phase = "cancelled"
	s.sendOSMProgress(ctx, nil, "osm-cancel", "building", p)
	p.Phase = "downloading"
	s.sendOSMProgress(ctx, nil, "osm-cancel", "building", p)
	entries := osmProgressEntries(s, "osm-cancel")
	if len(entries) != 1 || entries[0].Data["download_phase"] != "cancelled" {
		t.Fatalf("cancelled request stayed active: %+v", entries)
	}
}

func TestOSMProgressSameTimestampKeepsDistinctSnapshotIDs(t *testing.T) {
	projection := sessionRuntimeProjection{}
	const timestamp = int64(1792986105000)
	for _, key := range []string{"osm:rivers", "osm:railways"} {
		projection.appendWorkflowEvent(Message{Type: MsgTypeAgentWorkflow, Agent: "data_engineer", Phase: "progress", Content: key,
			Data: map[string]any{"source": "osm", "request_key": key, "download_phase": "preparing"}}, timestamp)
	}
	restored := projectionFromSnapshot("fixture", projectionToSnapshot(projection))
	entries := restored.WorkflowBlocks[0].Entries
	if len(entries) != 2 || entries[0].ID == entries[1].ID {
		t.Fatalf("same-millisecond downloads collided after restore: %+v", entries)
	}
	first := entries[0].ID
	restored.appendWorkflowEvent(Message{Type: MsgTypeAgentWorkflow, Agent: "data_engineer", Phase: "progress", Content: "bytes received",
		Data: map[string]any{"source": "osm", "request_key": "osm:rivers", "download_phase": "downloading"}}, timestamp+1000)
	if restored.WorkflowBlocks[0].Entries[0].ID != first || !strings.HasPrefix(first, "entry-progress-osm-") {
		t.Fatal("restored progress lost its stable source/request identity")
	}
	otherSource := buildWorkflowEntry(Message{Phase: "progress", Content: "other", Data: map[string]any{"source": "gee", "request_key": "osm:rivers"}}, timestamp)
	if otherSource.ID == first {
		t.Fatal("separate progress sources share an entry ID")
	}
}

func TestOSMProgressStalledSocketIsBounded(t *testing.T) {
	conn, _, transport := newGEEProgressTestConnection(t)
	transport.blocked = true
	s := NewDeepServer(0, nil)
	started := time.Now()
	s.sendOSMProgress(context.Background(), conn, "osm-slow", "building", osmdata.Progress{RequestKey: "osm:slow", Filename: "rivers.geojson", Phase: "waiting"})
	if time.Since(started) > time.Second || !transport.closed || len(osmProgressEntries(s, "osm-slow")) != 1 {
		t.Fatal("slow client blocked download progress or discarded its recoverable state")
	}
}

func TestOSMProgressBusySocketDoesNotWaitForOtherWrites(t *testing.T) {
	conn, _, _ := newGEEProgressTestConnection(t)
	s := NewDeepServer(0, nil)
	writeMu := s.getConnWriteMu(conn)
	writeMu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.sendOSMProgress(context.Background(), conn, "osm-busy", "building", osmdata.Progress{RequestKey: "osm:busy", Filename: "rivers.geojson", Phase: "downloading", CompletedBytes: 1024})
	}()
	select {
	case <-done:
		writeMu.Unlock()
	case <-time.After(time.Second):
		writeMu.Unlock()
		<-done
		t.Fatal("progress waited for another websocket writer")
	}
	entries := osmProgressEntries(s, "osm-busy")
	if len(entries) != 1 || entries[0].Data["download_phase"] != "downloading" {
		t.Fatal("backpressure lost the latest progress snapshot")
	}
}

type osmProgressAgent struct {
	pause   bool
	options osmdata.Options
}

func (*osmProgressAgent) Name(context.Context) string        { return "data_engineer" }
func (*osmProgressAgent) Description(context.Context) string { return "isolated OSM progress fixture" }
func (a *osmProgressAgent) Run(ctx context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	if a.pause {
		iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
		go func() { defer gen.Close(); gen.Send(adk.StatefulInterrupt(ctx, "allow fixture download", "paused")) }()
		return iter
	}
	return a.download(ctx)
}
func (a *osmProgressAgent) Resume(ctx context.Context, _ *adk.ResumeInfo, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	return a.download(ctx)
}
func (a *osmProgressAgent) download(ctx context.Context) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		result, err := osmdata.Invoke(ctx, "osm_get_features", map[string]any{"tags": []string{"waterway=river"}, "types": "way", "extents": []string{"bbox:30,114,31,115"}, "output": "rivers.geojson"}, a.options)
		if err != nil {
			gen.Send(&adk.AgentEvent{Err: err})
			return
		}
		gen.Send(adk.EventFromMessage(schema.AssistantMessage(fmt.Sprintf("downloaded %d", result.FeatureCount), nil), nil, schema.Assistant, ""))
	}()
	return iter
}

func TestOSMProgressWiredForInitialAndResumedRuns(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"elements":[{"type":"way","id":1,"tags":{"waterway":"river","name":"fixture"},"geometry":[{"lat":30.2,"lon":114.2},{"lat":30.3,"lon":114.3}]}]}`)
	}))
	defer endpoint.Close()
	for _, paused := range []bool{false, true} {
		t.Run(fmt.Sprintf("resumed=%t", paused), func(t *testing.T) {
			s := NewDeepServer(0, nil)
			sid := fmt.Sprintf("osm-wiring-%t", paused)
			project := filepath.Join(t.TempDir(), "project.qgz")
			if err := s.SaveSession(map[string]any{"session_id": sid, "messages": []any{}, "project_path": project}, project); err != nil {
				t.Fatal(err)
			}
			a := &osmProgressAgent{pause: paused, options: osmdata.Options{Enabled: true, Authorized: true, Endpoints: []string{endpoint.URL}, OutputDir: t.TempDir(), ProjectDir: filepath.Dir(project)}}
			result := s.runAgentPhase(context.Background(), nil, sid, project, "", s.sessionManager.GetOrCreate(sid), a, []*schema.Message{schema.UserMessage("download")}, consumeEventsOptions{executionMode: executionModeBuild, modePhase: "building"})
			if result.Err != nil {
				t.Fatal(result.Err)
			}
			if paused {
				pending, ok := s.popPendingInterrupt(nil, sid)
				if !ok {
					t.Fatal("missing fixture checkpoint")
				}
				targets := map[string]any{}
				for _, ic := range pending.Contexts {
					if ic.IsRootCause {
						targets[ic.ID] = map[string]any{"confirmed": true}
					}
				}
				s.resumeTask(context.Background(), nil, pending, targets)
			}
			entries := osmProgressEntries(s, sid)
			if len(entries) != 1 || entries[0].Data["download_phase"] != "completed" || !strings.Contains(entries[0].Content, "1 个要素") {
				t.Fatalf("download callback missing from execution context: %+v", entries)
			}
		})
	}
}
