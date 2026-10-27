package flowrun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
)

func reviewFile(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("synthetic fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAcquisitionRestoredListPreservesPreviouslyAcquiredEntries(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", filepath.Join(root, "home"))
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	first := reviewFile(t, filepath.Join(root, "session", "first.gpkg"))
	second := reviewFile(t, filepath.Join(root, "session", "second.gpkg"))
	run := &RunState{ID: "flow-review", SessionID: "session", SessionDir: filepath.Dir(first), ProjectRoot: root, Plan: testPlan(1), Inputs: map[string]any{"base": map[string]any{"$acquire": true}}, AcquiredInputs: map[string]any{"base": map[string]string{"first": first}}, EntryOverrides: map[string]map[string]string{"data_list.base": {"first": first}}}
	restored, err := cloneRun(run)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := restored.AcquiredInputs["base"].(map[string]any); !ok {
		t.Fatal("fixture did not cross the persisted JSON boundary")
	}
	(&Controller{}).applyAcquisitionResult(restored, flow.AcquisitionItem{Key: "second", Input: "base", Resource: "data_list.base", Entry: "second"}, acquire.ItemResult{Status: "completed", Output: acquire.OutputRecord{Paths: []string{second}}})
	effective, ok := EffectiveInputs(restored)["base"].(map[string]string)
	if !ok || len(effective) != 2 || effective["first"] != first || effective["second"] != second {
		t.Fatalf("restored list lost earlier rows: %#v", EffectiveInputs(restored))
	}
	if overrides := restored.EntryOverrides["data_list.base"]; len(overrides) != 2 || overrides["first"] != first || overrides["second"] != second {
		t.Fatalf("overrides diverged: %#v", overrides)
	}
}

func TestFlowFilesUseActualProjectSessionAndResolveRelativeOutputs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", filepath.Join(root, "home"))
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	project := reviewFile(t, filepath.Join(root, "qgis-project", "map.qgz"))
	dir, err := config.ResolveSessionDir(project, "session")
	if err != nil {
		t.Fatal(err)
	}
	acquired := reviewFile(t, filepath.Join(root, "home", "data", "acquired", "railway.gpkg"))
	relative := reviewFile(t, filepath.Join(dir, "outputs", "map.png"))
	projectOutput := reviewFile(t, filepath.Join(filepath.Dir(project), "export.pdf"))
	outside := reviewFile(t, filepath.Join(root, "application", "private.txt"))
	run := &RunState{ID: "flow-review", SessionID: "session", SessionDir: dir, ProjectRoot: filepath.Join(root, "application"), ProjectPath: project, Plan: testPlan(1), Inputs: map[string]any{}}
	c := &Controller{}
	c.applyAcquisitionResult(run, flow.AcquisitionItem{Key: "railway", Input: "data", Entry: "铁路"}, acquire.ItemResult{Status: "completed", SourceID: "source.fixture", Output: acquire.OutputRecord{Paths: []string{acquired}}})
	c.registerFlowOutputs(run, "s1", map[string]any{"image": "outputs/map.png", "export": []string{projectOutput}, "duplicate": []any{relative}, "unrelated": outside, "summary": "non-file receipt text"})
	store, err := sessionfiles.New(project, "session")
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.List(true)
	if err != nil || len(items) != 3 {
		t.Fatalf("wrong project-scoped index: %+v %v", items, err)
	}
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.Path] = true
		if item.FlowRunID != run.ID {
			t.Fatalf("missing run origin: %+v", item)
		}
	}
	for _, path := range []string{acquired, relative, projectOutput} {
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil || !seen[canonical] {
			t.Fatalf("missing output %s: %+v %v", path, items, err)
		}
	}
	wrong, err := config.ResolveSessionDir(run.ProjectRoot, "session")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wrong); !os.IsNotExist(err) {
		t.Fatalf("application root was used to create a second session index: %s %v", wrong, err)
	}
}

func TestLegacyRunWithoutProjectPathStillIndexesSessionOutputs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", filepath.Join(root, "home"))
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	file := reviewFile(t, filepath.Join(root, "session", "result.txt"))
	outside := reviewFile(t, filepath.Join(root, "application", "private.txt"))
	run := &RunState{ID: "legacy-run", SessionID: "session", SessionDir: filepath.Dir(file), ProjectRoot: filepath.Dir(outside)}
	(&Controller{}).registerFlowOutputs(run, "s1", map[string]any{"result": "result.txt", "outside": outside})
	store, err := runSessionFileStore(run)
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.List(true)
	if err != nil || len(items) != 1 || items[0].Name != "result.txt" {
		t.Fatalf("legacy session output was not safely indexed: %+v %v", items, err)
	}
}

