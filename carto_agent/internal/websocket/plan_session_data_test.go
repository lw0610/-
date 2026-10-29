package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	tooldefinition "github.com/autocarto/carto_agent/internal/tool"
	"github.com/cloudwego/eino/schema"
)

func TestCartographicPlanReusesRegisteredDataset(t *testing.T) {
	s, _, run, base := cartoRuntimeFixture(t)
	store, err := sessionfiles.New(run.ProjectPath, run.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(store.Dir, "intermediate", "route_knowledge")
	if err = os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	// Use the normal successful task-result registration, not a second registry
	// or a fabricated extraction step in the new plan.
	raw, _ := json.Marshal(map[string]any{"success": true, "data": map[string]any{
		"dataset_dir": directory,
		"handoff":     map[string]any{"plan_data": map[string]any{"from": "session", "path": directory, "kind": "dataset"}},
	}})
	message := &schema.Message{Content: string(raw), ToolCallID: "knowledge-task"}
	if failures := registerToolArtifacts(store, tooldefinition.ToolDefinition{}, "task", "orchestrator", message, consumeEventsOptions{}); len(failures) != 0 {
		t.Fatal(failures)
	}
	item, err := store.GetPath(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		data plan.Data
	}{
		{"tool path", plan.Data{Path: directory, Kind: "dataset"}},
		{"file index ID", plan.Data{FileID: item.ID, Kind: "dataset"}},
		{"legacy directory without kind", plan.Data{FileID: item.ID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, cloneErr := plan.Clone(base)
			if cloneErr != nil {
				t.Fatal(cloneErr)
			}
			d := tc.data
			d.ID, d.Name, d.From = "route_dataset", "路线数据集（六表）", "session"
			p.Data = []plan.Data{d}
			p.Steps[0].Uses = []string{d.ID}
			prepared, prepareErr := s.prepareCartographicPlan(context.Background(), run, p, nil)
			if prepareErr != nil {
				t.Fatal(prepareErr)
			}
			resolved := prepared.Snapshot.Plan.Data[0]
			if resolved.Path != item.Path || resolved.FileID != item.ID || resolved.Kind != "dataset" || resolved.Name != d.Name {
				t.Fatalf("snapshot lost the exact artifact identity: %+v", resolved)
			}
			if prepared.Inputs[d.ID] != item.Path || len(prepared.Executable.Steps) != 1 {
				t.Fatalf("existing dataset must bind directly without another producer: inputs=%+v steps=%+v", prepared.Inputs, prepared.Executable.Steps)
			}
		})
	}
}

func TestSessionPlanReferenceRequiresIdentityAndCorrectType(t *testing.T) {
	_, _, run, _ := cartoRuntimeFixture(t)
	store, err := sessionfiles.New(run.ProjectPath, run.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(store.Dir, "intermediate", "route_knowledge")
	if err = os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	origin := sessionfiles.Origin{Type: "tool", Tool: "task"}
	if _, err = store.Register(directory, origin); err != nil {
		t.Fatal(err)
	}
	table := filepath.Join(directory, "routes.csv")
	if err = os.WriteFile(table, []byte("route_id,name\nr1,路线一\n"), 0600); err != nil {
		t.Fatal(err)
	}
	tableItem, err := store.Register(table, origin)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveSessionPlanData(context.Background(), store, plan.Data{From: "session", Path: table, Geometry: "table"})
	if err != nil || resolved.Kind != "file" || resolved.FileID != tableItem.ID || resolved.Path != tableItem.Path {
		t.Fatalf("single registered table must retain geometry validation: %+v, %v", resolved, err)
	}
	for _, tc := range []struct {
		name, want string
		data       plan.Data
	}{
		{"display name alone", "缺少产物引用", plan.Data{Name: "路线数据集（六表）", LayerName: "路线数据集"}},
		{"relative path", "精确绝对路径", plan.Data{Path: "intermediate/route_knowledge"}},
		{"directory mistaken for table", "应使用 kind=dataset 并省略 geometry", plan.Data{Path: directory, Geometry: "table"}},
		{"directory mistaken for file", "应使用 kind=dataset", plan.Data{Path: directory, Kind: "file"}},
		{"file mistaken for dataset", "应使用 kind=file", plan.Data{Path: table, Kind: "dataset"}},
		{"file geometry mismatch", "几何类型不符", plan.Data{Path: table, Geometry: "line"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, resolveErr := resolveSessionPlanData(context.Background(), store, tc.data)
			if resolveErr == nil || !strings.Contains(resolveErr.Error(), tc.want) {
				t.Fatalf("want actionable error %q, got %v", tc.want, resolveErr)
			}
		})
	}
	unindexed := filepath.Join(directory, "not_registered.csv")
	if err = os.WriteFile(unindexed, []byte("name\n其他表\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = resolveSessionPlanData(context.Background(), store, plan.Data{Path: unindexed}); !errors.Is(err, sessionfiles.ErrNotIndexed) {
		t.Fatalf("a directory reference must not implicitly register its children: %v", err)
	}
	if _, err = store.GetPath(unindexed); !errors.Is(err, sessionfiles.ErrNotIndexed) {
		t.Fatalf("failed plan resolution changed the file index: %v", err)
	}
}
