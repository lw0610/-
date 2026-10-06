package api

import (
	"context"
	"errors"
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

type divergentRuntimeCoordinator struct {
	snapshot middlewareclient.RuntimeSnapshot
	services control.RuntimeServicesView
}

type blockingRuntimeCoordinator struct{}

func (d divergentRuntimeCoordinator) Snapshot(ctx context.Context) middlewareclient.RuntimeSnapshot {
	_ = ctx
	return d.snapshot
}

func (d divergentRuntimeCoordinator) Services(ctx context.Context) control.RuntimeServicesView {
	_ = ctx
	return d.services
}

func (blockingRuntimeCoordinator) Snapshot(ctx context.Context) middlewareclient.RuntimeSnapshot {
	<-ctx.Done()
	return middlewareclient.RuntimeSnapshot{}
}

func (blockingRuntimeCoordinator) Services(ctx context.Context) control.RuntimeServicesView {
	<-ctx.Done()
	return control.RuntimeServicesView{}
}

func TestRuntimeHandlersRejectUnsupportedContractVersion(t *testing.T) {
	registry, err := discovery.NewServiceRegistry(filepath.Join(t.TempDir(), "services.db"))
	if err != nil {
		t.Fatalf("new service registry failed: %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	if err := registry.Register(discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        3000,
		Protocol:    "http",
		Status:      "running",
	}); err != nil {
		t.Fatalf("register service port failed: %v", err)
	}

	handler := NewAPIWithServices(nil, nil, nil, registry, &stubRuntimeCoordinator{snapshot: middlewareclient.RuntimeSnapshot{
		Version:         2,
		Timestamp:       time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC),
		RuntimeRevision: "runtime-rev-2",
		ServicePhase:    "running",
		ActiveConfig:    "default",
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "qgis-2",
			InstanceCount: 1,
			RunningCount:  1,
		},
	}}, nil, nil, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	for _, path := range []string{"/api/runtime/snapshot", "/api/runtime/services", "/api/runtime/descriptor"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("X-Correlation-ID", "corr-test-1")
			req.Header.Set("X-Evidence-ID", "evid-test-1")
			req.Header.Set("X-AutoCarto-Contract-Version", "99")
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("unexpected status=%d body=%s", rr.Code, rr.Body.String())
			}
			if got := rr.Header().Get("X-AutoCarto-Contract-Version"); got != protocol.ContractVersionV1 {
				t.Fatalf("unexpected X-AutoCarto-Contract-Version: %q", got)
			}
			payload, envelope := decodeEnvelopeResponse(t, rr)
			assertEnvelopeResponse(t, payload, envelope, "HTTP_CONTRACT_VERSION_UNSUPPORTED", "control", "runtime_contract")

			data, ok := payload["data"].(map[string]any)
			if !ok {
				t.Fatalf("expected contract-version payload, got %#v", payload["data"])
			}
			if got := data["contract_version"]; got != protocol.ContractVersionV1 {
				t.Fatalf("unexpected contract_version: %#v", got)
			}
			if got := data["requested_contract_version"]; got != "99" {
				t.Fatalf("unexpected requested_contract_version: %#v", got)
			}
			supported, ok := data["supported_contract_versions"].([]any)
			if !ok || len(supported) != 1 || supported[0] != protocol.ContractVersionV1 {
				t.Fatalf("unexpected supported_contract_versions: %#v", data["supported_contract_versions"])
			}
		})
	}
}

func TestRuntimeSnapshotReturnsContractVersionHeader(t *testing.T) {
	service := &fakeToolSessionService{
		runtimeSnapshot: middlewareclient.RuntimeSnapshot{
			Version:   1,
			Timestamp: time.Date(2026, time.March, 12, 12, 30, 0, 0, time.UTC),
			Services: map[string]middlewareclient.ServiceStatus{
				"agent": {ID: "agent", Status: "running", Enabled: true},
			},
			Instances: middlewareclient.InstancesSnapshot{},
		},
	}
	router, _ := buildToolTestRouter(t, service)

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/snapshot", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/runtime/snapshot status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("X-AutoCarto-Contract-Version"); got != protocol.ContractVersionV1 {
		t.Fatalf("unexpected X-AutoCarto-Contract-Version: %q", got)
	}
	if got := rr.Header().Get("X-AutoCarto-Supported-Contract-Versions"); got != protocol.ContractVersionV1 {
		t.Fatalf("unexpected X-AutoCarto-Supported-Contract-Versions: %q", got)
	}
}

func TestRuntimeSnapshotPreflightAllowsContractVersionHeader(t *testing.T) {
	service := &fakeToolSessionService{
		runtimeSnapshot: middlewareclient.RuntimeSnapshot{
			Version:   1,
			Timestamp: time.Date(2026, time.April, 6, 18, 0, 0, 0, time.UTC),
			Services: map[string]middlewareclient.ServiceStatus{
				"agent": {ID: "agent", Status: "running", Enabled: true},
			},
			Instances: middlewareclient.InstancesSnapshot{},
		},
	}
	router, _ := buildToolTestRouter(t, service)

	req := httptest.NewRequest(http.MethodOptions, "/api/runtime/snapshot", nil)
	req.Header.Set("Origin", "http://127.0.0.1:3005")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "X-AutoCarto-Contract-Version, Content-Type")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS /api/runtime/snapshot status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:3005" {
		t.Fatalf("unexpected Access-Control-Allow-Origin: %q", got)
	}
	allowHeaders := rr.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(allowHeaders, "X-AutoCarto-Contract-Version") {
		t.Fatalf("expected preflight allow headers to contain contract version header, got %q", allowHeaders)
	}
}

