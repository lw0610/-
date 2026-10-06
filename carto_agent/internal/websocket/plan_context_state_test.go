package websocket

import (
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/session"
)

func TestEncodeDecodePlanContext(t *testing.T) {
	plan := &session.PlanContext{
		RawOutput: "plan-output",
		Summary:   "plan-summary",
		CreatedAt: time.Now(),
		Status:    session.PlanContextStatusDraft,
		EvalResult: &session.PlanEvalResult{
			Score:    7,
			Approved: true,
		},
	}

	encoded, err := encodePlanContext(plan)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded := decodePlanContext(encoded)
	if decoded == nil {
		t.Fatal("expected decoded plan context")
	}
	if decoded.RawOutput != plan.RawOutput {
		t.Fatalf("expected raw output %q, got %q", plan.RawOutput, decoded.RawOutput)
	}
	if decoded.Status != session.PlanContextStatusDraft {
		t.Fatalf("expected draft status, got %s", decoded.Status)
	}
}
