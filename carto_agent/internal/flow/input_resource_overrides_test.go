package flow

import (
	"context"
	"github.com/autocarto/carto_agent/internal/resource"
	"os"
	"path/filepath"
	"testing"
)

func TestListOverridesExplicitThenUserFolderThenAcquired(t *testing.T) {
	root := t.TempDir()
	user := t.TempDir()
	cache := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "resources", "data"), 0700); err != nil {
		t.Fatal(err)
	}
	csv := "名称,路径,类型,是否必需\n用户行,user.geojson,vector,是\n获取行,auto.geojson,vector,是\n指定行,explicit.geojson,vector,是\n"
	path := filepath.Join(root, "resources", "data", "list.csv")
	if err := os.WriteFile(path, []byte(csv), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(user, "user.geojson"), filepath.Join(user, "explicit.geojson"), filepath.Join(cache, "auto.geojson"), filepath.Join(cache, "explicit.geojson"), filepath.Join(cache, "unused.geojson")} {
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	library := resource.NewLibrary("", "", nil)
	linked := library.Resolve(context.Background(), resource.Ref{Type: "data_list", Name: "底图", Location: "resources/data/list.csv", BaseDirFromInput: "数据"}, resource.Scope{FlowID: "test", FlowDir: root})
	if len(linked.Entries) != 3 {
		t.Fatalf("%+v", linked)
	}
	plan := &FlowPlan{FlowID: "test", FlowDir: root, CompileKey: "test", Resources: []ResourcePlan{ResourcePlanFromResolved(linked)}}
	values := map[string]any{"数据": map[string]any{"$fill": true, "path": user, "files": map[string]string{"指定行": filepath.Join(cache, "explicit.geojson")}}}
	result := ResolveInputResourcesWithOverrides(plan, values, map[string]map[string]string{linked.Key: {"用户行": filepath.Join(cache, "unused.geojson"), "获取行": filepath.Join(cache, "auto.geojson"), "指定行": filepath.Join(cache, "unused.geojson")}})
	wantPaths := []string{filepath.Join(user, "user.geojson"), filepath.Join(cache, "auto.geojson"), filepath.Join(cache, "explicit.geojson")}
	wantOrigins := []string{"user", "acquired", "override"}
	for i, entry := range result.Resources[0].Entries {
		if entry.Path != wantPaths[i] || entry.Extra["origin"] != wantOrigins[i] || !entry.Exists {
			t.Fatalf("row %d: %+v", i, entry)
		}
	}
	if !plan.Resources[0].Entries[0].ExistsUnknown {
		t.Fatal("original cached plan mutated")
	}
	files, _ := os.ReadDir(user)
	if len(files) != 2 {
		t.Fatal("acquisition wrote into user's folder")
	}
	if result.HasErrors() {
		t.Fatal(result.Diagnostics)
	}
}

func TestLoadedLayerIDIsPreservedInDataListHandle(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "resources", "data"), 0700)
	list := filepath.Join(root, "resources", "data", "tiles.csv")
	_ = os.WriteFile(list, []byte("名称,路径,类型,是否必需\n在线底图,basemap.tif,raster,是\n"), 0600)
	definition := filepath.Join(t.TempDir(), "tiles.layer.json")
	_ = os.WriteFile(definition, []byte(`{"type":"tianditu_xyz","style":"vec","secret_ref":"tianditu"}`), 0600)
	library := resource.NewLibrary("", "", nil)
	linked := library.Resolve(context.Background(), resource.Ref{Type: "data_list", Name: "底图", Location: "resources/data/tiles.csv", BaseDirFromInput: "数据"}, resource.Scope{FlowID: "test", FlowDir: root})
	plan := &FlowPlan{FlowID: "test", FlowDir: root, CompileKey: "test", Resources: []ResourcePlan{ResourcePlanFromResolved(linked)}}
	result := ResolveInputResourcesWithLayers(plan, map[string]any{"数据": map[string]any{"$acquire": true}}, map[string]map[string]string{linked.Key: {"在线底图": definition}}, map[string]map[string]string{linked.Key: {"在线底图": "xyz-fixture"}})
	entry := result.Resources[0].Entries[0]
	if entry.LayerID != "xyz-fixture" || !entry.Exists || entry.Path != definition || entry.Kind != "raster" {
		t.Fatalf("%+v", entry)
	}
	handle := result.Resources[0].ResolvedResource().Handle()
	entries := handle["entries"].([]map[string]any)
	if entries[0]["layer_id"] != "xyz-fixture" {
		t.Fatal(handle)
	}
	if result.HasErrors() {
		t.Fatal(result.Diagnostics)
	}
}