func TestRuntimeSnapshotIncludesCoordinatedDiagnostics(t *testing.T) {
	service := &fakeToolSessionService{
		runtimeSnapshot: middlewareclient.RuntimeSnapshot{
			Version:                  7,
			Timestamp:                time.Date(2026, time.March, 12, 12, 30, 0, 0, time.UTC),
			ToolCatalogRevision:      "catalog-rev-test",
			ConfigRevision:           "config-rev-test",
			ActiveConfig:             "default",
			RuntimeActiveAgentConfig: "default",
			Services: map[string]middlewareclient.ServiceStatus{
				"agent": {
					ID:      "agent",
					Status:  "running",
					Enabled: true,
				},
				"middleware": {
					ID:      "middleware",
					Status:  "running",
					Enabled: true,
				},
			},
			Instances: middlewareclient.InstancesSnapshot{
				ActiveID:      "instance-1",
				InstanceCount: 1,
				RunningCount:  1,
			},
		},
	}
	router, _ := buildToolTestRouter(t, service)

	catalogReq := httptest.NewRequest(http.MethodGet, "/api/tools/catalog", nil)
	catalogRR := httptest.NewRecorder()
	router.ServeHTTP(catalogRR, catalogReq)
	if catalogRR.Code != http.StatusOK {
		t.Fatalf("GET /api/tools/catalog status=%d body=%s", catalogRR.Code, catalogRR.Body.String())
	}
	_, _, catalog := decodeToolCatalogContract(t, catalogRR)
	expectedSchemaRevision, _ := catalog["schema_revision"].(string)
	expectedAvailabilityRevision, _ := catalog["availability_revision"].(string)
	expectedCatalogRevision := control.JoinToolCatalogRevision(expectedSchemaRevision, expectedAvailabilityRevision)

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/snapshot", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/runtime/snapshot status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	if !resp.Success {
		t.Fatalf("runtime snapshot success=false: %+v", resp)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected runtime snapshot data type: %T", resp.Data)
	}
	if got := data["runtime_revision"]; got != "runtime-rev-test" {
		t.Fatalf("unexpected runtime_revision: %#v", got)
	}
	if got := data["service_phase"]; got != "degraded" {
		t.Fatalf("unexpected service_phase: %#v", got)
	}
	if got := data["tool_schema_revision"]; got != expectedSchemaRevision {
		t.Fatalf("unexpected tool_schema_revision: %#v", got)
	}
	if got := data["tool_availability_revision"]; got != expectedAvailabilityRevision {
		t.Fatalf("unexpected tool_availability_revision: %#v", got)
	}
	if got := data["last_rebuild_reason"]; got != "tool_schema_changed:schema-rev-test" {
		t.Fatalf("unexpected last_rebuild_reason: %#v", got)
	}
	if got := data["planner_ready_reason"]; got != "planner key missing" {
		t.Fatalf("unexpected planner_ready_reason: %#v", got)
	}
	toolRevisions, ok := data["tool_revisions"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected tool_revisions payload: %#v", data["tool_revisions"])
	}
	if got := data["tool_catalog_revision"]; got != expectedCatalogRevision {
		t.Fatalf("unexpected tool_catalog_revision: %#v", got)
	}
	if got := toolRevisions["catalog"]; got != expectedCatalogRevision {
		t.Fatalf("unexpected tool_revisions.catalog: %#v", got)
	}
	if got := toolRevisions["schema"]; got != expectedSchemaRevision {
		t.Fatalf("unexpected tool_revisions.schema: %#v", got)
	}
	if got := toolRevisions["availability"]; got != expectedAvailabilityRevision {
		t.Fatalf("unexpected tool_revisions.availability: %#v", got)
	}
	modeReadiness, ok := data["mode_readiness"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected mode_readiness payload: %#v", data["mode_readiness"])
	}
	planReadiness, ok := modeReadiness["plan"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected plan readiness payload: %#v", modeReadiness["plan"])
	}
	if got := planReadiness["ready"]; got != false {
		t.Fatalf("unexpected plan readiness: %#v", got)
	}
	if got := data["active_instance_id"]; got != "instance-1" {
		t.Fatalf("unexpected active_instance_id: %#v", got)
	}
	if got, ok := data["last_rebuild_at"].(string); !ok || got == "" {
		t.Fatalf("unexpected last_rebuild_at: %#v", data["last_rebuild_at"])
	}
}

