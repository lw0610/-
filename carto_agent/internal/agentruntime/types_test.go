package agentruntime

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
)

type stubAgent struct {
	name string
}

func (s *stubAgent) Name(context.Context) string { return s.name }

func (s *stubAgent) Description(context.Context) string { return s.name }

func (s *stubAgent) Run(context.Context, *adk.AgentInput, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Close()
	return iter
}

func TestRuntimeResolveAgent(t *testing.T) {
	rt := &Runtime{
		PlanAgent:  &stubAgent{name: "planner"},
		BuildAgent: &stubAgent{name: "builder"},
	}

	agent, fallback := rt.ResolveAgent("plan")
	if fallback {
		t.Fatalf("plan mode should not fallback")
	}
	if got := agent.Name(context.Background()); got != "planner" {
		t.Fatalf("unexpected plan agent: %s", got)
	}

	agent, fallback = rt.ResolveAgent("build")
	if fallback {
		t.Fatalf("build mode should not fallback")
	}
	if got := agent.Name(context.Background()); got != "builder" {
		t.Fatalf("unexpected build agent: %s", got)
	}
}

func TestRuntimeResolveAgentFallback(t *testing.T) {
	rt := &Runtime{
		BuildAgent: &stubAgent{name: "builder"},
	}

	agent, fallback := rt.ResolveAgent("plan")
	if !fallback {
		t.Fatalf("plan mode should fallback to build agent")
	}
	if got := agent.Name(context.Background()); got != "builder" {
		t.Fatalf("unexpected fallback agent: %s", got)
	}
}

func TestRuntimeModeIssue(t *testing.T) {
	rt := &Runtime{
		ModeIssues: map[string]ModeIssue{
			"plan": {Code: "PLAN_PROTOCOL_VIOLATION", Message: "plan unavailable"},
		},
	}

	issue, ok := rt.ModeIssue("plan")
	if !ok {
		t.Fatal("expected mode issue for plan")
	}
	if issue.Code != "PLAN_PROTOCOL_VIOLATION" {
		t.Fatalf("unexpected issue code: %s", issue.Code)
	}
	if issue.Message != "plan unavailable" {
		t.Fatalf("unexpected issue message: %s", issue.Message)
	}
}
