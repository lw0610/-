package websocket

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/agentruntime"
	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/security"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/gorilla/websocket"
)

func TestParseExecutionModeDefaultsToBuild(t *testing.T) {
	if got, err := parseExecutionModeFromMessageData(nil); err != nil || got != executionModeBuild {
		t.Fatalf("expected build when data is nil, got mode=%s err=%v", got, err)
	}

	if got, err := parseExecutionModeFromMessageData(map[string]any{}); err != nil || got != executionModeBuild {
		t.Fatalf("expected build when key is missing, got mode=%s err=%v", got, err)
	}
}

func TestParseExecutionModeRejectsInvalidValues(t *testing.T) {
	if _, err := parseExecutionModeFromMessageData(map[string]any{"execution_mode": "unknown"}); err == nil {
		t.Fatal("expected error for unknown execution mode")
	}

	if _, err := parseExecutionModeFromMessageData(map[string]any{"execution_mode": "auto"}); err == nil {
		t.Fatal("expected error for deprecated auto execution mode")
	}

	if _, err := parseExecutionModeFromMessageData(map[string]any{"run_intent": "auto"}); err == nil {
		t.Fatal("expected error for deprecated auto run_intent")
	}
}

func TestParseExecutionModeParsesValues(t *testing.T) {
	if got, err := parseExecutionModeFromMessageData(map[string]any{"execution_mode": "build"}); err != nil || got != executionModeBuild {
		t.Fatalf("expected build, got mode=%s err=%v", got, err)
	}

	if got, err := parseExecutionModeFromMessageData(map[string]any{"execution_mode": "plan"}); err != nil || got != executionModePlan {
		t.Fatalf("expected plan, got mode=%s err=%v", got, err)
	}

	if got, err := parseExecutionModeFromMessageData(map[string]any{"run_intent": "execute"}); err != nil || got != executionModeBuild {
		t.Fatalf("expected build from run_intent, got mode=%s err=%v", got, err)
	}
}

func TestInjectRuntimeExecutionModePrependsSystemMessage(t *testing.T) {
	setPlanRuntimeExecutionModeInstruction("")
	defer setPlanRuntimeExecutionModeInstruction("")

	origin := []*schema.Message{
		schema.UserMessage("hello"),
		schema.AssistantMessage("world", nil),
	}

	withPlan := injectRuntimeExecutionMode(origin, executionModePlan, false)
	if len(withPlan) != len(origin)+1 {
		t.Fatalf("expected length %d, got %d", len(origin)+1, len(withPlan))
	}
	if withPlan[0].Role != schema.System {
		t.Fatalf("expected first message role=system, got %v", withPlan[0].Role)
	}
	if !strings.Contains(withPlan[0].Content, "运行时执行模式（Plan）") {
		t.Fatalf("expected plan runtime instruction, got %q", withPlan[0].Content)
	}

	withBuildThinking := injectRuntimeExecutionMode(origin, executionModeBuild, true)
	if !strings.Contains(withBuildThinking[0].Content, "运行时执行模式（Build）") {
		t.Fatalf("expected build runtime instruction, got %q", withBuildThinking[0].Content)
	}
	if !strings.Contains(withBuildThinking[0].Content, "Think 模式已开启") {
		t.Fatalf("expected thinking runtime instruction, got %q", withBuildThinking[0].Content)
	}
}

func TestSetPlanRuntimeExecutionModeInstruction(t *testing.T) {
	origin := []*schema.Message{schema.UserMessage("hello")}

	setPlanRuntimeExecutionModeInstruction("custom-plan-instruction")
	defer setPlanRuntimeExecutionModeInstruction("")

	withPlan := injectRuntimeExecutionMode(origin, executionModePlan, false)
	if !strings.Contains(withPlan[0].Content, "custom-plan-instruction") {
		t.Fatalf("expected customized plan runtime instruction, got %q", withPlan[0].Content)
	}
}