func TestRevisionJumpReadModel(t *testing.T) {
	handler := NewAPIWithServices(nil, nil, nil, nil, divergentRuntimeCoordinator{
		snapshot: middlewareclient.RuntimeSnapshot{
			Version:                  11,
			Timestamp:                time.Date(2026, time.April, 4, 10, 30, 0, 0, time.UTC),
			ConfigRevision:           "config-rev-11",
			RuntimeRevision:          "runtime-rev-11",
			ActiveConfig:             "default",
			RuntimeActiveAgentConfig: "default",
			ServicePhase:             "degraded",
			ToolCatalogRevision:      "catalog-rev-11",
			ToolSchemaRevision:       "schema-rev-11",
			ToolAvailabilityRevision: "availability-rev-11",
			ToolRevisions: middlewareclient.ToolRevisions{
				Catalog:      "catalog-rev-11",
				Schema:       "schema-rev-11",
				Availability: "availability-rev-11",
			},
			ModeIssues: map[string]middlewareclient.ModeIssue{
				"plan": {Code: "PLANNER_BLOCKED", Message: "planner key missing"},
			},
			ModeReadiness: map[string]middlewareclient.ModeReadiness{
				"build": {Enabled: true, Ready: true},
				"plan":  {Enabled: true, Ready: false, Code: "PLANNER_BLOCKED", Message: "planner key missing"},
			},
			Services: map[string]middlewareclient.ServiceStatus{
				"agent": {ID: "agent", Status: "running", Source: "sidecar", Enabled: true},
				"qgis":  {ID: "qgis", Status: "running", Source: "reverse_ws", Enabled: true, InstanceCount: 1, RunningCount: 1},
			},
			Instances: middlewareclient.InstancesSnapshot{
				ActiveID:      "inst-11",
				InstanceCount: 1,
				RunningCount:  1,
			},
			Stale: true,
		},
		services: control.RuntimeServicesView{
			Services: map[string]middlewareclient.ServiceStatus{
				"agent": {ID: "agent", Status: "starting", Source: "legacy", Enabled: true},
			},
			Instances:                control.RuntimeInstancesView{ActiveID: "inst-old", InstanceCount: 0, RunningCount: 0},
			ConfigRevision:           "config-rev-old",
			RuntimeRevision:          "runtime-rev-old",
			RuntimeActiveAgentConfig: "old",
			ActiveInstanceID:         "inst-old",
			ToolCatalogRevision:      "catalog-rev-old",
			ToolSchemaRevision:       "schema-rev-old",
			ToolAvailabilityRevision: "availability-rev-old",
			Stale:                    false,
			Version:                  3,
			Timestamp:                "2000-01-01T00:00:00Z",
		},
	}, nil, nil, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	snapshotReq := httptest.NewRequest(http.MethodGet, "/api/runtime/snapshot", nil)
	snapshotRR := httptest.NewRecorder()
	router.ServeHTTP(snapshotRR, snapshotReq)
	if snapshotRR.Code != http.StatusOK {
		t.Fatalf("GET /api/runtime/snapshot status=%d body=%s", snapshotRR.Code, snapshotRR.Body.String())
	}
	snapshotResp := decodeAPIResponse(t, snapshotRR)
	snapshotData := snapshotResp.Data.(map[string]any)

	servicesReq := httptest.NewRequest(http.MethodGet, "/api/runtime/services", nil)
	servicesRR := httptest.NewRecorder()
	router.ServeHTTP(servicesRR, servicesReq)
	if servicesRR.Code != http.StatusOK {
		t.Fatalf("GET /api/runtime/services status=%d body=%s", servicesRR.Code, servicesRR.Body.String())
	}
	servicesResp := decodeAPIResponse(t, servicesRR)
	servicesData := servicesResp.Data.(map[string]any)

	for key, want := range map[string]any{
		"config_revision":             snapshotData["config_revision"],
		"runtime_revision":            snapshotData["runtime_revision"],
		"runtime_active_agent_config": snapshotData["runtime_active_agent_config"],
		"active_instance_id":          snapshotData["active_instance_id"],
		"tool_catalog_revision":       snapshotData["tool_catalog_revision"],
		"tool_schema_revision":        snapshotData["tool_schema_revision"],
		"tool_availability_revision":  snapshotData["tool_availability_revision"],
		"stale":                       snapshotData["stale"],
		"version":                     snapshotData["version"],
		"timestamp":                   snapshotData["timestamp"],
	} {
		if got := servicesData[key]; got != want {
			t.Fatalf("expected /services %s to share snapshot read model, got=%#v want=%#v", key, got, want)
		}
	}
}

func TestRuntimeSnapshotRefreshesTimestampAndOmitsLegacyHeadlessCompute(t *testing.T) {
	legacyTimestamp := time.Date(2026, time.March, 12, 12, 30, 0, 0, time.UTC)
	service := &fakeToolSessionService{
		runtimeSnapshot: middlewareclient.RuntimeSnapshot{
			Timestamp: legacyTimestamp,
			Services: map[string]middlewareclient.ServiceStatus{
				"agent":            {ID: "agent", Status: "running", Enabled: true},
				"cli":              {ID: "cli", Status: "running", Enabled: true},
				"headless_compute": {ID: "headless_compute", Status: "failed", Enabled: true},
			},
			Instances: middlewareclient.InstancesSnapshot{},
		},
	}
	router, _ := buildToolTestRouter(t, service)

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/snapshot", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/runtime/snapshot status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	if !resp.Success {
		t.Fatalf("runtime snapshot success=false: %+v", resp)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected runtime snapshot data type: %T", resp.Data)
	}
	timestampRaw, ok := data["timestamp"].(string)
	if !ok || timestampRaw == "" {
		t.Fatalf("expected runtime snapshot timestamp, got %#v", data["timestamp"])
	}
	timestamp, err := time.Parse(time.RFC3339, timestampRaw)
	if err != nil {
		t.Fatalf("parse runtime snapshot timestamp: %v", err)
	}
	if !timestamp.After(legacyTimestamp) {
		t.Fatalf("expected refreshed timestamp after legacy value, got %s", timestampRaw)
	}
	services, ok := data["services"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected services payload: %#v", data["services"])
	}
	if _, exists := services["headless_compute"]; exists {
		t.Fatalf("expected headless_compute omitted from public snapshot, got %#v", services)
	}
	if _, exists := services["cli"]; !exists {
		t.Fatalf("expected cli service kept in public snapshot, got %#v", services)
	}
}

