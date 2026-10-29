package plan

import "testing"

func TestRouteDrawingTablesAreOptionalAndSourceReferencesStillRequired(t *testing.T) {
	p := &Plan{Goal: "继续制图", Steps: []Step{{
		ID: "extract", Inputs: []string{"missing-source"}, Capabilities: []string{"route_knowledge"},
		Produces: []Product{{ID: "route", Kind: "dataset", Format: "route_dataset"}},
	}}}
	codes := map[string]bool{}
	for _, diagnostic := range ValidateV2(p) {
		codes[diagnostic.Code] = true
	}
	if !codes["PLAN_REF_INVALID"] || codes["PLAN_PARAMETERS_MISSING"] {
		t.Fatalf("optional drawing tables changed source checks: %v", codes)
	}
}