func TestProcessTaskPlanModeUnavailableSendsSingleCodedError(t *testing.T) {
	server := NewDeepServer(0, nil)
	seedTestRunningInstance(server)
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()

	server.processTask(context.Background(), serverConn, Message{
		Type:      "task",
		SessionID: "session-plan-unavailable",
		Content:   "请给我一个计划",
		Data: map[string]interface{}{
			"execution_mode": "plan",
		},
	})

	messages := readMessagesUntilTimeout(t, clientConn)
	errorMessages := make([]Message, 0, 2)
	for _, item := range messages {
		if item.Type == MsgTypeError {
			errorMessages = append(errorMessages, item)
		}
	}
	if len(errorMessages) != 1 {
		t.Fatalf("expected exactly one error message, got=%d all=%+v", len(errorMessages), messages)
	}
	errMsg := errorMessages[0]
	if strings.Contains(strings.ToLower(errMsg.Content), "api key") {
		t.Fatalf("unexpected API key hint in error content: %s", errMsg.Content)
	}
	if errMsg.Data == nil {
		t.Fatalf("expected error data with code, got nil")
	}
	code, _ := errMsg.Data["code"].(string)
	if code != ErrorCodeAgentUnavailable {
		t.Fatalf("unexpected error code: %q", code)
	}
}

func TestProcessTaskBuildModeUnavailableIncludesLLMHintWhenChatModelMissing(t *testing.T) {
	server := NewDeepServer(0, nil)
	seedTestRunningInstance(server)
	server.SetAgentRebuildConfig(&AgentRebuildConfig{})
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()

	server.processTask(context.Background(), serverConn, Message{
		Type:      "task",
		SessionID: "session-build-llm-missing",
		Content:   "请帮我生成专题图",
		Data: map[string]interface{}{
			"execution_mode": "build",
		},
	})

	messages := readMessagesUntilTimeout(t, clientConn)
	errorMessages := make([]Message, 0, 2)
	for _, item := range messages {
		if item.Type == MsgTypeError {
			errorMessages = append(errorMessages, item)
		}
	}
	if len(errorMessages) != 1 {
		t.Fatalf("expected exactly one error message, got=%d all=%+v", len(errorMessages), messages)
	}

	errMsg := errorMessages[0]
	if !strings.Contains(errMsg.Content, "LLM") {
		t.Fatalf("expected LLM hint in error content, got %q", errMsg.Content)
	}
	code, _ := errMsg.Data["code"].(string)
	if code != ErrorCodeAgentUnavailable {
		t.Fatalf("unexpected error code: %q", code)
	}
}

func TestProcessTaskPlanModeProtocolIssueDoesNotFallbackToBuild(t *testing.T) {
	server := NewDeepServer(0, nil)
	seedTestRunningInstance(server)
	server.SetRuntime(&agentruntime.Runtime{
		BuildAgent: &testExecutionModeAgent{name: "builder"},
		ModeIssues: map[string]agentruntime.ModeIssue{
			"plan": {
				Code:    ErrorCodePlanProtocolViolation,
				Message: "Plan 模式不可用：planner protocol mismatch",
			},
		},
	})

	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()

	server.processTask(context.Background(), serverConn, Message{
		Type:      "task",
		SessionID: "session-plan-protocol-issue",
		Content:   "你好",
		Data: map[string]interface{}{
			"execution_mode": "plan",
		},
	})

	messages := readMessagesUntilTimeout(t, clientConn)
	errorMessages := make([]Message, 0, 1)
	warningMessages := make([]Message, 0, 1)
	for _, item := range messages {
		if item.Type == MsgTypeError {
			errorMessages = append(errorMessages, item)
		}
		if item.Type == MsgTypeStatus {
			if status, _ := item.Data["status"].(string); status == "warning" {
				warningMessages = append(warningMessages, item)
			}
		}
	}
	if len(errorMessages) != 1 {
		t.Fatalf("expected exactly one error message, got=%d all=%+v", len(errorMessages), messages)
	}
	if len(warningMessages) != 0 {
		t.Fatalf("expected no fallback warning when plan protocol is broken, got=%+v", warningMessages)
	}
	code, _ := errorMessages[0].Data["code"].(string)
	if code != ErrorCodePlanProtocolViolation {
		t.Fatalf("unexpected error code: %q", code)
	}
}

