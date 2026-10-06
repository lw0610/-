package control

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/autocarto/carto_agent/internal/toolcatalog"
)

func decodeControlEnvelope(t *testing.T, err error) map[string]any {
	t.Helper()

	marshaler, ok := err.(json.Marshaler)
	if !ok {
		t.Fatalf("expected json.Marshaler error, got %T", err)
	}
	raw, marshalErr := marshaler.MarshalJSON()
	if marshalErr != nil {
		t.Fatalf("MarshalJSON failed: %v", marshalErr)
	}
	var payload map[string]any
	if unmarshalErr := json.Unmarshal(raw, &payload); unmarshalErr != nil {
		t.Fatalf("Unmarshal envelope failed: %v", unmarshalErr)
	}
	return payload
}

func assertControlEnvelopeFields(t *testing.T, payload map[string]any, expectedCode, expectedOrigin, expectedStage string) {
	t.Helper()

	if got, _ := payload["code"].(string); got != expectedCode {
		t.Fatalf("unexpected code: %q", got)
	}
	if got, _ := payload["origin"].(string); got != expectedOrigin {
		t.Fatalf("unexpected origin: %q", got)
	}
	if got, _ := payload["stage"].(string); got != expectedStage {
		t.Fatalf("unexpected stage: %q", got)
	}
	if _, ok := payload["message"].(string); !ok {
		t.Fatalf("expected message field, got %#v", payload["message"])
	}
	if _, ok := payload["retryable"].(bool); !ok {
		t.Fatalf("expected retryable field, got %#v", payload["retryable"])
	}
	if _, ok := payload["degraded"].(bool); !ok {
		t.Fatalf("expected degraded field, got %#v", payload["degraded"])
	}
	if _, ok := payload["user_action"].(string); !ok {
		t.Fatalf("expected user_action field, got %#v", payload["user_action"])
	}
	if _, ok := payload["debug_context"].(map[string]any); !ok {
		t.Fatalf("expected debug_context field, got %#v", payload["debug_context"])
	}
	if got, _ := payload["correlation_id"].(string); got != "" {
		t.Fatalf("expected empty correlation_id before request propagation, got %q", got)
	}
	if got, _ := payload["evidence_id"].(string); got != "" {
		t.Fatalf("expected empty evidence_id before request propagation, got %q", got)
	}
}

func TestSettingsMutationServiceConfigUnavailableErrorMarshalsAsEnvelope(t *testing.T) {
	service := NewSettingsMutationService(SettingsMutationServiceDeps{})

	_, err := service.GetAllConfig()
	if err == nil {
		t.Fatal("expected GetAllConfig error")
	}

	payload := decodeControlEnvelope(t, err)
	assertControlEnvelopeFields(t, payload, "CONFIG_SERVICE_UNAVAILABLE", "config", "settings_mutation")
}

func TestAssetCatalogServiceToolCatalogUnavailableErrorMarshalsAsEnvelope(t *testing.T) {
	service := NewAssetCatalogService(AssetCatalogServiceDeps{})

	_, err := service.GetToolCatalog(context.Background(), "global")
	if err == nil {
		t.Fatal("expected GetToolCatalog error")
	}

	payload := decodeControlEnvelope(t, err)
	assertControlEnvelopeFields(t, payload, "AGENT_CONFIG_UNAVAILABLE", "config", "asset_catalog")
}

func TestSharedErrorDictionary(t *testing.T) {
	envelope := NewErrorEnvelope("provider_missing", "Provider 未配置，当前能力已降级。", "catalog", "tool_catalog")
	envelope.UserAction = "请先配置至少一个 Provider 后重试。"
	envelope.Retryable = true
	envelope = envelope.WithIdentifiers("corr-shared", "EVIDENCE-shared")

	dictionary := SharedErrorDictionaryFromErrorEnvelope(envelope)
	if dictionary.Code != "provider_missing" {
		t.Fatalf("expected code provider_missing, got %q", dictionary.Code)
	}
	if dictionary.Message != "Provider 未配置，当前能力已降级。" {
		t.Fatalf("unexpected message: %q", dictionary.Message)
	}
	if dictionary.UserAction != "请先配置至少一个 Provider 后重试。" {
		t.Fatalf("unexpected user_action: %q", dictionary.UserAction)
	}
	if !dictionary.Retryable {
		t.Fatal("expected provider_missing dictionary to be retryable")
	}
	if dictionary.EvidenceID != "EVIDENCE-shared" {
		t.Fatalf("unexpected evidence id: %q", dictionary.EvidenceID)
	}
}

