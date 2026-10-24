package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/gorilla/mux"
)

const mutationFailureFlow = "---\n名称: 自动解析失败回归\n编号: saved-flow\n---\n## 步骤\n### 检查\n- 负责：用户\n- 做法：检查「未定义材料」\n- 完成标准：「确认」（确认结果）\n"

func flowFailureRequest(t *testing.T, router http.Handler, ctx context.Context, method, path string, body any, want int) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw)).WithContext(ctx)
	req.Header.Set("X-AutoCarto-Contract-Version", "v1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != want {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, want, w.Body.String())
	}
	var response struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Success != (want >= 200 && want < 300) {
		t.Fatalf("inconsistent success envelope: %s", w.Body.String())
	}
	return response.Data
}

func assertSavedFlowPreviewUnavailable(t *testing.T, raw json.RawMessage) flow.StoredFlow {
	t.Helper()
	var item flow.StoredFlow
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatal(err)
	}
	if item.Plan != nil || item.FlowMD == "" {
		t.Fatalf("saved document must remain editable without an executable plan: %+v", item)
	}
	warning := false
	for _, diagnostic := range item.Diagnostics {
		if diagnostic.Code == "FLOW_PREVIEW_UNAVAILABLE" {
			warning = diagnostic.Level == "warning" && strings.Contains(diagnostic.Message, "已保存") && !strings.Contains(diagnostic.Message, "重新编译")
		}
	}
	if !warning || strings.Contains(string(raw), "private-provider-detail") {
		t.Fatalf("missing safe saved-but-uncompiled diagnostic: %s", raw)
	}
	parsed := flow.Parse(item.FlowMD).Diagnostics
	if len(parsed) == 0 {
		t.Fatal("fixture must have offline diagnostics to preserve")
	}
	for _, expected := range parsed {
		found := false
		for _, actual := range item.Diagnostics {
			found = found || (actual.Code == expected.Code && actual.Message == expected.Message)
		}
		if !found {
			t.Fatalf("lost offline diagnostic: %+v", expected)
		}
	}
	return item
}

func TestFlowMutationKeepsSavedDocumentWhenLocalPreviewFails(t *testing.T) {
	for _, operation := range []string{"create", "save", "copy", "import"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("AUTOCARTO_HOME", root)
			t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
			t.Setenv("AUTOCARTO_DISABLE_KEYRING", "1")
			store := flow.NewStore("", filepath.Join(root, "flows"), bindings.New(filepath.Join(root, "config", "resource_bindings.yaml")))
			a := &API{}
			a.SetFlowStore(store)
			fail, environmentCalls := true, 0
			a.SetFlowCompiler(flow.NewCompiler(flow.CompilerOptions{Store: store, CacheDir: filepath.Join(root, "cache"), LoadEnvironment: func(context.Context, string) (flow.Environment, error) {
				environmentCalls++
				if fail {
					return flow.Environment{}, errors.New("private-provider-detail")
				}
				return flow.Environment{Scheme: "test"}, nil
			}}))
			router := mux.NewRouter()
			a.SetupRoutes(router)
			method, path := http.MethodPost, "/api/flows"
			body := map[string]any{"flow_md": mutationFailureFlow}
			if operation == "save" || operation == "copy" {
				if _, err := store.Create(mutationFailureFlow, "saved-flow"); err != nil {
					t.Fatal(err)
				}
			}
			switch operation {
			case "save":
				method, path = http.MethodPut, "/api/flows/saved-flow"
				body["flow_md"] = mutationFailureFlow + "\n## 说明\n已保存的修订\n"
			case "copy":
				path = "/api/flows/saved-flow/copy"
				body = map[string]any{"new_id": "copied-flow", "new_name": "已复制流程"}
			case "import":
				source := filepath.Join(root, "import.md")
				if err := os.WriteFile(source, []byte(mutationFailureFlow), 0600); err != nil {
					t.Fatal(err)
				}
				path, body = "/api/flows/import", map[string]any{"source_path": source}
			}
			ctx := context.Background()
			raw := flowFailureRequest(t, router, ctx, method, path, body, http.StatusOK)
			if operation == "import" {
				var wrapper struct {
					Flow json.RawMessage `json:"flow"`
				}
				if err := json.Unmarshal(raw, &wrapper); err != nil {
					t.Fatal(err)
				}
				raw = wrapper.Flow
			}
			item := assertSavedFlowPreviewUnavailable(t, raw)
			stored, err := store.Get(item.Meta.ID)
			if err != nil || stored.FlowMD != item.FlowMD {
				t.Fatalf("saved source was lost: %v", err)
			}
			if operation == "save" && !strings.Contains(stored.FlowMD, "已保存的修订") {
				t.Fatal("new content was not persisted")
			}
			if environmentCalls != 1 {
				t.Fatalf("failed compilation retried the environment during fallback: %d", environmentCalls)
			}
			// A storage conflict remains an error and must not reach compilation.
			flowFailureRequest(t, router, ctx, http.MethodPost, "/api/flows", map[string]any{"flow_md": item.FlowMD}, http.StatusConflict)
			if environmentCalls != 1 {
				t.Fatal("storage failure incorrectly reached compilation")
			}
			// Only the post-save decoration handles compile failure as a warning.
			flowFailureRequest(t, router, ctx, http.MethodGet, "/api/flows/"+item.Meta.ID, nil, http.StatusBadRequest)
			fail = false
			flowFailureRequest(t, router, ctx, http.MethodGet, "/api/flows/"+item.Meta.ID, nil, http.StatusOK)
			var recovered flow.StoredFlow
			if err := json.Unmarshal(flowFailureRequest(t, router, ctx, http.MethodGet, "/api/flows/"+item.Meta.ID, nil, http.StatusOK), &recovered); err != nil {
				t.Fatal(err)
			}
			if recovered.Plan == nil {
				t.Fatal("later compilation did not restore a current plan")
			}
		})
	}
}