func TestProcessTaskRejectsDeprecatedAutoMode(t *testing.T) {
	server := NewDeepServer(0, nil)
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()

	server.processTask(context.Background(), serverConn, Message{
		Type:      "task",
		SessionID: "session-auto-removed",
		Content:   "你好",
		Data: map[string]interface{}{
			"execution_mode": "auto",
		},
	})

	messages := readMessagesUntilTimeout(t, clientConn)
	errorMessages := make([]Message, 0, 1)
	for _, item := range messages {
		if item.Type == MsgTypeError {
			errorMessages = append(errorMessages, item)
		}
	}
	if len(errorMessages) != 1 {
		t.Fatalf("expected exactly one error message, got=%d all=%+v", len(errorMessages), messages)
	}
	code, _ := errorMessages[0].Data["code"].(string)
	if code != ErrorCodeInvalidExecutionMode {
		t.Fatalf("unexpected error code: %q", code)
	}
	if !strings.Contains(errorMessages[0].Content, "auto") {
		t.Fatalf("expected deprecated auto message, got %q", errorMessages[0].Content)
	}
}

func TestProcessTaskCancelsRuntimeStateWhenContextCanceled(t *testing.T) {
	server := NewDeepServer(0, nil)
	seedTestRunningInstance(server)
	server.SetRuntime(&agentruntime.Runtime{
		BuildAgent: &blockingExecutionModeAgent{name: "builder"},
	})

	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.processTask(ctx, serverConn, Message{
			Type:      "task",
			SessionID: "session-build-cancelled",
			Content:   "执行任务",
			Data: map[string]interface{}{
				"execution_mode": "build",
			},
		})
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("processTask did not return after context cancellation")
	}

	messages := readMessagesUntilTimeout(t, clientConn)
	projection := server.sessionRuntimeProjectionSnapshot("session-build-cancelled", nil)
	if projection.RuntimeState.Status != sessionRuntimeStatusCancelled {
		t.Fatalf("expected cancelled runtime projection, got %+v with messages %+v", projection.RuntimeState, messages)
	}
	if _, ok := server.activeSessionRuntime("session-build-cancelled"); ok {
		t.Fatal("expected active runtime state to be cleared after cancellation")
	}
}

func seedTestRunningInstance(server *DeepServer) {
	if server == nil {
		return
	}
	server.PrimeRuntimeState(middlewareclient.RuntimeSnapshot{
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "inst-test",
			InstanceCount: 1,
			RunningCount:  1,
			Items: []*discovery.QGISInstance{{
				ID:          "inst-test",
				ProjectPath: "/tmp/test.qgz",
				Status:      "running",
			}},
		},
	}, nil)
}

type testExecutionModeAgent struct {
	name string
}

func (a *testExecutionModeAgent) Name(context.Context) string { return a.name }

func (a *testExecutionModeAgent) Description(context.Context) string { return a.name }

func (a *testExecutionModeAgent) Run(context.Context, *adk.AgentInput, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Close()
	return iter
}

type blockingExecutionModeAgent struct {
	name string
}

func (a *blockingExecutionModeAgent) Name(context.Context) string { return a.name }

func (a *blockingExecutionModeAgent) Description(context.Context) string { return a.name }

func (a *blockingExecutionModeAgent) Run(ctx context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		<-ctx.Done()
		gen.Close()
	}()
	return iter
}

func newTestWebSocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn, func()) {
	t.Helper()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return security.IsAllowedLocalWebSocketRequest(r) },
	}
	serverConnCh := make(chan *websocket.Conn, 1)
	serveErrCh := make(chan error, 1)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			serveErrCh <- err
			return
		}
		serverConnCh <- conn
	}))

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	headers := http.Header{}
	headers.Set("Origin", "http://localhost:3000")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		httpServer.Close()
		t.Fatalf("dial websocket failed: %v", err)
	}

	var serverConn *websocket.Conn
	select {
	case err := <-serveErrCh:
		clientConn.Close()
		httpServer.Close()
		t.Fatalf("upgrade websocket failed: %v", err)
	case serverConn = <-serverConnCh:
	case <-time.After(2 * time.Second):
		clientConn.Close()
		httpServer.Close()
		t.Fatal("timeout waiting for server websocket connection")
	}

	cleanup := func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
		httpServer.Close()
	}
	return serverConn, clientConn, cleanup
}

func readMessagesUntilTimeout(t *testing.T, conn *websocket.Conn) []Message {
	t.Helper()

	messages := make([]Message, 0, 8)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(120 * time.Millisecond))
		_, payload, err := conn.ReadMessage()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				break
			}
			t.Fatalf("read websocket message failed: %v", err)
		}
		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("unmarshal websocket message failed: %v payload=%s", err, string(payload))
		}
		messages = append(messages, msg)
	}
	return messages
}
