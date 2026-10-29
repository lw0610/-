package api

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestResourceLayerPreviewUsesSelectedInstanceAndTemporaryImage(t *testing.T) {
	for _, outcome := range []string{"success", "renderer_error", "invalid_png"} {
		t.Run(outcome, func(t *testing.T) {
			registry, provider, conn, cleanup := setupInstanceActionBridge(t, "preview-instance", []map[string]any{
				{"name": "resource_preview", "inputSchema": map[string]any{"type": "object"}},
			})
			defer cleanup()
			root := t.TempDir()
			library := resource.NewLibrary("", filepath.Join(root, "resources"), nil)
			source := filepath.Join(root, "river.qml")
			if err := os.WriteFile(source, []byte("<qgis><layerGeometryType>1</layerGeometryType></qgis>"), 0600); err != nil {
				t.Fatal(err)
			}
			item, err := library.Import(source, "style")
			if err != nil {
				t.Fatal(err)
			}
			_, err = library.GeneratePreview(context.Background(), item.ID, func(_ context.Context, _ *resource.ResolvedResource, path string) error {
				var raw bytes.Buffer
				if err := png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
					return err
				}
				return os.WriteFile(path, raw.Bytes(), 0600)
			}, false)
			if err != nil {
				t.Fatal(err)
			}
			defaultPath, _, _ := library.PreviewPath(item.ID)
			defaultBefore, _ := os.ReadFile(defaultPath)
			a := NewAPI(nil, nil, nil, nil, "", registry)
			a.SetResourceLibrary(library)
			a.SetLocalBridgeProvider(provider)
			router := mux.NewRouter()
			a.SetupRoutes(router)
			type observedCall struct {
				tool   any
				params map[string]any
				err    error
			}
			calls := make(chan observedCall, 1)
			go func() {
				msg := readInstanceActionWSMessage(t, conn)
				params := msg.Data["params"].(map[string]any)
				var writeErr error
				if outcome != "renderer_error" {
					var raw bytes.Buffer
					if outcome == "success" {
						writeErr = png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 20, 10)))
					} else {
						raw.WriteString("not a PNG")
					}
					if writeErr == nil {
						writeErr = os.WriteFile(params["output_path"].(string), raw.Bytes(), 0600)
					}
				}
				calls <- observedCall{tool: msg.Data["tool_name"], params: params, err: writeErr}
				// Even a bridge success cannot conceal a failed Python result.
				sendInstanceActionWSMessage(t, conn, instanceActionWSMessage{Type: "tool_result", Data: map[string]any{
					"request_id": msg.Data["request_id"], "success": true,
					"output": map[string]any{"success": outcome != "renderer_error", "info": "图层缺少字段 population"},
				}})
			}()
			raw, _ := json.Marshal(map[string]string{"instance_id": "preview-instance", "layer_id": "actual-layer-id"})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("POST", "/api/resources/by-id/"+item.ID+"/preview-layer", bytes.NewReader(raw)))
			call := <-calls
			if call.err != nil || call.tool != "resource_preview" || call.params["layer_id"] != "actual-layer-id" || call.params["resource_path"] != item.Entry || call.params["resource_type"] != "style" {
				t.Fatalf("invalid preview invocation: %+v", call)
			}
			if outcome == "success" {
				if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal(w.Code, w.Header(), w.Body.String())
				}
				if _, err := png.Decode(bytes.NewReader(w.Body.Bytes())); err != nil {
					t.Fatal(err)
				}
			} else if w.Code != 502 || (outcome == "renderer_error" && !strings.Contains(w.Body.String(), "population")) {
				t.Fatal(w.Code, w.Body.String())
			}
			entries, err := os.ReadDir(filepath.Join(library.UserDir, ".staging"))
			if err != nil || len(entries) != 0 {
				t.Fatal("preview staging leaked", entries, err)
			}
			defaultAfter, err := os.ReadFile(defaultPath)
			if err != nil || !bytes.Equal(defaultBefore, defaultAfter) {
				t.Fatal("layer preview replaced the default symbol preview", err)
			}
			current, err := library.GetByID(item.ID)
			if err != nil || current.Digest != item.Digest {
				t.Fatal("layer preview changed resource", current, err)
			}
		})
	}
}

func TestResourceLayerPreviewRejectsMissingSelectionAndClientPaths(t *testing.T) {
	a := &API{}
	router := mux.NewRouter()
	a.SetupRoutes(router)
	for _, body := range []string{
		`{}`, `{"instance_id":"instance"}`, `{"instance_id":"instance","layer_id":" "}`,
		`{"instance_id":"instance","layer_id":"layer","resource_path":"/tmp/arbitrary.qml"}`,
		`{"instance_id":"instance","layer_id":"layer","output_path":"/tmp/arbitrary.png"}`,
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", "/api/resources/by-id/style.example/preview-layer", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatal(body, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/api/resources/by-id/style.example/preview-layer", strings.NewReader(`{"instance_id":"offline","layer_id":"layer"}`)))
	if w.Code != 503 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestResourceLayerPreviewRespectsReadLayerPolicy(t *testing.T) {
	registry, provider, _, cleanup := setupInstanceActionBridge(t, "denied", []map[string]any{{"name": "resource_preview", "inputSchema": map[string]any{"type": "object"}}})
	defer cleanup()
	router := buildInstanceTestRouterWithLocalBridgeAndAssetCatalog(t, registry, provider, stubInstanceAssetCatalogService{visibility: "hidden", known: true})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/api/resources/by-id/style.example/preview-layer", strings.NewReader(`{"instance_id":"denied","layer_id":"layer"}`)))
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
}
