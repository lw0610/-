package websocket

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestReplanInputProjectsCurrentFactsWithoutAssistantOrOldAttempts(t *testing.T) {
	s, _, run, p := cartoRuntimeFixture(t)
	p.Guide = &plan.GuideRef{ID: "marching-route"}
	p.Steps = append([]plan.Step{{ID: "loaded", Title: "底图", Produces: []plan.Product{{ID: "roads", Kind: "layer", Geometry: "line"}}}}, p.Steps...)
	user := schema.UserMessage("请保留上传史料的依据")
	user.Extra = map[string]any{"attachments": []any{map[string]any{"path": "/materials/history.txt", "file_id": "sf-source"}}}
	run.Session.AddMessage(user)
	run.Session.AddMessage(schema.AssistantMessage("旧助手声称空产出已经完成", nil))
	state := &flowrun.RunState{ID: "fr-replan", CartoPlan: p, Cursor: 1, Revisions: 1,
		Plan:        &flow.FlowPlan{FlowID: "internal-executable-should-not-appear"},
		PlanHistory: []*plan.Plan{{Goal: "outdated-history-should-not-appear"}},
		Guide:       &flow.GuideDoc{Body: "duplicate-guide-should-not-appear"},
		Inputs:      map[string]any{"file": "/materials/history.txt"}, AcquiredInputs: map[string]any{"metro": "/cache/metro.gpkg"},
		InputDetails:    map[string]map[string]any{"metro": {"source": "source.osm.overpass"}},
		DataPreparation: []flowrun.DataPreparationState{{DataID: "metro", Status: "completed", Ledger: []flowrun.PreparationAttempt{{Method: "overpass", Result: "completed", Paths: []string{"/raw/metro.geojson"}}}}},
		Steps: []flowrun.StepState{
			{ID: "loaded", Status: "completed", Outputs: map[string]any{"roads": "roads_actual_001"}, Attempts: []flowrun.Attempt{{BeforeLayers: []string{"expired-layer-id"}, Receipt: &flowrun.Receipt{Summary: "old-loaded-receipt"}}}},
			{ID: "work", Status: "failed", LastFailure: "图层不存在", UserGuidance: "零项请如实报告", Attempts: []flowrun.Attempt{
				{Receipt: &flowrun.Receipt{Summary: "old-failed-receipt"}},
				{Receipt: &flowrun.Receipt{Status: "completed", Summary: "latest-zero-items", Outputs: map[string]any{}}, Verification: flowrun.Verification{Errors: []string{"required output absent"}}},
			}},
		}}
	input, err := cartographicReplanInput(run, state)
	if err != nil {
		t.Fatal(err)
	}
	input, err = s.injectTaskContext(context.Background(), input, run.Scheme, run.SessionID, executionModePlan)
	if err != nil {
		t.Fatal(err)
	}
	var joined strings.Builder
	var projection map[string]json.RawMessage
	for _, m := range input {
		if m.Role == schema.Assistant {
			t.Fatal("assistant history leaked")
		}
		joined.WriteString(m.Content)
		if strings.HasPrefix(m.Content, "当前计划需要调整") {
			if err := json.Unmarshal([]byte(strings.SplitN(m.Content, "\n", 2)[1]), &projection); err != nil {
				t.Fatal(err)
			}
		}
	}
	text := joined.String()
	for _, forbidden := range []string{"旧助手声称", "old-loaded-receipt", "old-failed-receipt", "expired-layer-id", "outdated-history-should-not-appear", "internal-executable-should-not-appear", "duplicate-guide-should-not-appear"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("old state leaked: %s", forbidden)
		}
	}
	for _, required := range []string{"保留完整原文", "请保留上传史料", "roads_actual_001", "latest-zero-items", "required output absent", "零项请如实报告", "/raw/metro.geojson", "/cache/metro.gpkg", "source.osm.overpass"} {
		if !strings.Contains(text, required) {
			t.Errorf("current evidence lost: %s", required)
		}
	}
	if strings.Count(text, "【用户指定流程全文，已由系统读取】") != 1 {
		t.Fatal("guide must be injected once")
	}
	if input[1].Extra["attachments"] == nil || user.Extra["guide_id"] != nil {
		t.Fatal("user attachment metadata lost or source message mutated")
	}
	var projected plan.Plan
	if err := json.Unmarshal(projection["current_plan"], &projected); err != nil {
		t.Fatal(err)
	}
	if len(projected.Steps) != 1 || projected.Steps[0].ID != "work" || len(p.Steps) != 2 {
		t.Fatal("completed steps were duplicated or original plan mutated")
	}
	if !strings.Contains(string(projection["completed_steps"]), `"kind":"layer"`) || string(projection["revisions_remaining"]) != "2" {
		t.Fatal("typed completed output or revision budget missing")
	}
}

