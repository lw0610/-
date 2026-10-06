package config

import "testing"

func TestSessionModeAliases(t *testing.T) {
	if got := NormalizeSessionExecutionMode("execute"); got != "build" {
		t.Fatalf("expected execute -> build, got %q", got)
	}
	if got := NormalizeSessionExecutionMode("plan_only"); got != "plan" {
		t.Fatalf("expected plan_only -> plan, got %q", got)
	}
	if got := NormalizeSessionRunIntent("build"); got != "execute" {
		t.Fatalf("expected build -> execute, got %q", got)
	}
	if got := NormalizeSessionRunIntent("plan"); got != "plan_only" {
		t.Fatalf("expected plan -> plan_only, got %q", got)
	}
	if got := ExecutionModeToRunIntent("build"); got != "execute" {
		t.Fatalf("expected build -> execute, got %q", got)
	}
	if got := RunIntentToExecutionMode("execute"); got != "build" {
		t.Fatalf("expected execute -> build, got %q", got)
	}
	if got := NormalizeSessionExecutionMode("auto"); got != "" {
		t.Fatalf("expected auto execution mode to be rejected, got %q", got)
	}
	if got := NormalizeSessionRunIntent("auto"); got != "" {
		t.Fatalf("expected auto run_intent to be rejected, got %q", got)
	}
}
