package taskmaterials

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"github.com/autocarto/carto_agent/internal/toolsession"
)

func materialSession(t *testing.T) (context.Context, *sessionfiles.Store, string) {
	t.Helper()
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "map.qgz")
	ctx := toolsession.WithContext(context.Background(), "materials-test", project, "")
	store, err := sessionfiles.New(project, "materials-test")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := config.ResolveSessionDir(project, "materials-test")
	if err != nil {
		t.Fatal(err)
	}
	return ctx, store, dir
}

func documentFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestShortAttachmentIsRealSourceAndSurvivesChildContext(t *testing.T) {
	ctx, store, dir := materialSession(t)
	body := "1934年11月16日，从何家冲出发。\r\n经桐柏进入枣阳。\n"
	path := documentFile(t, "史料.txt", body)
	if _, err := store.RegisterProvided(path, sessionfiles.Origin{Type: "provided"}); err != nil {
		t.Fatal(err)
	}
	if err := dataservice.SaveBindings(dir, []dataservice.Binding{{Explicit: true, Current: true, Path: path}}); err != nil {
		t.Fatal(err)
	}
	ctx, err := Prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := ContextMessage(ctx)
	if err != nil || msg == nil || !strings.Contains(msg.Content, "何家冲") || msg.Extra[MessageMarker] != true {
		t.Fatalf("missing file input: %+v %v", msg, err)
	}
	// Nested tasks keep the same immutable source, not a model's task description.
	if err := os.WriteFile(path, []byte("later edit"), 0600); err != nil {
		t.Fatal(err)
	}
	child, err := Prepare(context.WithValue(ctx, struct{}{}, "child"))
	if err != nil {
		t.Fatal(err)
	}
	got, found, err := DefaultSource(child)
	if err != nil || !found || got != body {
		t.Fatalf("source changed across delegation: %q %t %v", got, found, err)
	}
	// New user turns read the current file; no process-global stale cache.
	fresh := toolsession.WithContext(context.Background(), "materials-test", toolsession.ProjectPathFromContext(ctx), "")
	got, _, err = DefaultSource(fresh)
	if err != nil || got != "later edit" {
		t.Fatalf("later turn retained stale file: %q %v", got, err)
	}
}