func TestReplanRequiresAcceptedRevisionOrRegisteredHITL(t *testing.T) {
	for _, mode := range []string{"text-only", "stale-pending", "revision", "confirmation", "needs-data", "hitl"} {
		t.Run(mode, func(t *testing.T) {
			s, _, run, p := cartoRuntimeFixture(t)
			service := sessionPlanService{server: s, run: run}
			raw, _ := json.Marshal(p)
			if _, err := service.ProposePlan(context.Background(), raw); err != nil {
				t.Fatal(err)
			}
			if mode != "stale-pending" {
				s.takeCartographicPlan(run.SessionID)
			}
			if mode == "confirmation" {
				s.configService = config.NewConfigService(nil, &config.Config{Plan: config.CartographicPlanConfig{Confirm: "always"}}, "", filepath.Join(t.TempDir(), "config.yaml"))
			}
			if mode == "needs-data" {
				revision := mustClonePlan(p)
				revision.Data = []plan.Data{{ID: "river", Name: "河流", Kind: "layer", Geometry: "line", Source: "user", Ref: filepath.Join(t.TempDir(), "missing-river.geojson")}}
				revision.Steps[0].Inputs = []string{"river"}
				raw, _ = json.Marshal(revision)
			}
			var revisionAnswer map[string]any
			probe := &replanProbeAgent{interrupt: mode == "hitl"}
			if mode == "revision" || mode == "confirmation" || mode == "needs-data" {
				probe.act = func(ctx context.Context) error {
					var err error
					revisionAnswer, err = service.RevisePlan(ctx, raw)
					return err
				}
			}
			s.runtime.PlanAgent = probe
			state := &flowrun.RunState{ID: "fr-replan", CartoPlan: p, Status: "needs_replan", Steps: []flowrun.StepState{{ID: "work", Status: "failed"}}}
			err := s.replanCartographic(context.Background(), run, state)
			if mode == "text-only" || mode == "stale-pending" {
				if err == nil || !strings.Contains(err.Error(), "未提交修订，已保留成果") {
					t.Fatalf("text-only accepted: %v", err)
				}
				runtime := s.sessionRuntimeProjectionSnapshot(run.SessionID, nil).RuntimeState
				if runtime.Status != sessionRuntimeStatusFailed {
					t.Fatalf("false success status: %+v", runtime)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if mode == "revision" {
				pending := s.takeCartographicPlan(run.SessionID)
				if pending == nil || pending.Snapshot.Plan.Version != 2 {
					t.Fatal("actual accepted revision missing")
				}
			}
			if mode == "hitl" {
				pending, ok := s.getPendingInterrupt(run.SessionID)
				if !ok || pending.FlowOptions.replanPlanID != p.ID || len(pending.Contexts) == 0 {
					t.Fatal("actual interrupt was not registered")
				}
				runtime, _ := s.activeSessionRuntime(run.SessionID)
				if runtime.Status != sessionRuntimeStatusInterrupted {
					t.Fatalf("HITL must remain interrupted: %+v", runtime)
				}
			}
			if mode == "confirmation" {
				store, _ := planStore(run)
				snapshot, err := store.Load(p.ID)
				if err != nil || snapshot.Plan.Version != 2 || snapshot.Status != "awaiting_confirmation" || !snapshot.NeedsConfirmation || s.hasPendingCartographicPlan(run.SessionID) {
					t.Fatalf("confirmation policy bypassed: %#v, %v", snapshot, err)
				}
				runtime, _ := s.activeSessionRuntime(run.SessionID)
				if runtime.Status != sessionRuntimeStatusInterrupted {
					t.Fatalf("confirmed revision should wait: %+v", runtime)
				}
			}
			if mode == "needs-data" {
				store, _ := planStore(run)
				snapshot, err := store.Load(p.ID)
				if err != nil || revisionAnswer["accepted"] != true || snapshot.Plan.Version != 2 || snapshot.Status != "needs_data" || s.hasPendingCartographicPlan(run.SessionID) {
					t.Fatalf("accepted missing-data revision was lost or queued: %#v, %v", snapshot, err)
				}
				if len(snapshot.Precheck) != 1 || snapshot.Precheck[0].Status != "unavailable" || snapshot.Precheck[0].Reason == "" {
					t.Fatalf("missing-data diagnostics lost: %+v", snapshot.Precheck)
				}
				runtime, _ := s.activeSessionRuntime(run.SessionID)
				if runtime.Status != sessionRuntimeStatusInterrupted || probe.calls != 1 {
					t.Fatalf("revision should wait without another model call: %+v, calls=%d", runtime, probe.calls)
				}
			}
			if len(run.Session.GetMessages()) != 1 {
				t.Fatal("replanner prose polluted session history")
			}
		})
	}
}

func TestReplanHITLResumeStillRequiresActualRevision(t *testing.T) {
	for _, revise := range []bool{false, true} {
		t.Run(map[bool]string{false: "only-prose", true: "real-revision"}[revise], func(t *testing.T) {
			s, _, run, p := cartoRuntimeFixture(t)
			service := sessionPlanService{server: s, run: run}
			raw, _ := json.Marshal(p)
			if _, err := service.ProposePlan(context.Background(), raw); err != nil {
				t.Fatal(err)
			}
			s.takeCartographicPlan(run.SessionID)
			probe := &replanProbeAgent{interrupt: true}
			s.runtime.PlanAgent = probe
			state := &flowrun.RunState{ID: "fr-replan", CartoPlan: p, Status: "needs_replan", Steps: []flowrun.StepState{{ID: "work", Status: "failed"}}}
			if err := s.replanCartographic(context.Background(), run, state); err != nil {
				t.Fatal(err)
			}
			pending, ok := s.popPendingInterrupt(nil, run.SessionID)
			if !ok {
				t.Fatal("missing actual interrupt")
			}
			probe.interrupt = false
			if revise {
				probe.act = func(ctx context.Context) error { _, err := service.RevisePlan(ctx, raw); return err }
			}
			iter, err := pending.Runner.ResumeWithParams(context.Background(), pending.CheckpointID, &adk.ResumeParams{Targets: map[string]any{pending.Contexts[0].ID: "保留零项文字说明"}})
			if err != nil {
				t.Fatal(err)
			}
			s.markSessionRuntimeResumed(run.SessionID)
			result := s.consumeEvents(context.Background(), nil, iter, pending.Runner, pending.CheckpointID, run.SessionID, run.ProjectPath, run.Session, pending.FlowOptions)
			err = s.finishCartographicReplan(run.SessionID, run.ProjectPath, pending.FlowOptions, result)
			if revise && err != nil {
				t.Fatal(err)
			}
			if !revise && (err == nil || !strings.Contains(err.Error(), "未提交修订，已保留成果")) {
				t.Fatalf("resume accepted prose: %v", err)
			}
			if len(run.Session.GetMessages()) != 1 {
				t.Fatal("resume prose polluted session history")
			}
		})
	}
}
