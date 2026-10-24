package flow

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const validFlowResult = `{"inputs":[{"label":"原文","type":"conversation_text","options":[]},{"label":"文件夹","type":"folder","options":[]},{"label":"开关","type":"boolean","options":[]},{"label":"纸张","type":"choice","options":["A3","A4"]}],"input_constraints":[],"triggers":["测试"]}`
const validStepResult = `{"suggested_tools":[],"missing_capabilities":[],"expected_outputs":[],"condition":null,"notes":""}`

func validResult(r ModelRequest) string {
	if r.Kind == "flow" {
		return validFlowResult
	}
	return validStepResult
}
func waitJob(t *testing.T, m *EnrichmentManager, id string) CompileJobStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if status, ok := m.Status(id, ""); ok && !activeJob(status.State) {
			return status
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("background job did not finish")
	return CompileJobStatus{}
}
func TestEnrichmentManagerDetachedDeduplicatedAndIncremental(t *testing.T) {
	env := fixtureEnvironment()
	var calls atomic.Int32
	entered := make(chan struct{}, 10)
	release := make(chan struct{})
	env.Model = modelFunc(func(ctx context.Context, r ModelRequest) (string, error) {
		calls.Add(1)
		entered <- struct{}{}
		select {
		case <-release:
			return validResult(r), nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	compiler, store, _ := compilerFixture(t, threeStepFlow, &env)
	lifecycle, cancel := context.WithCancel(context.Background())
	defer cancel()
	var eventMu sync.Mutex
	events := []CompileJobStatus{}
	manager := NewEnrichmentManager(compiler, EnrichmentOptions{Concurrency: 2, Broadcast: func(s CompileJobStatus) { eventMu.Lock(); events = append(events, s); eventMu.Unlock() }})
	manager.Start(lifecycle)
	request, disconnect := context.WithCancel(context.Background())
	_ = request
	first := manager.Enqueue("test-flow", "", EnqueueOptions{Reason: "selected"})
	disconnect()
	<-entered
	ids := make(chan string, 5)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids <- manager.Enqueue("test-flow", "", EnqueueOptions{Reason: "opened"}).JobID
		}()
	}
	wg.Wait()
	close(ids)
	for id := range ids {
		if id != first.JobID {
			t.Fatalf("deduplication: %s != %s", id, first.JobID)
		}
	}
	plan, err := compiler.Plan(context.Background(), "test-flow", "")
	if err != nil || plan.PlanStatus != PlanStatusBasic || plan.Enrichment.JobID != "" || calls.Load() != 1 {
		t.Fatalf("nonblocking plan: %v %+v calls=%d", err, plan, calls.Load())
	}
	close(release)
	done := waitJob(t, manager, "test-flow")
	if done.State != "succeeded" || done.Done != 4 || calls.Load() != 4 {
		t.Fatalf("completed: %+v calls=%d", done, calls.Load())
	}
	if _, err = store.Save("test-flow", strings.Replace(threeStepFlow, "按「汇总」排版出图", "按「汇总」导出新图", 1)); err != nil {
		t.Fatal(err)
	}
	manager.Enqueue("test-flow", "", EnqueueOptions{Reason: "saved"})
	done = waitJob(t, manager, "test-flow")
	if done.State != "succeeded" || calls.Load() != 5 {
		t.Fatalf("incremental: %+v calls=%d", done, calls.Load())
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		eventMu.Lock()
		n := len(events)
		eventMu.Unlock()
		if n >= 8 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	sawRunning, sawDone := false, false
	for _, e := range events {
		if e.State == "running" && e.Done > 0 {
			sawRunning = true
		}
		if e.State == "succeeded" {
			sawDone = true
		}
	}
	if !sawRunning || !sawDone {
		t.Fatalf("progress events: %+v", events)
	}
}
func TestEnrichmentManagerCancellationPersistsAndRestartContinues(t *testing.T) {
	env := fixtureEnvironment()
	var flowCalls, stepCalls atomic.Int32
	entered := make(chan struct{}, 4)
	var blocked atomic.Bool
	blocked.Store(true)
	env.Model = modelFunc(func(ctx context.Context, r ModelRequest) (string, error) {
		if r.Kind == "flow" {
			flowCalls.Add(1)
			return validResult(r), nil
		}
		stepCalls.Add(1)
		entered <- struct{}{}
		if blocked.Load() {
			<-ctx.Done()
			return "", ctx.Err()
		}
		return validResult(r), nil
	})
	compiler, _, _ := compilerFixture(t, threeStepFlow, &env)
	lifecycle, cancel := context.WithCancel(context.Background())
	manager := NewEnrichmentManager(compiler, EnrichmentOptions{Concurrency: 1})
	manager.Start(lifecycle)
	manager.Enqueue("test-flow", "", EnqueueOptions{})
	<-entered
	cancel()
	done := waitJob(t, manager, "test-flow")
	if done.State != "cancelled" {
		t.Fatalf("cancel: %+v", done)
	}
	blocked.Store(false)
	// A new compiler and manager simulate a process restart; completed flow cache is on disk.
	next := NewCompiler(compiler.options)
	nextCtx, nextCancel := context.WithCancel(context.Background())
	defer nextCancel()
	resumed := NewEnrichmentManager(next, EnrichmentOptions{Concurrency: 1})
	resumed.Start(nextCtx)
	resumed.Enqueue("test-flow", "", EnqueueOptions{})
	done = waitJob(t, resumed, "test-flow")
	if done.State != "succeeded" || flowCalls.Load() != 1 || stepCalls.Load() != 4 {
		t.Fatalf("restart: %+v flow=%d step=%d", done, flowCalls.Load(), stepCalls.Load())
	}
}
func TestEnrichmentManagerGlobalConcurrencyAndIsolatedTimeout(t *testing.T) {
	env := fixtureEnvironment()
	var active, max atomic.Int32
	env.Model = modelFunc(func(ctx context.Context, r ModelRequest) (string, error) {
		count := active.Add(1)
		defer active.Add(-1)
		for {
			prior := max.Load()
			if count <= prior || max.CompareAndSwap(prior, count) {
				break
			}
		}
		if r.Step == "s2" {
			<-ctx.Done()
			return "", ctx.Err()
		}
		select {
		case <-time.After(5 * time.Millisecond):
			return validResult(r), nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	compiler, store, _ := compilerFixture(t, threeStepFlow, &env)
	if _, err := store.Copy("test-flow", "other-flow", "副本"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := NewEnrichmentManager(compiler, EnrichmentOptions{Concurrency: 2, CallTimeout: 30 * time.Millisecond})
	manager.Start(ctx)
	manager.Enqueue("test-flow", "", EnqueueOptions{})
	manager.Enqueue("other-flow", "", EnqueueOptions{})
	for _, id := range []string{"test-flow", "other-flow"} {
		done := waitJob(t, manager, id)
		if done.State != "failed" || len(done.Failed) != 1 || done.Failed[0].StepID != "s2" || done.Failed[0].Reason != "timeout" {
			t.Fatalf("timeout: %+v", done)
		}
		plan, err := compiler.Plan(ctx, id, "")
		if err != nil || plan.Enrichment.Done != 2 || plan.HasErrors() {
			t.Fatalf("partial plan: %v %+v", err, plan)
		}
	}
	if max.Load() > 2 || max.Load() < 2 {
		t.Fatalf("global model concurrency=%d", max.Load())
	}
}
func TestEnrichmentManagerReplacesChangedSourceAndSkipsNoModel(t *testing.T) {
	env := fixtureEnvironment()
	compiler, store, _ := compilerFixture(t, threeStepFlow, &env)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := NewEnrichmentManager(compiler, EnrichmentOptions{Concurrency: 1})
	manager.Start(ctx)
	if job := manager.Enqueue("test-flow", "", EnqueueOptions{}); job.JobID != "" {
		t.Fatal("created a job without model")
	}
	entered := make(chan struct{}, 1)
	env.Model = modelFunc(func(ctx context.Context, r ModelRequest) (string, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return "", ctx.Err()
	})
	first := manager.Enqueue("test-flow", "", EnqueueOptions{})
	<-entered
	if _, err := store.Save("test-flow", strings.Replace(threeStepFlow, "按「汇总」排版出图", "按「汇总」更改制图", 1)); err != nil {
		t.Fatal(err)
	}
	second := manager.Enqueue("test-flow", "", EnqueueOptions{})
	if second.JobID == first.JobID {
		t.Fatal("changed source did not replace job")
	}
	if !manager.Cancel("test-flow", "") {
		t.Fatal("cancel failed")
	}
	if status := waitJob(t, manager, "test-flow"); status.State != "cancelled" {
		t.Fatalf("cancel state %+v", status)
	}
}
