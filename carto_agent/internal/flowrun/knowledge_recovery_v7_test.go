package flowrun

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/plan"
)

func TestV7KnowledgeFailureDoesNotRepeatFixedExecutorInput(t *testing.T) {
	ex, req := preparationV7Fixture(t, []plan.Data{{ID: "src", Kind: "file", Source: "user", Ref: "消息原文"}}, []plan.Step{{
		ID: "extract", Title: "抽取", Inputs: []string{"src"}, Capabilities: []string{"route_knowledge"},
		Produces: []plan.Product{{ID: "route", Kind: "dataset", Format: "route_dataset"}},
	}})
	calls := 0
	registry := NewExecutorRegistry()
	if err := registry.Register(NewRouteKnowledgeExecutor()); err != nil {
		t.Fatal(err)
	}
	c, err := NewController(Options{Store: FileRunStore{}, Executors: registry,
		DataGet: func(context.Context, *RunState, plan.Data) (DataResult, error) {
			return DataResult{Value: map[string]any{"source": "user", "text": "原文"}}, nil
		},
		Runner: cartoRunner(func(context.Context, StepRequest) (StepResult, error) {
			calls++
			return StepResult{LastContent: `{"success":false,"info":"尚未开始抽取：原文引用不存在"}`}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := c.StartFromPlan(context.Background(), req, ex)
	if err != nil || state.Status != "needs_replan" || calls != 1 || len(state.Steps[0].Attempts) != 1 {
		t.Fatalf("fixed executor input was retried: state=%+v err=%v calls=%d", state, err, calls)
	}
}

func TestV7ConfirmedKnowledgeCompileFailureContinuesAcrossRevision(t *testing.T) {
	ex, req := preparationV7Fixture(t, []plan.Data{{ID: "src", Kind: "file", Source: "user", Ref: "消息原文"}}, []plan.Step{{
		ID: "extract", Title: "抽取", Inputs: []string{"src"}, Capabilities: []string{"route_knowledge"},
		Parameters: map[string]string{"route_style": "旧样式"},
		Produces:   []plan.Product{{ID: "route", Kind: "dataset", Format: "route_dataset"}},
	}})
	dir := filepath.Join(req.SessionDir, "route_dataset")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"places.csv", "routes.csv", "route_paths.csv", "points.csv", "symbol_mapping.csv", "callouts.csv"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("id\n1\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	registry := NewExecutorRegistry()
	if err := registry.Register(NewRouteKnowledgeExecutor()); err != nil {
		t.Fatal(err)
	}
	calls := 0
	c, err := NewController(Options{Store: FileRunStore{}, Executors: registry,
		DataGet: func(context.Context, *RunState, plan.Data) (DataResult, error) {
			return DataResult{Value: map[string]any{"source": "user", "text": "原文"}}, nil
		},
		Runner: cartoRunner(func(_ context.Context, request StepRequest) (StepResult, error) {
			calls++
			if calls == 1 {
				return StepResult{Interrupted: true}, nil
			}
			var input map[string]any
			if err := json.Unmarshal([]byte(request.Messages[0].Content), &input); err != nil {
				t.Fatal(err)
			}
			if input["review_id"] != "confirmed-review" || input["revision"] != float64(2) || input["source_ref"] != "current_user_message" || input["route_style"] != nil {
				t.Fatalf("revised plan started extraction or retained the old preset: %v", input)
			}
			raw, _ := json.Marshal(map[string]any{"success": true, "data": map[string]any{"dataset_dir": dir}})
			return StepResult{LastContent: string(raw)}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := c.StartFromPlan(context.Background(), req, ex)
	if err != nil || state.Status != "waiting_user" {
		t.Fatal(state, err)
	}
	state, err = c.OnStepRunFinished(context.Background(), state.Steps[0].Attempts[0].RunID, StepResult{LastContent: `{"success":false,"info":"样式不存在","knowledge_review":{"review_id":"confirmed-review","revision":2,"status":"compile_failed"}}`})
	if err != nil || state.Status != "needs_replan" {
		t.Fatal(state, err)
	}
	saved, err := (FileRunStore{}).Load(context.Background(), req.SessionDir, state.ID)
	if err != nil || saved.Steps[0].Attempts[0].Receipt.KnowledgeReview == nil {
		t.Fatalf("failed compilation lost its durable review reference: %v", err)
	}
	next, _ := plan.Clone(req.CartoPlan)
	next.Version++
	next.Steps[0].Instructions = "按已确认的路线知识继续生成数据集"
	next.Steps[0].Parameters = nil
	ex.Steps[0].Instructions = next.Steps[0].Instructions
	ex.Steps[0].Parameters = nil
	state, err = c.Revise(context.Background(), req.SessionID, next, ex, req.Inputs, nil)
	if err != nil || state.Status != "completed" || calls != 2 || state.Steps[0].Outputs["route"] != dir {
		t.Fatalf("confirmed compilation did not continue: state=%+v err=%v calls=%d", state, err, calls)
	}
}

func TestV7ConfirmedKnowledgeRemainsCompletedAcrossRevision(t *testing.T) {
	ex, req := preparationV7Fixture(t, []plan.Data{{ID: "src", Kind: "file", Source: "user", Ref: "消息原文"}}, []plan.Step{{
		ID: "extract", Title: "抽取", Inputs: []string{"src"}, Capabilities: []string{"route_knowledge"},
		Parameters: map[string]string{"route_style": "红蓝对比", "symbols": "军标"},
		Produces:   []plan.Product{{ID: "route", Kind: "dataset", Format: "route_dataset"}},
	}, {ID: "draw", Title: "制图", Inputs: []string{"route"}}})
	registry := NewExecutorRegistry()
	if err := registry.Register(NewRouteKnowledgeExecutor()); err != nil {
		t.Fatal(err)
	}
	extractions, drawing := 0, 0
	c, err := NewController(Options{Store: FileRunStore{}, Executors: registry,
		DataGet: func(context.Context, *RunState, plan.Data) (DataResult, error) {
			return DataResult{Value: map[string]any{"source": "user", "text": "原文"}}, nil
		},
		Runner: cartoRunner(func(_ context.Context, r StepRequest) (StepResult, error) {
			if r.Step.ID == "extract" {
				extractions++
				return StepResult{Interrupted: true}, nil
			}
			drawing++
			if drawing == 1 {
				return StepResult{}, errors.New("版面参数需要修订")
			}
			return cartoReceipt(r), nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := c.StartFromPlan(context.Background(), req, ex)
	if err != nil || state.Status != "waiting_user" {
		t.Fatal(state, err)
	}
	dir := filepath.Join(req.SessionDir, "route_dataset")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"places.csv", "routes.csv", "route_paths.csv", "points.csv", "symbol_mapping.csv", "callouts.csv"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("id\n1\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(map[string]any{"success": true, "data": map[string]any{"dataset_dir": dir}})
	state, err = c.OnStepRunFinished(context.Background(), state.Steps[0].Attempts[0].RunID, StepResult{LastContent: string(raw)})
	if err != nil || state.Status != "needs_replan" || state.Steps[0].Status != "completed" {
		t.Fatal(state, err)
	}
	next, _ := plan.Clone(req.CartoPlan)
	next.Version++
	next.Steps = next.Steps[1:]
	next.Steps[0].Instructions = "使用已确认的路线成果继续制图"
	PreserveCompleted(state, next)
	ex.Steps[1].Instructions = next.Steps[1].Instructions
	state, err = c.Revise(context.Background(), req.SessionID, next, ex, req.Inputs, nil)
	if err != nil || state.Status != "completed" || extractions != 1 || drawing != 2 || state.Steps[0].Outputs["route"] != dir {
		t.Fatalf("confirmed extraction was not retained: state=%+v err=%v extracts=%d draw=%d", state, err, extractions, drawing)
	}
}
