package websocket

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/agentruntime"
	"github.com/autocarto/carto_agent/internal/config"
)

type initializationTestBuilder struct {
	runtime *agentruntime.Runtime
	err     error
}

func (b *initializationTestBuilder) Build(context.Context, string, *config.AgentConfigFile) (*agentruntime.Runtime, error) {
	return b.runtime, b.err
}

func (b *initializationTestBuilder) BuildActive(context.Context, *config.AgentConfigManager) (*agentruntime.Runtime, error) {
	return b.runtime, b.err
}

func runtimeInitializationFixture() (*DeepServer, *initializationTestBuilder) {
	s := NewDeepServer(0, nil)
	s.agentRebuildConfig = &AgentRebuildConfig{ChatModel: &fakeContextCompressionModel{}}
	s.agentConfigManager = &config.AgentConfigManager{}
	builder := &initializationTestBuilder{err: errors.New(`resolve middleware packages for cartographer: middleware package "route-cartography" not found in governed registry`)}
	s.SetRuntimeFactory(builder)
	return s, builder
}

func TestRuntimeStartupFailurePreservedUntilRuntimeInstalled(t *testing.T) {
	s := NewDeepServer(0, nil)
	err := errors.New(`创建 AgentRuntime 失败: middleware package "route-cartography" not found in governed registry`)
	s.SetRuntimeBuildError(err)
	s.SetRuntime(nil)
	for _, mode := range []string{"Build", "Plan"} {
		message := s.runtimeUnavailableMessage(mode)
		if !strings.HasPrefix(message, mode+" 模式不可用：") || !strings.Contains(message, err.Error()) {
			t.Fatalf("startup failure should be visible in %s mode, got %q", mode, message)
		}
	}
	s.SetRuntime(&agentruntime.Runtime{BuildAgent: &testExecutionModeAgent{name: "builder"}})
	if message := s.runtimeUnavailableMessage("Build"); message != "" {
		t.Fatalf("installing a runtime must clear startup failure, got %q", message)
	}
}

func TestRuntimeInitializationFailureReachesChatAndSession(t *testing.T) {
	s, _ := runtimeInitializationFixture()
	err := s.rebuildAgent()
	if err == nil {
		t.Fatal("expected missing package to prevent runtime initialization")
	}
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()
	const sessionID = "runtime-initialization-failure"
	if agent := s.getModeAgentWithWarning(serverConn, sessionID, executionModeBuild); agent != nil {
		t.Fatal("expected unavailable agent")
	}
	messages := readMessagesUntilTimeout(t, clientConn)
	errorsSeen := 0
	for _, message := range messages {
		if message.Type != MsgTypeError {
			continue
		}
		errorsSeen++
		if !strings.Contains(message.Content, err.Error()) {
			t.Fatalf("chat should show the runtime initialization cause, got %q", message.Content)
		}
		if code, _ := message.Data["code"].(string); code != ErrorCodeAgentUnavailable {
			t.Fatalf("unexpected error code %q", code)
		}
	}
	if errorsSeen != 1 {
		t.Fatalf("expected exactly one chat error, got %d", errorsSeen)
	}
	state := s.sessionRuntimeProjectionSnapshot(sessionID, nil).RuntimeState
	if state.FailureCode != ErrorCodeAgentUnavailable || !strings.Contains(state.FailureReason, err.Error()) {
		t.Fatalf("session should retain the initialization cause, got %+v", state)
	}
}

func TestRuntimeInitializationFailureClearedAfterSuccessfulRebuild(t *testing.T) {
	s, builder := runtimeInitializationFixture()
	if err := s.rebuildAgent(); err == nil {
		t.Fatal("expected initial runtime failure")
	}
	if message := s.runtimeUnavailableMessage("Build"); !strings.Contains(message, "route-cartography") {
		t.Fatalf("expected retained failure before recovery, got %q", message)
	}
	agent := &testExecutionModeAgent{name: "recovered-builder"}
	builder.runtime = &agentruntime.Runtime{BuildAgent: agent}
	builder.err = nil
	if err := s.rebuildAgent(); err != nil {
		t.Fatalf("rebuild should recover: %v", err)
	}
	if message := s.runtimeUnavailableMessage("Build"); message != "" {
		t.Fatalf("successful rebuild must clear old failure, got %q", message)
	}
	if got := s.GetAgent(executionModeBuild); got != agent {
		t.Fatal("expected recovered runtime agent")
	}
}

func TestRuntimeRebuildFailurePreservesWorkingAgent(t *testing.T) {
	s, _ := runtimeInitializationFixture()
	agent := &testExecutionModeAgent{name: "existing-builder"}
	s.SetRuntime(&agentruntime.Runtime{BuildAgent: agent})
	if err := s.rebuildAgent(); err == nil {
		t.Fatal("expected attempted rebuild to fail")
	}
	if got := s.getModeAgentWithWarning(nil, "working-runtime", executionModeBuild); got != agent {
		t.Fatal("failed rebuild must preserve the working runtime")
	}
	if issues := s.GetRuntimeModeIssues(); len(issues) != 0 {
		t.Fatalf("failed rebuild must not mark the working runtime unavailable: %+v", issues)
	}
}
