package flow

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
)

var updateFlowLocks = flag.Bool("update", false, "rewrite committed builtin and acceptance flow locks")

func lockWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func lockLibrary(t *testing.T, root string) *resource.Library {
	t.Helper()
	return resource.NewLibrary(filepath.Join(root, "builtin"), filepath.Join(root, "user"), bindings.New(filepath.Join(root, "bindings.yaml")))
}
func lockStyle(t *testing.T, root, id, name, version, content string) {
	t.Helper()
	lockWrite(t, filepath.Join(root, "styles", name+".qml"), "<qgis><!--"+content+"--></qgis>")
	lockWrite(t, filepath.Join(root, "styles", name+".resource.yaml"), "apiVersion: resource/v1\nid: "+id+"\ntype: style\nname: "+name+"\nversion: "+version+"\nentry: "+name+".qml\n")
}
func lockFlow(t *testing.T, root string, l *resource.Library, location string) (*Store, *StoredFlow) {
	t.Helper()
	s := NewStore(filepath.Join(root, "builtin-flows"), filepath.Join(root, "flows"), l.Bindings)
	raw := strings.Replace(testFlow, "## 步骤", "## 使用的资源\n- 样式「说明」："+location+"\n## 步骤", 1)
	item, err := s.Create(raw, "test-flow")
	if err != nil {
		t.Fatal(err)
	}
	return s, item
}
func lockResolve(t *testing.T, s *Store, l *resource.Library) (*resource.Lock, *resource.ResolvedResource) {
	t.Helper()
	_, _, lock, rs, err := s.ResolveResources(context.Background(), "test-flow", l)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 {
		t.Fatalf("resources=%+v", rs)
	}
	return lock, rs[0]
}
func lockCode(r *resource.ResolvedResource, code string) string {
	for _, d := range r.Problems {
		if d.Code == code {
			return d.Level
		}
	}
	return ""
}

