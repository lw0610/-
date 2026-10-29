package flowrun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
)

func TestV7VerifyLayerReadsActualInspectDataEnvelope(t *testing.T) {
	tools := toolFunc(func(_ context.Context, name string, args map[string]any) (any, error) {
		switch name {
		case "get_layers_info":
			return map[string]any{"success": true, "info": map[string]any{"layers": []any{map[string]any{"id": "memory-route"}}}}, nil
		case "layer_inspect":
			if args["layer_id"] != "memory-route" {
				t.Fatalf("unexpected layer: %v", args)
			}
			return map[string]any{"success": true, "data": map[string]any{"geometry": "polygon", "available_fields": []string{"route_id", "name"}}}, nil
		}
		return nil, fmt.Errorf("unexpected tool %s", name)
	})
	step := flow.StepPlan{ExpectedOutputs: []flow.OutputDeclaration{{Label: "route", Type: "layers", Required: true, Geometry: "polygon", Fields: []string{"route_id"}}}}
	receipt := Receipt{Outputs: map[string]any{"route": "memory-route"}}
	if v := VerifyOutputs(context.Background(), step, receipt, t.TempDir(), tools); !v.OK {
		t.Fatalf("actual QGIS envelope must pass: %v", v.Errors)
	}
	step.ExpectedOutputs[0].Fields = []string{"missing_field"}
	if v := VerifyOutputs(context.Background(), step, receipt, t.TempDir(), tools); v.OK || !strings.Contains(strings.Join(v.Errors, ";"), "missing_field") {
		t.Fatalf("field checks must still work: %+v", v)
	}
	step.ExpectedOutputs[0].Fields, step.ExpectedOutputs[0].Geometry = nil, "line"
	if v := VerifyOutputs(context.Background(), step, receipt, t.TempDir(), tools); v.OK {
		t.Fatal("a real geometry mismatch must still fail")
	}
}

