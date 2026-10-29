package dataservice

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/datasource"
)

func TestFindWhatAllowsSupplementalDescriptionWithoutChangingCategory(t *testing.T) {
	for _, tc := range []struct {
		name string
		what What
		want string
	}{
		{name: "category", what: What{Category: "water.river"}, want: "water.river"},
		{name: "description", what: What{Description: "河流线，需要河名"}, want: "water.river"},
		{name: "both", what: What{Category: "water.river", Description: "河流线，需要河名"}, want: "water.river"},
		{name: "category_is_authoritative", what: What{Category: "water.river", Description: "行政区划面"}, want: "water.river"},
		{name: "unknown_category_is_not_replaced", what: What{Category: "不存在的类别", Description: "河流线，需要河名"}},
		{name: "empty_category_with_description", what: What{Category: " ", Description: "河流线"}, want: "water.river"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, calls := fixture(t)
			s.options.Environment.Catalog.Categories = append(s.options.Environment.Catalog.Categories, datasource.Category{ID: "admin", Name: "行政区划面", Geometry: "polygon"})
			req := riverRequest()
			req.Need.What = tc.what
			result := invokeFindRequest(t, s, req)
			if result.Resolved.Category != tc.want || calls.Load() != 0 {
				t.Fatalf("category precedence/read-only behavior changed: %+v", result)
			}
			if tc.want == "" {
				if result.State != "needs_input" || len(result.Rows) != 0 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "DATA_NEED_UNRECOGNIZED" {
					t.Fatalf("invalid explicit category was silently corrected: %+v", result)
				}
				return
			}
			if result.State != "acquirable" || len(result.Rows) != 1 || len(result.Diagnostics) != 0 {
				t.Fatalf("valid combined definition rejected: %+v", result)
			}
			q, err := s.load(result.FindID)
			if err != nil || q.Request.Need.What != tc.what {
				t.Fatalf("supplemental description was discarded: %+v %v", q.Request.Need.What, err)
			}
		})
	}
}

func TestFindWhatRequiresAtLeastOneNonemptyField(t *testing.T) {
	for _, what := range []What{{}, {Category: " ", Description: "\t"}} {
		s, _ := fixture(t)
		req := riverRequest()
		req.Need.What = what
		raw, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		var args map[string]any
		if err := json.Unmarshal(raw, &args); err != nil {
			t.Fatal(err)
		}
		_, err = s.Invoke(context.Background(), "data_find", args)
		requireContractError(t, err, "DATA_ARGUMENT_INVALID")
		_, err = s.Find(context.Background(), req)
		requireContractError(t, err, "DATA_NEED_UNRECOGNIZED")
	}
}

func TestFindDiagnosticAgreesWithActualAcquisitionState(t *testing.T) {
	for _, tc := range []struct {
		name, state, code string
	}{
		{name: "direct", state: "acquirable"},
		{name: "exploration", state: "exploration_required", code: "DATA_EXPLORATION_REQUIRED"},
		{name: "unavailable", state: "unavailable", code: "DATA_NO_CANDIDATE"},
		{name: "invalid_provided", state: "needs_input", code: "DATA_PROVIDED_INVALID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := fixture(t)
			req := riverRequest()
			req.IncludeFallback = true
			if tc.name != "direct" {
				s.options.Environment.Catalog.Sources = nil
			}
			if tc.name == "unavailable" {
				s.options.Environment.Integrations = nil
			}
			if tc.name == "invalid_provided" {
				path := filepath.Join(t.TempDir(), "provided.geojson")
				writeGeo(t, path, "Point")
				req.Provided = []string{path}
			}
			result := invokeFindRequest(t, s, req)
			if result.State != tc.state {
				t.Fatalf("wrong state: %+v", result)
			}
			if tc.code == "" {
				if len(result.Diagnostics) != 0 {
					t.Fatal("usable direct source has a blocking diagnostic", result.Diagnostics)
				}
			} else if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != tc.code {
				t.Fatalf("state and diagnostic conflict: %+v", result)
			}
			cached, err := s.FindResult(context.Background(), result.FindID)
			if err != nil || cached.State != result.State || !reflect.DeepEqual(cached.Diagnostics, result.Diagnostics) {
				t.Fatalf("cached plan lookup has different diagnostics: %+v %v", cached, err)
			}
		})
	}
}

func TestFindExcludedDirectRouteDoesNotContradictSameSourceExploration(t *testing.T) {
	for _, browser := range []bool{true, false} {
		s, _ := fixture(t)
		s.options.Environment.Integrations = map[string]bool{"browser": browser}
		s.options.Environment.WebSearch = false
		s.options.Environment.Catalog.Sources = []datasource.Source{{ID: "source.custom_portal", Name: "custom portal", Kind: "portal", Status: "experimental", Requires: []string{"integration:browser"}, Provides: []datasource.Provide{{Category: "water.river"}}}}
		req := riverRequest()
		req.IncludeFallback = true
		result := invokeFindRequest(t, s, req)
		if len(result.Rows) != 1 || len(result.Fallback) != 1 || result.Fallback[0].Available != browser {
			t.Fatalf("unexpected portal availability: %+v", result)
		}
		var reason string
		for _, excluded := range result.Rows[0].Excluded {
			if excluded.SourceID == result.Fallback[0].SourceID {
				reason = excluded.Reason
			}
		}
		if !strings.Contains(reason, "该来源尚未完成验证") {
			t.Fatal("original exclusion reason was lost", reason)
		}
		if browser {
			if result.State != "exploration_required" || !strings.Contains(reason, "确定性获取路径被排除") || !strings.Contains(reason, "仍可通过探索获取") {
				t.Fatalf("direct exclusion globally denies an available source: %+v", result)
			}
		} else if result.State != "unavailable" || strings.Contains(reason, "仍可通过探索获取") {
			t.Fatalf("unavailable source advertised as exploratory: %+v", result)
		}
		cached, err := s.FindResult(context.Background(), result.FindID)
		if err != nil || !reflect.DeepEqual(cached.Rows, result.Rows) {
			t.Fatalf("cached exclusions differ: %+v %v", cached, err)
		}
	}
}
