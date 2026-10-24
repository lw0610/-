package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
)

func TestR73AvailablePlansCountersAcrossMultipleFlows(t *testing.T) {
	a, _, project, home := realFlowAPI(t)
	// Keep metadata within the documented one-second throttle window. The
	// existing metadata test separately exercises expiry and invalidation.
	a.flowEnvironments.now = func() time.Time { return time.Unix(100, 0) }
	library, err := a.resourceStore()
	if err != nil {
		t.Fatal(err)
	}
	store := flow.NewStore("", filepath.Join(home, "flows"), library.Bindings)
	a.SetFlowStore(store)
	if _, err = store.Import(filepath.Join(project, "docs/refactor/examples/traffic-route"), flow.ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	ids := []string{"traffic-route", "traffic-two", "traffic-three"}
	for _, id := range ids[1:] {
		if _, err = store.Copy(ids[0], id, id); err != nil {
			t.Fatal(err)
		}
	}
	var fixture struct {
		Flow  json.RawMessage     `json:"flow"`
		Tools map[string][]string `json:"tools"`
	}
	raw, err := os.ReadFile(filepath.Join(project, "carto_agent/internal/flow/testdata/compile-model-traffic-route.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	modelCalls, environmentCallbacks := 0, 0
	compiler := flow.NewCompiler(flow.CompilerOptions{Store: store, Resources: library, CacheDir: filepath.Join(home, "cache"), LoadEnvironment: func(ctx context.Context, scheme string) (flow.Environment, error) {
		environmentCallbacks++
		env, err := a.loadFlowEnvironment(ctx, scheme)
		env.ModelName = "r73-count-model"
		env.Model = apiFlowModelFunc(func(_ context.Context, request flow.ModelRequest) (string, error) {
			modelCalls++
			if request.Kind == "flow" {
				return string(fixture.Flow), nil
			}
			raw, _ := json.Marshal(map[string]any{"suggested_tools": fixture.Tools[request.Step], "missing_capabilities": []string{}, "expected_outputs": []any{}, "condition": nil, "notes": "计数夹具"})
			return string(raw), nil
		})
		return env, err
	}})
	ctx := context.Background()
	for _, id := range ids {
		plan, err := compiler.Compile(ctx, id, "compile-scheme", false)
		if err != nil || plan == nil {
			t.Fatalf("precompile %s: %v", id, err)
		}
		for _, diagnostic := range plan.Diagnostics {
			if diagnostic.Level == "error" || diagnostic.Code == "FLOW_COMPILE_LLM_FAILED" {
				t.Fatalf("precompile %s: %+v", id, diagnostic)
			}
		}
	}
	if modelCalls != 15 || environmentCallbacks != 3 || a.flowEnvironments.loads != 1 {
		t.Fatalf("cold baseline: models=%d callbacks=%d actual_loads=%d", modelCalls, environmentCallbacks, a.flowEnvironments.loads)
	}
	t.Logf("precompiled flows=3: model=%d environment_callbacks=%d actual_environment_loads=%d metadata_scans=%d", modelCalls, environmentCallbacks, a.flowEnvironments.loads, a.flowEnvironments.metadataScans)
	for i := 1; i <= 2; i++ {
		calls, callbacks, hashes := modelCalls, environmentCallbacks, resource.ContentHashCount()
		loads, scans := a.flowEnvironments.loads, a.flowEnvironments.metadataScans
		started := time.Now()
		plans, err := compiler.AvailablePlans(ctx, "compile-scheme")
		if err != nil || len(plans) != len(ids) {
			t.Fatalf("AvailablePlans %d: plans=%d err=%v", i, len(plans), err)
		}
		modelDelta, callbackDelta, hashDelta := modelCalls-calls, environmentCallbacks-callbacks, resource.ContentHashCount()-hashes
		loadDelta, scanDelta := a.flowEnvironments.loads-loads, a.flowEnvironments.metadataScans-scans
		if modelDelta != 0 || callbackDelta != 1 || hashDelta != 0 || loadDelta != 0 || scanDelta != 0 {
			t.Fatalf("AvailablePlans %d: model=%d callback=%d hash=%d loads=%d scans=%d", i, modelDelta, callbackDelta, hashDelta, loadDelta, scanDelta)
		}
		t.Logf("AvailablePlans pass=%d plans=%d: model_delta=%d environment_callback_delta=%d actual_environment_load_delta=%d content_hash_delta=%d metadata_scan_delta=%d elapsed=%s", i, len(plans), modelDelta, callbackDelta, loadDelta, hashDelta, scanDelta, time.Since(started))
	}
}
