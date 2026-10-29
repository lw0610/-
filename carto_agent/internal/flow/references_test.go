package flow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
)

func TestResourceReferencesIncludeAmbiguityAndSkipBundled(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	b := bindings.New(filepath.Join(root, "bindings.yaml"))
	library := resource.NewLibrary(filepath.Join(root, "resources/builtin"), filepath.Join(root, "resources/user"), b)
	for _, owner := range []string{"builtin", "user"} {
		dir := filepath.Join(root, "resources", owner, "styles")
		write(filepath.Join(dir, "notes.qml"), "<qgis/>")
		write(filepath.Join(dir, "notes.resource.yaml"), "apiVersion: resource/v1\nid: style."+owner+"\ntype: style\nname: 说明\nentry: notes.qml\n")
	}
	store := NewStore(filepath.Join(root, "flows/builtin"), filepath.Join(root, "flows/user"), b)
	md := "---\n名称: 引用样例\n编号: reference-flow\n---\n## 资源\n- 样式「说明」：公共资源库\n## 步骤\n### 阅读\n- 做法：阅读\n"
	write(filepath.Join(store.BuiltinDir, "reference-flow/FLOW.md"), md)
	write(filepath.Join(store.UserDir, "reference-flow/FLOW.md"), md)
	refs, err := store.ResourceReferences(library, "style.user")
	if err != nil || len(refs) != 2 {
		t.Fatal(refs, err)
	}
	write(filepath.Join(store.UserDir, "reference-flow/flow.lock"), "apiVersion: flow-lock/v1\nresources:\n  - {type: style, name: 说明, id: style.user, source: bundled, bundled: deps/style.user}\n")
	refs, err = store.ResourceReferences(library, "style.user")
	if err != nil || len(refs) != 1 || refs[0].Source != "builtin" {
		t.Fatal(refs, err)
	}
	write(filepath.Join(store.UserDir, "reference-flow/flow.lock"), "apiVersion: flow-lock/v1\nresources:\n  - {type: style, name: 说明, id: style.builtin, source: library_builtin}\n")
	refs, err = store.ResourceReferences(library, "style.user")
	if err != nil || len(refs) != 1 {
		t.Fatal(refs, err)
	}
	refs, err = store.ResourceReferences(library, "style.builtin")
	if err != nil || len(refs) != 2 || refs[1].Via != "lock" {
		t.Fatal(refs, err)
	}
}