func TestFlowLockIdentityDriftAndRepairs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	l := lockLibrary(t, root)
	lockStyle(t, l.BuiltinDir, "style.notes", "说明", "1.0.0", "one")
	s, item := lockFlow(t, root, l, "公共资源库")
	lock, r := lockResolve(t, s, l)
	original := lock.Resources[0]
	lockStyle(t, l.UserDir, "style.other", "说明", "1.0.0", "other")
	_, r = lockResolve(t, s, l)
	if r.ID != "style.notes" || resource.HasErrors(r.Problems) {
		t.Fatalf("same name changed identity %+v", r)
	}
	lockStyle(t, l.BuiltinDir, "style.notes", "说明", "1.0.1", "updated")
	lock, r = lockResolve(t, s, l)
	if lockCode(r, "FLOW_RESOURCE_CHANGED") != "info" || lock.Resources[0] != original {
		t.Fatalf("minor drift %+v %+v", lock, r)
	}
	lockStyle(t, l.BuiltinDir, "style.notes", "说明", "2.0.0", "major")
	_, r = lockResolve(t, s, l)
	if lockCode(r, "FLOW_RESOURCE_CHANGED") != "warning" {
		t.Fatal(r.Problems)
	}
	if _, err := s.RepairResource(context.Background(), "test-flow", "update_lock", r.Key, "", l); err != nil {
		t.Fatal(err)
	}
	_, r = lockResolve(t, s, l)
	if lockCode(r, "FLOW_RESOURCE_CHANGED") != "" {
		t.Fatal(r.Problems)
	}
	if _, err := s.RepairResource(context.Background(), "test-flow", "choose_resource", r.Key, "style.other", l); err != nil {
		t.Fatal(err)
	}
	_, r = lockResolve(t, s, l)
	if r.ID != "style.other" {
		t.Fatal(r)
	}
	if _, err := s.Save("test-flow", strings.Replace(item.FlowMD, "- 样式「说明」：公共资源库\n", "", 1)); err != nil {
		t.Fatal(err)
	}
	cleaned, err := ReadLock(item.Dir)
	if err != nil || len(cleaned.Resources) != 0 {
		t.Fatalf("stale lock %+v %v", cleaned, err)
	}
}
func TestFlowDependencyPortableRoundTrip(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	l := lockLibrary(t, root)
	lockStyle(t, l.BuiltinDir, "style.notes", "说明", "1.0.0", "portable bytes")
	s, _ := lockFlow(t, root, l, "公共资源库/style.notes")
	original, _ := lockResolve(t, s, l)
	zip := filepath.Join(root, "flow.zip")
	if _, err := s.ExportWithOptions("test-flow", zip, ExportOptions{IncludeLibraryDeps: true, Library: l}); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"missing", "same", "different"} {
		t.Run(scenario, func(t *testing.T) {
			destination := t.TempDir()
			local := lockLibrary(t, destination)
			if scenario != "missing" {
				content := "portable bytes"
				if scenario == "different" {
					content = "local different"
				}
				lockStyle(t, local.BuiltinDir, "style.notes", "说明", "1.0.0", content)
			}
			imported := NewStore(filepath.Join(destination, "builtin-flows"), filepath.Join(destination, "flows"), local.Bindings)
			item, err := imported.Import(zip, ImportOptions{}, local)
			if err != nil {
				t.Fatal(err)
			}
			lock, r := lockResolve(t, imported, local)
			if resource.HasErrors(r.Problems) {
				t.Fatal(r.Problems)
			}
			raw, err := os.ReadFile(r.Entry)
			if err != nil || string(raw) != "<qgis><!--portable bytes--></qgis>" {
				t.Fatalf("bundle content %q %v", raw, err)
			}
			if scenario == "same" {
				if r.Owner != "library_builtin" || lock.Resources[0].Bundled != "" {
					t.Fatal(r)
				}
				if _, err = os.Stat(filepath.Join(item.Dir, "resources/_deps/style.notes")); !os.IsNotExist(err) {
					t.Fatal("dedup did not remove bundle")
				}
			} else {
				if r.Owner != "bundled" || lockCode(r, "FLOW_RESOURCE_BUNDLED_DIFFERS") == "" {
					t.Fatal(r)
				}
				entries, _ := local.List("user")
				if len(entries) != 0 {
					t.Fatal("import wrote user library")
				}
				if scenario == "different" {
					if _, err = imported.RepairResource(context.Background(), "test-flow", "use_library", r.Key, "", local); err != nil {
						t.Fatal(err)
					}
					_, r = lockResolve(t, imported, local)
					if r.Owner != "library_builtin" {
						t.Fatal(r)
					}
				} else {
					if _, err = imported.RepairResource(context.Background(), "test-flow", "install_resource", r.Key, "", local); err != nil {
						t.Fatal(err)
					}
					_, r = lockResolve(t, imported, local)
					if r.Owner != "library_user" || r.ID == original.Resources[0].ID {
						t.Fatal(r)
					}
				}
			}
		})
	}
	noDeps := filepath.Join(root, "without-deps.zip")
	if _, err := s.ExportWithOptions("test-flow", noDeps, ExportOptions{Library: l}); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	empty := lockLibrary(t, dest)
	imported := NewStore("", filepath.Join(dest, "flows"), empty.Bindings)
	if _, err := imported.Import(noDeps, ImportOptions{}, empty); err != nil {
		t.Fatal(err)
	}
	_, r := lockResolve(t, imported, empty)
	if lockCode(r, "FLOW_RESOURCE_LOCK_MISSING") != "error" {
		t.Fatal(r.Problems)
	}
}
func TestLegacyFlowMigrationMovesVisualsAndPreservesDataBackup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	library := lockLibrary(t, root)
	store, item := lockFlow(t, root, library, "resources/styles/说明.qml")
	lockWrite(t, filepath.Join(item.Dir, "resources/styles/说明.qml"), "<qgis/>")
	raw := strings.Replace(item.FlowMD, "## 步骤", "- 底图数据「清单」：resources/data/list.csv\n## 步骤", 1)
	lockWrite(t, filepath.Join(item.Dir, "FLOW.md"), raw)
	lockWrite(t, filepath.Join(item.Dir, "resources/data/list.csv"), "名称,路径,类型,类别,说明\n河流,private.shp,矢量,河流线,真实河流\n")
	legacyLock, _ := ReadLock(item.Dir)
	legacyLock.DocumentFormat = ""
	if err := writeLock(item.Dir, legacyLock); err != nil {
		t.Fatal(err)
	}
	guides := NewGuideStore(store, filepath.Join(root, "guides.json"))
	store.SetLibrary(library)
	if _, err := guides.MigrateLegacy(); err != nil {
		t.Fatal(err)
	}
	got, err := guides.Resolve(context.Background(), item.Meta.ID, library)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Doc.Body, "河流") || strings.Contains(got.Doc.Body, "private.shp") || len(got.Doc.Resources) != 1 {
		t.Fatal(got.Doc)
	}
	if resource.HasErrors(got.Scope.Linked[0].Problems) {
		t.Fatal(got.Scope.Linked[0].Problems)
	}
	for _, path := range []string{"FLOW.md.bak", ".resources-v5-backup/data/list.csv", "conversion-report.json"} {
		if _, err = os.Stat(filepath.Join(item.Dir, path)); err != nil {
			t.Fatal(path, err)
		}
	}
	if _, err = os.Stat(filepath.Join(item.Dir, "resources")); !os.IsNotExist(err) {
		t.Fatal("legacy resources remained active")
	}
	before, _ := os.ReadFile(filepath.Join(item.Dir, "FLOW.md"))
	if _, err = guides.MigrateLegacy(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(item.Dir, "FLOW.md"))
	if string(before) != string(after) {
		t.Fatal("second migration changed document")
	}
}
func TestBuiltinFlowLocksUpToDate(t *testing.T) {
	project := compilerProject(t)
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	l := resource.NewLibrary(filepath.Join(project, "autocarto/resources/system"), filepath.Join(t.TempDir(), "resources"), nil)
	for _, root := range []string{filepath.Join(project, "autocarto/flows/system")} {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dir := filepath.Join(root, entry.Name())
			raw, err := os.ReadFile(filepath.Join(dir, "FLOW.md"))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			expected := &resource.Lock{APIVersion: resource.LockAPIVersion, DocumentFormat: documentFormatV6, Resources: []resource.LockEntry{}}
			for _, d := range resourceDocument(string(raw), l).Resources {
				if !publicDeclaration(d) {
					continue
				}
				r := l.Resolve(context.Background(), resource.Ref{Type: d.Type, Name: d.Label, Location: d.Location}, resource.Scope{FlowDir: dir})
				if r.ID == "" || r.Digest == "" {
					t.Fatalf("%s %s: %+v", dir, d.Label, r.Problems)
				}
				expected.Resources = append(expected.Resources, resource.LockResource(d.Type, d.Label, r))
			}
			if *updateFlowLocks {
				if err = writeLock(dir, expected); err != nil {
					t.Fatal(err)
				}
			}
			got, err := ReadLock(dir)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, expected) {
				t.Errorf("%s flow.lock is stale; run go test ./internal/flow -run TestBuiltinFlowLocksUpToDate -args -update", dir)
			}
		}
	}
}