func TestProjectPathSurvivesPersistenceResumeAndRestart(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(fmt.Sprint(restart), func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("AUTOCARTO_HOME", filepath.Join(root, "home"))
			t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
			project := reviewFile(t, filepath.Join(root, "project", "map.qgz"))
			original := resourcePlan("original", "/original/base.csv")
			c, start, _ := newTestController(t, original, func(context.Context, StepRequest) (StepResult, error) { return StepResult{Interrupted: true}, nil }, nil)
			start.ProjectPath = project
			state, err := c.Start(context.Background(), start)
			if err != nil {
				t.Fatal(err)
			}
			state.Status = "interrupted"
			if err := (FileRunStore{}).Save(context.Background(), state); err != nil {
				t.Fatal(err)
			}
			stored, err := (FileRunStore{}).Load(context.Background(), start.SessionDir, state.ID)
			if err != nil || stored.ProjectPath != project {
				t.Fatalf("project identity lost in run snapshot: %+v %v", stored, err)
			}
			current := original
			request := ResumeRequest{SessionID: start.SessionID, SessionDir: start.SessionDir, FlowRunID: state.ID}
			if restart {
				current = resourcePlan("new", "/current/base.csv")
				request.ResourceChoice, request.ResourceFingerprint = "restart", "new"
			}
			next, _, _ := newTestController(t, current, func(_ context.Context, r StepRequest) (StepResult, error) { return completed(r.Step), nil }, nil)
			var resumed *RunState
			if restart {
				resumed, err = next.Restart(context.Background(), request)
			} else {
				resumed, err = next.Resume(context.Background(), request)
			}
			if err != nil || resumed.Status != "completed" || resumed.ProjectPath != project {
				t.Fatalf("project identity lost after resume/restart: %+v %v", resumed, err)
			}
		})
	}
}

func TestAcquisitionProvidedInvalidDataKeepsChoiceThenRecovers(t *testing.T) {
	for _, invalid := range []string{"geometry", "fields", "unreadable"} {
		t.Run(invalid, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("AUTOCARTO_HOME", filepath.Join(root, "home"))
			t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
			p, engine := acquisitionFixture(t, func(context.Context, acquire.FetchRequest) (acquire.FetchResult, error) {
				return acquire.FetchResult{}, fmt.Errorf("offline")
			})
			p.Acquisition.Items[0].Fields = map[string]string{"NAME": "name"}
			valid := false
			calls := 0
			engine.Invoke = func(_ context.Context, name string, args map[string]any) (any, error) {
				calls++
				if name != "data_normalize" || len(args["fields"].(map[string][]string)) != 0 {
					t.Fatalf("provided file was not checked without synthetic field mapping: %s %#v", name, args)
				}
				if _, exists := args["clip"]; exists {
					t.Fatal("validation must not manufacture range coverage by clipping the original")
				}
				if !valid && invalid == "unreadable" {
					return nil, fmt.Errorf("unreadable geodata")
				}
				stats := map[string]any{"count": 1, "geometry_type": "MultiLineString", "crs": "EPSG:4326", "extent_wgs84": []float64{100, 20, 101, 21}, "fields": map[string]float64{"NAME": 1}}
				if !valid && invalid == "geometry" {
					stats["geometry_type"] = "Point"
				}
				if !valid && invalid == "fields" {
					stats["fields"] = map[string]float64{"other": 1}
				}
				return map[string]any{"success": true, "data": stats}, nil
			}
			var interrupted string
			confirmations := 0
			c, request, _ := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) {
				if r.Confirmation != nil {
					confirmations++
					interrupted = r.RunID
					return StepResult{Interrupted: true}, nil
				}
				return completed(r.Step), nil
			}, func(o *Options) {
				o.AcquisitionFactory = func(context.Context, *RunState) (*acquire.Engine, error) { return engine, nil }
			})
			run, err := startPreparedAcquisition(t, c, request, p)
			if err != nil || run.Status != "waiting_user" {
				t.Fatalf("expected initial failed fetch choice: %+v %v", run, err)
			}
			path := reviewFile(t, filepath.Join(request.SessionDir, "provided.geojson"))
			raw, _ := json.Marshal(Choice{Choice: "provide", Guidance: path})
			paused, err := c.OnStepRunFinished(context.Background(), interrupted, StepResult{LastContent: string(raw)})
			if err != nil || paused.Status != "waiting_user" || paused.Acquisition.Items[0].Status != "failed" || len(paused.AcquiredInputs) != 0 || confirmations != 2 || calls != 1 {
				t.Fatalf("invalid file advanced flow or lost retry: %+v %v confirmations=%d calls=%d", paused, err, confirmations, calls)
			}
			if paused.Acquisition.Items[0].Error == "" || strings.Contains(paused.Acquisition.Items[0].Error, "offline") {
				t.Fatal("new validation failure did not replace the fetch error")
			}
			valid = true
			resumed, err := c.OnStepRunFinished(context.Background(), interrupted, StepResult{LastContent: string(raw)})
			if err != nil || resumed.Status != "completed" || resumed.Acquisition.Items[0].Status != "user_provided" || EffectiveInputs(resumed)["data"] != path || calls != 2 {
				t.Fatalf("valid correction did not recover: %+v %v calls=%d", resumed, err, calls)
			}
			bytes, err := os.ReadFile(path)
			if err != nil || string(bytes) != "synthetic fixture" {
				t.Fatal("provided original was modified", err)
			}
			leftovers, err := filepath.Glob(filepath.Join(engine.Store.Root, ".verify-*"))
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("validation scratch files leaked: %v %v", leftovers, err)
			}
		})
	}
}

