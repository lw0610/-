package websocket

import (
	"errors"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/agentruntime"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
)

func enablePlanForReadinessTest(t *testing.T, s *DeepServer) {
	t.Helper()
	s.agentConfigManager = config.NewAgentConfigManager(t.TempDir())
	if !s.planEnabledForSnapshot() {
		t.Fatal("expected default test configuration to enable Plan")
	}
}

func TestRuntimeSnapshotUnavailableWithoutAgent(t *testing.T) {
	for _, cause := range []string{"", `middleware package "route-cartography" not found in governed registry`} {
		t.Run(cause, func(t *testing.T) {
			s := NewDeepServer(0, nil)
			enablePlanForReadinessTest(t, s)
			if cause != "" {
				s.SetRuntimeBuildError(errors.New(cause))
			}
			// A previously cached success must not override the live agent state.
			s.runtimeCache.ApplySnapshot(middlewareclient.RuntimeSnapshot{
				Version: 1, ServicePhase: "ready", PlanReady: true,
				ModeReadiness: map[string]middlewareclient.ModeReadiness{
					"build": {Enabled: true, Ready: true},
					"plan":  {Enabled: true, Ready: true},
				},
			})
			snapshot := s.GetRuntimeSnapshot()
			for _, mode := range []string{"build", "plan"} {
				readiness := snapshot.ModeReadiness[mode]
				issue := snapshot.ModeIssues[mode]
				if !readiness.Enabled || readiness.Ready || readiness.Code != ErrorCodeAgentUnavailable {
					t.Fatalf("%s should report unavailable without an agent, got %+v", mode, readiness)
				}
				if issue.Code != ErrorCodeAgentUnavailable || issue.Message == "" || !strings.Contains(issue.Message, cause) || readiness.Message != issue.Message {
					t.Fatalf("%s should expose the initialization cause consistently, readiness=%+v issue=%+v", mode, readiness, issue)
				}
			}
			if snapshot.PlanReady || snapshot.ServicePhase != "degraded" {
				t.Fatalf("failed initialization must not report healthy: %+v", snapshot)
			}
		})
	}
}

func TestRuntimeSnapshotReadinessRecoversWithFallbackAgent(t *testing.T) {
	s := NewDeepServer(0, nil)
	enablePlanForReadinessTest(t, s)
	s.SetRuntimeBuildError(errors.New("startup failed"))
	s.runtimeCache.ApplySnapshot(s.GetRuntimeSnapshot())
	s.SetRuntime(&agentruntime.Runtime{BuildAgent: &testExecutionModeAgent{name: "recovered"}})
	snapshot := s.GetRuntimeSnapshot()
	for _, mode := range []string{"build", "plan"} {
		if readiness := snapshot.ModeReadiness[mode]; !readiness.Enabled || !readiness.Ready || readiness.Code != "" {
			t.Fatalf("%s should recover using the existing fallback semantics, got %+v", mode, readiness)
		}
	}
	if !snapshot.PlanReady || len(snapshot.ModeIssues) != 0 || snapshot.ServicePhase != "ready" {
		t.Fatalf("successful initialization must clear cached failure: %+v", snapshot)
	}
}

func TestRuntimeSnapshotRebuildFailureKeepsUsableAgentReady(t *testing.T) {
	s, _ := runtimeInitializationFixture()
	enablePlanForReadinessTest(t, s)
	s.SetRuntime(&agentruntime.Runtime{BuildAgent: &testExecutionModeAgent{name: "existing"}})
	if err := s.rebuildAgent(); err == nil {
		t.Fatal("expected attempted rebuild to fail")
	}
	snapshot := s.GetRuntimeSnapshot()
	if !snapshot.ModeReadiness["build"].Ready || !snapshot.ModeReadiness["plan"].Ready || len(snapshot.ModeIssues) != 0 {
		t.Fatalf("failed rebuild must retain readiness of the usable runtime: %+v", snapshot)
	}
}

func TestRuntimeSnapshotDisabledPlanStaysDisabled(t *testing.T) {
	s := NewDeepServer(0, &testExecutionModeAgent{name: "builder"})
	snapshot := s.GetRuntimeSnapshot()
	if !snapshot.ModeReadiness["build"].Ready {
		t.Fatal("Build should be ready with a constructor-supplied agent")
	}
	if readiness := snapshot.ModeReadiness["plan"]; readiness.Enabled || readiness.Ready || snapshot.PlanReady {
		t.Fatalf("a fallback agent must not enable disabled Plan: %+v", readiness)
	}
	if _, ok := snapshot.ModeIssues["plan"]; ok {
		t.Fatal("disabled Plan must not produce a missing-agent issue")
	}
}

func TestRuntimeSnapshotExplicitModeIssueOverridesAgent(t *testing.T) {
	s := NewDeepServer(0, nil)
	enablePlanForReadinessTest(t, s)
	s.SetRuntime(&agentruntime.Runtime{
		BuildAgent: &testExecutionModeAgent{name: "builder"},
		PlanAgent:  &testExecutionModeAgent{name: "planner"},
		ModeIssues: map[string]agentruntime.ModeIssue{"plan": {Code: "PLANNER_BLOCKED", Message: "planner blocked"}},
	})
	snapshot := s.GetRuntimeSnapshot()
	if !snapshot.ModeReadiness["build"].Ready || snapshot.ModeReadiness["plan"].Ready || snapshot.ModeReadiness["plan"].Code != "PLANNER_BLOCKED" {
		t.Fatalf("an explicit mode issue must take precedence over agent existence: %+v", snapshot.ModeReadiness)
	}
}
