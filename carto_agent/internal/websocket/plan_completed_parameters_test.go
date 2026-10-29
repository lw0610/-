package websocket

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/plan"
)

func TestRevisionKeepsHistoricalCompletedExtractionWithoutParameters(t *testing.T) {
	for _, status := range []string{"completed", "skipped", "skipped_after_failure", "failed", "waiting_user"} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			s, factory, run, ordinary := cartoRuntimeFixture(t)
			env, err := s.cartoRuntime.options.LoadEnvironment(ctx, run.Scheme)
			if err != nil {
				t.Fatal(err)
			}
			env.Roles = append(env.Roles, flow.RoleSpec{ID: "knowledge", Kind: "executor", Executor: "route_knowledge", Enabled: true})
			s.cartoRuntime.options.LoadEnvironment = func(context.Context, string) (flow.Environment, error) { return env, nil }
			p := materialSourcePlan("消息原文")
			p.Steps[0].RoleHint = "knowledge"
			p.Steps[0].Parameters = nil
			ordinary.Steps[0].Inputs = []string{"route"}
			p.Steps = append(p.Steps, ordinary.Steps[0])
			prepared, err := s.preparePlanV2(ctx, run, p, nil)
			if err != nil {
				t.Fatal(err)
			}
			prepared.Snapshot.RunID = "fr-historical-extraction"
			prepared.Snapshot.Status = "needs_replan"
			store, err := planStore(run)
			if err != nil {
				t.Fatal(err)
			}
			if err = store.Save(prepared.Snapshot, 0); err != nil {
				t.Fatal(err)
			}
			dir, err := config.ResolveSessionDir(run.ProjectPath, run.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			dataset := filepath.Join(dir, "confirmed-dataset")
			if err = os.MkdirAll(dataset, 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"places.csv", "routes.csv", "route_paths.csv", "points.csv", "symbol_mapping.csv", "callouts.csv"} {
				if err = os.WriteFile(filepath.Join(dataset, name), []byte("id\nconfirmed\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			preserved := status == "completed" || status == "skipped" || status == "skipped_after_failure"
			cursor := 0
			if preserved {
				cursor = 1
			}
			state := &flowrun.RunState{
				APIVersion: flowrun.APIVersion, ID: prepared.Snapshot.RunID,
				SessionID: run.SessionID, SessionDir: dir, ProjectPath: run.ProjectPath,
				ProjectRoot: filepath.Dir(run.ProjectPath), Plan: prepared.Executable, CartoPlan: p,
				Status: "needs_replan", Cursor: cursor, Inputs: prepared.Inputs,
				Result: flowrun.RunResult{Deliverables: map[string]any{}},
				Steps: []flowrun.StepState{
					{ID: "extract", Status: status, Outputs: map[string]any{"route": dataset}},
					{ID: ordinary.Steps[0].ID, Status: "failed", Outputs: map[string]any{}},
				},
			}
			if err = (flowrun.FileRunStore{}).Save(ctx, state); err != nil {
				t.Fatal(err)
			}
			next, err := plan.Clone(p)
			if err != nil {
				t.Fatal(err)
			}
			if preserved {
				// The model revises only remaining work. Production must restore
				// completed extraction from its trusted run record without rerunning it.
				next.Steps = next.Steps[1:]
			}
			next.Steps[len(next.Steps)-1].Instructions = "使用已保留的数据集完成剩余步骤"
			raw, err := json.Marshal(next)
			if err != nil {
				t.Fatal(err)
			}
			answer, err := (sessionPlanService{server: s, run: run}).RevisePlan(ctx, raw)
			if err != nil || answer["status"] != "ready" || !s.hasPendingCartographicPlan(run.SessionID) {
				t.Fatalf("historical preserved extraction blocked ordinary revision: answer=%v err=%v", answer, err)
			}
			updated, err := store.Load(p.ID)
			if err != nil || len(updated.Plan.Steps) != 2 || len(updated.Plan.Steps[0].Parameters) != 0 {
				t.Fatalf("historical completed step was rewritten: %+v, %v", updated, err)
			}
			if status == "completed" {
				finished, err := s.startPendingCartographicPlan(ctx, run.SessionID)
				if err != nil || finished.Status != "completed" || len(factory.roles) != 1 || len(finished.Steps[0].Attempts) != 0 || finished.Steps[0].Outputs["route"] != dataset {
					t.Fatalf("revision did not continue only ordinary work: state=%+v roles=%v err=%v", finished, factory.roles, err)
				}
			}
		})
	}
}