func TestRetryableFlagConsistency(t *testing.T) {
	tests := []struct {
		name string
		code string
		want bool
	}{
		{name: "provider missing", code: "provider_missing", want: true},
		{name: "unsupported runtime", code: "unsupported_runtime", want: false},
		{name: "abnormal empty", code: toolcatalog.MissingReasonAbnormalEmptyCatalog, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RetryableForSharedErrorCode(tt.code); got != tt.want {
				t.Fatalf("RetryableForSharedErrorCode(%q)=%v, want %v", tt.code, got, tt.want)
			}
		})
	}

	providerMissing := SharedErrorDictionaryFromRegistryAvailability(RegistryAvailability{
		Status:  "degraded",
		Code:    "provider_missing",
		Message: "registry provider missing for runtimes: cli",
	}, "")
	if !providerMissing.Retryable {
		t.Fatal("expected provider_missing registry dictionary retryable=true")
	}

	unsupportedRuntime := SharedErrorDictionaryFromToolCatalogContract(toolcatalog.Contract{
		Availability:            toolcatalog.AvailabilityUnsupported,
		MissingDependencyReason: toolcatalog.MissingReasonUnsupportedRuntime,
		DegradationStatus:       toolcatalog.DegradationStatusUnsupportedRuntime,
	}, "")
	if unsupportedRuntime.Retryable {
		t.Fatal("expected unsupported_runtime catalog dictionary retryable=false")
	}
}

func TestEvidenceIDPropagation(t *testing.T) {
	envelope := NewErrorEnvelope("provider_missing", "Provider 未配置，当前能力已降级。", "catalog", "tool_catalog").WithIdentifiers("corr-evidence", "EVIDENCE-envelope")
	if got := SharedErrorDictionaryFromErrorEnvelope(envelope).EvidenceID; got != "EVIDENCE-envelope" {
		t.Fatalf("expected envelope evidence id to propagate, got %q", got)
	}

	registryDictionary := SharedErrorDictionaryFromRegistryAvailability(RegistryAvailability{
		Status:  "degraded",
		Code:    "provider_missing",
		Message: "registry provider missing for runtimes: cli",
	}, "EVIDENCE-registry")
	if registryDictionary.EvidenceID != "EVIDENCE-registry" {
		t.Fatalf("expected registry evidence id to propagate, got %q", registryDictionary.EvidenceID)
	}

	catalogDictionary := SharedErrorDictionaryFromToolCatalogContract(toolcatalog.Contract{
		Availability:            toolcatalog.AvailabilityUnavailable,
		MissingDependencyReason: toolcatalog.MissingReasonAbnormalEmptyCatalog,
		DegradationStatus:       toolcatalog.DegradationStatusAbnormalEmpty,
	}, "EVIDENCE-catalog")
	if catalogDictionary.EvidenceID != "EVIDENCE-catalog" {
		t.Fatalf("expected catalog evidence id to propagate, got %q", catalogDictionary.EvidenceID)
	}
}

func TestRegistryAvailabilityProjectionUsesSharedDictionarySeeds(t *testing.T) {
	t.Parallel()

	dictionary := SharedErrorDictionaryFromRegistryAvailability(RegistryAvailability{
		Status:  "degraded",
		Code:    "provider_missing",
		Message: "",
	}, "EVIDENCE-governance")
	if dictionary.Code != "provider_missing" {
		t.Fatalf("unexpected code: %#v", dictionary)
	}
	if dictionary.Message != "Provider 未配置，当前能力已降级。" {
		t.Fatalf("unexpected message: %#v", dictionary)
	}
	if dictionary.UserAction != "请先配置至少一个 Provider 后重试。" {
		t.Fatalf("unexpected user action: %#v", dictionary)
	}
	if !dictionary.Retryable {
		t.Fatalf("expected registry projection to preserve retryable seed: %#v", dictionary)
	}
	if dictionary.EvidenceID != "EVIDENCE-governance" {
		t.Fatalf("unexpected evidence id: %#v", dictionary)
	}
}
