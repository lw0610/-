package websocket

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"github.com/cloudwego/eino/schema"
)

type nonblockingCompileModel struct{ calls *atomic.Int32 }

func (m nonblockingCompileModel) Complete(ctx context.Context, _ flow.ModelRequest) (string, error) {
	m.calls.Add(1)
	<-ctx.Done()
	return "", ctx.Err()
}

func TestV50WebSocketFlowEntriesUseCurrentPlanWithoutCallingModel(t *testing.T) {
	server, _, session := flowRuntimeFixture(t, []flow.StepPlan{flowWorkerStep()})
	root := t.TempDir()
	store := flow.NewStore("", filepath.Join(root, "flows"), bindings.New(filepath.Join(root, "bindings.yaml")))
	raw := "---\n编号: test-flow\n名称: 非阻塞入口\n---\n## 适用场景\n本地入口计数验收\n## 步骤\n### 处理\n- 负责：处理角色\n- 做法：处理输入\n- 完成标准：「结果」（文本）\n"
	if _, err := store.Create(raw, ""); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	compiler := flow.NewCompiler(flow.CompilerOptions{Store: store, CacheDir: filepath.Join(root, "cache"), LoadEnvironment: func(context.Context, string) (flow.Environment, error) {
		return flow.Environment{Scheme: "test", ModelName: "blocked", Model: nonblockingCompileModel{&calls}, Roles: []flow.RoleSpec{{ID: "worker", DisplayName: "处理角色", Kind: "agent", Enabled: true, Tools: []string{"read"}}}}, nil
	}})
	if err := server.SetFlowCompiler(compiler); err != nil {
		t.Fatal(err)
	}
	// These are independent requests in production. Each retains the same one
	// second bound, rather than spending a shared budget on earlier disk work.
	entry := func(name string, action func(context.Context)) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		started := time.Now()
		action(ctx)
		t.Logf("%s: model=%d elapsed=%s", name, calls.Load(), time.Since(started))
	}
	service := sessionFlowService{server: server, session: session}
	entry("available catalog", func(ctx context.Context) {
		messages := server.injectAvailableFlows(ctx, []*schema.Message{schema.UserMessage("执行本地流程")}, "test")
		if len(messages) != 2 {
			t.Fatal("available flow catalog was not injected")
		}
	})
	entry("preview", func(ctx context.Context) {
		preview, err := service.PreviewFlow(ctx, "test-flow", nil)
		if err != nil {
			t.Fatal(err)
		}
		if preview["plan"].(*flow.FlowPlan).PlanStatus != flow.PlanStatusBasic {
			t.Fatal("preview did not return a usable local plan")
		}
	})
	entry("queue", func(ctx context.Context) {
		accepted, err := service.StartFlow(ctx, "test-flow", nil)
		if err != nil || accepted["accepted"] != true {
			t.Fatalf("queue failed: %+v %v", accepted, err)
		}
	})
	entry("queued Start", func(ctx context.Context) {
		queued, err := server.startPendingFlow(ctx, session.SessionID)
		if err != nil || queued.Status != "completed" {
			t.Fatalf("queued Start blocked: %+v %v", queued, err)
		}
	})
	var direct *flowrun.RunState
	entry("direct Start", func(ctx context.Context) {
		var err error
		direct, err = server.runDirectFlow(ctx, session, flowTaskRequest{ID: "test-flow"})
		if err != nil || direct.Status != "completed" {
			t.Fatalf("direct Start blocked: %+v %v", direct, err)
		}
	})
	entry("snapshot read", func(ctx context.Context) {
		persisted, err := (flowrun.FileRunStore{}).Load(ctx, direct.SessionDir, direct.ID)
		if err != nil || persisted.Plan.PlanStatus != flow.PlanStatusBasic || persisted.Plan.Steps[0].Enrichment != flow.StepEnrichmentNone {
			t.Fatalf("run.json lost local plan snapshot: %v %+v", err, persisted)
		}
	})
	if calls.Load() != 0 {
		t.Fatalf("WebSocket entrypoints called compiler model %d times", calls.Load())
	}
}