func TestFlowBundledFolderAndLegacyPortability(t *testing.T) {
	project := compilerProject(t)
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", root)
	library := resource.NewLibrary(filepath.Join(project, "autocarto/resources/system"), filepath.Join(root, "resources"), nil)
	store := NewStore(filepath.Join(project, "autocarto/flows/system"), filepath.Join(root, "flows"), nil)
	archive := filepath.Join(root, "traffic.zip")
	if _, err := store.ExportWithOptions("traffic-route", archive, ExportOptions{IncludeLibraryDeps: true, Library: library}); err != nil {
		t.Fatal(err)
	}
	empty := lockLibrary(t, t.TempDir())
	dest := NewStore("", filepath.Join(root, "imported"), nil)
	if _, err := dest.Import(archive, ImportOptions{}, empty); err != nil {
		t.Fatal(err)
	}
	_, _, lock, rs, err := dest.ResolveResources(context.Background(), "traffic-route", empty)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Resources) != 7 {
		t.Fatal(lock)
	}
	if len(rs) != 7 {
		t.Fatalf("expected only seven visual dependencies: %+v", rs)
	}
	for _, r := range rs {
		if resource.HasErrors(r.Problems) || r.Owner != "bundled" || (r.Type != "style" && r.Type != "layout") {
			t.Fatal(r)
		}
	}
	// Existing public files without manifests remain portable through their lock
	// identity. Installing the bundle supplies a new formal manifest.
	legacyRoot := t.TempDir()
	legacyLib := lockLibrary(t, legacyRoot)
	lockWrite(t, filepath.Join(legacyLib.UserDir, "styles/说明.qml"), "<qgis><!--legacy portable--></qgis>")
	legacyStore, _ := lockFlow(t, legacyRoot, legacyLib, "公共资源库")
	lockResolve(t, legacyStore, legacyLib)
	archive = filepath.Join(legacyRoot, "legacy.zip")
	if _, err = legacyStore.ExportWithOptions("test-flow", archive, ExportOptions{IncludeLibraryDeps: true, Library: legacyLib}); err != nil {
		t.Fatal(err)
	}
	if _, err = dest.Import(archive, ImportOptions{}, empty); err != nil {
		t.Fatal(err)
	}
	_, r := lockResolve(t, dest, empty)
	if resource.HasErrors(r.Problems) || r.Owner != "bundled" {
		t.Fatal(r)
	}
}
func TestBuiltinLockImmutability(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	l := lockLibrary(t, root)
	s, item := lockFlow(t, root, l, "公共资源库")
	lockWrite(t, filepath.Join(l.UserDir, "styles/共享.qml"), "<qgis><!--legacy--></qgis>")
	old := l.Resolve(context.Background(), resource.Ref{Type: "style", Name: "共享", Location: "公共资源库"}, resource.Scope{})
	builtin := filepath.Join(s.BuiltinDir, "builtin-flow")
	lockWrite(t, filepath.Join(builtin, "FLOW.md"), item.FlowMD)
	frozen := &resource.Lock{APIVersion: resource.LockAPIVersion, Resources: []resource.LockEntry{resource.LockResource("style", "说明", old)}}
	if err := writeLock(builtin, frozen); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(builtin, "flow.lock"))
	updated, err := l.Upgrade(old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MigrateResourceIdentity(old.ID, updated); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(builtin, "flow.lock"))
	if string(before) != string(after) {
		t.Fatal("builtin lock changed")
	}
	_, _, _, resolved, err := s.ResolveResources(context.Background(), "builtin-flow", l)
	if err != nil {
		t.Fatal(err)
	}
	if lockCode(resolved[0], "FLOW_RESOURCE_LOCK_MISSING") != "error" {
		t.Fatal(resolved)
	}
	after, _ = os.ReadFile(filepath.Join(builtin, "flow.lock"))
	if string(before) != string(after) {
		t.Fatal("builtin link wrote lock")
	}
}
func TestFlowLockRejectsUnsafeBundleAndTampering(t *testing.T) {
	root := t.TempDir()
	l := lockLibrary(t, root)
	lockStyle(t, l.BuiltinDir, "style.notes", "说明", "1.0.0", "original")
	s, _ := lockFlow(t, root, l, "公共资源库")
	lockResolve(t, s, l)
	zip := filepath.Join(root, "flow.zip")
	if _, err := s.ExportWithOptions("test-flow", zip, ExportOptions{IncludeLibraryDeps: true, Library: l}); err != nil {
		t.Fatal(err)
	}
	unpack := filepath.Join(root, "unpack")
	if err := extractZIP(zip, unpack); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(unpack, "test-flow")
	lockWrite(t, filepath.Join(pkg, "deps/style.notes/说明.qml"), "tampered")
	dest := NewStore("", filepath.Join(root, "dest"), nil)
	if _, err := dest.Import(pkg, ImportOptions{}, lockLibrary(t, t.TempDir())); err == nil {
		t.Fatal("tampered package accepted")
	}
	if _, err := os.Stat(filepath.Join(dest.UserDir, "test-flow")); !os.IsNotExist(err) {
		t.Fatal("failed import was published")
	}
	lock, err := ReadLock(pkg)
	if err != nil {
		t.Fatal(err)
	}
	lock.Resources[0].Bundled = "resources/_deps/../../../outside"
	if err = writeLock(pkg, lock); err == nil {
		t.Fatal("unsafe path accepted")
	}
}

