package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/protocol"
	"github.com/autocarto/carto_agent/internal/toolcatalog"
)

func buildVersionedControlRouter(t *testing.T) *mux.Router {
	t.Helper()
	handler := NewAPIWithServices(nil, nil, nil, nil, &stubRuntimeCoordinator{snapshot: middlewareclient.RuntimeSnapshot{
		Version:           3,
		Timestamp:         time.Date(2026, time.April, 5, 10, 0, 0, 0, time.UTC),
		ConfigRevision:    "config-rev-3",
		DiscoveryRevision: "discovery-rev-3",
		RuntimeRevision:   "runtime-rev-3",
		Services: map[string]middlewareclient.ServiceStatus{
			"agent": {ID: "agent", Status: "running", Enabled: true},
		},
		Instances: middlewareclient.InstancesSnapshot{Items: []*discovery.QGISInstance{}},
	}}, stubConfigMutationSettingsService{}, stubAPIAssetCatalogService{view: control.ToolCatalogView{
		Scope:           "global",
		ConfigName:      "default",
		RuntimeRevision: "runtime-rev-3",
		Catalog: toolcatalog.Contract{
			ID:                "tool_catalog/global",
			Version:           toolcatalog.ContractVersion,
			Source:            "go_control_plane",
			CapabilityType:    toolcatalog.CapabilityTypeToolCatalog,
			RuntimeTarget:     "mixed",
			Availability:      toolcatalog.AvailabilityAvailable,
			DegradationStatus: toolcatalog.DegradationStatusNone,
			Items:             []toolcatalog.Capability{},
		},
	}}, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)
	return router
}

func TestHTTPContractVersionNegotiation(t *testing.T) {
	router := buildVersionedControlRouter(t)
	cases := []struct {
		name    string
		request *http.Request
	}{
		{
			name:    "header",
			request: httptest.NewRequest(http.MethodGet, "/api/runtime/snapshot", nil),
		},
		{
			name:    "query",
			request: httptest.NewRequest(http.MethodGet, "/api/tools/catalog?contract_version="+protocol.ContractVersionV1, nil),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.request
			if tc.name == "header" {
				req.Header.Set(httpContractVersionHeader, protocol.ContractVersionV1)
			}
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("unexpected status=%d body=%s", rr.Code, rr.Body.String())
			}
			if got := rr.Header().Get(httpContractVersionHeader); got != protocol.ContractVersionV1 {
				t.Fatalf("unexpected %s: %q", httpContractVersionHeader, got)
			}
			if got := rr.Header().Get(httpSupportedContractVersionsHeader); got != protocol.ContractVersionV1 {
				t.Fatalf("unexpected %s: %q", httpSupportedContractVersionsHeader, got)
			}
		})
	}
}

func TestRejectUnsupportedHTTPVersion(t *testing.T) {
	router := buildVersionedControlRouter(t)
	cases := []struct {
		name string
		req  *http.Request
	}{
		{
			name: "header",
			req:  httptest.NewRequest(http.MethodGet, "/api/runtime/snapshot", nil),
		},
		{
			name: "query",
			req:  httptest.NewRequest(http.MethodGet, "/api/tools/catalog?contract_version=1", nil),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.req
			req.Header.Set("X-Correlation-ID", "corr-test-1")
			req.Header.Set("X-Evidence-ID", "evid-test-1")
			if tc.name == "header" {
				req.Header.Set(httpContractVersionHeader, "1")
			}
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("unexpected status=%d body=%s", rr.Code, rr.Body.String())
			}
			payload, envelope := decodeEnvelopeResponse(t, rr)
			assertEnvelopeResponse(t, payload, envelope, "HTTP_CONTRACT_VERSION_UNSUPPORTED", "control", "runtime_contract")
			data, ok := payload["data"].(map[string]any)
			if !ok {
				t.Fatalf("expected structured version mismatch payload, got %#v", payload["data"])
			}
			if got := data["contract_version"]; got != protocol.ContractVersionV1 {
				t.Fatalf("unexpected contract_version: %#v", got)
			}
			if got := data["requested_contract_version"]; got != "1" {
				t.Fatalf("unexpected requested_contract_version: %#v", got)
			}
			versions, ok := data["supported_contract_versions"].([]any)
			if !ok || len(versions) != 1 || versions[0] != protocol.ContractVersionV1 {
				t.Fatalf("unexpected supported_contract_versions: %#v", data["supported_contract_versions"])
			}
		})
	}
}

func TestHTTPContractVersionOptionsBypass(t *testing.T) {
	router := buildVersionedControlRouter(t)
	req := httptest.NewRequest(http.MethodOptions, "/api/runtime/snapshot?contract_version=1", nil)
	req.Header.Set("Origin", "http://127.0.0.1:3005")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", httpContractVersionHeader)
	req.Header.Set(httpContractVersionHeader, "1")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("unexpected status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Access-Control-Allow-Headers"); got == "" || !strings.Contains(got, httpContractVersionHeader) {
		t.Fatalf("expected preflight allow headers to contain %s, got %q", httpContractVersionHeader, got)
	}
}

func TestHTTPDegradedEnvelope(t *testing.T) {
	registry, err := discovery.NewServiceRegistry(filepath.Join(t.TempDir(), "services.db"))
	if err != nil {
		t.Fatalf("new service registry failed: %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	handler := NewAPIWithServices(nil, nil, nil, registry, &stubRuntimeCoordinator{snapshot: middlewareclient.RuntimeSnapshot{
		Version:   1,
		Timestamp: time.Date(2026, time.April, 7, 10, 0, 0, 0, time.UTC),
	}}, nil, nil, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/descriptor", nil)
	req.Header.Set("X-Correlation-ID", "corr-test-1")
	req.Header.Set("X-Evidence-ID", "evid-test-1")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get(httpContractVersionHeader); got != protocol.ContractVersionV1 {
		t.Fatalf("unexpected %s: %q", httpContractVersionHeader, got)
	}
	if got := rr.Header().Get(httpSupportedContractVersionsHeader); got != protocol.ContractVersionV1 {
		t.Fatalf("unexpected %s: %q", httpSupportedContractVersionsHeader, got)
	}
	payload, envelope := decodeEnvelopeResponse(t, rr)
	assertEnvelopeResponse(t, payload, envelope, "DISCOVERY_DESCRIPTOR_INVALID", "discovery", "runtime_descriptor")
	if envelope == nil {
		t.Fatal("expected structured envelope")
	}
	if _, ok := payload["data"].(map[string]any); ok {
		t.Fatalf("expected degraded/error response without fallback data, got %#v", payload["data"])
	}
}
