package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cloudwego/eino/schema"
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
)

func TestV7ProposalUsesPriorProductsWithoutDuplicatingData(t *testing.T) {
	s, factory, run, p := cartoRuntimeFixture(t)
	p.Data = nil
	p.Steps = []plan.Step{
		{ID: "prepare", Title: "准备表格", Capabilities: []string{"read"}, Produces: []plan.Product{{ID: "prepared_table", Kind: "table"}}},
		{ID: "consume", Title: "读取已准备的表格", Capabilities: []string{"read"}, Inputs: []string{"prepared_table"}, Produces: []plan.Product{{ID: "result", Kind: "file", Format: "csv"}}},
	}
	p.Deliverables = []flow.OutputDeclaration{{Label: "result"}}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), raw)
	if err != nil || answer["status"] != "ready" {
		t.Fatal(answer, err)
	}
	prepared := s.takeCartographicPlan(run.SessionID)
	if prepared == nil {
		t.Fatal("ready plan was not queued")
	}
	if len(prepared.Snapshot.Plan.Data) != 0 || len(prepared.Executable.Steps) != 2 {
		t.Fatalf("added data/acquisition steps: %+v", prepared)
	}
	refs := prepared.Executable.Steps[1].Refs.Outputs
	if len(refs) != 1 || refs[0].Step != "prepare" || refs[0].Label != "prepared_table" {
		t.Fatalf("lost actual producer: %+v", refs)
	}
	if len(factory.roles) != 0 {
		t.Fatal("proposal ran an agent")
	}
}

func TestV7ProposalRejectsForwardDependencyBeforeSavingOrExecuting(t *testing.T) {
	s, factory, run, p := cartoRuntimeFixture(t)
	p.Steps = []plan.Step{
		{ID: "consume", Title: "提前消费", Capabilities: []string{"read"}, Inputs: []string{"later"}},
		{ID: "produce", Title: "后来产生", Capabilities: []string{"read"}, Produces: []plan.Product{{ID: "later", Kind: "file"}}},
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), raw)
	if err == nil {
		t.Fatal("forward dependency was accepted")
	}
	if s.hasPendingCartographicPlan(run.SessionID) || len(factory.roles) != 0 {
		t.Fatal("invalid proposal queued or executed")
	}
	store, err := planStore(run)
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 0 {
		t.Fatal("invalid proposal was persisted")
	}
}

func TestV7MissingLocalLayerPrecheckPersistsNeedsDataAndBlocksStart(t *testing.T) {
	s, factory, run, p := cartoRuntimeFixture(t)
	missing := filepath.Join(t.TempDir(), "missing-river.geojson")
	p.Data = []plan.Data{{ID: "river", Name: "河流", Kind: "layer", Geometry: "line", Source: "user", Ref: missing}}
	p.Steps = []plan.Step{{ID: "load", Title: "加载河流", Capabilities: []string{"read"}, Inputs: []string{"river"}, Produces: []plan.Product{{ID: "layer", Kind: "layer"}}}}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), raw)
	if err != nil || answer["status"] != "needs_data" {
		t.Fatal(answer, err)
	}
	store, err := planStore(run)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.Load(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Precheck) != 1 || saved.Precheck[0].Status != "unavailable" || saved.Precheck[0].Reason == "" || saved.Precheck[0].Suggestion == "" {
		t.Fatalf("missing actionable evidence: %+v", saved.Precheck)
	}
	if saved.Status != "needs_data" || s.hasPendingCartographicPlan(run.SessionID) {
		t.Fatal("unavailable data queued")
	}
	_, err = s.ActOnPlan(context.Background(), run.SessionID, run.ProjectPath, p.ID, plan.ActionRequest{Version: 1, Action: "start"})
	if err == nil {
		t.Fatal("start accepted a missing local layer")
	}
	if len(factory.roles) != 0 {
		t.Fatal("missing input invoked an agent")
	}
}

func TestV7UploadedFilenameResolvesOnlyWhenUnique(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			s, _, run, p := cartoRuntimeFixture(t)
			attachments := []any{}
			var source string
			for i := 0; i < count; i++ {
				source = filepath.Join(t.TempDir(), "史料.txt")
				if err := os.WriteFile(source, []byte("真实原文"), 0600); err != nil {
					t.Fatal(err)
				}
				attachments = append(attachments, map[string]any{"path": source})
			}
			if err := s.bindTaskReferences(context.Background(), run.SessionID, run.ProjectPath, Message{Data: map[string]any{"attachments": attachments}}, schema.UserMessage("按上传史料制图")); err != nil {
				t.Fatal(err)
			}
			p.Data = []plan.Data{{ID: "source", Name: "史料", Kind: "file", Source: "user", Ref: "史料.txt"}}
			p.Steps[0].Inputs = []string{"source"}
			prepared, err := s.prepareCartographicPlan(context.Background(), run, p, nil)
			if err != nil {
				t.Fatal(err)
			}
			if count == 1 {
				actual, err := filepath.EvalSymlinks(prepared.Snapshot.Plan.Data[0].Ref)
				expected, _ := filepath.EvalSymlinks(source)
				if err != nil || actual != expected || prepared.Snapshot.Precheck[0].Status != "available" {
					t.Fatal(prepared.Snapshot, err)
				}
			} else if prepared.Snapshot.Status != "needs_data" || prepared.Snapshot.Precheck[0].Status != "unavailable" {
				t.Fatal("ambiguous uploaded name was guessed", prepared.Snapshot)
			}
		})
	}
}
