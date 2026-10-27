package osmdata

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProgressSpeedUsesByteIntervalsAndResetsBetweenResponses(t *testing.T) {
	start := time.Unix(1700000000, 0)
	state := &progressState{}
	p := Progress{Phase: "downloading", Endpoint: "first.example", Attempt: 1, ExtentIndex: 1, TotalBytes: 100000}
	state.updateMetrics(&p, start)
	p.CompletedBytes = 4000
	state.updateMetrics(&p, start.Add(2*time.Second))
	if p.BytesPerSecond != 2000 || p.Percent == nil || *p.Percent != 4 {
		t.Fatalf("wrong measured rate/percentage: %+v", p)
	}
	// Heartbeats use the same sampler, so a stalled stream loses its old rate.
	state.updateMetrics(&p, start.Add(8*time.Second))
	if p.BytesPerSecond != 0 {
		t.Fatalf("stalled stream retained old speed: %+v", p)
	}
	for _, change := range []func(*Progress){
		func(p *Progress) { p.Endpoint = "second.example" },
		func(p *Progress) { p.Attempt++ },
		func(p *Progress) { p.ExtentIndex++ },
		func(p *Progress) { p.CompletedBytes = 0 },
	} {
		start = start.Add(10 * time.Second)
		change(&p)
		state.updateMetrics(&p, start)
		if p.BytesPerSecond != 0 {
			t.Fatalf("new response inherited old speed: %+v", p)
		}
		p.CompletedBytes += 2000
		state.updateMetrics(&p, start.Add(2*time.Second))
		if p.BytesPerSecond != 1000 {
			t.Fatalf("new response failed to establish its own speed: %+v", p)
		}
	}
}

func TestProgressPercentOnlyUsesReliableSizeAndSavedCompletion(t *testing.T) {
	state := &progressState{}
	now := time.Now()
	for _, tc := range []struct {
		name    string
		p       Progress
		percent *float64
		text    string
	}{
		{name: "unknown", p: Progress{Phase: "downloading", CompletedBytes: 2000000}, text: "总大小未知，无法计算百分比"},
		{name: "known", p: Progress{Phase: "downloading", CompletedBytes: 250, TotalBytes: 1000}, percent: progressPercent(25), text: "25.0%"},
		{name: "multiple extents", p: Progress{Phase: "downloading", CompletedBytes: 250, TotalBytes: 1000, ExtentIndex: 2, ExtentCount: 3}, percent: progressPercent(25), text: "第 2/3 个范围"},
		{name: "near complete", p: Progress{Phase: "downloading", CompletedBytes: 999999, TotalBytes: 1000000}, percent: progressPercent(99.9999), text: "99.9%"},
		{name: "received", p: Progress{Phase: "downloading", CompletedBytes: 1000, TotalBytes: 1000}, text: "接收完毕，正在处理"},
		{name: "saving", p: Progress{Phase: "saving", FeatureCount: 1}, text: "正在保存"},
		{name: "saved", p: Progress{Phase: "completed", FeatureCount: 1, ArtifactPaths: []string{"rivers.geojson"}}, percent: progressPercent(100), text: "100%"},
		{name: "empty result", p: Progress{Phase: "completed"}, text: "没有符合条件的要素"},
		{name: "partial", p: Progress{Phase: "partial", ArtifactPaths: []string{"partial.geojson"}}, text: "部分完成"},
		{name: "cancelled", p: Progress{Phase: "cancelled"}, text: "已取消"},
		{name: "failed", p: Progress{Phase: "failed"}, text: "未完成"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state.updateMetrics(&tc.p, now)
			if (tc.p.Percent == nil) != (tc.percent == nil) || tc.percent != nil && absProgress(*tc.p.Percent-*tc.percent) > 0.000001 {
				t.Fatalf("unexpected percent: %+v", tc.p)
			}
			if !strings.Contains(tc.p.Text(), tc.text) {
				t.Fatalf("missing honest progress label %q: %s", tc.text, tc.p.Text())
			}
			if tc.p.Phase == "downloading" && !strings.Contains(tc.p.Text(), "KB/s") {
				t.Fatal("download speed missing")
			}
		})
	}
}

func progressPercent(v float64) *float64 { return &v }

