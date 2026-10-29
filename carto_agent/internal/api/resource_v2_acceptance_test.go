package api

import (
	"bytes"
	"context"
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
func resourceV2GuideStore(a *API, store *flow.Store, library *resource.Library, env flow.Environment, cache string) {
	a.SetFlowStore(store)
	a.SetResourceLibrary(library)
	guides := flow.NewGuideStore(store, filepath.Join(cache, "guides.yaml"))
	a.SetGuideStore(guides)

}
func resourceV2Plan(t *testing.T, raw json.RawMessage) *flow.FlowPlan {
	t.Helper()
	var g flow.StoredGuide
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if g.Guide == nil {
		t.Fatal("missing parsed guide")
	}
	plan := &flow.FlowPlan{FlowID: g.Meta.ID, Diagnostics: g.Diagnostics}
	for _, r := range g.ResolvedResources {
		plan.Resources = append(plan.Resources, flow.ResourcePlanFromResolved(r))
	}
	return plan
}
func TestResourceV6FlowPackageIsolatedVisualDependencies(t *testing.T) {
	source, _, _, sourceHome := realFlowAPI(t)
	router := mux.NewRouter()
	source.SetupRoutes(router)
	archive := filepath.Join(sourceHome, "marching.zip")
	resourceV2Call(t, router, "POST", "/api/guides/marching-route/export", map[string]any{"target_path": archive, "include_library_deps": true}, 200)
	destination := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", destination)
	library := resource.NewLibrary("", filepath.Join(destination, "resources"), bindings.New(filepath.Join(destination, "bindings.yaml")))
	store := flow.NewStore("", filepath.Join(destination, "flows"), library.Bindings)
	target := &API{}
	resourceV2GuideStore(target, store, library, flow.Environment{}, filepath.Join(destination, "cache"))
	targetRouter := mux.NewRouter()
	target.SetupRoutes(targetRouter)
	resourceV2Call(t, targetRouter, "POST", "/api/guides/import", map[string]string{"source_path": archive}, 200)
	var before flow.StoredGuide
	if err := json.Unmarshal(resourceV2Call(t, targetRouter, "GET", "/api/guides/marching-route", nil, 200), &before); err != nil {
		t.Fatal(err)
	}
	if len(before.ResolvedResources) != 9 {
		t.Fatalf("expected eight styles and one layout: %+v", before.ResolvedResources)
	}
	for _, r := range before.ResolvedResources {
		if resource.HasErrors(r.Problems) || r.Owner != "bundled" || (r.Type != "style" && r.Type != "layout") {
			t.Fatal(r)
		}
	}
	listed, err := library.List("all")
	if err != nil || len(listed) != 0 {
		t.Fatalf("import installed shared resources: %+v %v", listed, err)
	}
	stored, err := store.Get("marching-route")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(stored.Dir, "resources")); !os.IsNotExist(err) {
		t.Fatal("flow still carries data resources")
	}
	// Arbitrary user prose does not create a fixed data binding or executable step.
	raw := stored.FlowMD + "\n## 本次使用说明\n任意自然语言和表格可以说明如何准备数据，不形成资源清单。\n"
	resourceV2Call(t, targetRouter, "PUT", "/api/guides/marching-route", map[string]string{"guide_md": raw}, 200)
	var after flow.StoredGuide
	if err = json.Unmarshal(resourceV2Call(t, targetRouter, "GET", "/api/guides/marching-route", nil, 200), &after); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after.Guide.Body, "任意自然语言") || len(after.ResolvedResources) != len(before.ResolvedResources) {
		t.Fatal(after)
	}
	for i, r := range after.ResolvedResources {
		if r.ID != before.ResolvedResources[i].ID {
			t.Fatal("edit changed bundled identity")
		}
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
	resourceV2GuideStore(a, store, library, env, filepath.Join(home, "cache"))
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
	plan := resourceV2Plan(t, resourceV2Call(t, router, "GET", "/api/guides/traffic-route?scheme=resource-v2", nil, 200))
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
	restored := resourceV2Plan(t, resourceV2Call(t, router, "GET", "/api/guides/traffic-route?scheme=resource-v2", nil, 200))
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