func TestRuntimeDescriptorReturnsAuthoritativeDescriptor(t *testing.T) {
	registry, err := discovery.NewServiceRegistry(filepath.Join(t.TempDir(), "services.db"))
	if err != nil {
		t.Fatalf("new service registry failed: %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	if err := registry.Register(discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        50321,
		Protocol:    "websocket",
		Status:      "running",
	}); err != nil {
		t.Fatalf("register service port failed: %v", err)
	}

	publicCatalog := toolcatalog.WithPublicRevisions(toolcatalog.Contract{
		ID:                "tool_catalog/global",
		Version:           toolcatalog.ContractVersion,
		Source:            "go_control_plane",
		CapabilityType:    toolcatalog.CapabilityTypeToolCatalog,
		RuntimeTarget:     "qgis",
		Availability:      toolcatalog.AvailabilityAvailable,
		DegradationStatus: toolcatalog.DegradationStatusNone,
		Policy:            &toolcatalog.Policy{Scope: "global", ConfigName: "default"},
		Items: []toolcatalog.Capability{{
			ID:             "open_project",
			CapabilityType: toolcatalog.CapabilityTypeTool,
			RuntimeTarget:  "qgis",
			Availability:   toolcatalog.AvailabilityAvailable,
		}},
	}, "public-schema-rev", "public-availability-rev")

	handler := NewAPIWithServices(nil, nil, nil, registry, &stubRuntimeCoordinator{snapshot: middlewareclient.RuntimeSnapshot{
		Version:         9,
		Timestamp:       time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC),
		RuntimeRevision: "runtime-rev-9",
		ServicePhase:    "running",
		ActiveConfig:    "default",
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "qgis-9",
			InstanceCount: 1,
			RunningCount:  1,
		},
	}}, nil, stubAPIAssetCatalogService{view: control.ToolCatalogView{
		Scope:           "global",
		ConfigName:      "default",
		RuntimeRevision: "runtime-rev-9",
		Catalog:         publicCatalog,
	}}, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/descriptor", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/runtime/descriptor status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	if !resp.Success {
		t.Fatalf("runtime descriptor success=false: %+v", resp)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected runtime descriptor data type: %T", resp.Data)
	}
	if got := data["backend_url"]; got != "http://127.0.0.1:50321" {
		t.Fatalf("unexpected backend_url: %#v", got)
	}
	if got := data["ws_url"]; got != "ws://127.0.0.1:50321/ws" {
		t.Fatalf("unexpected ws_url: %#v", got)
	}
	if got := data["version"]; got != "v1" {
		t.Fatalf("unexpected version: %#v", got)
	}
	if got := data["revision"]; got != float64(9) {
		t.Fatalf("unexpected revision: %#v", got)
	}
	if got := data["readiness"]; got != "ready" {
		t.Fatalf("unexpected readiness: %#v", got)
	}
	if got := data["degraded"]; got != false {
		t.Fatalf("unexpected degraded flag: %#v", got)
	}
	retryHints, ok := data["retry_hints"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected retry_hints payload: %#v", data["retry_hints"])
	}
	if got := retryHints["retryable"]; got != false {
		t.Fatalf("unexpected retry_hints.retryable: %#v", got)
	}
	if got := data["port"]; got != float64(50321) {
		t.Fatalf("unexpected port: %#v", got)
	}
	if got := data["runtime_revision"]; got != "runtime-rev-9" {
		t.Fatalf("unexpected runtime_revision: %#v", got)
	}
	if got := data["restart_sequence"]; got != float64(9) {
		t.Fatalf("unexpected restart_sequence: %#v", got)
	}
	executionManifest, ok := data["execution_manifest"].([]any)
	if !ok || len(executionManifest) == 0 {
		t.Fatalf("expected authoritative execution_manifest, got %#v", data["execution_manifest"])
	}
	foundOpenProjectBinding := false
	for _, raw := range executionManifest {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if entry["name"] == "open_project" {
			if strings.TrimSpace(asString(entry["handler_file"])) == "" {
				t.Fatalf("expected open_project handler_file in execution manifest, got %#v", entry)
			}
			if strings.TrimSpace(asString(entry["handler_function"])) == "" {
				t.Fatalf("expected open_project handler_function in execution manifest, got %#v", entry)
			}
			foundOpenProjectBinding = true
		}
	}
	if !foundOpenProjectBinding {
		t.Fatalf("expected open_project execution binding in descriptor, got %#v", executionManifest)
	}
	publicToolCatalog, ok := data["tool_catalog"].([]any)
	if !ok || len(publicToolCatalog) != 1 {
		t.Fatalf("expected descriptor public tool_catalog, got %#v", data["tool_catalog"])
	}
	toolEntry, ok := publicToolCatalog[0].(map[string]any)
	if !ok || toolEntry["name"] != "open_project" {
		t.Fatalf("unexpected descriptor tool_catalog entry: %#v", publicToolCatalog)
	}
}

func TestRuntimeHandlersDegradeExplicitlyWhenPublicCatalogLoadFails(t *testing.T) {
	handler := NewAPIWithServices(nil, nil, nil, nil, divergentRuntimeCoordinator{
		snapshot: middlewareclient.RuntimeSnapshot{
			Version:                  18,
			Timestamp:                time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC),
			ConfigRevision:           "config-rev-internal",
			DiscoveryRevision:        "discovery-rev-internal",
			RuntimeRevision:          "runtime-rev-public",
			ActiveConfig:             "default",
			RuntimeActiveAgentConfig: "default",
			ServicePhase:             "ready",
			ToolCatalogRevision:      "internal-schema/internal-availability",
			ToolSchemaRevision:       "internal-schema",
			ToolAvailabilityRevision: "internal-availability",
			ToolRevisions: middlewareclient.ToolRevisions{
				Catalog:      "internal-schema/internal-availability",
				Schema:       "internal-schema",
				Availability: "internal-availability",
			},
			Instances: middlewareclient.InstancesSnapshot{
				ActiveID:      "inst-public",
				InstanceCount: 1,
				RunningCount:  1,
			},
		},
		services: control.RuntimeServicesView{
			Services: map[string]middlewareclient.ServiceStatus{
				"agent": {ID: "agent", Status: "running", Enabled: true},
			},
			Instances:                control.RuntimeInstancesView{ActiveID: "inst-public", InstanceCount: 1, RunningCount: 1},
			ConfigRevision:           "config-rev-internal",
			DiscoveryRevision:        "discovery-rev-internal",
			RuntimeRevision:          "runtime-rev-public",
			RuntimeActiveAgentConfig: "default",
			ActiveInstanceID:         "inst-public",
			ToolCatalogRevision:      "internal-schema/internal-availability",
			ToolSchemaRevision:       "internal-schema",
			ToolAvailabilityRevision: "internal-availability",
			ToolRevisions: middlewareclient.ToolRevisions{
				Catalog:      "internal-schema/internal-availability",
				Schema:       "internal-schema",
				Availability: "internal-availability",
			},
			Version:   18,
			Timestamp: "2026-04-09T12:00:00Z",
		},
	}, nil, stubAPIAssetCatalogService{err: errors.New("public catalog unavailable")}, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	for _, path := range []string{"/api/runtime/snapshot", "/api/runtime/services"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("GET %s status=%d body=%s", path, rr.Code, rr.Body.String())
			}
			resp := decodeAPIResponse(t, rr)
			data, ok := resp.Data.(map[string]any)
			if !ok {
				t.Fatalf("unexpected runtime data type: %T", resp.Data)
			}
			for _, key := range []string{"tool_schema_revision", "tool_availability_revision", "tool_catalog_revision"} {
				if got := strings.TrimSpace(asString(data[key])); got != "" {
					t.Fatalf("expected %s to stop exposing internal fallback revision, got %#v", key, data[key])
				}
			}
		})
	}
}

