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
)

func TestPlanPinsDistinctChoicesFromSameFindPerDataID(t *testing.T) {
	s, factory, run, p := cartoRuntimeFixture(t)
	run.Mode = executionModePlan
	s.setFlowContext(run)
	home := t.TempDir()
	s.configService = config.NewConfigService(nil, &config.Config{}, "", filepath.Join(home, "config.yaml"))
	dir, err := config.ResolveSessionDir(run.ProjectPath, run.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	bindings := []dataservice.Binding{}
	for _, name := range []string{"first", "second"} {
		path := filepath.Join(home, name+".geojson")
		if err = os.WriteFile(path, []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"LineString","coordinates":[[110,25],[111,26]]},"properties":{}}]}`), 0600); err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, dataservice.Binding{Name: "河流", Path: path})
	}
	if err = dataservice.SaveBindings(dir, bindings); err != nil {
		t.Fatal(err)
	}
	service, err := s.dataService(context.Background(), run.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	found, err := service.Find(context.Background(), dataservice.FindRequest{Need: dataservice.Need{Name: "河流", What: dataservice.What{Category: "河流"}, Extent: "bbox:24,109,27,112", Scale: "auto"}})
	if err != nil || len(found.Rows) != 1 {
		t.Fatal(found, err)
	}
	want := map[string]string{}
	for _, candidate := range found.Rows[0].Candidates {
		if candidate.Kind != "user" {
			continue
		}
		id := filepath.Base(candidate.Path)
		want[id] = candidate.Path
		p.Data = append(p.Data, plan.Data{ID: id, Name: id, From: "user", Path: candidate.Path, Geometry: "line", FindID: found.FindID, Choice: candidate.ID})
		p.Steps[0].Uses = append(p.Steps[0].Uses, id)
	}
	if len(want) != 2 {
		t.Fatalf("fixture must expose two independent source choices: %+v", found)
	}
	prepared, err := s.prepareCartographicPlan(context.Background(), run, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	for id, path := range want {
		var selection dataservice.Selection
		if err = json.Unmarshal(prepared.Snapshot.Selections[id], &selection); err != nil {
			t.Fatal(err)
		}
		if len(selection.Rows) != 1 || selection.Rows[0].Candidate.Path != path {
			t.Fatalf("data %s lost its confirmed choice: %+v; want %s", id, selection, path)
		}
	}
	raw, _ := json.Marshal(p)
	if _, err = (sessionPlanService{server: s, run: run}).ProposePlan(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	changed, err := s.EditPlan(context.Background(), run.SessionID, run.ProjectPath, p.ID, plan.EditRequest{Version: 1, DataID: p.Data[0].ID, From: "acquire", Choice: p.Data[1].Choice})
	if err != nil || changed.Plan.Data[0].From != "user" || changed.Plan.Data[0].Path != p.Data[1].Path {
		t.Fatal(changed, err)
	}
	factory.invoke = func(context.Context, string, map[string]any) (any, error) {
		return map[string]any{"layers": []map[string]any{{"id": "project-river", "name": "工程河流", "geometry_type": "LineString"}}}, nil
	}
	changed, err = s.EditPlan(context.Background(), run.SessionID, run.ProjectPath, p.ID, plan.EditRequest{Version: 2, DataID: p.Data[0].ID, From: "project", Layer: "project-river"})
	if err != nil || changed.Plan.Data[0].FindID != "" || changed.Plan.Data[0].Layer != "project-river" {
		t.Fatal(changed, err)
	}

}
