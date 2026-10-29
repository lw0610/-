package api

import (
	"context"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"image"
	"image/png"
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
	library := resource.NewLibrary("", filepath.Join(root, "user"), bindings.New(filepath.Join(root, "bindings.yaml")))
	source := filepath.Join(root, "sample.qml")
	if err := os.WriteFile(source, []byte("<qgis/>"), 0600); err != nil {
		t.Fatal(err)
	}
	item, err := library.Import(source, "style")
	if err != nil {
		t.Fatal(err)
	}
	a := &API{}
	a.SetResourceLibrary(library)
	router := mux.NewRouter()
	a.SetupRoutes(router)
	call := func(path string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return w
	}
	path := "/api/resources/by-id/" + item.ID + "/preview"
	pending := call(path, 200)
	if pending.Header().Get("X-Preview-Status") != "pending" || !strings.Contains(pending.Body.String(), "连接 QGIS") {
		t.Fatal(pending)
	}
	_, err = library.GeneratePreview(context.Background(), item.ID, func(_ context.Context, _ *resource.ResolvedResource, path string) error {
		f, e := os.Create(path)
		if e != nil {
			return e
		}
		defer f.Close()
		return png.Encode(f, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	ready := call(path, 200)
	if ready.Header().Get("Content-Type") != "image/png" || ready.Header().Get("X-Preview-Status") != "ready" {
		t.Fatal(ready)
	}
	call("/api/resources/doc/missing/preview", 404)
	call(path+"?contract_version=unsupported", 400)
	// User-controlled query paths are never used to choose a file.
	confined := call(path+"?entry=../../outside.png", 200)
	if confined.Body.String() != ready.Body.String() {
		t.Fatal("query selected another file")
	}
	file, _, _ := library.PreviewPath(item.ID)
	outside := filepath.Join(root, "outside.png")
	if err = os.WriteFile(outside, ready.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, file); err != nil {
		t.Fatal(err)
	}
	invalid := call(path, 200)
	if invalid.Header().Get("X-Preview-Status") != "pending" {
		t.Fatal("served symlink preview")
	}
}