func TestRuntimeDescriptorDegradesWhenPublicCatalogLoadFails(t *testing.T) {
	registry, err := discovery.NewServiceRegistry(filepath.Join(t.TempDir(), "services.db"))
	if err != nil {
		t.Fatalf("new service registry failed: %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	if err := registry.Register(discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        50322,
		Protocol:    "http",
		Status:      "running",
	}); err != nil {
		t.Fatalf("register service port failed: %v", err)
	}

	handler := NewAPIWithServices(nil, nil, nil, registry, &stubRuntimeCoordinator{snapshot: middlewareclient.RuntimeSnapshot{
		Version:         19,
		Timestamp:       time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC),
		RuntimeRevision: "runtime-rev-19",
		ServicePhase:    "ready",
		ActiveConfig:    "default",
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "inst-public",
			InstanceCount: 1,
			RunningCount:  1,
		},
	}}, nil, stubAPIAssetCatalogService{err: errors.New("public catalog unavailable")}, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/descriptor", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/runtime/descriptor status=%d body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeAPIResponse(t, rr)
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected runtime descriptor data type: %T", resp.Data)
	}
	if got := data["readiness"]; got != "degraded" {
		t.Fatalf("expected degraded readiness when public catalog load fails, got %#v", got)
	}
	if got := data["reason_code"]; got != "CATALOG_DEGRADED" {
		t.Fatalf("expected CATALOG_DEGRADED reason_code, got %#v", got)
	}
}

func TestRuntimeDescriptorDegradesWhenPublicCatalogReadTimesOut(t *testing.T) {
	prevTimeout := assetCatalogReadTimeout
	assetCatalogReadTimeout = 10 * time.Millisecond
	defer func() {
		assetCatalogReadTimeout = prevTimeout
	}()

	registry, err := discovery.NewServiceRegistry(filepath.Join(t.TempDir(), "services.db"))
	if err != nil {
		t.Fatalf("new service registry failed: %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	if err := registry.Register(discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        50323,
		Protocol:    "http",
		Status:      "running",
	}); err != nil {
		t.Fatalf("register service port failed: %v", err)
	}

	handler := NewAPIWithServices(nil, nil, nil, registry, &stubRuntimeCoordinator{snapshot: middlewareclient.RuntimeSnapshot{
		Version:         20,
		Timestamp:       time.Date(2026, time.April, 9, 12, 30, 0, 0, time.UTC),
		RuntimeRevision: "runtime-rev-20",
		ServicePhase:    "ready",
		ActiveConfig:    "default",
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "inst-timeout",
			InstanceCount: 1,
			RunningCount:  1,
		},
	}}, nil, stubAPIAssetCatalogService{blockUntil: make(chan struct{})}, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/descriptor", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/runtime/descriptor status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected runtime descriptor data type: %T", resp.Data)
	}
	if got := data["readiness"]; got != "degraded" {
		t.Fatalf("expected degraded readiness when public catalog read times out, got %#v", got)
	}
	if got := data["reason_code"]; got != "CATALOG_DEGRADED" {
		t.Fatalf("expected CATALOG_DEGRADED reason_code, got %#v", got)
	}
}

func TestRuntimeDescriptorFallsBackWhenRuntimeSnapshotTimesOut(t *testing.T) {
	prevTimeout := runtimeSnapshotReadTimeout
	runtimeSnapshotReadTimeout = 10 * time.Millisecond
	defer func() {
		runtimeSnapshotReadTimeout = prevTimeout
	}()

	registry, err := discovery.NewServiceRegistry(filepath.Join(t.TempDir(), "services.db"))
	if err != nil {
		t.Fatalf("new service registry failed: %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	handler := NewAPIWithServices(nil, nil, nil, registry, blockingRuntimeCoordinator{}, nil, nil, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:50324/api/runtime/descriptor", nil)
	req.Host = "127.0.0.1:50324"
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/runtime/descriptor status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected runtime descriptor data type: %T", resp.Data)
	}
	if got := data["readiness"]; got != "degraded" {
		t.Fatalf("expected degraded readiness when runtime snapshot times out, got %#v", got)
	}
	if got := data["reason_code"]; got != "RUNTIME_SNAPSHOT_TIMEOUT" {
		t.Fatalf("expected RUNTIME_SNAPSHOT_TIMEOUT reason_code, got %#v", got)
	}
	if got := data["backend_url"]; got != "http://127.0.0.1:50324" {
		t.Fatalf("unexpected backend_url fallback: %#v", got)
	}
	if got := data["ws_url"]; got != "ws://127.0.0.1:50324/ws" {
		t.Fatalf("unexpected ws_url fallback: %#v", got)
	}
}

func TestRuntimeSnapshotFallsBackWhenRuntimeSnapshotTimesOut(t *testing.T) {
	prevTimeout := runtimeSnapshotReadTimeout
	runtimeSnapshotReadTimeout = 10 * time.Millisecond
	defer func() {
		runtimeSnapshotReadTimeout = prevTimeout
	}()

	handler := NewAPIWithServices(nil, nil, nil, nil, blockingRuntimeCoordinator{}, nil, nil, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/snapshot", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/runtime/snapshot status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected runtime snapshot data type: %T", resp.Data)
	}
	if got := data["service_phase"]; got != "degraded" {
		t.Fatalf("expected degraded service_phase when runtime snapshot times out, got %#v", got)
	}
	if got := data["stale"]; got != true {
		t.Fatalf("expected stale runtime snapshot fallback, got %#v", got)
	}
	services, ok := data["services"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected services payload: %#v", data["services"])
	}
	if _, ok := services["agent"].(map[string]any); !ok {
		t.Fatalf("expected agent service in fallback snapshot, got %#v", services["agent"])
	}
	modeIssues, ok := data["mode_issues"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected mode_issues payload: %#v", data["mode_issues"])
	}
	runtimeIssue, ok := modeIssues["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("expected runtime mode issue in fallback snapshot, got %#v", modeIssues["runtime"])
	}
	if got := runtimeIssue["code"]; got != "RUNTIME_SNAPSHOT_TIMEOUT" {
		t.Fatalf("unexpected runtime mode issue code: %#v", got)
	}
}