func TestFlowCreateKeepsDocumentWhenModelContextCanceled(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", root)
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_DISABLE_KEYRING", "1")
	store := flow.NewStore("", filepath.Join(root, "flows"), bindings.New(filepath.Join(root, "config", "resource_bindings.yaml")))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls, environmentCalls := 0, 0
	a := &API{}
	a.SetFlowStore(store)
	a.SetFlowCompiler(flow.NewCompiler(flow.CompilerOptions{Store: store, CacheDir: filepath.Join(root, "cache"), LoadEnvironment: func(context.Context, string) (flow.Environment, error) {
		environmentCalls++
		return flow.Environment{Scheme: "test", Model: apiFlowModelFunc(func(ctx context.Context, _ flow.ModelRequest) (string, error) {
			calls++
			if _, err := store.Get("saved-flow"); err != nil {
				t.Fatalf("model was invoked before saving the source: %v", err)
			}
			cancel()
			return "", ctx.Err()
		})}, nil
	}}))
	router := mux.NewRouter()
	a.SetupRoutes(router)
	raw := flowFailureRequest(t, router, ctx, http.MethodPost, "/api/flows", map[string]any{"flow_md": mutationFailureFlow}, http.StatusOK)
	var item flow.StoredFlow
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatal(err)
	}
	if item.Plan == nil || calls != 0 || environmentCalls != 1 {
		t.Fatalf("saving must return basic plan without waiting on model: %s calls=%d env=%d", raw, calls, environmentCalls)
	}
	cancel()
	if _, err := store.Get("saved-flow"); err != nil {
		t.Fatal(err)
	}
	flowFailureRequest(t, router, context.Background(), http.MethodGet, "/api/flows/saved-flow", nil, http.StatusOK)
	if calls != 0 {
		t.Fatal("GET invoked model")
	}
}

func TestFlowCreateKeepsDocumentWhenCompilerUnavailable(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", root)
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", filepath.Join(root, "private-provider-detail"))
	t.Setenv("AUTOCARTO_DISABLE_KEYRING", "1")
	a := &API{assetCatalogService: stubAPIAssetCatalogService{}}
	store := flow.NewStore("", filepath.Join(root, "flows"), bindings.New(filepath.Join(root, "config", "resource_bindings.yaml")))
	a.SetFlowStore(store)
	router := mux.NewRouter()
	a.SetupRoutes(router)
	ctx := context.Background()
	assertSavedFlowPreviewUnavailable(t, flowFailureRequest(t, router, ctx, http.MethodPost, "/api/flows", map[string]any{"flow_md": mutationFailureFlow}, http.StatusOK))
	if _, err := store.Get("saved-flow"); err != nil {
		t.Fatal(err)
	}
	flowFailureRequest(t, router, ctx, http.MethodGet, "/api/flows/saved-flow", nil, http.StatusBadRequest)
}
