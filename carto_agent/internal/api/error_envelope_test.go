package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/settings"
)

func decodeEnvelopeResponse(t *testing.T, rr *httptest.ResponseRecorder) (map[string]any, map[string]any) {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	errorPayload, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured error payload, got %#v", payload["error"])
	}
	return payload, errorPayload
}

func assertEnvelopeResponse(t *testing.T, payload map[string]any, envelope map[string]any, expectedCode, expectedOrigin, expectedStage string) {
	t.Helper()

	if success, _ := payload["success"].(bool); success {
		t.Fatalf("expected success=false, got %#v", payload)
	}
	if got, _ := payload["code"].(string); got != expectedCode {
		t.Fatalf("unexpected top-level code: %q", got)
	}
	if got, _ := envelope["code"].(string); got != expectedCode {
		t.Fatalf("unexpected envelope code: %q", got)
	}
	if got, _ := envelope["origin"].(string); got != expectedOrigin {
		t.Fatalf("unexpected envelope origin: %q", got)
	}
	if got, _ := envelope["stage"].(string); got != expectedStage {
		t.Fatalf("unexpected envelope stage: %q", got)
	}
	if got, _ := envelope["correlation_id"].(string); got != "corr-test-1" {
		t.Fatalf("unexpected correlation_id: %q", got)
	}
	if got, _ := envelope["evidence_id"].(string); got != "evid-test-1" {
		t.Fatalf("unexpected evidence_id: %q", got)
	}
	if _, ok := envelope["message"].(string); !ok {
		t.Fatalf("expected message field, got %#v", envelope["message"])
	}
	if _, ok := envelope["retryable"].(bool); !ok {
		t.Fatalf("expected retryable field, got %#v", envelope["retryable"])
	}
	if _, ok := envelope["degraded"].(bool); !ok {
		t.Fatalf("expected degraded field, got %#v", envelope["degraded"])
	}
	if _, ok := envelope["user_action"].(string); !ok {
		t.Fatalf("expected user_action field, got %#v", envelope["user_action"])
	}
	if _, ok := envelope["debug_context"].(map[string]any); !ok {
		t.Fatalf("expected debug_context field, got %#v", envelope["debug_context"])
	}
}

func TestTargetedHandlersReturnStructuredErrorEnvelopesWithRequestIDs(t *testing.T) {
	handler := NewAPIWithServices(nil, nil, nil, nil, nil, nil, nil, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	tests := []struct {
		name         string
		method       string
		path         string
		body         string
		expectedCode string
		expectedHTTP int
		expectedFrom string
		expectedStep string
	}{
		{
			name:         "config path",
			method:       http.MethodPut,
			path:         "/api/config/llm",
			body:         `{"value":{"provider":"demo"}}`,
			expectedCode: "CONFIG_SERVICE_UNAVAILABLE",
			expectedHTTP: http.StatusServiceUnavailable,
			expectedFrom: "config",
			expectedStep: "config_api",
		},
		{
			name:         "catalog path",
			method:       http.MethodGet,
			path:         "/api/tools/catalog",
			expectedCode: "ASSET_CATALOG_UNAVAILABLE",
			expectedHTTP: http.StatusServiceUnavailable,
			expectedFrom: "catalog",
			expectedStep: "catalog_api",
		},
		{
			name:         "discovery path",
			method:       http.MethodGet,
			path:         "/api/services/ports",
			expectedCode: "SERVICE_REGISTRY_UNAVAILABLE",
			expectedHTTP: http.StatusServiceUnavailable,
			expectedFrom: "discovery",
			expectedStep: "discovery_api",
		},
		{
			name:         "instance registration path",
			method:       http.MethodPost,
			path:         "/api/instances/register",
			body:         `{"instance_id":"qgis-a","pid":1001}`,
			expectedCode: "INSTANCE_REGISTRY_UNAVAILABLE",
			expectedHTTP: http.StatusServiceUnavailable,
			expectedFrom: "discovery",
			expectedStep: "instance_registration",
		},
		{
			name:         "runtime snapshot path",
			method:       http.MethodGet,
			path:         "/api/runtime/snapshot",
			expectedCode: "RUNTIME_COORDINATOR_UNAVAILABLE",
			expectedHTTP: http.StatusServiceUnavailable,
			expectedFrom: "discovery",
			expectedStep: "runtime_snapshot",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body *bytes.Buffer
			if tc.body != "" {
				body = bytes.NewBufferString(tc.body)
			} else {
				body = bytes.NewBuffer(nil)
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Correlation-ID", "corr-test-1")
			req.Header.Set("X-Evidence-ID", "evid-test-1")
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != tc.expectedHTTP {
				t.Fatalf("unexpected status=%d body=%s", rr.Code, rr.Body.String())
			}

			payload, envelope := decodeEnvelopeResponse(t, rr)
			assertEnvelopeResponse(t, payload, envelope, tc.expectedCode, tc.expectedFrom, tc.expectedStep)
		})
	}
}

func TestWriteSettingsMutationErrorReturnsStructuredEnvelope(t *testing.T) {
	rr := httptest.NewRecorder()
	err := &control.ActiveRunBlockedError{Mutation: settings.SettingsMutationResult{
		Scope:          "config",
		RequiredAction: "stop_active_runs",
		NextStep:       "请先停止受影响的运行中会话，再重新保存该设置。",
	}}

	req := httptest.NewRequest(http.MethodPut, "/api/config/llm", nil)
	req.Header.Set("X-Correlation-ID", "corr-test-1")
	req.Header.Set("X-Evidence-ID", "evid-test-1")

	if handled := writeSettingsMutationError(rr, req, err); !handled {
		t.Fatal("expected writeSettingsMutationError to handle blocked error")
	}
	if rr.Code != http.StatusConflict {
		t.Fatalf("unexpected status=%d body=%s", rr.Code, rr.Body.String())
	}

	payload, envelope := decodeEnvelopeResponse(t, rr)
	if success, _ := payload["success"].(bool); success {
		t.Fatalf("expected success=false, got %#v", payload)
	}
	if got, _ := payload["code"].(string); got != "ACTIVE_RUN_BLOCKED" {
		t.Fatalf("unexpected top-level code: %q", got)
	}
	if got, _ := envelope["code"].(string); got != "ACTIVE_RUN_BLOCKED" {
		t.Fatalf("unexpected envelope code: %q", got)
	}
	if got, _ := envelope["origin"].(string); got != "config" {
		t.Fatalf("unexpected origin: %q", got)
	}
	if got, _ := envelope["stage"].(string); got != "settings_mutation" {
		t.Fatalf("unexpected stage: %q", got)
	}
	if got, _ := envelope["correlation_id"].(string); got != "corr-test-1" {
		t.Fatalf("unexpected correlation_id: %#v", envelope["correlation_id"])
	}
	if got, _ := envelope["evidence_id"].(string); got != "evid-test-1" {
		t.Fatalf("unexpected evidence_id: %#v", envelope["evidence_id"])
	}
}
