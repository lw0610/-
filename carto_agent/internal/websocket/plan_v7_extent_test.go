package websocket

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/plan"
)

func TestV7ExtentUsesProducedIDAndActualRouteBounds(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "places.csv"), []byte("id,name,longitude,latitude\n1,甲,110,30\n2,乙,115,35\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server := &DeepServer{}
	run := &flowrun.RunState{CartoPlan: &plan.Plan{Steps: []plan.Step{{ID: "extract", Produces: []plan.Product{{ID: "route", Kind: "dataset"}}}}}, Steps: []flowrun.StepState{{ID: "extract", Status: "completed", Outputs: map[string]any{"route": dir}}}}
	extent, err := server.planDataExtent(context.Background(), run, plan.Data{ID: "rivers", Name: "任意显示名", Extent: &plan.Extent{From: "route", Expand: .2}})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []float64{109, 29, 116, 36} {
		if math.Abs(extent.BBox[i]-want) > 1e-6 {
			t.Fatalf("bounds=%v", extent.BBox)
		}
	}
	run.Steps[0].Status = "pending"
	if _, err = server.planDataExtent(context.Background(), run, plan.Data{ID: "rivers", Extent: &plan.Extent{From: "route"}}); err == nil {
		t.Fatal("unproduced extent accepted")
	}
}

func TestV7ExtentUnionsAllProducedLayerPathsBeforeExpansion(t *testing.T) {
	paths := []any{}
	for _, coordinates := range []string{"110,30\n112,32", "114,34\n116,36"} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "places.csv"), []byte("longitude,latitude\n"+coordinates+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, map[string]any{"path": dir})
	}
	run := &flowrun.RunState{CartoPlan: &plan.Plan{Steps: []plan.Step{{ID: "producer", Produces: []plan.Product{{ID: "parts", Kind: "layer"}}}}}, Steps: []flowrun.StepState{{ID: "producer", Status: "completed", Outputs: map[string]any{"parts": paths}}}}
	got, err := (&DeepServer{}).planDataExtent(context.Background(), run, plan.Data{ID: "river", Extent: &plan.Extent{From: "parts", Expand: .1}})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []float64{109.4, 29.4, 116.6, 36.6} {
		if math.Abs(got.BBox[i]-want) > 1e-6 {
			t.Fatal(got.BBox)
		}
	}
}
