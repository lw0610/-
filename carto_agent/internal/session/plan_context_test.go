package session

import (
	"testing"
	"time"
)

func TestSessionPlanContextCloneIsolation(t *testing.T) {
	s := NewSession("s1", 10)
	original := &PlanContext{
		RawOutput: "plan",
		Summary:   "summary",
		CreatedAt: time.Now(),
		Status:    PlanContextStatusDraft,
		StructuredPlan: &StructuredPlan{
			Goal: "goal",
			Steps: []PlanStep{
				{Index: 1, Description: "step1", Status: "pending"},
			},
		},
	}

	s.SetPlanContext(original)
	got := s.GetPlanContext()
	if got == nil {
		t.Fatal("expected plan context")
	}

	got.RawOutput = "changed"
	got.StructuredPlan.Steps[0].Description = "changed-step"

	stored := s.GetPlanContext()
	if stored.RawOutput != "plan" {
		t.Fatalf("expected stored raw output unchanged, got %q", stored.RawOutput)
	}
	if stored.StructuredPlan.Steps[0].Description != "step1" {
		t.Fatalf("expected stored step unchanged, got %q", stored.StructuredPlan.Steps[0].Description)
	}
}

func TestSessionClearPlanContext(t *testing.T) {
	s := NewSession("s1", 10)
	s.SetPlanContext(&PlanContext{RawOutput: "plan", Status: PlanContextStatusDraft})

	s.ClearPlanContext()
	if s.GetPlanContext() != nil {
		t.Fatal("expected plan context to be cleared")
	}
}