func TestV7ActualLayerReceiptsNormalizeVerifyAndKeepLoadedLayers(t *testing.T) {
	raw, err := os.ReadFile("testdata/layer-receipts-live-v7.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name           string           `json:"name"`
		Receipt        json.RawMessage  `json:"receipt"`
		ObservedLayers []map[string]any `json:"observed_layers"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			p := testPlan(1)
			p.Steps[0].ExpectedOutputs = []flow.OutputDeclaration{{Label: "basemap_loaded", Type: "layers", Required: true}}
			loaded, removed, calls := false, 0, 0
			tools := toolFunc(func(_ context.Context, name string, args map[string]any) (any, error) {
				if name == "layer_remove_created" {
					removed++
					return map[string]any{"success": true}, nil
				}
				if name != "get_layers_info" {
					t.Fatalf("unexpected tool %s", name)
				}
				layers := []map[string]any{}
				if loaded {
					layers = tc.ObservedLayers
				}
				return map[string]any{"success": true, "info": map[string]any{"layers": layers}}, nil
			})
			c, err := NewController(Options{Tools: tools, Store: FileRunStore{}, Runner: runnerFunc(func(context.Context, StepRequest) (StepResult, error) {
				loaded, calls = true, calls+1
				return StepResult{LastContent: string(tc.Receipt)}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			run, err := c.StartFromPlan(context.Background(), StartRequest{SessionID: "layer-receipts", SessionDir: dir, ProjectRoot: dir, CartoPlan: &plan.Plan{APIVersion: plan.APIVersion, ID: "cp-layer-receipts", Version: 1}}, p)
			if err != nil || run.Status != "completed" || calls != 1 || removed != 0 {
				t.Fatalf("actual loaded layers were retried or removed: run=%+v err=%v calls=%d removed=%d", run, err, calls, removed)
			}
			outputs := run.Steps[0].Outputs["basemap_loaded"].([]any)
			if len(outputs) != 8 || !run.Steps[0].Attempts[0].LayersTracked {
				t.Fatalf("canonical output or layer tracking missing: %+v", run.Steps[0])
			}
			if tc.Name == "name-to-id-collection" {
				for _, output := range outputs {
					if _, ok := output.(string); !ok {
						t.Fatalf("name-to-ID entry was not normalized to an ID: %v", output)
					}
				}
			}
			receipt, err := ParseReceipt(string(tc.Receipt))
			if err != nil {
				t.Fatal(err)
			}
			missing := toolFunc(func(context.Context, string, map[string]any) (any, error) {
				return map[string]any{"success": true, "info": map[string]any{"layers": tc.ObservedLayers[1:]}}, nil
			})
			if v := VerifyOutputs(context.Background(), p.Steps[0], receipt, dir, missing); v.OK || !strings.Contains(strings.Join(v.Errors, ";"), tc.ObservedLayers[0]["id"].(string)) {
				t.Fatalf("missing real layer was accepted: %+v", v)
			}
		})
	}
}

func TestV7LayerReceiptDoesNotGuessAmbiguousOrArbitraryOutputKeys(t *testing.T) {
	for _, labels := range [][]string{{"one", "two"}, {"one"}} {
		step := flow.StepPlan{}
		for _, label := range labels {
			step.ExpectedOutputs = append(step.ExpectedOutputs, flow.OutputDeclaration{Label: label, Type: "layers", Required: true})
		}
		key := "layers"
		if len(labels) == 1 {
			key = "some_other_result"
		}
		r := Receipt{Outputs: map[string]any{key: []any{"actual-id"}}}
		if v := VerifyOutputs(context.Background(), step, r, t.TempDir(), nil); v.OK || len(v.Errors) != len(labels) {
			t.Fatalf("ambiguous or unrelated output was silently bound: %+v", v)
		}
	}
}

func TestV7VerifyLayerResolvesUniqueNameFromActualSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name    string
		layers  []any
		refs    []any
		wantIDs []string
		wantErr []string
	}{
		{
			name: "unique names share one snapshot",
			layers: []any{
				map[string]any{"id": "route-123", "name": "路线"},
				map[string]any{"id": "route-456", "name": "支线"},
			},
			refs: []any{map[string]any{"name": "路线"}, "支线"}, wantIDs: []string{"route-123", "route-456"},
		},
		{
			name: "duplicate name requires exact id",
			layers: []any{
				map[string]any{"id": "route-123", "name": "路线"},
				map[string]any{"id": "route-456", "name": "路线"},
			},
			refs: []any{map[string]any{"name": "路线"}}, wantErr: []string{"不唯一", "route-123", "route-456"},
		},
		{
			name: "exact id ignores duplicate names",
			layers: []any{
				map[string]any{"id": "route-123", "name": "路线"},
				map[string]any{"id": "route-456", "name": "路线"},
			},
			refs: []any{map[string]any{"layer_id": "route-456"}}, wantIDs: []string{"route-456"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshots, inspected := 0, []string{}
			tools := toolFunc(func(_ context.Context, name string, args map[string]any) (any, error) {
				switch name {
				case "get_layers_info":
					snapshots++
					return map[string]any{"success": true, "output": "成功获取图层信息", "info": map[string]any{"layers": tc.layers}}, nil
				case "layer_inspect":
					inspected = append(inspected, fmt.Sprint(args["layer_id"]))
					return map[string]any{"success": true, "data": map[string]any{"geometry": "polygon", "available_fields": []string{"route_id"}}}, nil
				}
				return nil, fmt.Errorf("unexpected tool %s", name)
			})
			step := flow.StepPlan{ExpectedOutputs: []flow.OutputDeclaration{{Label: "route", Type: "layers", Required: true, Geometry: "polygon", Fields: []string{"route_id"}}}}
			verification := VerifyOutputs(context.Background(), step, Receipt{Outputs: map[string]any{"route": tc.refs}}, t.TempDir(), tools)
			if verification.OK != (len(tc.wantErr) == 0) {
				t.Fatalf("verification = %+v", verification)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(strings.Join(verification.Errors, ";"), want) {
					t.Fatalf("error must contain %q: %+v", want, verification)
				}
			}
			if snapshots != 1 || strings.Join(inspected, ",") != strings.Join(tc.wantIDs, ",") {
				t.Fatalf("snapshots=%d inspected=%v want=%v", snapshots, inspected, tc.wantIDs)
			}
		})
	}
}
