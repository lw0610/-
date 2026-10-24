package flow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"gopkg.in/yaml.v3"
)

func bindingFixtureWrite(t *testing.T, path, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestBindingsMigrateV1(t *testing.T) {
	workspace := t.TempDir()
	project := filepath.Join(workspace, "enio2")
	t.Setenv("AUTOCARTO_PROJECT_ROOT", project)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", "autocarto")
	t.Setenv("AUTOCARTO_HOME", "autocarto_data")
	if err := os.MkdirAll(filepath.Join(project, "autocarto/skills/system"), 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(workspace, "数据/底图数据/China_map")
	bindingFixtureWrite(t, filepath.Join(outside, "road.geojson"), "{}")
	flowDir := filepath.Join(project, "autocarto/flows/system/marching-route")
	markdown := "---\n编号: marching-route\n名称: 路线\n---\n## 使用的资源\n- 底图数据「中国底图」：resources/data/中国底图.csv\n## 步骤\n### 检查\n- 做法：加载底图\n"
	bindingFixtureWrite(t, filepath.Join(flowDir, "FLOW.md"), markdown)
	bindingFixtureWrite(t, filepath.Join(flowDir, "resources/data/中国底图.csv"), "name,path\n道路,road.geojson\n")
	bindingFixtureWrite(t, filepath.Join(project, "autocarto_data/resources/data/共享.csv"), "name,path\n道路,road.geojson\n")
	bindingFixtureWrite(t, filepath.Join(project, "autocarto_data/resources/data/共享.resource.yaml"), "apiVersion: resource/v1\nid: data_list.shared\ntype: data_list\nname: 共享\nentry: 共享.csv\n")
	file, err := config.ResolveResourceBindingsFile()
	if err != nil {
		t.Fatal(err)
	}
	original := "bindings:\n  flow:marching-route/resource_6d294f11b03f: ../数据/底图数据/China_map\n  library:共享: ../数据/底图数据/China_map\n  flow:old-flow/resource_0123456789ab: ../unmatched\n"
	bindingFixtureWrite(t, file, original)
	library, err := resource.NewDefaultLibrary()
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewDefaultStore(library)
	if err != nil {
		t.Fatal(err)
	}
	if store.Bindings != library.Bindings {
		t.Fatal("production constructors did not share the same binding store")
	}
	if got := bindings.LegacyResourceKey("data_list", "中国底图"); got != "resource_6d294f11b03f" {
		t.Fatal(got)
	}
	key := "flow:marching-route/path:data/中国底图.csv"
	if got, found, err := library.Bindings.Get(key); err != nil || !found || got != outside {
		t.Fatalf("%s %v %v", got, found, err)
	}
	if got, found, err := library.Bindings.Get("library:data_list.shared"); err != nil || !found || got != outside {
		t.Fatalf("%s %v %v", got, found, err)
	}
	current, _ := os.ReadFile(file)
	if string(current) != original {
		t.Fatal("startup read wrote user configuration")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(file), "backups")); !os.IsNotExist(err) {
		t.Fatal("read created a backup")
	}
	if count, err := store.OrphanedBindingCount(); err != nil || count != 1 {
		t.Fatalf("%d %v", count, err)
	}
	// Renaming the presentation label cannot change the new key or its data paths.
	bindingFixtureWrite(t, filepath.Join(flowDir, "FLOW.md"), strings.ReplaceAll(markdown, "「中国底图」", "「新的显示名称」"))
	resolved := library.Resolve(context.Background(), resource.Ref{Type: "data_list", Name: "新的显示名称", Location: "resources/data/中国底图.csv"}, resource.Scope{FlowID: "marching-route", FlowDir: flowDir})
	if resolved.Key != "path:data/中国底图.csv" || resolved.BaseDir != outside || len(resolved.Entries) != 1 || !resolved.Entries[0].Exists {
		t.Fatalf("%+v", resolved)
	}
	if err := store.SetBinding("marching-route", resolved.Key, outside); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(file), "backups/resource_bindings-*.yaml"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("%v %v", backups, err)
	}
	backup, _ := os.ReadFile(backups[0])
	if string(backup) != original {
		t.Fatal("backup did not preserve original bytes")
	}
	current, _ = os.ReadFile(file)
	var doc bindings.Document
	if err := yaml.Unmarshal(current, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.APIVersion != bindings.APIVersion || doc.Bindings[key] != outside || doc.Orphaned["flow:old-flow/resource_0123456789ab"] != "../unmatched" {
		t.Fatalf("%+v", doc)
	}
	// Recreating the production services after a label rename must not remigrate v2.
	again, err := NewDefaultStore(library)
	if err != nil {
		t.Fatal(err)
	}
	if err := again.SetBinding("marching-route", resolved.Key, outside); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(file)
	if string(current) != string(after) {
		t.Fatal("second migration changed v2")
	}
	backups, _ = filepath.Glob(filepath.Join(filepath.Dir(file), "backups/resource_bindings-*.yaml"))
	if len(backups) != 1 {
		t.Fatal("v2 write created another v1 backup")
	}
}
