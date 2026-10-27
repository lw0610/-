package api

import (
	"bytes"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/gorilla/mux"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResourceCaptureKindsConflictReplacementAndCleanup(t *testing.T) {
	for _, kind := range []string{"layout", "style", "style_set"} {
		t.Run(kind, func(t *testing.T) {
			tools := []map[string]any{}
			for _, name := range []string{"layout_save_template", "layer_save_style", "style_set_export", "layout_list"} {
				tools = append(tools, map[string]any{"name": name, "inputSchema": map[string]any{"type": "object"}})
			}
			registry, provider, conn, cleanup := setupInstanceActionBridge(t, "capture-instance", tools)
			defer cleanup()
			root := t.TempDir()
			library := resource.NewLibrary("", filepath.Join(root, "resources"), nil)
			a := NewAPI(nil, nil, nil, nil, "", registry)
			a.SetLocalBridgeProvider(provider)
			a.SetResourceLibrary(library)
			router := mux.NewRouter()
			a.SetupRoutes(router)
			respond := func(valid bool) {
				msg := readInstanceActionWSMessage(t, conn)
				params := msg.Data["params"].(map[string]any)
				if valid {
					if kind == "style_set" {
						dir := params["directory"].(string)
						_ = os.MkdirAll(dir, 0700)
						_ = os.WriteFile(filepath.Join(dir, "roads.qml"), []byte("<qgis/>"), 0600)
					} else {
						content := "<qgis/>"
						if kind == "layout" {
							content = "<Layout><LayoutItem id=\"main_map\"/></Layout>"
						}
						_ = os.WriteFile(params["path"].(string), []byte(content), 0600)
					}
				}
				sendInstanceActionWSMessage(t, conn, instanceActionWSMessage{Type: "tool_result", Data: map[string]any{"request_id": msg.Data["request_id"], "success": true, "output": map[string]any{"success": true}}})
			}
			call := func(replace string, want int, valid bool) map[string]any {
				t.Helper()
				done := make(chan bool, 1)
				go func() { defer close(done); respond(valid) }()
				payload := map[string]any{"instance_id": "capture-instance", "kind": kind, "name": "保存资源", "layout": "版面", "layers": []string{"layer-id"}}
				if replace != "" {
					payload["replace_id"] = replace
				}
				raw, _ := json.Marshal(payload)
				req := httptest.NewRequest("POST", "/api/resources/capture", bytes.NewReader(raw))
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				if w.Code != want {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
				select {
				case <-done:
				case <-time.After(6 * time.Second):
					t.Fatal("capture bridge response did not complete")
				}
				var body map[string]any
				_ = json.Unmarshal(w.Body.Bytes(), &body)
				entries, err := os.ReadDir(filepath.Join(library.UserDir, ".staging"))
				if err != nil || len(entries) != 0 {
					t.Fatal("staging leaked", entries, err)
				}
				return body
			}
			created := call("", 200, true)["data"].(map[string]any)
			if created["type"] != kind || created["legacy"] != false {
				t.Fatal(created)
			}
			call("", 409, true)
			updated := call(created["id"].(string), 200, true)["data"].(map[string]any)
			if updated["id"] != created["id"] || updated["version"] != "1.0.1" {
				t.Fatal(updated)
			}
			call("", 400, false)
		})
	}
}
func TestResourceCaptureDisconnectedAndDenied(t *testing.T) {
	a := &API{}
	router := mux.NewRouter()
	a.SetupRoutes(router)
	req := httptest.NewRequest("POST", "/api/resources/capture", bytes.NewBufferString(`{"instance_id":"offline","kind":"style","name":"样式","layers":["layer"]}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 503 {
		t.Fatal(w.Code, w.Body.String())
	}
	registry, provider, _, cleanup := setupInstanceActionBridge(t, "denied", []map[string]any{{"name": "layer_save_style", "inputSchema": map[string]any{"type": "object"}}})
	defer cleanup()
	router = buildInstanceTestRouterWithLocalBridgeAndAssetCatalog(t, registry, provider, stubInstanceAssetCatalogService{visibility: "hidden", known: true})
	req = httptest.NewRequest("POST", "/api/resources/capture", bytes.NewBufferString(`{"instance_id":"denied","kind":"style","name":"样式","layers":["layer"]}`))
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestInstanceLayoutsProjection(t *testing.T) {
	registry, provider, conn, cleanup := setupInstanceActionBridge(t, "layouts", []map[string]any{{"name": "layout_list", "inputSchema": map[string]any{"type": "object"}}})
	defer cleanup()
	router := buildInstanceTestRouterWithLocalBridge(t, registry, provider)
	go func() {
		msg := readInstanceActionWSMessage(t, conn)
		sendInstanceActionWSMessage(t, conn, instanceActionWSMessage{Type: "tool_result", Data: map[string]any{"request_id": msg.Data["request_id"], "success": true, "output": map[string]any{"success": true, "data": map[string]any{"layouts": []any{map[string]any{"name": "地图", "map_items": []any{map[string]any{"uuid": "main-id"}}}}}}}})
	}()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/instances/layouts/layouts", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			Layouts []struct {
				Name     string           `json:"name"`
				MapItems []map[string]any `json:"map_items"`
			} `json:"layouts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Data.Layouts) != 1 || body.Data.Layouts[0].MapItems[0]["uuid"] != "main-id" {
		t.Fatal(body, err)
	}
}
