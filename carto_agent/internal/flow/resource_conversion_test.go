package flow

import (
	"github.com/autocarto/carto_agent/internal/resource"
	"reflect"
	"testing"
)

func TestResourcePlanConversionPreservesSnapshot(t *testing.T) {
	r := &resource.ResolvedResource{ID: "symbols.test", Key: "symbol-key", Type: "symbols", Problems: []resource.Diagnostic{{Level: "warning", Code: "EXAMPLE", Fix: map[string]any{"candidates": []map[string]any{{"id": "symbols.other"}}}}}, Entries: []resource.ResourceEntry{{Fields: map[string]any{"priority": 7, "event_types": []string{"meeting", "battle"}, "required": true}}}}
	plan := ResourcePlanFromResolved(r)
	roundtrip := plan.ResolvedResource()
	if !reflect.DeepEqual(r, roundtrip) {
		t.Fatalf("conversion lost normalized fields: %#v", roundtrip)
	}
	roundtrip.Entries[0].Fields["priority"] = 99
	if r.Entries[0].Fields["priority"] != 7 || plan.Entries[0].Fields["priority"] != 7 {
		t.Fatal("conversion shares mutable execution state")
	}
}
