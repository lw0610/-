package flow

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestV50ForceUpgradesRunningIncrementalJob(t *testing.T) {
	env := fixtureEnvironment()
	var flowCalls atomic.Int32
	var blocked atomic.Bool
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	env.Model = modelFunc(func(ctx context.Context, r ModelRequest) (string, error) {
		if r.Kind == "flow" {
			flowCalls.Add(1)
		} else if blocked.Load() {
			entered <- struct{}{}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-release:
			}
		}
		return validResult(r), nil
	})
	compiler, store, _ := compilerFixture(t, threeStepFlow, &env)
	if _, err := compiler.Compile(context.Background(), "test-flow", "", false); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := NewEnrichmentManager(compiler, EnrichmentOptions{Concurrency: 1})
	m.Start(ctx)
	if _, err := store.Save("test-flow", strings.Replace(threeStepFlow, "按「汇总」排版出图", "按「汇总」导出改进图", 1)); err != nil {
		t.Fatal(err)
	}
	blocked.Store(true)
	incremental := m.Enqueue("test-flow", "", EnqueueOptions{Reason: "saved"})
	<-entered
	forced := m.Enqueue("test-flow", "", EnqueueOptions{Force: true, Reason: "manual"})
	if forced.JobID == incremental.JobID {
		t.Fatal("manual force reused an incremental job")
	}
	if duplicate := m.Enqueue("test-flow", "", EnqueueOptions{Force: true, Reason: "manual"}); duplicate.JobID != forced.JobID {
		t.Fatal("equivalent force requests were not deduplicated")
	}
	close(release)
	done := waitJob(t, m, "test-flow")
	if done.State != "succeeded" || flowCalls.Load() != 2 {
		t.Fatalf("force did not redo all units: %+v flow_calls=%d", done, flowCalls.Load())
	}
}
func TestLegacyJobFailuresDoNotContaminateSavedPlan(t *testing.T) {
	for _, failure := range []string{"timeout", "cache_io"} {
		t.Run(failure, func(t *testing.T) {
			env := fixtureEnvironment()
			env.Model = modelFunc(func(ctx context.Context, r ModelRequest) (string, error) {
				if failure == "timeout" && r.Kind == "step" {
					<-ctx.Done()
					return "", ctx.Err()
				}
				return validResult(r), nil
			})
			compiler, store, _ := compilerFixture(t, testFlow, &env)
			if failure == "cache_io" {
				source, err := compiler.source(context.Background(), "test-flow", "")
				if err != nil {
					t.Fatal(err)
				}
				unit := compiler.enrichmentUnits(source)[1]
				if err = os.MkdirAll(enrichmentFile(compiler.enrichmentDir("test-flow"), unit.Kind, unit.Key), 0700); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			m := NewEnrichmentManager(compiler, EnrichmentOptions{JobTimeout: 40 * time.Millisecond, CallTimeout: time.Second})
			m.Start(ctx)
			m.Enqueue("test-flow", "", EnqueueOptions{})
			job := waitJob(t, m, "test-flow")
			if job.State != "failed" {
				t.Fatalf("job status %+v", job)
			}
			plan, err := compiler.Plan(context.Background(), "test-flow", "")
			if err != nil || plan.PlanStatus != PlanStatusBasic || len(plan.Enrichment.Failed) != 0 || plan.Enrichment.JobID != "" || hasCode(plan, "FLOW_COMPILE_LLM_FAILED") {
				t.Fatalf("legacy failure contaminated saved plan: %v %+v", err, plan)
			}
			if _, err = store.Save("test-flow", strings.Replace(testFlow, "检查「文件夹」", "重新检查「文件夹」", 1)); err != nil {
				t.Fatal(err)
			}
			fresh, err := compiler.Plan(context.Background(), "test-flow", "")
			if err != nil || fresh.Enrichment.JobID != "" || len(fresh.Enrichment.Failed) > 0 {
				t.Fatalf("unrelated stale job contaminated changed plan: %v %+v", err, fresh.Enrichment)
			}
		})
	}
}
