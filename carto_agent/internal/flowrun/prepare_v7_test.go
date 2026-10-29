package flowrun

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/planexec"
)

func preparationV7Fixture(t *testing.T, data []plan.Data, steps []plan.Step) (*flow.FlowPlan, StartRequest) {
	t.Helper()
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	p := &plan.Plan{APIVersion: plan.APIVersion, ID: "cp-prepare-v7", Version: 1, Goal: "核验数据准备和消费顺序", Data: data, Steps: steps}
	for i := range p.Steps {
		if len(p.Steps[i].Capabilities) == 0 {
			p.Steps[i].Capabilities = []string{"read"}
		}
		if p.Steps[i].Instructions == "" {
			p.Steps[i].Instructions = "使用已准备的数据完成本步"
		}
		if len(p.Steps[i].Produces) == 0 {
			p.Steps[i].Produces = []plan.Product{{ID: "result_" + p.Steps[i].ID, Kind: "text"}}
		}
	}
	env := flow.Environment{Scheme: "test", Roles: []flow.RoleSpec{{ID: "worker", Enabled: true, Kind: "agent", ToolPackages: []string{"read", "statistical-data-tools", "data-loading"}}, {ID: "knowledge_engineer", Enabled: true, Kind: "executor", Executor: "route_knowledge"}}, Packages: []flow.PackageSpec{
		{ID: "read", Kind: "category", Enabled: true, Tools: []string{"file_read"}},
		{ID: "statistical-data-tools", Kind: "category", Enabled: true, Tools: []string{"table_read"}},
		{ID: "data-loading", Kind: "category", Enabled: true, Tools: []string{"data_load_vector"}},
	}}
	executable, inputs, err := planexec.Build(p, planexec.BuildOptions{Environment: env})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	return executable, StartRequest{SessionID: "prepare-v7", SessionDir: dir, ProjectRoot: dir, FlowID: p.ID, Scheme: "test", CartoPlan: p, Inputs: inputs}
}

