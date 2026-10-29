package websocket

import (
	"context"
	"reflect"
	"testing"

	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/plan"
)

func TestV7MemoryExtentReadsActualInspectDataEnvelope(t *testing.T) {
	s, factory, session, p := cartoRuntimeFixture(t)
	factory.invoke = func(_ context.Context, name string, args map[string]any) (any, error) {
		if name != "layer_inspect" || args["layer_id"] != "memory-route" || args["path"] != nil {
			t.Fatalf("memory layer must be inspected by ID: %s %v", name, args)
		}
		return map[string]any{"success": true, "data": map[string]any{"geometry": "polygon", "coverage": map[string]any{"bbox": []float64{112, 32, 114, 34}}}}, nil
	}
	state := &flowrun.RunState{SessionID: session.SessionID, Scheme: session.Scheme, CartoPlan: p, Inputs: map[string]any{"route": "memory-route"}}
	extent, err := s.planDataExtent(context.Background(), state, plan.Data{ID: "river", Name: "沿线河流", Extent: &plan.Extent{From: "route"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(extent.BBox, []float64{112, 32, 114, 34}) {
		t.Fatalf("WGS84 memory layer bounds lost: %+v", extent)
	}
}
