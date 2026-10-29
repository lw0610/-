package sessionfiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func toolEntry(name, result string, parts []any) map[string]any {
	return map[string]any{"type": "tool", "data": map[string]any{"tool": name, "call_id": "call-" + name, "result": result, "result_parts": parts}}
}

func TestRecoverOnlyThisSessionsToolArtifacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", home)
	project := filepath.Join(t.TempDir(), "map.qgz")
	store, err := New(project, "recovery")
	if err != nil {
		t.Fatal(err)
	}
	boundary := put(t, filepath.Join(home, "data", "boundaries", "city.geojson"), "city")
	provenance := put(t, filepath.Join(home, "data", "boundaries", "city.provenance.json"), "source")
	provenance, _ = filepath.EvalSymlinks(provenance)
	put(t, filepath.Join(home, "data", "boundaries", "unrelated.geojson"), "other session")
	input := put(t, filepath.Join(filepath.Dir(project), "input.geojson"), "input")
	oldFailedOutput := put(t, filepath.Join(filepath.Dir(project), "failed-export.pdf"), "old output")
	outside := put(t, filepath.Join(t.TempDir(), "external.pdf"), "external export")
	png := put(t, filepath.Join(store.Dir, "output", "layout.png"), "png")
	pdf := put(t, filepath.Join(filepath.Dir(project), "layout.pdf"), "pdf")
	svg := put(t, filepath.Join(store.Dir, "exports", "layout.svg"), "svg")
	preview := put(t, filepath.Join(store.Dir, "temp", "previews", "preview.png"), "preview")
	raw, _ := json.Marshal(map[string]any{"success": true, "data": map[string]any{"artifact_paths": []string{boundary, provenance}}, "source_record": map[string]any{"path": input}})
	entries := []any{
		toolEntry("admin_boundary_extract", string(raw)+"\n[文件] city.geojson", []any{map[string]any{"type": "text", "text": string(raw)}}),
		toolEntry("export_layout_image", `{"output":`+quoted(png)+`}`, nil),
		toolEntry("export_layout_pdf", `{"output_path":`+quoted(pdf)+`}`, nil),
		toolEntry("export_layout_svg", `{"svg_path":`+quoted(svg)+`}`, nil),
		toolEntry("data_load_vector", `{"path":`+quoted(input)+`}`, nil),
		toolEntry("export_layout_pdf", `{"success":false,"output":`+quoted(oldFailedOutput)+`}`, nil),
		toolEntry("export_layout_pdf", `{"output":`+quoted(outside)+`}`, nil),
		map[string]any{"type": "thinking", "data": map[string]any{"tool": "export", "result": `{"output":` + quoted(input) + `}`}},
		map[string]any{"type": "tool", "data": map[string]any{"tool": "export", "tool_args": map[string]any{"output": input}}},
	}
	session := map[string]any{"workflow_blocks": []any{map[string]any{"agent": "data_engineer", "entries": entries}}, "messages": []any{map[string]any{"role": "user", "content": `{"output":` + quoted(input) + `}`}}}
	failures := store.RecoverToolArtifacts(session)
	if len(failures) != 0 {
		t.Fatalf("local artifact recovery failed: %v", failures)
	}
	items, err := store.List(false)
	if err != nil || len(items) != 5 {
		t.Fatalf("missing artifacts or unrelated inputs exposed: %+v %v", items, err)
	}
	for _, item := range items {
		if item.Name == "city.geojson" {
			if item.Group != "data" || len(item.Members) != 1 || item.Members[0] != provenance || item.Origin.CallID != "call-admin_boundary_extract" {
				t.Fatalf("boundary provenance lost: %+v", item)
			}
		} else if item.Group != "deliverable" {
			t.Fatalf("export hidden: %+v", item)
		}
		if item.Path == preview {
			t.Fatal("preview exposed as deliverable")
		}
	}
	// Recovery is additive only: an existing replacement's identity/provenance
	// must not be rolled back to the historical export result on refresh.
	updated, err := store.Register(png, Origin{Type: "tool", Tool: "replace", Label: "replacement"})
	if err != nil {
		t.Fatal(err)
	}
	store.RecoverToolArtifacts(session)
	got, err := store.Get(updated.ID)
	if err != nil || got.Origin.Label != "replacement" || !got.RegisteredAt.Equal(updated.RegisteredAt) {
		t.Fatalf("history overwrote newer registration: %+v %v", got, err)
	}
	if _, err := store.Register(outside, Origin{}); err != nil {
		t.Fatalf("external output registration failed: %v", err)
	}
	link := filepath.Join(home, "data", "boundaries", "escape.geojson")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := store.Register(link, Origin{}); err != nil {
			t.Fatalf("local output symlink registration failed: %v", err)
		}
	}
}