func TestAcquisitionStepsHaveDistinctConfirmationRunIDs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", filepath.Join(root, "home"))
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	p, engine := acquisitionFixture(t, func(context.Context, acquire.FetchRequest) (acquire.FetchResult, error) {
		return acquire.FetchResult{}, fmt.Errorf("offline")
	})
	p.Acquisition.Items[0].Required = false
	second := p.Acquisition.Items[0]
	second.Key, second.Requirement, second.Entry, second.Input = "公路", "公路", "公路", "roads"
	p.Acquisition.Items = append(p.Acquisition.Items, second)
	p.Steps = append(p.Steps[:2], append([]flow.StepPlan{{ID: "acquire-2", Index: 0, Kind: "acquire", Title: "准备公路", AcquisitionRequirement: "公路"}}, p.Steps[2:]...)...)
	ids := []string{}
	c, request, _ := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) {
		if r.Confirmation != nil {
			ids = append(ids, r.RunID)
			return StepResult{Interrupted: true}, nil
		}
		return completed(r.Step), nil
	}, func(o *Options) {
		o.AcquisitionFactory = func(context.Context, *RunState) (*acquire.Engine, error) { return engine, nil }
	})
	run, err := startPreparedAcquisition(t, c, request, p)
	if err != nil || run.Status != "waiting_user" || len(ids) != 1 {
		t.Fatalf("first confirmation unavailable: %+v %v %v", run, err, ids)
	}
	paused, err := c.OnStepRunFinished(context.Background(), ids[0], StepResult{LastContent: `{"choice":"skip"}`})
	if err != nil || paused.Status != "waiting_user" || len(ids) != 2 {
		t.Fatalf("second confirmation unavailable: %+v %v %v", paused, err, ids)
	}
	if ids[0] == ids[1] {
		t.Fatalf("system steps collided in the checkpoint key: %v", ids)
	}
	if duplicate, err := c.OnStepRunFinished(context.Background(), ids[0], StepResult{LastContent: `{"choice":"skip"}`}); err != nil || duplicate != nil {
		t.Fatalf("old confirmation ID was not ignored: %+v %v", duplicate, err)
	}
	finished, err := c.OnStepRunFinished(context.Background(), ids[1], StepResult{LastContent: `{"choice":"skip"}`})
	if err != nil || finished.Status != "completed_with_issues" {
		t.Fatalf("second confirmation could not finish independently: %+v %v", finished, err)
	}
}

type failAfterAcquisitionStarts struct {
	FileRunStore
	started <-chan struct{}
}

func (s failAfterAcquisitionStarts) Save(ctx context.Context, run *RunState) error {
	if run.Acquisition != nil && len(run.Steps[0].Attempts) > 0 {
		select {
		case <-s.started:
			return fmt.Errorf("synthetic disk full")
		case <-time.After(time.Second):
			return fmt.Errorf("fixture worker did not start")
		}
	}
	return s.FileRunStore.Save(ctx, run)
}

func TestAcquisitionStorageFailureCancelsPrefetchAndReleasesSession(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", filepath.Join(root, "home"))
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	started, stopped := make(chan struct{}), make(chan struct{})
	p, engine := acquisitionFixture(t, func(ctx context.Context, _ acquire.FetchRequest) (acquire.FetchResult, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return acquire.FetchResult{}, ctx.Err()
	})
	lifecycle, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, request, _ := newTestController(t, p, func(context.Context, StepRequest) (StepResult, error) {
		t.Fatal("step executed after the state could not be saved")
		return StepResult{}, nil
	}, func(o *Options) {
		o.Lifecycle = lifecycle
		o.Store = failAfterAcquisitionStarts{started: started}
		o.AcquisitionFactory = func(context.Context, *RunState) (*acquire.Engine, error) { return engine, nil }
	})
	run, err := startPreparedAcquisition(t, c, request, p)
	if err == nil || !strings.Contains(err.Error(), "synthetic disk full") || run.Status != "interrupted" {
		t.Fatalf("storage failure was not surfaced: %+v %v", run, err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("acquisition worker survived a failed state save")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessions[request.SessionID] != "" || len(c.waiting) != 0 {
		t.Fatalf("failed execution retained the session guard: %v %v", c.sessions, c.waiting)
	}
}
