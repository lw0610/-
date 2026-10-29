package dataservice

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	canonicaltool "github.com/autocarto/carto_agent/internal/tool"
)

func TestBuildRegistersAlreadyAcquiredFileWithExplicitNeed(t *testing.T) {
	definitions, err := canonicaltool.LoadDefinitionsFromDirectories("../../../autocarto/tools/system/data-service")
	if err != nil || len(definitions) != 3 {
		t.Fatal("data-service tool contracts could not load", err)
	}
	s, calls := fixture(t)
	request := contractRegistration(t, s, "")
	need := riverRequest().Need
	request.Need = &need
	request.Row, request.Kind = "", ""
	request.SourceTitle, request.Method = "OSM fixture river", "osm"
	var network atomic.Int32
	previous := http.DefaultTransport
	http.DefaultTransport = failTransport{&network}
	defer func() { http.DefaultTransport = previous }()
	raw, _ := json.Marshal(request)
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		t.Fatal(err)
	}
	value, err := s.Invoke(context.Background(), "data_register", args)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(GetResult)
	if result.Status != "completed" || len(result.Rows) != 1 || result.Rows[0].StoreKey == "" || result.Rows[0].Category != "water.river" {
		t.Fatalf("explicit need was not registered: %+v", result)
	}
	if network.Load() != 0 || calls.Load() == 0 {
		t.Fatalf("registration should use the existing local normalization without downloading: network=%d tools=%d", network.Load(), calls.Load())
	}
	if queries, _ := filepath.Glob(filepath.Join(s.options.SessionDir, "data", "finds", "*.json")); len(queries) != 0 {
		t.Fatal("registration manufactured a discovery query", queries)
	}
	if !IsRegistered(s.options.SessionDir, result.Rows[0].Paths[0]) {
		t.Fatal("completed registration did not record its verified file")
	}
	// The direct entry uses the same geometry/content validation as a plan need.
	wrong := filepath.Join(s.options.SessionDir, "wrong.geojson")
	writeGeo(t, wrong, "Point")
	request.Path = wrong
	_, err = s.Register(context.Background(), request)
	requireContractError(t, err, "DATA_CONTENT_INVALID")
}

func TestRegistrationMissingContextIsNotAnExpiredQuery(t *testing.T) {
	s, _ := fixture(t)
	_, err := s.Register(context.Background(), RegisterRequest{Path: "already-downloaded.geojson"})
	requireContractError(t, err, "DATA_REGISTRATION_REQUIRED")
	for _, field := range []string{"find_id", "row", "kind", "need"} {
		if !strings.Contains(err.Error(), field) {
			t.Fatal("missing actionable registration contract", err)
		}
	}
	for _, tc := range []struct{ id, code string }{
		{"", "DATA_QUERY_REQUIRED"},
		{"guessed-query", "DATA_QUERY_INVALID"},
		{"df-00000000000000000000000000000001", "DATA_QUERY_NOT_FOUND"},
	} {
		_, err = s.load(tc.id)
		requireContractError(t, err, tc.code)
	}
}

func TestInvalidDiscoveryDoesNotPublishOrStoreAQuery(t *testing.T) {
	for _, kind := range []string{"category", "extent", "scale"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := fixture(t)
			request := riverRequest()
			switch kind {
			case "category":
				request.Need.What.Category = "地铁和人口混合需求"
			case "extent":
				request.Need.Extent = "not-a-bound-layer"
			case "scale":
				request.Need.Scale = "100米"
			}
			result, err := s.Find(context.Background(), request)
			if result.FindID != "" || (err == nil && result.State != "needs_input") {
				t.Fatalf("invalid request published a query: %+v %v", result, err)
			}
			if files, _ := filepath.Glob(filepath.Join(s.options.SessionDir, "data", "finds", "*.json")); len(files) != 0 {
				t.Fatal("invalid request persisted a usable-looking query", files)
			}
		})
	}
}

func TestExplicitRegistrationNeedDoesNotReplacePlanBindings(t *testing.T) {
	for _, mode := range []string{"need", "selection"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := fixture(t)
			found, selection := contractExplore(t, s)
			request := contractRegistration(t, s, found.FindID)
			request.Need = &Need{Name: "unrelated", What: What{Category: "invented"}}
			request.SourceTitle, request.Method = "Fixture river", "web_search"
			ctx := WithAcquisitionSelection(context.Background(), selection)
			if mode == "need" {
				request.FindID, request.Row, request.Kind = "", "", ""
				ctx = WithAcquisitionNeed(context.Background(), AcquisitionNeed{ID: "plan/river", Need: selection.Request.Need, Item: selection.Rows[0].Item})
			}
			result, err := s.Register(ctx, request)
			if err != nil || len(result.Rows) != 1 || result.Rows[0].Category != "water.river" {
				t.Fatalf("explicit need replaced trusted %s binding: %+v %v", mode, result, err)
			}
		})
	}
}
