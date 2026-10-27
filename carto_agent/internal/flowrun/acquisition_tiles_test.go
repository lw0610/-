package flowrun

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/acquire"
	"strings"
	"testing"
)

func TestTilesWaitForExplicitConfirmationBeforeLoading(t *testing.T) {
	for _, decision := range []string{"load_tiles", "stop"} {
		t.Run(decision, func(t *testing.T) {
			p, engine := acquisitionFixture(t, func(context.Context, acquire.FetchRequest) (acquire.FetchResult, error) {
				return acquire.FetchResult{LayerDefinition: map[string]any{"type": "tianditu_xyz", "style": "vec", "secret_ref": "tianditu", "requires_project_key_confirmation": true}, Notes: []string{"在线底图"}}, nil
			})
			p.Inputs[0].Type = "layer"
			p.Acquisition.Items[0].InputType = "layer"
			p.Acquisition.Items[0].Geometry = "tiles"
			engine.Catalog.Categories[0].Geometry = "tiles"
			calls := 0
			interrupted := ""
			engine.Invoke = func(_ context.Context, name string, args map[string]any) (any, error) {
				if name != "data_load_tiles" || args["confirmed"] != true || args["definition_path"] == nil {
					return nil, fmt.Errorf("unconfirmed load")
				}
				if len(args) != 2 {
					t.Fatal("unexpected credential parameter", args)
				}
				calls++
				return map[string]any{"success": true, "data": map[string]any{"layer_id": "xyz-fixture"}}, nil
			}
			c, req, _ := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) {
				if r.Confirmation != nil {
					if !strings.Contains(r.Confirmation.Description, "密钥将保存在 QGIS 工程") {
						t.Fatal("missing key persistence disclosure")
					}
					interrupted = r.RunID
					return StepResult{Interrupted: true}, nil
				}
				return completed(r.Step), nil
			}, func(o *Options) {
				o.AcquisitionFactory = func(context.Context, *RunState) (*acquire.Engine, error) { return engine, nil }
			})
			run, err := startPreparedAcquisition(t, c, req, p)
			if err != nil || run.Status != "waiting_user" || calls != 0 {
				t.Fatalf("%+v %v calls=%d", run, err, calls)
			}
			raw, _ := json.Marshal(Choice{Choice: decision})
			run, err = c.OnStepRunFinished(context.Background(), interrupted, StepResult{LastContent: string(raw)})
			if err != nil {
				t.Fatal(err)
			}
			if decision == "stop" {
				if calls != 0 || run.Status != "cancelled" {
					t.Fatal(run.Status, calls)
				}
				return
			}
			if calls != 1 || run.Status != "completed" || EffectiveInputs(run)["data"] != "xyz-fixture" || run.Acquisition.Items[0].Output.LayerID != "xyz-fixture" {
				t.Fatalf("%+v calls=%d", run, calls)
			}
		})
	}
}
