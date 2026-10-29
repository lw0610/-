package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/gorilla/mux"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type planAPIStub struct {
	edits, actions int
	last           plan.ActionRequest
	failure        error
}

func (s *planAPIStub) ListPlans(context.Context, string, string) ([]*plan.Snapshot, error) {
	return []*plan.Snapshot{{PlanID: "cp-test", PlanVersion: 2, Status: "preview"}}, nil
}
func (s *planAPIStub) EditPlan(context.Context, string, string, string, plan.EditRequest) (*plan.Snapshot, error) {
	s.edits++
	return &plan.Snapshot{PlanVersion: 3}, s.failure
}
func (s *planAPIStub) ActOnPlan(_ context.Context, _, _, _ string, a plan.ActionRequest) (*plan.Snapshot, error) {
	s.actions++
	s.last = a
	return &plan.Snapshot{PlanVersion: a.Version}, s.failure
}
func TestPlanHTTPRoutesDoNotExecuteOnListOrEdit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	stub := &planAPIStub{}
	a := &API{}
	a.SetPlanService(stub)
	router := mux.NewRouter()
	a.SetupRoutes(router)
	call := func(method, url string, body any, status int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, url, bytes.NewReader(raw))
		r.Header.Set("X-AutoCarto-Contract-Version", "v1")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, url, w.Code, w.Body.String())
		}
		out := map[string]any{}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	call("GET", "/api/sessions/session/plans", nil, 200)
	call("POST", "/api/sessions/session/plans/cp-test/edits", map[string]any{"version": 2, "data_id": "d1", "from": "user", "path": "sample.geojson"}, 200)
	if stub.actions != 0 || stub.edits != 1 {
		t.Fatal("edit invoked execution")
	}
	call("POST", "/api/sessions/session/plans/cp-test/start", map[string]any{"version": 3, "resume_run_id": "fr-test", "resume_resource_choice": "continue_snapshot", "resource_fingerprint": "digest"}, 200)
	if stub.last.Action != "start" || stub.last.ResourceChoice != "continue_snapshot" {
		t.Fatal(stub.last)
	}
	stub.failure = plan.ErrVersionConflict
	call("POST", "/api/sessions/session/plans/cp-test/start", map[string]any{"version": 2}, 409)
	stub.failure = &flowrun.ResourceChangeError{Change: flowrun.ResourceChange{Fingerprint: "new", Message: "资源已变化"}}
	out := call("POST", "/api/sessions/session/plans/cp-test/start", map[string]any{"version": 3}, 409)
	if out["error"] == nil {
		t.Fatal(out)
	}
	file := filepath.Join(root, "sample.geojson")
	if err := os.WriteFile(file, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/chat/attachments/check", map[string]any{"paths": []string{"sample.geojson", root}}, 200)
	call("POST", "/api/chat/attachments/check", map[string]any{"paths": []string{"missing"}}, 400)
}
