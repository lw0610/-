package plan_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/autocarto/carto_agent/internal/capability"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
)

func TestPlanValidationErrorSurvivesToolErrorMapping(t *testing.T) {
	err := plan.NewValidationError([]flow.Diagnostic{{Level: "error", Code: "PLAN_CAPABILITY_UNKNOWN", Step: "join-data", Message: "use the current package ID"}}, plan.Environment{Capabilities: map[string]bool{"custom-fields": true}})
	result, mapErr := (capability.DefaultErrorMapper{}).MapToolError(context.Background(), "plan_propose", fmt.Errorf("tool wrapper: %w", err))
	if mapErr != nil || len(result.Parts) != 1 {
		t.Fatalf("tool error mapping failed: %v", mapErr)
	}
	var payload struct {
		Code                string `json:"code"`
		Retryable           bool   `json:"retryable"`
		ProviderDiagnostics struct {
			NextAction          string            `json:"next_action"`
			AllowedCapabilities []string          `json:"allowed_capabilities"`
			Diagnostics         []flow.Diagnostic `json:"diagnostics"`
		} `json:"provider_diagnostics"`
	}
	if err := json.Unmarshal([]byte(result.Parts[0].Text), &payload); err != nil {
		t.Fatal(err)
	}
	details := payload.ProviderDiagnostics
	if payload.Code != "PLAN_VALIDATION_FAILED" || !payload.Retryable || details.NextAction != "correct_plan" || len(details.Diagnostics) != 1 || details.Diagnostics[0].Step != "join-data" || len(details.AllowedCapabilities) != 1 || details.AllowedCapabilities[0] != "custom-fields" {
		t.Fatalf("model received an opaque or incomplete error: %+v", payload)
	}
}
