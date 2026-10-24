package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"github.com/gorilla/mux"
	"gopkg.in/yaml.v3"
)

func resourceV2Call(t *testing.T, router *mux.Router, method, path string, body any, want int) json.RawMessage {
	t.Helper()
	raw, _ := json.Marshal(body)
	request := httptest.NewRequest(method, path, bytes.NewReader(raw))
	request.Header.Set("X-AutoCarto-Contract-Version", "v1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("%s %s: %d %s", method, path, response.Code, response.Body.String())
	}
	var result struct {
		Data    json.RawMessage `json:"data"`
		Details json.RawMessage `json:"details"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if want >= 400 {
		return result.Details
	}
	return result.Data
}
func resourceV2Compiler(a *API, store *flow.Store, library *resource.Library, env flow.Environment, cache string) {
	env.Model = nil
	env.ModelName = "离线验收"
	a.SetFlowStore(store)
	a.SetResourceLibrary(library)
	a.SetFlowCompiler(flow.NewCompiler(flow.CompilerOptions{Store: store, Resources: library, CacheDir: cache, LoadEnvironment: func(context.Context, string) (flow.Environment, error) { return env, nil }}))
}
func resourceV2Plan(t *testing.T, raw json.RawMessage) *flow.FlowPlan {
	t.Helper()
	var result struct {
		Plan *flow.FlowPlan `json:"plan"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Plan == nil {
		t.Fatal("missing compiled plan")
	}
	return result.Plan
}
func TestResourceV2MarchingPortablePackageAndStableBinding(t *testing.T) {
	source, manager, project, sourceHome := realFlowAPI(t)
	environment, err := resourceV2DefaultEnvironment(source, manager, project)
	if err != nil {
		t.Fatal(err)
	}
	sourceRouter := mux.NewRouter()
	source.SetupRoutes(sourceRouter)
	archive := filepath.Join(sourceHome, "marching-portable.zip")
	resourceV2Call(t, sourceRouter, "POST", "/api/flows/marching-route/export", map[string]any{"target_path": archive, "include_library_deps": true}, 200)
	// Empty runtime has neither builtin nor user public resources. Tool/role
	// declarations come from the real catalog snapshot; no model or QGIS runs.
	destination := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", destination)
	bindingStore := bindings.New(filepath.Join(destination, "config/resource_bindings.yaml"))
	emptyLibrary := resource.NewLibrary(filepath.Join(destination, "empty-builtin-resources"), filepath.Join(destination, "resources"), bindingStore)
	store := flow.NewStore("", filepath.Join(destination, "flows"), bindingStore)
	target := &API{}
	resourceV2Compiler(target, store, emptyLibrary, environment, filepath.Join(destination, "cache"))
	router := mux.NewRouter()
	target.SetupRoutes(router)
	resourceV2Call(t, router, "POST", "/api/flows/import?scheme=resource-v2", map[string]string{"source_path": archive}, 200)
	plan := resourceV2Plan(t, resourceV2Call(t, router, "GET", "/api/flows/marching-route?scheme=resource-v2", nil, 200))
	var dataKey, inputLabel string
	bundled := 0
	local := 0
	for _, r := range plan.Resources {
		if r.Type == "data_list" {
			dataKey = r.Key
			inputLabel = r.BaseDirFromInput
			continue
		}
		if resource.HasErrors(r.Problems) {
			t.Fatalf("portable dependency errors: %+v", r)
		}
		if r.Owner == "bundled" {
			bundled++
		}
		if r.Owner == "flow" {
			local++
		}
	}
	if bundled != 2 || local != 3 || dataKey != "path:data/中国底图.csv" {
		t.Fatalf("unexpected owners %d/%d key=%q", bundled, local, dataKey)
	}
	listed, err := emptyLibrary.List("all")
	if err != nil || len(listed) != 0 {
		t.Fatalf("import installed public resources: %+v %v", listed, err)
	}
	// Existence-only shapefile fixture tests resource linking, not GIS decoding.
	// The fixture paths come from the committed CSV rather than a second list.
	raw, err := os.ReadFile(filepath.Join(project, "autocarto/flows/system/marching-route/resources/data/中国底图.csv"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(raw), "\ufeff"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(destination, "chosen-basemap")
	if err = os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows[1:] {
		base := strings.TrimSuffix(row[1], filepath.Ext(row[1]))
		for _, suffix := range []string{".shp", ".shx", ".dbf", ".prj"} {
			if err = os.WriteFile(filepath.Join(dataDir, base+suffix), []byte("existence-only acceptance fixture"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	detailRaw := resourceV2Call(t, router, "PUT", "/api/flows/marching-route/bindings?scheme=resource-v2", map[string]string{"resource_key": dataKey, "base_dir": dataDir}, 200)
	bound := resourceV2Plan(t, detailRaw)
	if bound.HasErrors() {
		t.Fatalf("bound package errors: %+v", bound.Diagnostics)
	}
	imported, err := store.Get("marching-route")
	if err != nil {
		t.Fatal(err)
	}
	renamed := strings.ReplaceAll(imported.FlowMD, "「中国底图」", "「迁移后底图」")
	resourceV2Call(t, router, "PUT", "/api/flows/marching-route?scheme=resource-v2", map[string]string{"flow_md": renamed}, 200)
	after := resourceV2Plan(t, resourceV2Call(t, router, "GET", "/api/flows/marching-route?scheme=resource-v2", nil, 200))
	if after.HasErrors() {
		t.Fatalf("rename lost input association: %+v", after.Diagnostics)
	}
	memoryRaw := resourceV2Call(t, router, "GET", "/api/flows/marching-route/inputs/memory", nil, 200)
	remembered := map[string]string{}
	if err = json.Unmarshal(memoryRaw, &remembered); err != nil || remembered[inputLabel] != dataDir {
		t.Fatal(remembered, err)
	}
	runtimePlan := flow.ResolveInputResources(after, map[string]any{inputLabel: remembered[inputLabel]})
	found := false
	for _, r := range runtimePlan.Resources {
		if r.Type == "data_list" {
			found = true
			if r.Label != "迁移后底图" || r.Key != dataKey || r.BaseDirFromInput != inputLabel || r.BaseDir != dataDir || len(r.Entries) != 8 {
				t.Fatalf("input association changed: %+v", r)
			}
			for _, entry := range r.Entries {
				if !entry.Exists || filepath.Dir(entry.Path) != dataDir {
					t.Fatal(entry)
				}
			}
		}
	}
	if !found {
		t.Fatal("data list disappeared")
	}
}

func TestResourceV2FourCornerOverrideProtectedDeleteAndRestore(t *testing.T) {
	a, manager, project, home := realFlowAPI(t)
	env, err := resourceV2DefaultEnvironment(a, manager, project)
	if err != nil {
		t.Fatal(err)
	}
	store, err := a.flowStore()
	if err != nil {
		t.Fatal(err)
	}
	library, err := a.resourceStore()
	if err != nil {
		t.Fatal(err)
	}
	resourceV2Compiler(a, store, library, env, filepath.Join(home, "cache"))
	router := mux.NewRouter()
	a.SetupRoutes(router)
	flowItem, err := store.Get("traffic-route")
	if err != nil {
		t.Fatal(err)
	}
	beforeLock, err := os.ReadFile(filepath.Join(flowItem.Dir, "flow.lock"))
	if err != nil {
		t.Fatal(err)
	}
	copied := resourceV2Call(t, router, "POST", "/api/resources/layout.four-corner/copy", map[string]any{"override": true, "name": "验收四角覆盖"}, 200)
	var override resource.ResolvedResource
	if err = json.Unmarshal(copied, &override); err != nil {
		t.Fatal(err)
	}
	if override.ID == "layout.four-corner" || override.Overrides != "layout.four-corner" || override.Owner != "library_user" {
		t.Fatal(override)
	}
	plan := resourceV2Plan(t, resourceV2Call(t, router, "GET", "/api/flows/traffic-route?scheme=resource-v2", nil, 200))
	if plan.HasErrors() {
		t.Fatal(plan.Diagnostics)
	}
	found := false
	for _, r := range plan.Resources {
		if r.Type == "layout" {
			if r.ID != override.ID {
				t.Fatalf("builtin flow did not use override: %+v", r)
			}
			for _, problem := range r.Problems {
				if problem.Code == "FLOW_RESOURCE_OVERRIDDEN" && problem.Level == "info" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("missing override diagnostic")
	}
	var refs []flow.ResourceReference
	if err = json.Unmarshal(resourceV2Call(t, router, "GET", "/api/resources/"+override.ID+"/references", nil, 200), &refs); err != nil {
		t.Fatal(err)
	}
	affected := false
	for _, ref := range refs {
		if ref.FlowID == "traffic-route" && ref.Source == "builtin" && ref.Via == "lock" {
			affected = true
		}
	}
	if !affected {
		t.Fatalf("missing affected builtin flow: %+v", refs)
	}
	details := resourceV2Call(t, router, "DELETE", "/api/resources/"+override.ID, nil, 409)
	if !bytes.Contains(details, []byte("traffic-route")) {
		t.Fatalf("deletion omitted references: %s", details)
	}
	resourceV2Call(t, router, "GET", "/api/resources/by-id/"+override.ID, nil, 200)
	resourceV2Call(t, router, "DELETE", "/api/resources/"+override.ID+"?force=true", nil, 200)
	restored := resourceV2Plan(t, resourceV2Call(t, router, "GET", "/api/flows/traffic-route?scheme=resource-v2", nil, 200))
	if restored.HasErrors() {
		t.Fatal(restored.Diagnostics)
	}
	found = false
	for _, r := range restored.Resources {
		if r.Type == "layout" {
			found = true
			if r.ID != "layout.four-corner" || r.Owner != "library_builtin" {
				t.Fatal(r)
			}
			for _, problem := range r.Problems {
				if problem.Code == "FLOW_RESOURCE_OVERRIDDEN" {
					t.Fatal(problem)
				}
			}
		}
	}
	if !found {
		t.Fatal("restored layout missing")
	}
	afterLock, err := os.ReadFile(filepath.Join(flowItem.Dir, "flow.lock"))
	if err != nil || !bytes.Equal(beforeLock, afterLock) {
		t.Fatal("builtin lock was modified", err)
	}
}

func resourceV2DefaultEnvironment(a *API, manager *config.AgentConfigManager, project string) (flow.Environment, error) {
	raw, err := os.ReadFile(filepath.Join(project, "carto_agent/defaults/agent_configs/default/agent.yaml"))
	if err != nil {
		return flow.Environment{}, err
	}
	var data config.AgentConfigData
	if err = yaml.Unmarshal(raw, &data); err != nil {
		return flow.Environment{}, err
	}
	if err = manager.SaveConfig("", "resource-v2", &data); err != nil {
		return flow.Environment{}, err
	}
	return a.loadFlowEnvironment(context.Background(), "resource-v2")
}
