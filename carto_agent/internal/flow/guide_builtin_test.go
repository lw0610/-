package flow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/layers"
)

func TestBuiltinGuidesParse(t *testing.T) {
	paths, err := filepath.Glob("../../../autocarto/flows/system/*/FLOW.md")
	if err != nil || len(paths) != 3 {
		t.Fatalf("guides=%v err=%v", paths, err)
	}
	assets, _ := filepath.Abs("../../../autocarto")
	t.Setenv("AUTOCARTO_ASSETS_ROOT", assets)
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	env, err := LoadGuideEnvironment(assets)
	if err != nil {
		t.Fatal(err)
	}
	// Other guides still explicitly reference imported background layers.
	for _, name := range []string{"省域", "地区界线", "河流线", "河流面", "公路", "国道", "铁路", "县级城市驻地"} {
		env.Layers = append(env.Layers, layers.Item{Manifest: layers.Manifest{ID: "layer.user.fixture." + name, Name: name}, Status: "ready"})
	}
	for _, path := range paths {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			g := ParseGuide(string(raw), env)
			if len(g.Diagnostics) != 0 {
				t.Errorf("builtin guide diagnostics: %+v", g.Diagnostics)
			}
		})
	}
}