func TestV7ParallelPreparationKeepsSuccessfulSiblingAndBothLedgers(t *testing.T) {
	ex, req := preparationV7Fixture(t, []plan.Data{
		{ID: "river", Name: "河流", Source: "retrieve", Need: &plan.Need{Category: "hydro.river_line"}},
		{ID: "railway", Name: "铁路", Source: "retrieve", Need: &plan.Need{Category: "transport.railway"}},
	}, []plan.Step{{ID: "draw", Title: "制图", Inputs: []string{"river", "railway"}}})
	path := filepath.Join(req.SessionDir, "railway.geojson")
	if err := os.WriteFile(path, []byte(`{"type":"FeatureCollection","features":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	entered := make(chan string, 2)
	release := make(chan struct{})
	var consumed atomic.Int32
	controller, err := NewController(Options{Store: FileRunStore{}, AcquisitionConfig: func() config.AcquisitionConfig { return config.AcquisitionConfig{Concurrency: 2} }, DataGet: func(ctx context.Context, _ *RunState, d plan.Data) (DataResult, error) {
		entered <- d.ID
		select {
		case <-release:
		case <-ctx.Done():
			return DataResult{}, ctx.Err()
		}
		result := DataResult{Ledger: []PreparationAttempt{{Method: "fixture", Action: "读取本地核验夹具", Result: "completed", Paths: []string{path}}}}
		if d.ID == "river" {
			result.Ledger[0].Result = "failed"
			result.Ledger[0].Paths = nil
			return result, errors.New("河流文件内容不符")
		}
		result.Value = path
		return result, nil
	}, Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
		consumed.Add(1)
		return cartoReceipt(r), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type outcome struct {
		run *RunState
		err error
	}
	done := make(chan outcome, 1)
	go func() { run, e := controller.StartFromPlan(ctx, req, ex); done <- outcome{run, e} }()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("independent acquisitions did not start concurrently")
		}
	}
	for _, id := range []string{"river", "railway"} {
		raw, err := os.ReadFile(filepath.Join(req.SessionDir, "data", "ledgers", id+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var state DataPreparationState
		if err := json.Unmarshal(raw, &state); err != nil {
			t.Fatal(err)
		}
		if state.Status != "running" || state.StartedAt == nil || state.StartedAt.IsZero() {
			t.Fatalf("active download has no clock origin: %+v", state)
		}
	}
	close(release)
	got := <-done
	if got.err != nil || got.run.Status != "needs_replan" || consumed.Load() != 0 || got.run.AcquiredInputs["railway"] != path {
		t.Fatalf("successful sibling lost: run=%+v err=%v consumers=%d", got.run, got.err, consumed.Load())
	}
	saved, err := (FileRunStore{}).Load(context.Background(), req.SessionDir, got.run.ID)
	if err != nil || saved.AcquiredInputs["railway"] != path {
		t.Fatal(saved, err)
	}
	for id, want := range map[string]string{"river": "failed", "railway": "completed"} {
		raw, e := os.ReadFile(filepath.Join(req.SessionDir, "data", "ledgers", id+".json"))
		if e != nil {
			t.Fatal(e)
		}
		var state DataPreparationState
		if e = json.Unmarshal(raw, &state); e != nil {
			t.Fatal(e)
		}
		if state.Status != want || len(state.Ledger) != 1 || state.StartedAt != nil {
			t.Fatal(state)
		}
	}
}

func TestV7PreparesInputOnlyBeforeItsConsumer(t *testing.T) {
	ex, req := preparationV7Fixture(t, []plan.Data{{ID: "statistics", Name: "统计表", Source: "retrieve", Need: &plan.Need{Category: "stats.annual_indicator"}}}, []plan.Step{{ID: "inspect", Title: "查看任务"}, {ID: "consume", Title: "消费统计", Inputs: []string{"statistics"}}})
	path := filepath.Join(req.SessionDir, "statistics.csv")
	if err := os.WriteFile(path, []byte("name,value\n郑州市,1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var inspected atomic.Bool
	var gets atomic.Int32
	controller, err := NewController(Options{Store: FileRunStore{}, DataGet: func(context.Context, *RunState, plan.Data) (DataResult, error) {
		gets.Add(1)
		if !inspected.Load() {
			return DataResult{}, errors.New("retrieval ran before earlier unrelated step")
		}
		return DataResult{Value: path}, nil
	}, Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
		if r.Step.ID == "inspect" {
			if gets.Load() != 0 {
				return StepResult{}, errors.New("unused data fetched eagerly")
			}
			inspected.Store(true)
		}
		if r.Step.ID == "consume" && !strings.Contains(r.Messages[0].Content, path) {
			return StepResult{}, errors.New("consumer did not receive prepared file")
		}
		return cartoReceipt(r), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	run, err := controller.StartFromPlan(context.Background(), req, ex)
	if err != nil || run.Status != "completed" || gets.Load() != 1 {
		t.Fatal(run, err, gets.Load())
	}
}

func TestV7CancelPausedPreparationPersistsCancelled(t *testing.T) {
	ex, req := preparationV7Fixture(t, []plan.Data{{ID: "table", Name: "统计表", Source: "retrieve", Need: &plan.Need{Category: "stats.annual_indicator"}}}, []plan.Step{{ID: "consume", Title: "消费", Inputs: []string{"table"}}})
	controller, err := NewController(Options{Store: FileRunStore{}, DataGet: func(context.Context, *RunState, plan.Data) (DataResult, error) {
		return DataResult{}, errors.New("status code: 402, Insufficient Balance")
	}, Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) { return cartoReceipt(r), nil })})
	if err != nil {
		t.Fatal(err)
	}
	run, err := controller.StartFromPlan(context.Background(), req, ex)
	if err != nil || run.Status != "interrupted" {
		t.Fatal(run, err)
	}
	cancelled, err := controller.Cancel(context.Background(), req.SessionID)
	if err != nil || cancelled == nil || cancelled.Status != "cancelled" {
		t.Fatal(cancelled, err)
	}
	saved, err := (FileRunStore{}).Load(context.Background(), req.SessionDir, run.ID)
	if err != nil || saved.Status != "cancelled" {
		t.Fatal(saved, err)
	}
}

func TestV7TableStepDoesNotNeedQGISConnection(t *testing.T) {
	ex, req := preparationV7Fixture(t, nil, []plan.Step{{ID: "table", Title: "整理表格", Capabilities: []string{"statistical-data-tools"}, Produces: []plan.Product{{ID: "table_result", Kind: "file", Format: "csv"}}}})
	path := filepath.Join(req.SessionDir, "table.csv")
	calls := 0
	controller, err := NewController(Options{Store: FileRunStore{}, Tools: toolFunc(func(context.Context, string, map[string]any) (any, error) {
		calls++
		return nil, errors.New("QGIS disconnected")
	}), Runner: cartoRunner(func(context.Context, StepRequest) (StepResult, error) {
		if e := os.WriteFile(path, []byte("name,value\n郑州市,1\n"), 0600); e != nil {
			return StepResult{}, e
		}
		raw, _ := json.Marshal(Receipt{Status: "completed", Outputs: map[string]any{"table_result": path}})
		return StepResult{LastContent: string(raw)}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	run, err := controller.StartFromPlan(context.Background(), req, ex)
	if err != nil || run.Status != "completed" || calls != 0 {
		t.Fatal(run, err, calls)
	}
}

func TestV7QGISSnapshotFailurePausesBeforeRunningStep(t *testing.T) {
	ex, req := preparationV7Fixture(t, nil, []plan.Step{{ID: "load", Title: "加载图层", Capabilities: []string{"data-loading"}}})
	calls, ran := 0, 0
	controller, err := NewController(Options{Store: FileRunStore{}, Tools: toolFunc(func(_ context.Context, name string, _ map[string]any) (any, error) {
		calls++
		if name != "get_layers_info" {
			t.Errorf("unexpected tool %s", name)
		}
		return nil, errors.New("QGIS disconnected")
	}), Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) { ran++; return cartoReceipt(r), nil })})
	if err != nil {
		t.Fatal(err)
	}
	run, err := controller.StartFromPlan(context.Background(), req, ex)
	if err != nil || run.Status != "interrupted" || ran != 0 || calls != 1 || !strings.Contains(run.Steps[0].LastFailure, "QGIS disconnected") {
		t.Fatal(run, err, ran, calls)
	}
	saved, err := (FileRunStore{}).Load(context.Background(), req.SessionDir, run.ID)
	if err != nil || saved.Status != "interrupted" {
		t.Fatal(saved, err)
	}
}
