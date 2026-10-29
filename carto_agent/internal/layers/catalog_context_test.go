package layers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/resource"
	"gopkg.in/yaml.v3"
)

func TestCatalogUsesSuppliedStylesAndReloadsOnNextRead(t *testing.T) {
	library := NewLibrary("", t.TempDir())
	item := fixture(t, library, "测试行政区")
	item.DefaultStyle = "本次提供的样式"
	raw, err := yaml.Marshal(item.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(item.Directory, "layer.yaml"), raw, 0600); err != nil {
		t.Fatal(err)
	}

	library.AssetsRoot = t.TempDir()
	categories := filepath.Join(library.AssetsRoot, "tools", "system", "data-acquisition", "assets", "categories.yaml")
	if err = os.MkdirAll(filepath.Dir(categories), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(categories, []byte("categories:\n  - id: admin.division\n"), 0600); err != nil {
		t.Fatal(err)
	}
	styles := []resource.LibraryItem{{Type: "style", Name: item.DefaultStyle}}
	items, err := library.ListWithResources(styles)
	if err != nil || len(items) != 1 || items[0].Status != "ready" {
		t.Fatalf("%+v %v", items, err)
	}

	if _, err = library.ListWithResources(nil); err == nil || !strings.Contains(err.Error(), "默认样式不存在") {
		t.Fatalf("a previous request's styles were retained: %v", err)
	}
	if err = os.WriteFile(categories, []byte("categories:\n  - id: hydro.river_line\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = library.ListWithResources(styles); err == nil || !strings.Contains(err.Error(), "未知数据类别") {
		t.Fatalf("category edits did not take effect: %v", err)
	}
}
