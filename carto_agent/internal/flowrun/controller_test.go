package flowrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/cloudwego/eino/schema"
)

type runnerFunc func(context.Context, StepRequest) (StepResult, error)

func (f runnerFunc) Run(ctx context.Context, r StepRequest) (StepResult, error) { return f(ctx, r) }

type toolFunc func(context.Context, string, map[string]any) (any, error)

func (f toolFunc) Invoke(ctx context.Context, name string, args map[string]any) (any, error) {
	return f(ctx, name, args)
}

type modelFunc func(context.Context, string) (string, error)

func (f modelFunc) CallJSON(ctx context.Context, prompt string) (string, error) {
	return f(ctx, prompt)
}

type eventLog struct {
	mu     sync.Mutex
	events []Event
}

func (l *eventLog) Emit(_ context.Context, e Event) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
	return int64(len(l.events))
}
func (l *eventLog) has(name string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.events {
		if e.Event == name {
			return true
		}
	}
	return false
}

func testPlan(steps int) *flow.FlowPlan {
	p := &flow.FlowPlan{FlowID: "example", Name: "示例流程", Scheme: "test", CompileKey: "snapshot", FlowVersion: "1", Roles: []flow.RoleSpec{{ID: "worker", DisplayName: "处理专家", Kind: "agent", Enabled: true, Tools: []string{"read"}}}}
	for i := 1; i <= steps; i++ {
		p.Steps = append(p.Steps, flow.StepPlan{ID: fmt.Sprintf("s%d", i), Index: i, Title: fmt.Sprintf("步骤%d", i), Kind: "agent", Role: "worker", Instructions: "完成本步", OnFailure: "retry_then_ask", ExpectedOutputs: []flow.OutputDeclaration{{Label: fmt.Sprintf("结果%d", i), Type: "text", Required: true}}})
	}
	return p
}
func completed(step flow.StepPlan) StepResult {
	raw, _ := json.Marshal(Receipt{Status: "completed", Summary: "完成", Outputs: map[string]any{step.ExpectedOutputs[0].Label: "真实结果"}, Issues: []string{}})
	return StepResult{LastContent: string(raw)}
}
func newTestController(t *testing.T, p *flow.FlowPlan, runner runnerFunc, edit func(*Options)) (*planTestController, StartRequest, *eventLog) {
	t.Helper()
	events := &eventLog{}
	options := Options{Runner: runner, Store: FileRunStore{}, Events: events}
	if edit != nil {
		edit(&options)
	}
	c, err := NewController(options)
	if err != nil {
		t.Fatal(err)
	}
	return &planTestController{Controller: c, plan: p}, StartRequest{SessionID: "session", SessionDir: t.TempDir(), FlowID: p.FlowID, Scheme: p.Scheme}, events
}

// The fixture always enters through the same immutable plan entry as production.
type planTestController struct {
	*Controller
	plan *flow.FlowPlan
}

func (c *planTestController) Start(ctx context.Context, request StartRequest) (*RunState, error) {
	request.CartoPlan = &plan.Plan{APIVersion: plan.LegacyAPIVersion, ID: "test-plan", Version: 1, Goal: "测试计划"}
	for _, s := range c.plan.Steps {
		request.CartoPlan.Steps = append(request.CartoPlan.Steps, plan.Step{ID: s.ID, Instructions: s.Instructions})
	}
	if request.ProjectRoot == "" {
		request.ProjectRoot = request.SessionDir
	}
	if request.Inputs == nil {
		request.Inputs = map[string]any{}
	}
	for _, input := range c.plan.Inputs {
		if input.Type == "conversation_text" {
			request.Inputs[input.Key] = ConversationReference(request.OriginalSource)
		}
	}
	return c.StartFromPlan(ctx, request, c.plan)
}
func TestControllerSuccessEventsAndSnapshot(t *testing.T) {
	p := testPlan(3)
	calls := []StepRequest{}
	c, request, events := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) {
		calls = append(calls, r)
		return completed(r.Step), nil
	}, nil)
	run, err := c.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "completed" || run.Cursor != 3 || run.Steps[1].Status != "completed" || len(calls) != 3 {
		t.Fatalf("%+v", run)
	}
	if !events.has("flow.started") || !events.has("flow.finished") {
		t.Fatal(events.events)
	}
	snapshot, err := c.Snapshot("session")
	if err != nil || snapshot == nil || snapshot.Status != "completed" {
		t.Fatal("latest completed snapshot missing")
	}
	if !strings.Contains(calls[0].Messages[0].Content, p.Steps[0].Instructions) || calls[0].RunID == calls[1].RunID {
		t.Fatal("bad isolated step request")
	}
	stored, err := (FileRunStore{}).Load(context.Background(), request.SessionDir, run.ID)
	if err != nil || stored.Result.Deliverables["结果3"] != "真实结果" {
		t.Fatalf("%+v %v", stored, err)
	}
	// A finished flow releases the session guard.
	if _, err = c.Start(context.Background(), request); err != nil {
		t.Fatal(err)
	}
}