func absProgress(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestProgressHTTPCompressionAndChunkingNeverInventTotal(t *testing.T) {
	payload := []byte(`{"elements":[],"padding":"` + strings.Repeat("x", 10000) + `"}`)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write(payload)
	_ = writer.Close()
	for _, mode := range []string{"gzip", "chunked", "plain"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch mode {
				case "gzip":
					if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
						t.Error("fixture did not exercise Go automatic gzip")
					}
					w.Header().Set("Content-Encoding", "gzip")
					w.Header().Set("Content-Length", strconv.Itoa(compressed.Len()))
					_, _ = w.Write(compressed.Bytes())
				case "chunked":
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
					_, _ = w.Write(payload)
				default:
					w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
					_, _ = w.Write(payload)
				}
			}))
			defer server.Close()
			client, err := newClient(Options{Enabled: true, Authorized: true, HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			ctx, finish := beginProgress(context.Background(), "osm_get_features", "fixture.geojson")
			defer finish(nil, nil)
			body, err := client.requestRaw(ctx, http.MethodGet, server.URL, "")
			if err != nil || !bytes.Equal(body, payload) {
				t.Fatalf("unexpected response: %d bytes, %v", len(body), err)
			}
			state := ctx.Value(progressStateKey{}).(*progressState)
			state.mu.Lock()
			last := state.last
			state.mu.Unlock()
			if last.CompletedBytes != int64(len(payload)) || last.Percent != nil {
				t.Fatalf("wrong body byte units or premature completion: %+v", last)
			}
			if mode == "plain" {
				if last.TotalBytes != int64(len(payload)) || !strings.Contains(last.Text(), "接收完毕") {
					t.Fatalf("reliable total lost: %+v", last)
				}
			} else if last.TotalBytes != 0 || !strings.Contains(last.Text(), "总大小未知") {
				t.Fatalf("invented total for %s: %+v", mode, last)
			}
		})
	}
}

func TestProgressCoalescesBytesAndKeepsTerminalState(t *testing.T) {
	var events []Progress
	ctx := WithProgressCallback(context.Background(), func(p Progress) { events = append(events, p) })
	ctx, finish := beginProgress(ctx, "osm_get_features", "/output/railways.geojson")
	request := withProgressMetadata(ctx, Progress{Endpoint: "https://overpass.example/api/interpreter", Attempt: 1, MaxAttempts: 3, ExtentIndex: 1, ExtentCount: 2})
	reportProgress(request, Progress{Phase: "waiting"})
	for n := 1; n <= 100; n++ {
		reportProgress(request, Progress{Phase: "downloading", CompletedBytes: int64(n) * 1024})
	}
	finish(&Result{FeatureCount: 10, ArtifactPaths: []string{"/output/railways.geojson"}}, nil)
	reportProgress(request, Progress{Phase: "downloading", CompletedBytes: 1})
	if len(events) != 4 {
		t.Fatalf("byte updates flooded UI or late update survived: %+v", events)
	}
	if events[1].ExtentIndex != 1 || events[2].Attempt != 1 || events[2].Endpoint != "overpass.example" || events[2].Filename != "railways.geojson" {
		t.Fatalf("metadata lost: %+v", events)
	}
	if strings.Contains(events[2].Text(), "%") {
		t.Fatal("unknown response size shown as percentage")
	}
	last := events[len(events)-1]
	if last.Phase != "completed" || last.FeatureCount != 10 || !strings.Contains(last.Text(), "已保存") {
		t.Fatal(last)
	}
	for _, p := range events {
		if p.RequestKey == "" || p.RequestKey != last.RequestKey {
			t.Fatal("unstable request identity")
		}
	}
}