func TestFlowBundleKeepsIdentityAgainstLocalOverride(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	source := lockLibrary(t, root)
	lockStyle(t, source.BuiltinDir, "style.notes", "说明", "1.0.0", "base")
	store, _ := lockFlow(t, root, source, "公共资源库")
	lockResolve(t, store, source)
	zip := filepath.Join(root, "flow.zip")
	if _, err := store.ExportWithOptions("test-flow", zip, ExportOptions{IncludeLibraryDeps: true, Library: source}); err != nil {
		t.Fatal(err)
	}
	localRoot := t.TempDir()
	local := lockLibrary(t, localRoot)
	lockStyle(t, local.BuiltinDir, "style.notes", "说明", "1.0.0", "base")
	override, err := local.Copy("style.notes", true, "覆盖说明")
	if err != nil {
		t.Fatal(err)
	}
	dest := NewStore("", filepath.Join(localRoot, "flows"), nil)
	if _, err = dest.Import(zip, ImportOptions{}, local); err != nil {
		t.Fatal(err)
	}
	_, r := lockResolve(t, dest, local)
	if r.Owner != "bundled" || r.ID == override.ID {
		t.Fatalf("override silently replaced bundled snapshot: %+v", r)
	}
}
func TestLegacyStyleFolderMigratesToIndividualStyles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	library := lockLibrary(t, root)
	store, item := lockFlow(t, root, library, "resources/styles/说明")
	lockWrite(t, filepath.Join(item.Dir, "resources/styles/说明/对照表.csv"), "图层,样式文件\nroad,line.qml\n")
	lockWrite(t, filepath.Join(item.Dir, "resources/styles/说明/line.qml"), "<qgis><layerGeometryType>1</layerGeometryType></qgis>")
	lockWrite(t, filepath.Join(item.Dir, "FLOW.md"), strings.Replace(item.FlowMD, "样式「说明」", "样式集「说明」", 1))
	store.SetLibrary(library)
	legacyLock, _ := ReadLock(item.Dir)
	legacyLock.DocumentFormat = ""
	if err := writeLock(item.Dir, legacyLock); err != nil {
		t.Fatal(err)
	}
	guides := NewGuideStore(store, filepath.Join(root, "guides.json"))
	if _, err := guides.MigrateLegacy(); err != nil {
		t.Fatal(err)
	}
	got, err := guides.Resolve(context.Background(), item.Meta.ID, library)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Doc.Resources) != 1 || got.Doc.Resources[0].Type != "style" || resource.HasErrors(got.Scope.Linked[0].Problems) {
		t.Fatal(got)
	}
	archive := filepath.Join(root, "migrated.zip")
	if _, err = store.ExportWithOptions(item.Meta.ID, archive, ExportOptions{IncludeLibraryDeps: true, Library: library}); err != nil {
		t.Fatal(err)
	}
	empty := lockLibrary(t, t.TempDir())
	dest := NewGuideStore(NewStore("", filepath.Join(t.TempDir(), "flows"), nil), filepath.Join(t.TempDir(), "guides.json"))
	if _, err = dest.Import(archive, ImportOptions{}, empty); err != nil {
		t.Fatal(err)
	}
	linked, err := dest.Resolve(context.Background(), item.Meta.ID, empty)
	if err != nil || len(linked.Scope.Linked) != 1 || resource.HasErrors(linked.Scope.Linked[0].Problems) {
		t.Fatal(linked, err)
	}
}