func TestControllerRepairAndRetryVerification(t *testing.T) {
	p := testPlan(1)
	p.Steps[0].ExpectedOutputs = []flow.OutputDeclaration{{Label: "文件", Type: "file", Format: "txt", Required: true}}
	modelCalls, runnerCalls := 0, 0
	c, request, _ := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) {
		runnerCalls++
		if runnerCalls == 1 {
			return StepResult{LastContent: "完成了操作", SuccessfulTools: []ToolSummary{{Name: "write", Result: strings.Repeat("资料", 2000)}}}, nil
		}
		path := filepath.Join(r.SessionDir, "result.txt")
		if err := os.WriteFile(path, []byte("verified"), 0o600); err != nil {
			return StepResult{}, err
		}
		raw, _ := json.Marshal(Receipt{Status: "completed", Outputs: map[string]any{"文件": path}})
		return StepResult{LastContent: string(raw)}, nil
	}, func(o *Options) {
		o.Model = modelFunc(func(_ context.Context, prompt string) (string, error) {
			modelCalls++
			if strings.Contains(prompt, strings.Repeat("资料", 1500)) {
				t.Fatal("tool result was not limited")
			}
			return `{"status":"completed","outputs":{"文件":"missing.txt"},"issues":[]}`, nil
		})
	})
	run, err := c.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "completed" || runnerCalls != 2 || modelCalls != 1 || !run.Steps[0].Attempts[0].ReceiptRepaired || run.Steps[0].Attempts[0].Repair == nil {
		t.Fatalf("%+v", run)
	}
	if len(run.Result.Issues) != 0 {
		t.Fatal("successful retry retained resolved failure as a final issue")
	}
}

func TestLayerFileReceiptContinuesWithoutRepeatingSpatialWork(t *testing.T) {
	p := testPlan(2)
	p.Steps[0].ExpectedOutputs = []flow.OutputDeclaration{{Label: "专题图层", Type: "layers", Required: true}}
	calls := []string{}
	c, request, _ := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) {
		calls = append(calls, r.Step.ID)
		if r.Step.ID == "s2" {
			if !strings.Contains(r.Messages[0].Content, "population.gpkg") {
				t.Fatal("next step lost the exported layer")
			}
			return completed(r.Step), nil
		}
		path := filepath.Join(r.SessionDir, "population.gpkg")
		if err := os.WriteFile(path, []byte("existing exported layer"), 0o600); err != nil {
			return StepResult{}, err
		}
		raw, _ := json.Marshal(Receipt{Status: "completed", Outputs: map[string]any{"专题图层": path}})
		return StepResult{LastContent: string(raw)}, nil
	}, nil)
	run, err := c.Start(context.Background(), request)
	if err != nil || run.Status != "completed" || strings.Join(calls, ",") != "s1,s2" {
		t.Fatalf("run=%+v error=%v calls=%v", run, err, calls)
	}
}

func TestReceiptFailureAndPaymentFailureDoNotRepeatStep(t *testing.T) {
	for _, serviceError := range []bool{false, true} {
		t.Run(fmt.Sprintf("service_error=%t", serviceError), func(t *testing.T) {
			calls, repairs := 0, 0
			c, request, _ := newTestController(t, testPlan(1), func(context.Context, StepRequest) (StepResult, error) {
				calls++
				if serviceError {
					return StepResult{Err: errors.New("402 Payment Required: Insufficient Balance")}, nil
				}
				return StepResult{LastContent: "工具已经执行，无法整理回执"}, nil
			}, func(o *Options) {
				o.Model = modelFunc(func(context.Context, string) (string, error) {
					repairs++
					return "格式仍不正确", nil
				})
			})
			run, err := c.Start(context.Background(), request)
			wantStatus := "failed"
			if serviceError {
				wantStatus = "interrupted"
			}
			if err != nil || run.Status != wantStatus || calls != 1 || repairs > 1 || (serviceError && repairs != 0) {
				t.Fatalf("run=%+v error=%v executions=%d receipt_repairs=%d", run, err, calls, repairs)
			}
		})
	}
}

