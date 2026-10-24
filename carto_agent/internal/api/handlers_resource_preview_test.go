package api

import (
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/gorilla/mux"
)

func TestResourcePreviewIsConfinedToResolvedLibraryAssets(t *testing.T) {
	root := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "builtin/docs/说明.md"), "说明正文")
	write(filepath.Join(root, "builtin/docs/blocked.html"), "<script>bad</script>")
	write(filepath.Join(root, "builtin/docs/说明书.pdf"), "%PDF-1.4\n")
	write(filepath.Join(root, "builtin/symbols/markers/对照表.csv"), "symbol_type,svg_path\nmarker,marker.svg\n")
	write(filepath.Join(root, "builtin/symbols/markers/marker.svg"), `<svg xmlns="http://www.w3.org/2000/svg"/>`)
	write(filepath.Join(root, "outside.svg"), `<svg/>`)
	a := &API{}
	a.SetResourceLibrary(resource.NewLibrary(filepath.Join(root, "builtin"), filepath.Join(root, "user"), bindings.New(filepath.Join(root, "bindings.yaml"))))
	router := mux.NewRouter()
	a.SetupRoutes(router)
	call := func(path string, want int, header bool) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest("GET", path, nil)
		if header {
			request.Header.Set("X-AutoCarto-Contract-Version", "v1")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
		return response
	}
	response := call("/api/resources/doc/说明/preview", 200, true)
	if response.Body.String() != "说明正文" || !strings.Contains(response.Header().Get("Content-Type"), "text/plain") || !strings.Contains(response.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatal(response)
	}
	response = call("/api/resources/symbols/markers/preview?entry=0", 200, true)
	if response.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatal(response.Header())
	}
	call("/api/resources/symbols/markers/preview?entry=../outside.svg", 400, true)
	call("/api/resources/symbols/markers/preview?entry=-1", 400, true)
	call("/api/resources/symbols/markers/preview?entry=9", 400, true)
	call("/api/resources/doc/missing/preview", 404, true)
	response = call("/api/resources/doc/说明书/preview", 200, true)
	if response.Header().Get("Content-Type") != "application/pdf" {
		t.Fatal(response.Header())
	}
	call("/api/resources/doc/说明/preview", 200, false)
	call("/api/resources/doc/说明/preview?contract_version=unsupported", 400, false)
	// A mapping cannot use a symlink to expose another file.
	if err := os.Remove(filepath.Join(root, "builtin/symbols/markers/marker.svg")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside.svg"), filepath.Join(root, "builtin/symbols/markers/marker.svg")); err != nil {
		t.Fatal(err)
	}
	call("/api/resources/symbols/markers/preview?entry=0", 400, true)
}