func TestExportReusesEffectiveOverrideAndDeduplicatesReferences(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	library := lockLibrary(t, root)
	lockStyle(t, library.BuiltinDir, "style.notes", "说明", "1.0.0", "base")
	store, item := lockFlow(t, root, library, "公共资源库/style.notes")
	raw := strings.Replace(item.FlowMD, "## 步骤", "- 样式「说明」：公共资源库/style.notes\n## 步骤", 1)
	if _, err := store.Save("test-flow", raw); err != nil {
		t.Fatal(err)
	}
	_, _, before, _, err := store.ResolveResources(context.Background(), "test-flow", library)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Resources) != 1 {
		t.Fatal(before)
	}
	override, err := library.Copy("style.notes", true, "覆盖版本")
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "alias.zip")
	if _, err = store.ExportWithOptions("test-flow", archive, ExportOptions{IncludeLibraryDeps: true, Library: library}); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	empty := lockLibrary(t, destination)
	dest := NewStore("", filepath.Join(destination, "flows"), nil)
	imported, err := dest.Import(archive, ImportOptions{}, empty)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := ReadLock(imported.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range lock.Resources {
		if e.ID != override.ID || e.Source != "bundled" {
			t.Fatal(lock)
		}
	}
	dirs, err := os.ReadDir(filepath.Join(imported.Dir, "deps"))
	if err != nil || len(dirs) != 1 {
		t.Fatal(dirs, err)
	}
	same := NewStore("", filepath.Join(t.TempDir(), "flows"), nil)
	deduplicated, err := same.Import(archive, ImportOptions{}, library)
	if err != nil {
		t.Fatal(err)
	}
	sameLock, err := ReadLock(deduplicated.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range sameLock.Resources {
		if e.Bundled != "" || e.Source != "library_user" {
			t.Fatal(sameLock)
		}
	}
	unchanged, err := ReadLock(item.Dir)
	if err != nil || !reflect.DeepEqual(before, unchanged) {
		t.Fatal("export mutated source lock", unchanged, err)
	}
	if _, err = store.RepairResource(context.Background(), "test-flow", "update_lock", "library:"+override.ID, "", library); err != nil {
		t.Fatal(err)
	}
	repaired, err := ReadLock(item.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range repaired.Resources {
		if e.ID != override.ID {
			t.Fatalf("alias lock not updated: %+v", repaired)
		}
	}

}
func TestResourceIdentityMigrationOnlyRewritesExactLocations(t *testing.T) {
	oldID := "legacy.doc.0123456789"
	newID := "doc.replacement"
	raw := "---\n编号: migration-flow\n名称: 迁移\n---\n## 使用的资源\n- 样式「精确引用」：公共资源库/" + oldID + "\n- 样式「前缀相邻」：公共资源库/" + oldID + "-next\n## 步骤\n### 阅读\n- 做法：请对比公共资源库/" + oldID + " 和原文。\n"
	expected := strings.Replace(raw, "样式「精确引用」：公共资源库/"+oldID, "样式「精确引用」：公共资源库/"+newID, 1)
	if got := rewritePublicResourceID(raw, oldID, newID); got != expected {
		t.Fatalf("rewrote user text: %s", got)
	}
	root := t.TempDir()
	store := NewStore("", filepath.Join(root, "flows"), nil)
	if _, err := store.Create(raw, "migration-flow"); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateResourceIdentity(oldID, &resource.ResolvedResource{ID: newID, Type: "doc", Version: "1.0.0", Digest: "sha256:" + strings.Repeat("0", 64), Owner: "library_user"}); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Get("migration-flow")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored.FlowMD, "公共资源库/"+newID+"-next") || !strings.Contains(stored.FlowMD, "做法：请对比公共资源库/"+oldID) {
		t.Fatal(stored.FlowMD)
	}
}