func TestRuntimeServicesFallsBackWhenRuntimeSnapshotTimesOut(t *testing.T) {
	prevTimeout := runtimeSnapshotReadTimeout
	runtimeSnapshotReadTimeout = 10 * time.Millisecond
	defer func() {
		runtimeSnapshotReadTimeout = prevTimeout
	}()

	handler := NewAPIWithServices(nil, nil, nil, nil, blockingRuntimeCoordinator{}, nil, nil, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/services", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/runtime/services status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected runtime services data type: %T", resp.Data)
	}
	if got := data["service_phase"]; got != "degraded" {
		t.Fatalf("expected degraded service_phase when runtime services snapshot times out, got %#v", got)
	}
	if got := data["stale"]; got != true {
		t.Fatalf("expected stale runtime services fallback, got %#v", got)
	}
	services, ok := data["services"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected services payload: %#v", data["services"])
	}
	if _, ok := services["agent"].(map[string]any); !ok {
		t.Fatalf("expected agent service in fallback services view, got %#v", services["agent"])
	}
	modeIssues, ok := data["mode_issues"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected mode_issues payload: %#v", data["mode_issues"])
	}
	runtimeIssue, ok := modeIssues["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("expected runtime mode issue in fallback services view, got %#v", modeIssues["runtime"])
	}
	if got := runtimeIssue["code"]; got != "RUNTIME_SNAPSHOT_TIMEOUT" {
		t.Fatalf("unexpected runtime mode issue code: %#v", got)
	}
}

func asString(value any) string {
	if str, ok := value.(string); ok {
		return str
	}
	return ""
}

func TestRuntimeDescriptorReturnsBlockReadinessWithoutFallbackSynthesis(t *testing.T) {
	registry, err := discovery.NewServiceRegistry(filepath.Join(t.TempDir(), "services.db"))
	if err != nil {
		t.Fatalf("new service registry failed: %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	if err := registry.Register(discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        3000,
		Protocol:    "http",
		Status:      "running",
	}); err != nil {
		t.Fatalf("register service port failed: %v", err)
	}

	handler := NewAPIWithServices(nil, nil, nil, registry, &stubRuntimeCoordinator{snapshot: middlewareclient.RuntimeSnapshot{
		Version:         2,
		Timestamp:       time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC),
		RuntimeRevision: "runtime-rev-2",
		ServicePhase:    "starting",
		ActiveConfig:    "default",
		Instances: middlewareclient.InstancesSnapshot{
			InstanceCount: 0,
			RunningCount:  0,
		},
	}}, nil, nil, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/descriptor", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/runtime/descriptor status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	if !resp.Success {
		t.Fatalf("runtime descriptor success=false: %+v", resp)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected runtime descriptor data type: %T", resp.Data)
	}
	if got := data["readiness"]; got != "block" {
		t.Fatalf("unexpected readiness: %#v", got)
	}
	if got := data["degraded"]; got != false {
		t.Fatalf("unexpected degraded flag: %#v", got)
	}
	if got := data["reason_code"]; got != "BACKEND_STARTING" {
		t.Fatalf("unexpected reason_code: %#v", got)
	}
	retryHints, ok := data["retry_hints"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected retry_hints payload: %#v", data["retry_hints"])
	}
	if got := retryHints["retryable"]; got != true {
		t.Fatalf("unexpected retry_hints.retryable: %#v", got)
	}
	if got, ok := retryHints["after_ms"].(float64); !ok || got <= 0 {
		t.Fatalf("unexpected retry_hints.after_ms: %#v", retryHints["after_ms"])
	}
	if got := data["backend_url"]; got != "http://127.0.0.1:3000" {
		t.Fatalf("unexpected backend_url: %#v", got)
	}
	if got := data["ws_url"]; got != "ws://127.0.0.1:3000/ws" {
		t.Fatalf("unexpected ws_url: %#v", got)
	}
}

func TestDiscoveryDescriptor_WaitingPluginIsDegradedButRetryable(t *testing.T) {
	registry, err := discovery.NewServiceRegistry(filepath.Join(t.TempDir(), "services.db"))
	if err != nil {
		t.Fatalf("new service registry failed: %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	if err := registry.Register(discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        3000,
		Protocol:    "http",
		Status:      "running",
	}); err != nil {
		t.Fatalf("register service port failed: %v", err)
	}

	handler := NewAPIWithServices(nil, nil, nil, registry, &stubRuntimeCoordinator{snapshot: middlewareclient.RuntimeSnapshot{
		Version:         3,
		Timestamp:       time.Date(2026, time.April, 8, 5, 0, 0, 0, time.UTC),
		RuntimeRevision: "runtime-rev-3",
		ServicePhase:    "waiting_plugin",
		ActiveConfig:    "default",
		Instances: middlewareclient.InstancesSnapshot{
			InstanceCount: 0,
			RunningCount:  0,
		},
	}}, nil, nil, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/descriptor", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/runtime/descriptor status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	if !resp.Success {
		t.Fatalf("runtime descriptor success=false: %+v", resp)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected runtime descriptor data type: %T", resp.Data)
	}
	if got := data["readiness"]; got != "degraded" {
		t.Fatalf("unexpected readiness: %#v", got)
	}
	if got := data["degraded"]; got != true {
		t.Fatalf("unexpected degraded flag: %#v", got)
	}
	if got := data["reason_code"]; got != "PLUGIN_REGISTRATION_PENDING" {
		t.Fatalf("unexpected reason_code: %#v", got)
	}
	retryHints, ok := data["retry_hints"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected retry_hints payload: %#v", data["retry_hints"])
	}
	if got := retryHints["retryable"]; got != true {
		t.Fatalf("unexpected retry_hints.retryable: %#v", got)
	}
	if got, ok := retryHints["after_ms"].(float64); !ok || got <= 0 {
		t.Fatalf("unexpected retry_hints.after_ms: %#v", retryHints["after_ms"])
	}
}

