package flow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltinGuidesParse(t *testing.T) {
	paths, err := filepath.Glob("../../../autocarto/flows/system/*/FLOW.md")
	if err != nil || len(paths) != 4 {
		t.Fatalf("guides=%v err=%v", paths, err)
	}
	env, err := LoadGuideEnvironment("../../../autocarto")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			g := ParseGuide(string(raw), env)
			if len(g.Diagnostics) != 0 {
				t.Errorf("builtin guide must have zero diagnostics: %+v", g.Diagnostics)
			}
		})
	}
}
