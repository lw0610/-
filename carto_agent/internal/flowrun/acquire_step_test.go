package flowrun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
)

type fixtureAcquireAdapter struct {
	fetch func(context.Context, acquire.FetchRequest) (acquire.FetchResult, error)
}

func (fixtureAcquireAdapter) Kind() string { return "offline_asset" }
func (a fixtureAcquireAdapter) Fetch(ctx context.Context, r acquire.FetchRequest) (acquire.FetchResult, error) {
	return a.fetch(ctx, r)
}
func acquisitionFixture(t *testing.T, fetch func(context.Context, acquire.FetchRequest) (acquire.FetchResult, error)) (*flow.FlowPlan, *acquire.Engine) {
	t.Helper()
	p := testPlan(2)
	p.Inputs = []flow.InputPlan{{Key: "data", Label: "数据", Type: "file", Acquirable: true}}
	item := flow.AcquisitionItem{Key: "铁路", Requirement: "铁路", Entry: "铁路", Input: "data", InputType: "file", Category: "transport.railway", Mode: "auto", Required: true, Available: true, Acquirable: true, Extent: &flow.ExtentPlan{BBox: []float64{100, 20, 101, 21}}, Chosen: &flow.SourceChoice{SourceID: "source.fixture", Tier: 3, License: "CC0", Attribution: "合成测试", Provide: datasource.Provide{Category: "transport.railway"}}}
	p.Acquisition = &flow.AcquisitionPlan{Items: []flow.AcquisitionItem{item}}
	p.Steps = append(p.Steps[:1], append([]flow.StepPlan{{ID: "acquire-1", Kind: "acquire", Title: "准备铁路", AcquisitionRequirement: "铁路"}}, p.Steps[1:]...)...)
	engine := &acquire.Engine{Store: acquire.NewStore(t.TempDir(), 1024*1024), Config: config.AcquisitionConfig{}.WithDefaults(), Catalog: &datasource.Catalog{Categories: []datasource.Category{{ID: "transport.railway", Geometry: "line", Validate: datasource.Validation{MinFeatures: 1}}}, Sources: []datasource.Source{{ID: "source.fixture", Version: "1", Kind: "offline_asset", Status: "active", Provides: []datasource.Provide{{Category: "transport.railway"}}, License: "CC0"}}}, Adapters: map[string]acquire.Adapter{"offline_asset": fixtureAcquireAdapter{fetch: fetch}}, Invoke: func(ctx context.Context, name string, args map[string]any) (any, error) {
		if name != "data_normalize" {
			return nil, fmt.Errorf("unexpected %s", name)
		}
		out := args["output"].(string)
		if err := os.WriteFile(out, []byte("synthetic normalized data"), 0600); err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "data": map[string]any{"paths": []string{out}, "count": 1, "geometry_type": "MultiLineString", "crs": "EPSG:4326", "extent_wgs84": []float64{100, 20, 101, 21}, "fields": map[string]float64{}}}, nil
	}}
	return p, engine
}
func successfulFetch(ctx context.Context, r acquire.FetchRequest) (acquire.FetchResult, error) {
	path := filepath.Join(r.WorkDir, "input.geojson")
	if err := os.WriteFile(path, []byte("synthetic raw data"), 0600); err != nil {
		return acquire.FetchResult{}, err
	}
	return acquire.FetchResult{Files: []string{path}, Format: "geojson", Notes: []string{"合成测试"}}, nil
}
func startPreparedAcquisition(t *testing.T, c *Controller, request StartRequest, p *flow.FlowPlan) (*RunState, error) {
	t.Helper()
	request.ProjectRoot = request.SessionDir
	return c.startPreparedPlan(context.Background(), request, p, map[string]any{"data": map[string]any{"$acquire": true}})
}
func TestAcquisitionPrefetchParallelEffectiveInputAndCache(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	p, engine := acquisitionFixture(t, func(ctx context.Context, r acquire.FetchRequest) (acquire.FetchResult, error) {
		if calls.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return acquire.FetchResult{}, ctx.Err()
			}
		}
		return successfulFetch(ctx, r)
	})
	runnerCalls := 0
	c, request, events := newTestController(t, p, func(ctx context.Context, r StepRequest) (StepResult, error) {
		runnerCalls++
		if r.Step.ID == "s1" {
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("acquisition did not start alongside model step")
			}
			close(release)
		} else {
			if !strings.Contains(r.Messages[0].Content, "自动获取；来源") || strings.Contains(r.Messages[0].Content, "$acquire") {
				t.Fatal(r.Messages[0].Content)
			}
		}
		return completed(r.Step), nil
	}, func(o *Options) {
		o.AcquisitionFactory = func(context.Context, *RunState) (*acquire.Engine, error) { return engine, nil }
	})
	run, err := startPreparedAcquisition(t, c, request, p)
	if err != nil || run.Status != "completed" {
		t.Fatalf("%+v %v", run, err)
	}
	if runnerCalls != 2 || len(run.Acquisition.Items) != 1 || run.Acquisition.Items[0].Status != "completed" || !events.has("flow.acquire_item_done") {
		t.Fatalf("%+v", run)
	}
	if ok, e := EvaluateRule(&flow.ConditionRule{Op: "provided", Input: "data"}, run); !ok || e != nil {
		t.Fatal("acquired input unavailable to conditions")
	}
	if _, ok := run.Inputs["data"].(map[string]any); !ok {
		t.Fatal("original choice overwritten")
	}
	if _, err := os.Stat(EffectiveInputs(run)["data"].(string)); err != nil {
		t.Fatal(err)
	}
	c2, r2, _ := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) { return completed(r.Step), nil }, func(o *Options) {
		o.AcquisitionFactory = func(context.Context, *RunState) (*acquire.Engine, error) { return engine, nil }
	})
	again, err := startPreparedAcquisition(t, c2, r2, p)
	if err != nil || again.Acquisition.Items[0].Status != "reused" || calls.Load() != 1 {
		t.Fatalf("%+v %v calls=%d", again, err, calls.Load())
	}
}
func TestAcquisitionPendingOutputDoesNotStartEarly(t *testing.T) {
	var producerDone atomic.Bool
	p, engine := acquisitionFixture(t, func(ctx context.Context, r acquire.FetchRequest) (acquire.FetchResult, error) {
		if !producerDone.Load() {
			return acquire.FetchResult{}, fmt.Errorf("started before producer")
		}
		return successfulFetch(ctx, r)
	})
	p.Acquisition.Items[0].Extent = &flow.ExtentPlan{Pending: true, DependencyStep: "s1", DependencyOutput: "范围", ExpandPercent: 20}
	p.Steps[0].ExpectedOutputs = []flow.OutputDeclaration{{Label: "范围", Type: "file", Required: true}}
	c, request, _ := newTestController(t, p, func(ctx context.Context, r StepRequest) (StepResult, error) {
		if r.Confirmation != nil {
			t.Fatalf("unexpected confirmation: %+v", r.Confirmation)
		}
		if r.Step.ID == "s1" {
			path := filepath.Join(r.SessionDir, "range.geojson")
			_ = os.WriteFile(path, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"LineString","coordinates":[[100,20],[101,21]]}}]}`), 0600)
			producerDone.Store(true)
			raw, _ := json.Marshal(Receipt{Status: "completed", Outputs: map[string]any{"范围": path}})
			return StepResult{LastContent: string(raw)}, nil
		}
		return completed(r.Step), nil
	}, func(o *Options) {
		o.AcquisitionFactory = func(context.Context, *RunState) (*acquire.Engine, error) { return engine, nil }
	})
	run, err := startPreparedAcquisition(t, c, request, p)
	if err != nil || run.Status != "completed" {
		t.Fatalf("%+v %v", run, err)
	}
	if run.Acquisition.Plan.Items[0].Extent.Pending || run.Acquisition.Plan.Items[0].Extent.BBox[0] >= 100 {
		t.Fatal("extent not resolved/expanded")
	}
}
func TestAcquisitionFailureProvideAndRequiredCannotSkip(t *testing.T) {
	p, engine := acquisitionFixture(t, func(context.Context, acquire.FetchRequest) (acquire.FetchResult, error) {
		return acquire.FetchResult{}, fmt.Errorf("offline")
	})
	var interrupted string
	c, request, events := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) {
		if r.Confirmation != nil {
			if r.Confirmation.Category != "flow_acquisition" {
				t.Fatal(r.Confirmation)
			}
			for _, o := range r.Confirmation.Options {
				if o.Value == "skip" {
					t.Fatal("required data may not be skipped")
				}
			}
			interrupted = r.RunID
			return StepResult{Interrupted: true}, nil
		}
		return completed(r.Step), nil
	}, func(o *Options) {
		o.AcquisitionFactory = func(context.Context, *RunState) (*acquire.Engine, error) { return engine, nil }
	})
	run, err := startPreparedAcquisition(t, c, request, p)
	if err != nil || run.Status != "waiting_user" || !events.has("flow.acquire_failed") {
		t.Fatalf("%+v %v", run, err)
	}
	path := filepath.Join(request.SessionDir, "provided.geojson")
	_ = os.WriteFile(path, []byte(`{"type":"FeatureCollection","features":[]}`), 0600)
	raw, _ := json.Marshal(Choice{Choice: "provide", Guidance: path})
	resumed, err := c.OnStepRunFinished(context.Background(), interrupted, StepResult{LastContent: string(raw)})
	if err != nil || resumed.Status != "completed" || EffectiveInputs(resumed)["data"] != path {
		t.Fatalf("%+v %v", resumed, err)
	}
}
func TestAcquisitionCancellationStopsWorker(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	p, engine := acquisitionFixture(t, func(ctx context.Context, r acquire.FetchRequest) (acquire.FetchResult, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return acquire.FetchResult{}, ctx.Err()
	})
	c, request, _ := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) { return completed(r.Step), nil }, func(o *Options) {
		o.AcquisitionFactory = func(context.Context, *RunState) (*acquire.Engine, error) { return engine, nil }
	})
	done := make(chan error, 1)
	go func() {
		request.ProjectRoot = request.SessionDir
		_, err := c.startPreparedPlan(context.Background(), request, p, map[string]any{})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker not started")
	}
	state, err := c.Cancel(context.Background(), request.SessionID)
	if err != nil || state.Status != "cancelled" {
		t.Fatal(state, err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("worker survived cancel")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("controller did not stop")
	}
}