func TestDiscoveryDescriptor_ReadyWithoutPluginRegistrationDegradesWithoutPublicCatalog(t *testing.T) {
	registry, err := discovery.NewServiceRegistry(filepath.Join(t.TempDir(), "services.db"))
	if err != nil {
		t.Fatalf("new service registry failed: %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	if err := registry.Register(discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        3000,
		Protocol:    "http",
		Status:      "running",
	}); err != nil {
		t.Fatalf("register service port failed: %v", err)
	}

	handler := NewAPIWithServices(nil, nil, nil, registry, &stubRuntimeCoordinator{snapshot: middlewareclient.RuntimeSnapshot{
		Version:         4,
		Timestamp:       time.Date(2026, time.April, 9, 5, 0, 0, 0, time.UTC),
		RuntimeRevision: "runtime-rev-4",
		ServicePhase:    "ready",
		ActiveConfig:    "default",
		Instances: middlewareclient.InstancesSnapshot{
			InstanceCount: 0,
			RunningCount:  0,
		},
	}}, nil, nil, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/descriptor", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/runtime/descriptor status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	if !resp.Success {
		t.Fatalf("runtime descriptor success=false: %+v", resp)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected runtime descriptor data type: %T", resp.Data)
	}
	if got := data["readiness"]; got != "degraded" {
		t.Fatalf("unexpected readiness: %#v", got)
	}
	if got := data["degraded"]; got != true {
		t.Fatalf("unexpected degraded flag: %#v", got)
	}
	if got := data["reason_code"]; got != "CATALOG_DEGRADED" {
		t.Fatalf("expected CATALOG_DEGRADED reason_code, got %#v", got)
	}
}