func TestCurrentMaterialsReplaceDefaultWithoutLosingPreviousReferences(t *testing.T) {
	ctx, store, dir := materialSession(t)
	old := documentFile(t, "旧史料.txt", "old source")
	current := documentFile(t, "新史料.txt", "current source")
	for _, p := range []string{old, current} {
		if _, err := store.RegisterProvided(p, sessionfiles.Origin{Type: "provided"}); err != nil {
			t.Fatal(err)
		}
	}
	err := dataservice.SaveBindings(dir, []dataservice.Binding{{Explicit: true, Path: old}, {Explicit: true, Current: true, Path: current}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = Prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := DefaultSource(ctx)
	if err != nil || got != "current source" {
		t.Fatalf("wrong current source %q %v", got, err)
	}
	got, err = ResolveSource(ctx, []string{reference(old, false).ID})
	if err != nil || got != "old source" {
		t.Fatalf("previous explicit source lost %q %v", got, err)
	}
}

func TestFolderListsDocumentsAndResolvesSelectedChildren(t *testing.T) {
	ctx, _, dir := materialSession(t)
	folder := t.TempDir()
	path := filepath.Join(folder, "史料.txt")
	if err := os.WriteFile(path, []byte("从甲地行军至乙地"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := dataservice.SaveBindings(dir, []dataservice.Binding{{Explicit: true, Current: true, Path: folder}}); err != nil {
		t.Fatal(err)
	}
	ctx, err := Prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := ContextMessage(ctx)
	if err != nil || msg == nil || !strings.Contains(msg.Content, "史料.txt") {
		t.Fatalf("folder not enumerated: %+v %v", msg, err)
	}
	if _, found, err := DefaultSource(ctx); !found || err == nil {
		t.Fatalf("folder description accepted as original source: %t %v", found, err)
	}
	got, err := ResolveSource(ctx, []string{reference(path, true).ID})
	if err != nil || got != "从甲地行军至乙地" {
		t.Fatalf("cannot read selected folder document: %q %v", got, err)
	}
	outside := documentFile(t, "unattached.txt", "unrelated")
	if _, err := ResolveSource(ctx, []string{outside}); err == nil {
		t.Fatal("unattached external file accepted")
	}
}

func TestLargeSourceIsReferencedWithoutTruncatingExtraction(t *testing.T) {
	ctx, store, dir := materialSession(t)
	body := strings.Repeat("原始材料保留完整内容\n", 5000)
	path := documentFile(t, "长史料.txt", body)
	if _, err := store.RegisterProvided(path, sessionfiles.Origin{Type: "provided"}); err != nil {
		t.Fatal(err)
	}
	if err := dataservice.SaveBindings(dir, []dataservice.Binding{{Explicit: true, Current: true, Path: path}}); err != nil {
		t.Fatal(err)
	}
	ctx, err := Prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := ContextMessage(ctx)
	if err != nil || strings.Contains(msg.Content, body[:100]) {
		t.Fatalf("large body auto-expanded: %v", err)
	}
	got, _, err := DefaultSource(ctx)
	if err != nil || got != body {
		t.Fatalf("source silently truncated: %d/%d %v", len(got), len(body), err)
	}
}

func TestUnavailableAttachmentKeepsReasonInsteadOfUsingTaskText(t *testing.T) {
	ctx, store, dir := materialSession(t)
	path := documentFile(t, "失效史料.txt", "original")
	if _, err := store.RegisterProvided(path, sessionfiles.Origin{Type: "provided"}); err != nil {
		t.Fatal(err)
	}
	if err := dataservice.SaveBindings(dir, []dataservice.Binding{{Explicit: true, Current: true, Path: path}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	ctx, err := Prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := ContextMessage(ctx)
	if err != nil || !strings.Contains(msg.Content, "unavailable") {
		t.Fatalf("missing attachment hidden: %+v %v", msg, err)
	}
	if got, found, err := DefaultSource(ctx); !found || err == nil || got != "" {
		t.Fatalf("missing source became usable: %q %t %v", got, found, err)
	}
}

func TestAliasesShareAnIDAndHistoricalFolderChildRemainsResolvable(t *testing.T) {
	ctx, store, dir := materialSession(t)
	folder := t.TempDir()
	path := filepath.Join(folder, "史料.txt")
	if err := os.WriteFile(path, []byte("folder original"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "同一史料.txt")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, alias} {
		if _, err := store.RegisterProvided(p, sessionfiles.Origin{Type: "provided"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := dataservice.SaveBindings(dir, []dataservice.Binding{{Explicit: true, Path: path}, {Explicit: true, Current: true, Path: alias}}); err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s := prepared.Value(contextKey{}).(*snapshot)
	if len(s.references) != 1 || !s.references[0].Current {
		t.Fatalf("same source duplicated: %+v", s.references)
	}
	got, err := ResolveSource(prepared, []string{reference(path, true).ID})
	if err != nil || got != "folder original" {
		t.Fatalf("source ID ambiguous: %q %v", got, err)
	}
	// An ID previously shown for a folder child remains valid after another
	// attachment becomes current, even though that old folder is not prelisted.
	if err := dataservice.SaveBindings(dir, []dataservice.Binding{{Explicit: true, Current: true, Path: folder}}); err != nil {
		t.Fatal(err)
	}
	first, err := Prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	oldID := first.Value(contextKey{}).(*snapshot).references[0].Entries[0].ID
	newFile := documentFile(t, "后来上传.txt", "new source")
	if _, err := store.RegisterProvided(newFile, sessionfiles.Origin{Type: "provided"}); err != nil {
		t.Fatal(err)
	}
	if err := dataservice.SaveBindings(dir, []dataservice.Binding{{Explicit: true, Path: folder}, {Explicit: true, Current: true, Path: newFile}}); err != nil {
		t.Fatal(err)
	}
	later, err := Prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err = ResolveSource(later, []string{oldID})
	if err != nil || got != "folder original" {
		t.Fatalf("historical child reference lost: %q %v", got, err)
	}
}
