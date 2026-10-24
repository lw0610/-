package resource

import (
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"path/filepath"
	"testing"
)

func TestPrivateToolAssetsAreNotPublicResources(t *testing.T) {
	root, err := filepath.Abs("../../../autocarto/resources/system")
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	library := NewLibrary(root, filepath.Join(temporary, "resources"), bindings.New(filepath.Join(temporary, "bindings.yaml")))
	items, err := library.List("builtin")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Name == "中国行政区边界" || item.Name == "route-defaults" || item.Name == "presets" {
			t.Fatalf("工具私有资产出现在公共资源库: %+v", item)
		}
	}
}
