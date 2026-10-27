package flow

import (
	"github.com/autocarto/carto_agent/internal/datasource"
	"testing"
)

func TestRequirementsValidateRequiredSourceDefaultsWithoutCapturingThem(t *testing.T) {
	catalog, err := datasource.Load("../../../autocarto/tools/system/data-acquisition/assets", "")
	if err != nil {
		t.Fatal(err)
	}
	catalog.Category("population.grid").Params = map[string]datasource.Parameter{"year": {Required: true, Values: []string{"2019", "2020"}}}
	for _, params := range []map[string]string{nil, {"year": "2019"}} {
		doc := &FlowDoc{Requirements: []RequirementDoc{{Name: "人口", Category: "population.grid", Extent: "全国", Params: params}}}
		plan := &FlowPlan{Inputs: []InputPlan{{Key: "人口", Label: "人口", Type: "file", Required: true}}}
		buildRequirements(doc, plan, nil, Environment{DataSources: catalog, Integrations: map[string]bool{"gee": true}})
		if HasErrors(plan.Diagnostics) {
			t.Fatalf("%+v", plan.Diagnostics)
		}
		if plan.Acquisition == nil || len(plan.Acquisition.Items) != 1 || !plan.Acquisition.Items[0].Available {
			t.Fatalf("%+v", plan.Acquisition)
		}
		if params == nil && len(plan.Acquisition.Items[0].Params) != 0 {
			t.Fatal("compile captured source-specific defaults")
		}
	}
}