func TestParallelDownloadsHaveDistinctProgressAndPublishSavedArtifacts(t *testing.T) {
	fastNetwork(t)
	data, _ := json.Marshal(Response{Elements: fixture()})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if strings.Contains(r.Form.Get("data"), "out skel") {
			fmt.Fprint(w, `{"elements":[]}`)
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	var mu sync.Mutex
	var completed []Progress
	ctx := WithProgressCallback(context.Background(), func(p Progress) {
		if p.Phase != "completed" {
			return
		}
		for _, path := range p.ArtifactPaths {
			if _, err := os.Stat(path); err != nil {
				t.Errorf("completion before saved artifact: %v", err)
			}
		}
		mu.Lock()
		completed = append(completed, p)
		mu.Unlock()
	})
	var wg sync.WaitGroup
	for _, filename := range []string{"river.geojson", "railway.geojson"} {
		wg.Add(1)
		options := Options{Enabled: true, Authorized: true, Endpoints: []string{server.URL}, OutputDir: t.TempDir()}
		go func(name string, opt Options) {
			defer wg.Done()
			r, err := Invoke(ctx, "osm_get_features", map[string]any{"extents": []string{"bbox:29,113,31,115"}, "tags": []string{"amenity"}, "output": name}, opt)
			if err != nil || r.FeatureCount != 2 {
				t.Errorf("download failed: %+v %v", r, err)
			}
		}(filename, options)
	}
	wg.Wait()
	if len(completed) != 2 || completed[0].RequestKey == completed[1].RequestKey || completed[0].Filename == completed[1].Filename {
		t.Fatalf("parallel progress mixed: %+v", completed)
	}
}

func TestDownloadBudgetPreservesCompletedExtentsAsPartial(t *testing.T) {
	fastNetwork(t)
	payload, _ := json.Marshal(Response{Elements: fixture()})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		q := r.Form.Get("data")
		if strings.Contains(q, "out skel") {
			fmt.Fprint(w, `{"elements":[]}`)
			return
		}
		if strings.Contains(q, "(0,0,1,1)") {
			<-r.Context().Done()
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	var terminal Progress
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	ctx = WithProgressCallback(ctx, func(p Progress) {
		if terminalProgress(p.Phase) {
			terminal = p
		}
	})
	r, err := Invoke(ctx, "osm_get_features", map[string]any{"extents": []string{"bbox:29,113,31,115", "bbox:0,0,1,1"}, "tags": []string{"amenity"}}, Options{
		Enabled: true, Authorized: true, Endpoints: []string{server.URL}, OutputDir: t.TempDir(),
	})
	if err != nil || !r.Partial || r.FeatureCount != 2 || len(r.FailedExtents) != 1 || len(r.ArtifactPaths) == 0 {
		t.Fatalf("completed data lost at deadline: %+v %v", r, err)
	}
	if terminal.Phase != "partial" || terminal.FeatureCount != 2 {
		t.Fatalf("partial success misrepresented: %+v", terminal)
	}
	for _, path := range r.ArtifactPaths {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCancelledDownloadDoesNotReportSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var last Progress
	ctx = WithProgressCallback(ctx, func(p Progress) { last = p })
	ctx, finish := beginProgress(ctx, "osm_get_features", "rail.geojson")
	cancel()
	reportProgress(ctx, Progress{Phase: "downloading"})
	finish(nil, context.Canceled)
	if last.Phase != "cancelled" {
		t.Fatalf("cancel shown as success: %+v", last)
	}
}

func TestDownloadValidatesAllInputsBeforeNetwork(t *testing.T) {
	fastNetwork(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"elements":[]}`)
	}))
	defer server.Close()
	for _, args := range []map[string]any{
		{"extents": []string{"bbox:29,113,31,115", "bbox:invalid"}, "tags": []string{"railway"}},
		{"extents": []string{"bbox:29,113,31,115"}, "tags": []string{"railway"}, "geometry": "invalid"},
	} {
		_, err := Invoke(context.Background(), "osm_get_features", args, Options{Enabled: true, Authorized: true, Endpoints: []string{server.URL}, OutputDir: t.TempDir()})
		if err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("download started before all inputs validated: %d", calls.Load())
	}
}

func TestDownloadDistinguishesOwnBudgetFromParentDeadline(t *testing.T) {
	fastNetwork(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if strings.Contains(r.Form.Get("data"), "out skel") {
			fmt.Fprint(w, `{"elements":[]}`)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	for _, own := range []bool{false, true} {
		ctx := context.Background()
		cancel := func() {}
		budget := 600
		if own {
			budget = 1
		} else {
			ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
		}
		_, err := Invoke(ctx, "osm_get_features", map[string]any{"extents": []string{"bbox:29,113,31,115"}, "tags": []string{"railway"}}, Options{
			Enabled: true, Authorized: true, Endpoints: []string{server.URL}, OutputDir: t.TempDir(), TotalTimeoutSeconds: budget,
		})
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline identity lost: %v", err)
		}
		if strings.Contains(err.Error(), "已达到总时限") != own {
			t.Fatalf("wrong budget blamed: own=%v err=%v", own, err)
		}
	}
}
