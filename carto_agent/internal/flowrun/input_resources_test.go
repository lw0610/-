package flowrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
)

func inputResourcePlan(t *testing.T) (*flow.FlowPlan, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", filepath.Join(root, "home"))
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	folder := filepath.Join(root, "package/resources/data")
	if err := os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	list := filepath.Join(folder, "list.csv")
	if err := os.WriteFile(list, []byte("name,path,required\n行政区,area.shp,true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	library := resource.NewLibrary("", "", nil)
	value := library.Resolve(context.Background(), resource.Ref{Type: "data_list", Name: "底图", Location: list, BaseDirFromInput: "数据目录"}, resource.Scope{FlowID: "example", FlowDir: filepath.Dir(filepath.Dir(folder))})
	if resource.HasErrors(value.Problems) || len(value.Entries) != 1 || !value.Entries[0].ExistsUnknown {
		t.Fatal(value)
	}
	p := testPlan(1)
	p.FlowDir = filepath.Dir(filepath.Dir(folder))
	p.Inputs = []flow.InputPlan{{Key: "数据目录", Label: "数据目录", Type: "folder", Required: true, Remember: true}}
	p.Resources = []flow.ResourcePlan{flow.ResourcePlanFromResolved(value)}
	p.Steps[0].Refs.Resources = []string{value.Key}
	data := filepath.Join(root, "datasets")
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	return p, data
}
func TestInputValidationMatchesRunAndRemembersOnlySuccessfulStart(t *testing.T) {
	plan, data := inputResourcePlan(t)
	values := map[string]any{"数据目录": data}
	problems := ValidateInputs(plan, values, filepath.Dir(data))
	if len(problems) == 0 || problems[0].Label != "数据目录" || problems[0].Code != "FLOW_DATA_FILE_MISSING" {
		t.Fatal(problems)
	}
	calls := 0
	c, request, _ := newTestController(t, plan, func(_ context.Context, r StepRequest) (StepResult, error) { calls++; return completed(r.Step), nil }, nil)
	request.Inputs = values
	request.ProjectRoot = filepath.Dir(data)
	_, err := c.Start(context.Background(), request)
	var invalid *InputError
	if !errors.As(err, &invalid) || invalid.Reasons[0].Code != problems[0].Code || calls != 0 {
		t.Fatal(err, calls)
	}
	memory, _ := flow.DefaultInputMemory()
	remembered, _ := memory.Get(plan.FlowID)
	if len(remembered) != 0 {
		t.Fatal("failed start wrote memory", remembered)
	}
	for _, ext := range []string{".shp", ".shx", ".dbf", ".prj"} {
		if err = os.WriteFile(filepath.Join(data, "area"+ext), []byte("real fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if got := ValidateInputs(plan, values, filepath.Dir(data)); len(got) != 0 {
		t.Fatal(got)
	}
	run, err := c.Start(context.Background(), request)
	if err != nil || calls != 1 {
		t.Fatal(run, err, calls)
	}
	r := run.Plan.Resources[0]
	if r.BaseDir != data || r.Entries[0].Path != filepath.Join(data, "area.shp") || r.Entries[0].ExistsUnknown || !r.Entries[0].Exists {
		t.Fatal(r)
	}
	if !plan.Resources[0].Entries[0].ExistsUnknown || plan.Resources[0].BaseDir != "" {
		t.Fatal("shared plan mutated", plan.Resources[0])
	}
	remembered, _ = memory.Get(plan.FlowID)
	if remembered["数据目录"] != data {
		t.Fatal(remembered)
	}
	run.Cursor = 0
	text, _, err := BuildStepMessage(plan.Steps[0], run)
	if err != nil || !strings.Contains(text, "位于「数据目录」") {
		t.Fatal(text, err)
	}
}
func TestInputResourceSnapshotFingerprintSeesFileChangesOnResume(t *testing.T) {
	plan, data := inputResourcePlan(t)
	for _, ext := range []string{".shp", ".shx", ".dbf", ".prj"} {
		_ = os.WriteFile(filepath.Join(data, "area"+ext), []byte("fixture"), 0600)
	}
	c, request, _ := newTestController(t, plan, func(context.Context, StepRequest) (StepResult, error) { return StepResult{Interrupted: true}, nil }, nil)
	request.Inputs = map[string]any{"数据目录": data}
	request.ProjectRoot = filepath.Dir(data)
	run, err := c.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	resume := ResumeRequest{SessionID: request.SessionID, SessionDir: request.SessionDir, FlowRunID: run.ID}
	if _, err = c.CheckResume(context.Background(), resume); err != nil {
		t.Fatal("unchanged input resource falsely changed", err)
	}
	_ = os.WriteFile(filepath.Join(data, "area.dbf"), []byte("different larger contents"), 0600)
	_, err = c.CheckResume(context.Background(), resume)
	var changed *ResourceChangeError
	if !errors.As(err, &changed) {
		t.Fatal("changed data was not detected", err)
	}
}
func TestDraftConversationReferenceDoesNotBypassActualSourceRequirement(t *testing.T) {
	plan := testPlan(1)
	plan.Inputs = []flow.InputPlan{{Key: "史料", Label: "史料", Type: "conversation_text", Required: true}}
	values := map[string]any{"史料": map[string]any{"$ref": "current_user_message"}}
	if problems := ValidateInputs(plan, values, t.TempDir()); len(problems) != 0 {
		t.Fatal(problems)
	}
	if _, err := ResolveInputs(plan, values, "", t.TempDir()); err == nil {
		t.Fatal("placeholder bypassed real source")
	}
	if problems := ValidateInputs(plan, map[string]any{"史料": ""}, t.TempDir()); len(problems) == 0 {
		t.Fatal("empty message accepted")
	}
}

type inputResourceEventFunc func(Event) int64

func (f inputResourceEventFunc) Emit(_ context.Context, event Event) int64 { return f(event) }

func TestRestartInputResourcesUsesConfirmedSnapshotAfterEndingOldRun(t *testing.T) {
	plan, data := inputResourcePlan(t)
	for _, ext := range []string{".shp", ".shx", ".dbf", ".prj"} {
		_ = os.WriteFile(filepath.Join(data, "area"+ext), []byte("fixture"), 0600)
	}
	calls := 0
	c, start, _ := newTestController(t, plan, func(_ context.Context, step StepRequest) (StepResult, error) {
		calls++
		if calls == 1 {
			return StepResult{Interrupted: true}, nil
		}
		return completed(step.Step), nil
	}, nil)
	start.Inputs = map[string]any{"数据目录": data}
	start.ProjectRoot = filepath.Dir(data)
	old, err := c.Start(context.Background(), start)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(data, "area.dbf"), []byte("a newer resource revision"), 0600)
	resume := ResumeRequest{SessionID: start.SessionID, SessionDir: start.SessionDir, FlowRunID: old.ID}
	_, err = c.CheckResume(context.Background(), resume)
	var changed *ResourceChangeError
	if !errors.As(err, &changed) {
		t.Fatal(err)
	}
	confirmed := changed.Change.Fingerprint
	c.options.Events = inputResourceEventFunc(func(event Event) int64 {
		if event.Event == "flow.finished" && event.FlowRunID == old.ID {
			_ = os.Remove(filepath.Join(data, "area.dbf"))
		}
		return 0
	})
	resume.ResourceChoice, resume.ResourceFingerprint = "restart", confirmed
	next, err := c.Restart(context.Background(), resume)
	if err != nil || next == nil || next.ID == old.ID || calls != 2 {
		t.Fatalf("restart reparsed after ending old run: %+v, %v, calls=%d", next, err, calls)
	}
	if next.Plan.LinkFingerprint != confirmed {
		t.Fatal("restart adopted an unconfirmed resource revision", next.Plan.LinkFingerprint, confirmed)
	}
}
