package dataservice

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/autocarto/carto_agent/internal/datasource"
)

func TestFindCategoryParameterDiagnostics(t *testing.T) {
	for _, value := range []string{"", "地级市"} {
		t.Run("level="+value, func(t *testing.T) {
			s, calls := fixture(t)
			s.options.Environment.Catalog.Categories = append(s.options.Environment.Catalog.Categories, datasource.Category{
				ID: "admin.division", Name: "行政区划面", Geometry: "polygon",
				Params: map[string]datasource.Parameter{"level": {Required: true, Values: []string{"省", "市", "县"}}},
			})
			req := riverRequest()
			req.Need.What = What{Description: "某区域地级市行政区划面"}
			req.Need.Params = map[string]string{"level": value}
			result := invokeFindRequest(t, s, req)
			if result.Resolved.Category != "admin.division" || len(result.Diagnostics) != 1 || len(result.Rows) != 0 {
				t.Fatalf("recognized category was lost: %+v", result)
			}
			diagnostic := result.Diagnostics[0]
			want := []ParameterRequirement{{Path: "need.params.level", Required: true, AllowedValues: []string{"省", "市", "县"}, ProvidedValue: value}}
			if diagnostic.Code != "DATA_NEED_PARAMS_INVALID" || !reflect.DeepEqual(diagnostic.Parameters, want) {
				t.Fatalf("missing correction contract: %+v", diagnostic)
			}
			if len(diagnostic.Suggestions) != 1 || !strings.Contains(diagnostic.Suggestions[0], "need.params.level") || !strings.Contains(diagnostic.Message, "省、市、县") {
				t.Fatalf("parameter error suggests unrelated categories: %+v", diagnostic)
			}
			if req.Need.Params["level"] != value || calls.Load() != 0 {
				t.Fatal("Find guessed a level or executed a tool")
			}
		})
	}
}

func TestFindParameterCorrectionsUseCustomCatalogAndRemainReadOnly(t *testing.T) {
	s, calls := fixture(t)
	category := &s.options.Environment.Catalog.Categories[0]
	category.Params = map[string]datasource.Parameter{
		"edition": {Required: true},
		"detail":  {Values: []string{"fine", "coarse"}},
	}
	req := riverRequest()
	req.Need.Params = map[string]string{"detail": "medium"}
	input := filepath.Join(s.options.Environment.Catalog.Sources[0].Root, "source.geojson")
	before, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	var network atomic.Int32
	previous := http.DefaultTransport
	http.DefaultTransport = failTransport{&network}
	defer func() { http.DefaultTransport = previous }()

	result := invokeFindRequest(t, s, req)
	want := []ParameterRequirement{
		{Path: "need.params.detail", Required: false, AllowedValues: []string{"fine", "coarse"}, ProvidedValue: "medium"},
		{Path: "need.params.edition", Required: true},
	}
	if result.Resolved.Category != category.ID || len(result.Diagnostics) != 1 || !reflect.DeepEqual(result.Diagnostics[0].Parameters, want) {
		t.Fatalf("custom constraints missing or hardcoded: %+v", result)
	}
	req.Need.Params = map[string]string{"detail": "fine", "edition": "survey-v2"}
	corrected := invokeFindRequest(t, s, req)
	if len(corrected.Diagnostics) != 0 || len(corrected.Rows) != 1 || corrected.Rows[0].Recommended == "" {
		t.Fatalf("corrected request did not find local data: %+v", corrected)
	}
	after, err := os.ReadFile(input)
	if err != nil || string(before) != string(after) || calls.Load() != 0 || network.Load() != 0 {
		t.Fatalf("Find performed work: tools=%d network=%d err=%v", calls.Load(), network.Load(), err)
	}
}

func TestFindUnknownCategoryStillSuggestsCategories(t *testing.T) {
	s, _ := fixture(t)
	req := riverRequest()
	req.Need.What = What{Description: "不存在的数据类别"}
	result := invokeFindRequest(t, s, req)
	if result.Resolved.Category != "" || len(result.Diagnostics) != 1 {
		t.Fatalf("%+v", result)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "DATA_NEED_UNRECOGNIZED" || len(diagnostic.Parameters) != 0 || !reflect.DeepEqual(diagnostic.Suggestions, []string{"河流"}) {
		t.Fatalf("unknown category contract changed: %+v", diagnostic)
	}
}

func TestFindRequiredParameterStillUsesSourceDefault(t *testing.T) {
	s, _ := fixture(t)
	s.options.Environment.Catalog.Categories[0].Params = map[string]datasource.Parameter{
		"season": {Required: true, Values: []string{"dry", "wet"}},
	}
	s.options.Environment.Catalog.Sources[0].Provides[0].Defaults = map[string]string{"season": "dry"}
	for _, params := range []map[string]string{nil, {"season": "wet"}} {
		req := riverRequest()
		req.Need.Params = params
		result := invokeFindRequest(t, s, req)
		if len(result.Diagnostics) != 0 || len(result.Rows) != 1 || result.Rows[0].Recommended == "" {
			t.Fatalf("valid default or explicit override was rejected: %+v", result)
		}
		query, err := s.load(result.FindID)
		if err != nil {
			t.Fatal(err)
		}
		declared := query.Items[req.Need.Name].Params
		if declared["season"] != params["season"] {
			t.Fatal("source defaults leaked into the declared request")
		}
	}
	req := riverRequest()
	req.Need.Params = map[string]string{"season": "unknown"}
	result := invokeFindRequest(t, s, req)
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "DATA_NEED_PARAMS_INVALID" || result.Diagnostics[0].Parameters[0].ProvidedValue != "unknown" {
		t.Fatal("explicit invalid value was replaced with a default", result)
	}
	if s.options.Environment.Catalog.Sources[0].Provides[0].Defaults["season"] != "dry" {
		t.Fatal("source defaults mutated")
	}
}

func invokeFindRequest(t *testing.T, s *Service, request FindRequest) FindResult {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		t.Fatal(err)
	}
	result, err := s.Invoke(context.Background(), "data_find", args)
	if err != nil {
		t.Fatal(err)
	}
	return result.(FindResult)
}
