package flowrun

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
)

func reviewFile(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("synthetic fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFlowFilesUseActualProjectSessionAndResolveRelativeOutputs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", filepath.Join(root, "home"))
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	project := reviewFile(t, filepath.Join(root, "qgis-project", "map.qgz"))
	dir, err := config.ResolveSessionDir(project, "session")
	if err != nil {
		t.Fatal(err)
	}
	acquired := reviewFile(t, filepath.Join(root, "home", "data", "acquired", "railway.gpkg"))
	relative := reviewFile(t, filepath.Join(dir, "outputs", "map.png"))
	projectOutput := reviewFile(t, filepath.Join(filepath.Dir(project), "export.pdf"))
	outside := reviewFile(t, filepath.Join(root, "application", "private.txt"))
	run := &RunState{ID: "flow-review", SessionID: "session", SessionDir: dir, ProjectRoot: filepath.Join(root, "application"), ProjectPath: project, Plan: testPlan(1), Inputs: map[string]any{}}
	c := &Controller{}
	c.registerFlowOutputs(run, "acquire", map[string]any{"railway": acquired})
	c.registerFlowOutputs(run, "s1", map[string]any{"image": "outputs/map.png", "export": []string{projectOutput}, "duplicate": []any{relative}, "unrelated": outside, "summary": "non-file receipt text"})
	store, err := sessionfiles.New(project, "session")
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.List(true)
	if err != nil || len(items) != 3 {
		t.Fatalf("wrong project-scoped index: %+v %v", items, err)
	}
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.Path] = true
		if item.FlowRunID != run.ID {
			t.Fatalf("missing run origin: %+v", item)
		}
	}
	for _, path := range []string{acquired, relative, projectOutput} {
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil || !seen[canonical] {
			t.Fatalf("missing output %s: %+v %v", path, items, err)
		}
	}
	wrong, err := config.ResolveSessionDir(run.ProjectRoot, "session")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wrong); !os.IsNotExist(err) {
		t.Fatalf("application root was used to create a second session index: %s %v", wrong, err)
	}
}

func TestLegacyRunWithoutProjectPathStillIndexesSessionOutputs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", filepath.Join(root, "home"))
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	file := reviewFile(t, filepath.Join(root, "session", "result.txt"))
	outside := reviewFile(t, filepath.Join(root, "application", "private.txt"))
	run := &RunState{ID: "legacy-run", SessionID: "session", SessionDir: filepath.Dir(file), ProjectRoot: filepath.Dir(outside)}
	(&Controller{}).registerFlowOutputs(run, "s1", map[string]any{"result": "result.txt", "outside": outside})
	store, err := runSessionFileStore(run)
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.List(true)
	if err != nil || len(items) != 1 || items[0].Name != "result.txt" {
		t.Fatalf("legacy session output was not safely indexed: %+v %v", items, err)
	}
}
