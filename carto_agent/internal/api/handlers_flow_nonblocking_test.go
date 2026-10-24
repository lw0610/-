package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"github.com/gorilla/mux"
)

const nonblockingFlow = "---\n名称: 快速读取\n编号: saved-flow\n---\n## 步骤\n### 确认\n- 负责：用户\n- 做法：确认执行结果\n- 完成标准：「确认」（确认结果）\n"

func TestFlowHTTPReadAndMutationNeverStartModelAnalysis(t *testing.T) {
	root := t.TempDir()
	store := flow.NewStore("", filepath.Join(root, "flows"), bindings.New(filepath.Join(root, "config", "resource_bindings.yaml")))
	var calls atomic.Int32
	compiler := flow.NewCompiler(flow.CompilerOptions{Store: store, CacheDir: filepath.Join(root, "cache"), LoadEnvironment: func(context.Context, string) (flow.Environment, error) {
		return flow.Environment{Scheme: "test", ModelName: "slow", Model: apiFlowModelFunc(func(ctx context.Context, r flow.ModelRequest) (string, error) {
			calls.Add(1)
			<-ctx.Done()
			return "", ctx.Err()
		})}, nil
	}})
	api := &API{}
	api.SetFlowStore(store)
	api.SetFlowCompiler(compiler)
	router := mux.NewRouter()
	api.SetupRoutes(router)
	// Even if an internal legacy manager exists, HTTP operations must not enqueue work.
	lifecycle, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := flow.NewEnrichmentManager(compiler, flow.EnrichmentOptions{})
	manager.Start(lifecycle)
	began := time.Now()
	flowFailureRequest(t, router, context.Background(), "POST", "/api/flows", map[string]any{"flow_md": nonblockingFlow}, 200)
	for _, path := range []string{"/api/flows", "/api/flows/saved-flow"} {
		flowFailureRequest(t, router, context.Background(), "GET", path, nil, 200)
	}
	saved := flowFailureRequest(t, router, context.Background(), "PUT", "/api/flows/saved-flow", map[string]any{"flow_md": strings.Replace(nonblockingFlow, "### 确认", "### 更新后的步骤", 1)}, 200)
	var current struct {
		Plan *flow.FlowPlan `json:"plan"`
	}
	if err := json.Unmarshal(saved, &current); err != nil || current.Plan == nil || len(current.Plan.Steps) != 1 || current.Plan.Steps[0].Title != "更新后的步骤" {
		t.Fatalf("saving did not return the current step preview: %s err=%v", saved, err)
	}
	flowFailureRequest(t, router, context.Background(), "POST", "/api/flows/saved-flow/copy", map[string]any{"new_id": "copied-flow"}, 200)
	source := filepath.Join(root, "import.md")
	if err := os.WriteFile(source, []byte(strings.Replace(nonblockingFlow, "saved-flow", "imported-flow", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	flowFailureRequest(t, router, context.Background(), "POST", "/api/flows/import", map[string]any{"source_path": source}, 200)
	if _, err := compiler.AvailablePlans(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.EnsureRunnable(context.Background(), "saved-flow", "test"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("request model calls=%d", calls.Load())
	}
	t.Logf("all local request paths model=0 elapsed=%s", time.Since(began))
	if job, ok := manager.Status("saved-flow", "test"); ok {
		t.Fatalf("saving unexpectedly enqueued model analysis: %+v", job)
	}
	for _, method := range []string{http.MethodPost, http.MethodDelete, http.MethodGet} {
		path := "/api/flows/saved-flow/compile"
		if method == http.MethodGet {
			path += "-status"
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("removed endpoint %s %s returned %d", method, path, response.Code)
		}
	}
}
