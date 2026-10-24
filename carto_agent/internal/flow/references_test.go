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
		dir := filepath.Join(root, "resources", owner, "docs")
		write(filepath.Join(dir, "notes.md"), "notes")
		write(filepath.Join(dir, "notes.resource.yaml"), "apiVersion: resource/v1\nid: doc."+owner+"\ntype: doc\nname: 说明\nentry: notes.md\n")
	}
	store := NewStore(filepath.Join(root, "flows/builtin"), filepath.Join(root, "flows/user"), b)
	md := "---\n名称: 引用样例\n编号: reference-flow\n---\n## 资源\n- 文档「说明」：公共资源库\n## 步骤\n### 阅读\n- 做法：阅读\n"
	write(filepath.Join(store.BuiltinDir, "reference-flow/FLOW.md"), md)
	write(filepath.Join(store.UserDir, "reference-flow/FLOW.md"), md)
	refs, err := store.ResourceReferences(library, "doc.user")
	if err != nil || len(refs) != 2 {
		t.Fatal(refs, err)
	}
	write(filepath.Join(store.UserDir, "reference-flow/flow.lock"), "apiVersion: flow-lock/v1\nresources:\n  - {type: doc, name: 说明, id: doc.user, source: bundled, bundled: resources/_deps/doc.user}\n")
	refs, err = store.ResourceReferences(library, "doc.user")
	if err != nil || len(refs) != 1 || refs[0].Source != "builtin" {
		t.Fatal(refs, err)
	}
	write(filepath.Join(store.UserDir, "reference-flow/flow.lock"), "apiVersion: flow-lock/v1\nresources:\n  - {type: doc, name: 说明, id: doc.builtin, source: library_builtin}\n")
	refs, err = store.ResourceReferences(library, "doc.user")
	if err != nil || len(refs) != 1 {
		t.Fatal(refs, err)
	}
	refs, err = store.ResourceReferences(library, "doc.builtin")
	if err != nil || len(refs) != 2 || refs[1].Via != "lock" {
		t.Fatal(refs, err)
	}
}
