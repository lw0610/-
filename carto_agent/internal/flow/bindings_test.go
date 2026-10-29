package flow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
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

func TestGuideMigrationPreservesLegacyBindingFileAndExternalData(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	b := bindings.New(filepath.Join(root, "bindings.yaml"))
	original := "bindings:\n  flow:legacy/resource_6d294f11b03f: ./external\n"
	bindingFixtureWrite(t, filepath.Join(root, "bindings.yaml"), original)
	external := filepath.Join(root, "external/road.geojson")
	bindingFixtureWrite(t, external, "unchanged")
	library := resource.NewLibrary("", filepath.Join(root, "resources"), b)
	store := NewStore("", filepath.Join(root, "flows"), b)
	store.SetLibrary(library)
	raw := "---\n名称: 旧流程\n编号: legacy\n---\n## 步骤\n### 阅读\n- 做法：参考已提供数据。\n"
	bindingFixtureWrite(t, filepath.Join(store.UserDir, "legacy/FLOW.md"), raw)
	guides := NewGuideStore(store, filepath.Join(root, "guides.yaml"))
	if _, err := guides.MigrateLegacy(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "bindings.yaml"))
	if string(after) != original {
		t.Fatal("migration rewrote obsolete data binding")
	}
	data, _ := os.ReadFile(external)
	if string(data) != "unchanged" {
		t.Fatal("migration modified external user data")
	}
}
