package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"github.com/gorilla/mux"
)

func TestSessionFilesHTTPRecoversReferencedSharedBoundaryAndExports(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", home)
	project := filepath.Join(t.TempDir(), "map.qgz")
	store, err := sessionfiles.New(project, "files-test")
	if err != nil {
		t.Fatal(err)
	}
	write := func(path string) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	boundary := write(filepath.Join(home, "data", "boundaries", "city.geojson"))
	write(filepath.Join(home, "data", "boundaries", "other-session.geojson"))
	png := write(filepath.Join(store.Dir, "output", "layout.png"))
	pdf := write(filepath.Join(filepath.Dir(project), "layout.pdf"))
	svg := write(filepath.Join(filepath.Dir(project), "layout.svg"))
	outside := write(filepath.Join(home, "config", "private.json"))
	entries := []any{}
	for _, tc := range []struct{ tool, path string }{{"admin_boundary_extract", boundary}, {"export_layout_image", png}, {"export_layout_pdf", pdf}, {"export_layout_svg", svg}, {"export_layout_pdf", outside}} {
		raw, _ := json.Marshal(map[string]any{"success": true, "data": map[string]any{"artifact_paths": []string{tc.path}}})
		entries = append(entries, map[string]any{"type": "tool", "data": map[string]any{"tool": tc.tool, "call_id": "call-" + tc.tool, "result": string(raw) + "\n[文件] " + filepath.Base(tc.path), "result_parts": []any{map[string]any{"type": "text", "text": string(raw)}}}})
	}
	svc := newInMemorySessionService()
	if err := svc.SaveSession(map[string]any{"session_id": "files-test", "workflow_blocks": []any{map[string]any{"agent": "cartographer", "entries": entries}}}, project); err != nil {
		t.Fatal(err)
	}
	a := &API{sessionService: svc}
	router := mux.NewRouter()
	a.SetupRoutes(router)
	var before map[string]sessionfiles.Item
	for attempt := 0; attempt < 2; attempt++ {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions/files-test/files?project_path="+url.QueryEscape(project), nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var payload struct {
			Data struct {
				Items    []sessionfiles.Item `json:"items"`
				Warnings []string            `json:"warnings"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil || w.Code != http.StatusOK || len(payload.Data.Items) != 4 || len(payload.Data.Warnings) != 1 {
			t.Fatalf("recovery endpoint: code=%d body=%s err=%v", w.Code, w.Body.String(), err)
		}
		current := map[string]sessionfiles.Item{}
		for _, item := range payload.Data.Items {
			current[item.Name] = item
			if item.Name != "city.geojson" && item.Group != "deliverable" {
				t.Fatalf("layout export not visible: %+v", item)
			}
			if attempt == 1 && (before[item.Name].ID != item.ID || !before[item.Name].RegisteredAt.Equal(item.RegisteredAt)) {
				t.Fatalf("refresh changed registration: %+v", item)
			}
		}
		before = current
	}
	// The newly recovered identity is usable by the normal content endpoint.
	if w := fileRequest(router, project, before["city.geojson"].ID, "content"); w.Code != http.StatusOK {
		t.Fatalf("recovered boundary is inaccessible: %d %s", w.Code, w.Body.String())
	}
	// A different session sharing the same trusted global root gains no files.
	if err := svc.SaveSession(map[string]any{"session_id": "other-session"}, project); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/sessions/other-session/files?project_path="+url.QueryEscape(project), nil))
	var payload struct {
		Data struct {
			Items []sessionfiles.Item `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil || w.Code != http.StatusOK || len(payload.Data.Items) != 0 {
		t.Fatalf("shared boundary leaked across sessions: %d %s %v", w.Code, w.Body.String(), err)
	}
}
