package datasource

import "testing"

func TestPortalExplorationKeepsDeterministicAndPermissionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, kind, status string
		browser, secret    bool
		params             map[string]string
		available          bool
	}{
		{name: "portal", kind: "portal", status: "experimental", browser: true, secret: true, params: map[string]string{"level": "city"}, available: true},
		{name: "disabled", kind: "portal", status: "disabled", browser: true, secret: true, params: map[string]string{"level": "city"}},
		{name: "integration_off", kind: "portal", status: "experimental", secret: true, params: map[string]string{"level": "city"}},
		{name: "secret_missing", kind: "portal", status: "experimental", browser: true, params: map[string]string{"level": "city"}},
		{name: "parameters", kind: "portal", status: "experimental", browser: true, secret: true, params: map[string]string{"level": "county"}},
		{name: "experimental_query_adapter", kind: "http_file", status: "experimental", browser: true, secret: true, params: map[string]string{"level": "city"}},
		{name: "experimental_search", kind: "web_search", status: "experimental", browser: true, secret: true, params: map[string]string{"level": "city"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := &Catalog{Categories: []Category{{ID: "stats", Name: "统计", Geometry: "table", Params: map[string]Parameter{"level": {Values: []string{"city", "county"}}}}}, Sources: []Source{{ID: "source.fixture", Kind: tc.kind, Status: tc.status, Requires: []string{"integration:browser", "secret:fixture", "login:user"}, Provides: []Provide{{Category: "stats", Params: map[string]string{"level": "city"}}}}}}
			env := PlanningEnvironment{Catalog: catalog, Integrations: map[string]bool{"browser": tc.browser}, Secrets: map[string]bool{"fixture": tc.secret}, WebSearch: true}
			item := AcquisitionItem{Category: "stats", Acquirable: true, Params: tc.params}
			if direct := env.Rank(item); direct.Available {
				t.Fatal("unverified deterministic adapter became available", direct)
			}
			if explored := env.RankPortalExploration(item); explored.Available != tc.available {
				t.Fatalf("portal exploration prerequisites changed: %+v", explored)
			}
			if direct := env.Rank(item); direct.Available || env.portalExploration {
				t.Fatal("exploration mode leaked into ordinary ranking")
			}
		})
	}
}
