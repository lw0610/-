package websocket

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/cloudwego/eino/schema"
)

func TestPlanV2KeepsDistinctUserReferencesPerDataID(t *testing.T) {
	s, factory, run, p := cartoRuntimeFixture(t)
	run.Mode = executionModePlan
	s.setFlowContext(run)
	home := t.TempDir()
	s.configService = config.NewConfigService(nil, &config.Config{}, "", filepath.Join(home, "config.yaml"))
	bindings := []dataservice.Binding{}
	for _, name := range []string{"first", "second"} {
		path := filepath.Join(home, name+".geojson")
		if err := os.WriteFile(path, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"LineString","coordinates":[[110,25],[111,26]]},"properties":{}}]}`), 0600); err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, dataservice.Binding{Name: "河流", Path: path})
	}
	attachments := []plan.Attachment{}
	for _, binding := range bindings {
		attachments = append(attachments, plan.Attachment{Path: binding.Path})
	}
	if err := s.bindTaskReferences(context.Background(), run.SessionID, run.ProjectPath, Message{Data: map[string]any{"attachments": attachments}}, schema.UserMessage("使用已选数据")); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, binding := range bindings {
		id := filepath.Base(binding.Path)
		canonicalPath, err := filepath.EvalSymlinks(binding.Path)
		if err != nil {
			t.Fatal(err)
		}
		want[id] = canonicalPath
		p.Data = append(p.Data, plan.Data{ID: id, Name: id, Source: "user", Ref: binding.Path, Kind: "layer", Geometry: "line"})
		p.Steps[0].Inputs = append(p.Steps[0].Inputs, id)
	}
	prepared, err := s.prepareCartographicPlan(context.Background(), run, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Snapshot.Selections) != 0 {
		t.Fatal("v2 user inputs must not depend on frozen discovery selections")
	}
	for id, path := range want {
		if prepared.Inputs[id] != path {
			t.Fatalf("data %s lost its exact source reference: %+v; want %s", id, prepared.Inputs, path)
		}
	}
	raw, _ := json.Marshal(p)
	if _, err = (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	changed, err := s.EditPlan(context.Background(), run.SessionID, run.ProjectPath, p.ID, plan.EditRequest{Version: 1, DataID: p.Data[0].ID, Source: "user", Ref: p.Data[1].Ref})
	if err != nil || changed.Plan.Data[0].Source != "user" || changed.Plan.Data[0].Ref != p.Data[1].Ref {
		t.Fatal(changed, err)
	}
	factory.invoke = func(context.Context, string, map[string]any) (any, error) {
		return map[string]any{"layers": []map[string]any{{"id": "project-river", "name": "工程河流", "geometry_type": "LineString"}}}, nil
	}
	changed, err = s.EditPlan(context.Background(), run.SessionID, run.ProjectPath, p.ID, plan.EditRequest{Version: 2, DataID: p.Data[0].ID, Source: "project", Ref: "project-river"})
	if err != nil || changed.Plan.Data[0].FindID != "" || changed.Plan.Data[0].Ref != "project-river" {
		t.Fatal(changed, err)
	}

}