func TestRuntimeDescriptorReturnsStructuredEnvelopeWhenUnavailableOrCorrupted(t *testing.T) {
	t.Run("missing agent port", func(t *testing.T) {
		registry, err := discovery.NewServiceRegistry(filepath.Join(t.TempDir(), "services.db"))
		if err != nil {
			t.Fatalf("new service registry failed: %v", err)
		}
		t.Cleanup(func() { _ = registry.Close() })

		handler := NewAPIWithServices(nil, nil, nil, registry, &stubRuntimeCoordinator{snapshot: middlewareclient.RuntimeSnapshot{
			Version:   1,
			Timestamp: time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC),
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
		payload, envelope := decodeEnvelopeResponse(t, rr)
		assertEnvelopeResponse(t, payload, envelope, "DISCOVERY_DESCRIPTOR_UNAVAILABLE", "discovery", "runtime_descriptor")
		if payload["data"] != nil {
			t.Fatalf("expected no fallback descriptor data, got %#v", payload["data"])
		}
	})

	t.Run("invalid agent port", func(t *testing.T) {
		registry, err := discovery.NewServiceRegistry(filepath.Join(t.TempDir(), "services.db"))
		if err != nil {
			t.Fatalf("new service registry failed: %v", err)
		}
		t.Cleanup(func() { _ = registry.Close() })
		if err := registry.Register(discovery.ServicePortEntry{
			ServiceName: discovery.ServiceNameAgentQGISBridge,
			Host:        "127.0.0.1",
			Port:        0,
			Protocol:    "http",
			Status:      "running",
		}); err != nil {
			t.Fatalf("register invalid service port failed: %v", err)
		}

		handler := NewAPIWithServices(nil, nil, nil, registry, &stubRuntimeCoordinator{snapshot: middlewareclient.RuntimeSnapshot{
			Version:   1,
			Timestamp: time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC),
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
		payload, envelope := decodeEnvelopeResponse(t, rr)
		assertEnvelopeResponse(t, payload, envelope, "DISCOVERY_DESCRIPTOR_INVALID", "discovery", "runtime_descriptor")
		if payload["data"] != nil {
			t.Fatalf("expected no fallback descriptor data, got %#v", payload["data"])
		}
	})
}

func TestRuntimeHandlersUsePublicCatalogRevisionsForUiFacingResponses(t *testing.T) {
	publicCatalog := toolcatalog.WithPublicRevisions(toolcatalog.Contract{
		ID:                "tool_catalog/global",
		Version:           toolcatalog.ContractVersion,
		Source:            "go_control_plane",
		CapabilityType:    toolcatalog.CapabilityTypeToolCatalog,
		RuntimeTarget:     "mixed",
		Availability:      toolcatalog.AvailabilityAvailable,
		DegradationStatus: toolcatalog.DegradationStatusNone,
		Policy:            &toolcatalog.Policy{Scope: "global", ConfigName: "default"},
		Items: []toolcatalog.Capability{{
			ID:             "buffer",
			CapabilityType: toolcatalog.CapabilityTypeTool,
			RuntimeTarget:  "qgis",
			Availability:   toolcatalog.AvailabilityAvailable,
		}},
	}, "public-schema-rev", "public-availability-rev")
	handler := NewAPIWithServices(nil, nil, nil, nil, divergentRuntimeCoordinator{
		snapshot: middlewareclient.RuntimeSnapshot{
			Version:                  15,
			Timestamp:                time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC),
			ConfigRevision:           "config-rev-internal",
			DiscoveryRevision:        "discovery-rev-internal",
			RuntimeRevision:          "runtime-rev-public",
			ActiveConfig:             "default",
			RuntimeActiveAgentConfig: "default",
			ServicePhase:             "ready",
			ToolCatalogRevision:      "internal-schema/internal-availability",
			ToolSchemaRevision:       "internal-schema",
			ToolAvailabilityRevision: "internal-availability",
			ToolRevisions: middlewareclient.ToolRevisions{
				Catalog:      "internal-schema/internal-availability",
				Schema:       "internal-schema",
				Availability: "internal-availability",
			},
			Instances: middlewareclient.InstancesSnapshot{
				ActiveID:      "inst-public",
				InstanceCount: 1,
				RunningCount:  1,
			},
		},
		services: control.RuntimeServicesView{
			Services: map[string]middlewareclient.ServiceStatus{
				"agent": {ID: "agent", Status: "running", Enabled: true},
			},
			Instances:                control.RuntimeInstancesView{ActiveID: "inst-public", InstanceCount: 1, RunningCount: 1},
			ConfigRevision:           "config-rev-internal",
			DiscoveryRevision:        "discovery-rev-internal",
			RuntimeRevision:          "runtime-rev-public",
			RuntimeActiveAgentConfig: "default",
			ActiveInstanceID:         "inst-public",
			ToolCatalogRevision:      "internal-schema/internal-availability",
			ToolSchemaRevision:       "internal-schema",
			ToolAvailabilityRevision: "internal-availability",
			ToolRevisions: middlewareclient.ToolRevisions{
				Catalog:      "internal-schema/internal-availability",
				Schema:       "internal-schema",
				Availability: "internal-availability",
			},
			Version:   15,
			Timestamp: "2026-04-09T12:00:00Z",
		},
	}, nil, stubAPIAssetCatalogService{view: control.ToolCatalogView{
		Scope:           "global",
		ConfigName:      "default",
		RuntimeRevision: "runtime-rev-public",
		Catalog:         publicCatalog,
	}}, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	for _, path := range []string{"/api/runtime/snapshot", "/api/runtime/services"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("GET %s status=%d body=%s", path, rr.Code, rr.Body.String())
			}
			resp := decodeAPIResponse(t, rr)
			data, ok := resp.Data.(map[string]any)
			if !ok {
				t.Fatalf("unexpected runtime data type: %T", resp.Data)
			}
			if got := data["tool_schema_revision"]; got != "public-schema-rev" {
				t.Fatalf("expected public tool_schema_revision, got %#v", got)
			}
			if got := data["tool_availability_revision"]; got != "public-availability-rev" {
				t.Fatalf("expected public tool_availability_revision, got %#v", got)
			}
			if got := data["tool_catalog_revision"]; got != "public-schema-rev/public-availability-rev" {
				t.Fatalf("expected public tool_catalog_revision, got %#v", got)
			}
			toolRevisions, ok := data["tool_revisions"].(map[string]any)
			if !ok {
				t.Fatalf("unexpected tool_revisions payload: %#v", data["tool_revisions"])
			}
			if got := toolRevisions["catalog"]; got != "public-schema-rev/public-availability-rev" {
				t.Fatalf("expected public tool_revisions.catalog, got %#v", got)
			}
			if got := toolRevisions["schema"]; got != "public-schema-rev" {
				t.Fatalf("expected public tool_revisions.schema, got %#v", got)
			}
			if got := toolRevisions["availability"]; got != "public-availability-rev" {
				t.Fatalf("expected public tool_revisions.availability, got %#v", got)
			}
		})
	}
}

func TestRuntimeDescriptorUsesPublicCatalogDegradedSemantics(t *testing.T) {
	registry, err := discovery.NewServiceRegistry(filepath.Join(t.TempDir(), "services.db"))
	if err != nil {
		t.Fatalf("new service registry failed: %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	if err := registry.Register(discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        50321,
		Protocol:    "http",
		Status:      "running",
	}); err != nil {
		t.Fatalf("register service port failed: %v", err)
	}

	degradedCatalog := toolcatalog.WithPublicRevisions(toolcatalog.Contract{
		ID:                      "tool_catalog/global",
		Version:                 toolcatalog.ContractVersion,
		Source:                  "go_control_plane",
		CapabilityType:          toolcatalog.CapabilityTypeToolCatalog,
		RuntimeTarget:           "cli",
		Availability:            toolcatalog.AvailabilityDegraded,
		MissingDependencyReason: toolcatalog.MissingReasonProviderMissing,
		DegradationStatus:       toolcatalog.DegradationStatusProviderMissing,
		Policy:                  &toolcatalog.Policy{Scope: "global", ConfigName: "default"},
		Items: []toolcatalog.Capability{{
			ID:                      "output_init_dir",
			CapabilityType:          toolcatalog.CapabilityTypeTool,
			RuntimeTarget:           "cli",
			Availability:            toolcatalog.AvailabilityUnavailable,
			MissingDependencyReason: toolcatalog.MissingReasonProviderMissing,
			DegradationStatus:       toolcatalog.DegradationStatusProviderMissing,
		}},
	}, "public-schema-rev", "public-availability-rev")

	handler := NewAPIWithServices(nil, nil, nil, registry, &stubRuntimeCoordinator{snapshot: middlewareclient.RuntimeSnapshot{
		Version:         21,
		Timestamp:       time.Date(2026, time.April, 9, 15, 0, 0, 0, time.UTC),
		RuntimeRevision: "runtime-rev-21",
		ServicePhase:    "ready",
		ActiveConfig:    "default",
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "qgis-21",
			InstanceCount: 1,
			RunningCount:  1,
		},
	}}, nil, stubAPIAssetCatalogService{view: control.ToolCatalogView{
		Scope:           "global",
		ConfigName:      "default",
		RuntimeRevision: "runtime-rev-21",
		Catalog:         degradedCatalog,
	}}, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/descriptor", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/runtime/descriptor status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected runtime descriptor data type: %T", resp.Data)
	}
	if got := data["readiness"]; got != "degraded" {
		t.Fatalf("expected degraded readiness from public catalog semantics, got %#v", got)
	}
	if got := data["degraded"]; got != true {
		t.Fatalf("expected degraded flag=true, got %#v", got)
	}
	if got := data["reason_code"]; got != "PROVIDER_MISSING" {
		t.Fatalf("expected PROVIDER_MISSING reason_code, got %#v", got)
	}
	if got := data["reason_message"]; got != "Provider 未配置，当前能力已降级。" {
		t.Fatalf("unexpected reason_message: %#v", got)
	}
	retryHints, ok := data["retry_hints"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected retry_hints payload: %#v", data["retry_hints"])
	}
	if got := retryHints["retryable"]; got != true {
		t.Fatalf("expected retryable provider_missing descriptor, got %#v", got)
	}
	if got, ok := retryHints["after_ms"].(float64); !ok || got <= 0 {
		t.Fatalf("expected retryable provider_missing descriptor after_ms > 0, got %#v", retryHints["after_ms"])
	}
}