func TestControllerConfirmCheckpointAndDuplicateCallback(t *testing.T) {
	p := testPlan(1)
	p.Steps[0].Kind = "confirm"
	p.Steps[0].Role = ""
	p.Steps[0].ExpectedOutputs = []flow.OutputDeclaration{{Label: "决定", Type: "confirmation", Required: true}}
	c, request, _ := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) {
		if r.Mode != "confirm" || r.Confirmation == nil {
			t.Fatal("confirmation not isolated")
		}
		return StepResult{Interrupted: true}, nil
	}, nil)
	run, err := c.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	id := run.Steps[0].Attempts[0].RunID
	run, err = c.OnStepRunFinished(context.Background(), id, StepResult{LastContent: `{"choice":"confirm"}`})
	if err != nil || run.Status != "completed" || run.Steps[0].Outputs["决定"] != "confirm" {
		t.Fatalf("%+v %v", run, err)
	}
	if duplicate, err := c.OnStepRunFinished(context.Background(), id, StepResult{}); err != nil || duplicate != nil {
		t.Fatal(duplicate, err)
	}
}

func TestControllerRestartResumeAndOriginalSourceIdentity(t *testing.T) {
	p := testPlan(1)
	p.Inputs = []flow.InputPlan{{Key: "source", Type: "conversation_text", Required: true}}
	c, request, _ := newTestController(t, p, func(context.Context, StepRequest) (StepResult, error) { return StepResult{Interrupted: true}, nil }, nil)
	request.OriginalSource = "原始文本\n不应重复写入记录"
	run, err := c.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(run.RunJSONPath)
	if strings.Contains(string(raw), request.OriginalSource) {
		t.Fatal("run.json contains original source")
	}
	run.Status = "running"
	if err = (FileRunStore{}).Save(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	ids, err := (FileRunStore{}).MarkInterrupted(context.Background(), request.SessionDir)
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	resumed, _, _ := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) {
		if r.OriginalSource != request.OriginalSource {
			t.Fatal("source changed")
		}
		return completed(r.Step), nil
	}, nil)
	resume := ResumeRequest{SessionID: request.SessionID, SessionDir: request.SessionDir, FlowRunID: run.ID, OriginalSource: "modified"}
	if _, err = resumed.Resume(context.Background(), resume); err == nil {
		t.Fatal("changed source accepted")
	}
	resume.OriginalSource = request.OriginalSource
	result, err := resumed.Resume(context.Background(), resume)
	if err != nil || result.Status != "completed" || len(result.Steps[0].Attempts) != 2 {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestControllerCancellationAndStartupErrors(t *testing.T) {
	started := make(chan struct{})
	c, request, _ := newTestController(t, testPlan(1), func(ctx context.Context, _ StepRequest) (StepResult, error) {
		close(started)
		<-ctx.Done()
		return StepResult{}, ctx.Err()
	}, nil)
	var startedErr error
	done := make(chan struct{})
	go func() { _, startedErr = c.Start(context.Background(), request); close(done) }()
	<-started
	// Session attach must remain responsive while a role is running.
	attached := make(chan *RunState, 1)
	go func() { snapshot, _ := c.Snapshot(request.SessionID); attached <- snapshot }()
	select {
	case snapshot := <-attached:
		if snapshot == nil || snapshot.Status != "running" || snapshot.EventSeq == 0 {
			t.Fatal(snapshot)
		}
	case <-time.After(time.Second):
		t.Fatal("snapshot blocked behind step execution")
	}
	result, err := c.Cancel(context.Background(), request.SessionID)
	if err != nil || result.Status != "cancelled" {
		t.Fatal(result, err)
	}
	<-done
	if startedErr != nil {
		t.Fatal(startedErr)
	}
	if result, err = c.Cancel(context.Background(), "none"); err != nil || result != nil {
		t.Fatal(result, err)
	}
	if _, err = NewController(Options{}); err == nil {
		t.Fatal("invalid dependencies accepted")
	}
	p := testPlan(1)
	p.Diagnostics = []flow.Diagnostic{{Code: "invalid", Level: "error", Message: "bad"}}
	c, request, _ = newTestController(t, p, func(context.Context, StepRequest) (StepResult, error) {
		t.Fatal("invalid flow executed")
		return StepResult{}, nil
	}, nil)
	if _, err = c.Start(context.Background(), request); err == nil {
		t.Fatal("invalid flow did not block")
	}
	request.SessionID = ""
	if _, err = c.Start(context.Background(), request); err == nil {
		t.Fatal("missing session accepted")
	}
}

type testExecutor struct{}

func (testExecutor) Type() string { return "test" }
func (testExecutor) BuildInput(_ flow.StepPlan, _ *RunState) ([]*schema.Message, error) {
	return []*schema.Message{schema.UserMessage("executor input")}, nil
}
func (testExecutor) ToReceipt(step flow.StepPlan, text string) (Receipt, error) {
	return ParseReceipt(text)
}
func TestControllerExecutorAndFinalReviewIssues(t *testing.T) {
	p := testPlan(1)
	p.Steps[0].Kind = "executor"
	p.Steps[0].Executor = "test"
	p.Roles[0].Kind = "executor"
	p.Roles[0].Executor = "test"
	registry := NewExecutorRegistry()
	if err := registry.Register(testExecutor{}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(testExecutor{}); err == nil {
		t.Fatal("duplicate executor registered")
	}
	if err := registry.Register(nil); err == nil {
		t.Fatal("nil executor registered")
	}
	c, request, _ := newTestController(t, p, func(_ context.Context, r StepRequest) (StepResult, error) {
		if r.Messages[0].Content != "executor input" {
			t.Fatal("adapter not used")
		}
		return completed(r.Step), nil
	}, func(o *Options) { o.Executors = registry })
	if run, err := c.Start(context.Background(), request); err != nil || run.Status != "completed" {
		t.Fatal(run, err)
	}
	p = testPlan(1)
	p.Steps[0].ExpectedOutputs = []flow.OutputDeclaration{{Label: "审查", Type: "review_final", Required: true}}
	c, request, _ = newTestController(t, p, func(context.Context, StepRequest) (StepResult, error) {
		return StepResult{LastContent: `{"status":"completed","outputs":{"审查":{"final_passed":false,"remaining_issues":["可读性"],"suggestion_results":[]}},"issues":[]}`}, nil
	}, nil)
	if run, err := c.Start(context.Background(), request); err != nil || run.Status != "completed_with_issues" || run.Steps[0].Status != "completed" {
		t.Fatal(run, err)
	}
}

func TestControllerClockAndOrigin(t *testing.T) {
	now := time.Date(2026, 10, 21, 10, 30, 0, 0, time.UTC)
	c, request, _ := newTestController(t, testPlan(1), func(_ context.Context, r StepRequest) (StepResult, error) { return completed(r.Step), nil }, func(o *Options) { o.Now = func() time.Time { return now } })
	request.Origin = "orchestrator"
	run, err := c.Start(context.Background(), request)
	if err != nil || !strings.HasPrefix(run.ID, "fr-20261021-103000-") || run.Origin != "orchestrator" {
		t.Fatal(run, err)
	}
}

func TestControllerResumeFailurePausesForRevisionAndKeepsWatermark(t *testing.T) {
	p := testPlan(1)
	calls := 0
	c, request, _ := newTestController(t, p, func(context.Context, StepRequest) (StepResult, error) {
		calls++
		if calls == 1 {
			return StepResult{Interrupted: true}, nil
		}
		return StepResult{}, errors.New("resume failed")
	}, nil)
	run, err := c.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := run.Steps[0].Attempts[0].RunID
	run, err = c.OnStepRunFinished(context.Background(), checkpoint, StepResult{Err: errors.New("resume failed")})
	if err != nil || run.Status != "needs_replan" || run.Steps[0].AutomaticFailures != 2 {
		t.Fatal(run, err)
	}
	snapshot, err := c.Snapshot(request.SessionID)
	if err != nil || snapshot.EventSeq != run.EventSeq || snapshot.EventSeq == 0 {
		t.Fatal(snapshot, err)
	}
}
