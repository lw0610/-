package api

import (
	"github.com/autocarto/carto_agent/internal/flow"
	"reflect"
	"testing"
)

func TestFlowDataSummaryCountsRequirementsAndAvailabilitySeparately(t *testing.T) {
	plan := &flow.FlowPlan{Requirements: []flow.RequirementDoc{{Name: "base"}}, Acquisition: &flow.AcquisitionPlan{Items: []flow.AcquisitionItem{{Acquirable: true, Available: true}, {Acquirable: true}, {Acquirable: false}}}}
	want := map[string]int{"requirements": 1, "items": 3, "acquirable": 2, "available": 1}
	if got := summarizeFlowData(plan); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