func TestRecoverCommandOutputIsNotAnArtifact(t *testing.T) {
	store, err := NewAt("command-output", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	input := put(t, filepath.Join(t.TempDir(), "source.geojson"), "input")
	artifact := put(t, filepath.Join(store.Dir, "output", "result.geojson"), "output")
	entries := []any{}
	for _, output := range []string{input, input + "\n" + artifact + "\n2 files found"} {
		raw, _ := json.Marshal(map[string]any{"success": true, "exit_code": 0, "output": output})
		entries = append(entries, toolEntry("shell", string(raw), nil))
	}
	raw, _ := json.Marshal(map[string]any{"success": true, "exit_code": 0, "output": "created output", "artifact_paths": []string{artifact}})
	entries = append(entries, toolEntry("shell", string(raw), nil))
	if failures := store.RecoverToolArtifacts(map[string]any{"workflow_blocks": []any{map[string]any{"entries": entries}}}); len(failures) != 0 {
		t.Fatalf("command stdout caused artifact warnings: %v", failures)
	}
	items, err := store.List(true)
	if err != nil || len(items) != 1 || items[0].Name != "result.geojson" {
		t.Fatalf("only explicit command artifacts should be recovered: %+v %v", items, err)
	}
}

func TestRecoverBusinessNameOnlyFromMatchingReceipt(t *testing.T) {
	store, _ := NewAt("legacy-name", t.TempDir())
	path := put(t, filepath.Join(store.Dir, "output", "boundary_county_420100.geojson"), "fixture")
	before, err := store.Register(path, Origin{Type: "tool", Tool: "admin_boundary_extract", CallID: "call-admin_boundary_extract", Label: "提取行政区边界"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"success": true, "output": path, "data": map[string]any{"source": path, "layer_name": "武汉市区县边界"}})
	session := map[string]any{"workflow_blocks": []any{map[string]any{"entries": []any{toolEntry("admin_boundary_extract", string(raw), nil)}}}}
	if failures := store.RecoverToolArtifacts(session); len(failures) != 0 {
		t.Fatal(failures)
	}
	got, err := store.Get(before.ID)
	if err != nil || got.Name != "武汉市区县边界.geojson" || got.Path != before.Path || got.Origin.Label != before.Origin.Label || !got.RegisteredAt.Equal(before.RegisteredAt) {
		t.Fatalf("name recovery changed identity or provenance: %+v %v", got, err)
	}
	// A later replacement's name and provenance belong to that new call.
	replacement, err := store.Register(path, Origin{Type: "tool", Tool: "replace", CallID: "call-replacement", Name: "用户修订区界"})
	if err != nil {
		t.Fatal(err)
	}
	store.RecoverToolArtifacts(session)
	got, err = store.Get(replacement.ID)
	if err != nil || got.Name != replacement.Name || got.Origin != replacement.Origin || !got.RegisteredAt.Equal(replacement.RegisteredAt) {
		t.Fatalf("historical receipt overwrote replacement: %+v %v", got, err)
	}
}

func quoted(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func TestResultPathsDeduplicatesBridgePartsAndExcludesSourceMetadata(t *testing.T) {
	raw := `{"output":"/tmp/layout.pdf","data":{"artifact_paths":["/tmp/layout.pdf"]},"source_record":{"path":"/tmp/input.geojson"}}`
	parts := []map[string]any{
		{"type": "text", "text": raw},
		{"type": "file_url", "file": map[string]any{"extra": map[string]any{"path": "/tmp/layout.pdf"}}},
	}
	paths := ResultPaths(raw+"\n[文件] layout.pdf", parts)
	if len(paths) != 1 || paths[0] != "/tmp/layout.pdf" {
		t.Fatal(paths)
	}
}

func TestResultPathsRejectFailedEnvelopesButKeepSuccessfulPartial(t *testing.T) {
	filePart := map[string]any{"type": "file_url", "file": map[string]any{"extra": map[string]any{"path": "/tmp/old.pdf"}}}
	for _, raw := range []string{
		`{"success":false,"output":"/tmp/old.pdf"}`,
		`{"success":true,"result":{"success":false,"output":"/tmp/old.pdf"}}`,
		`{"success":false,"result":{"success":true,"output":"/tmp/old.pdf"}}`,
		`{"data":{"success":false,"output":"/tmp/old.pdf"}}`,
		`{"error":"export failed","output":"/tmp/old.pdf"}`,
	} {
		if paths := ArtifactPaths(raw); len(paths) != 0 {
			t.Fatalf("failed output accepted: %s %v", raw, paths)
		}
		if paths := ResultPaths(raw, []map[string]any{filePart}); len(paths) != 0 {
			t.Fatalf("failed envelope bypassed via file part: %v", paths)
		}
		parts := []map[string]any{{"type": "text", "text": raw}, filePart}
		if paths := ResultPaths(raw+"\n[文件] old.pdf", parts); len(paths) != 0 {
			t.Fatalf("failed multipart result accepted: %v", paths)
		}
	}
	value := map[string]any{"success": true, "partial": true, "warnings": []string{"one extent unavailable"}, "artifact_paths": []string{"/tmp/partial.geojson"}}
	if paths := ArtifactPaths(value); len(paths) != 1 || paths[0] != "/tmp/partial.geojson" {
		t.Fatalf("successful partial Go result lost: %v", paths)
	}
}

func TestRecoverSkipsDataFindCandidateInputs(t *testing.T) {
	project := t.TempDir()
	store, err := NewAt("preview-inputs", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inside := put(t, filepath.Join(project, "roads.geojson"), "existing input")
	outside := put(t, filepath.Join(t.TempDir(), "省界.qml"), "existing style")
	// A data_find response carries existing input candidates, not new artifacts.
	raw, _ := json.Marshal(map[string]any{"find_id": "df-fixture", "rows": []any{map[string]any{"row": "roads", "candidates": []any{map[string]any{"path": inside}, map[string]any{"path": outside}}}}})
	session := map[string]any{"workflow_blocks": []any{map[string]any{"entries": []any{toolEntry("data_find", string(raw), nil)}}}}
	if failures := store.RecoverToolArtifacts(session); len(failures) != 0 {
		t.Fatalf("preview inputs caused registration warnings: %v", failures)
	}
	items, err := store.List(true)
	if err != nil || len(items) != 0 {
		t.Fatalf("preview input registered as an artifact: %+v %v", items, err)
	}
}

func TestRecoverSkipsFileReadAndDirectoryInputs(t *testing.T) {
	project := t.TempDir()
	store, err := NewAt("file-read-inputs", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	input := put(t, filepath.Join(project, "source.csv"), "region,value\nA,10\n")
	outside := put(t, filepath.Join(t.TempDir(), "bound.pdf"), "source")
	fileResult, _ := json.Marshal(map[string]any{"success": true, "path": input})
	outsideResult, _ := json.Marshal(map[string]any{"success": true, "path": outside})
	directoryResult, _ := json.Marshal(map[string]any{"success": true, "path": project, "entries": []any{map[string]any{"path": input}}})
	entries := []any{
		toolEntry("file_read", string(fileResult), nil),
		toolEntry("file_read", string(outsideResult), []any{map[string]any{"type": "file_url", "file": map[string]any{"extra": map[string]any{"path": outside}}}}),
		toolEntry("file_list", string(directoryResult), nil),
	}
	if failures := store.RecoverToolArtifacts(map[string]any{"workflow_blocks": []any{map[string]any{"entries": entries}}}); len(failures) != 0 {
		t.Fatalf("read-only file results caused artifact warnings: %v", failures)
	}
	items, err := store.List(true)
	if err != nil || len(items) != 0 {
		t.Fatalf("read-only file results were recovered as outputs: %+v %v", items, err)
	}
}
