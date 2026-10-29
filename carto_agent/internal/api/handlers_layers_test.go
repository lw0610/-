package api

import (
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/layers"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestLayerImportScanConfirmPreviewAndRemoveRegistration(t *testing.T) {
	a, router, _ := acquisitionSettingsAPI(t)
	_, provider, conn, cleanup := setupInstanceActionBridge(t, "layers-test", []map[string]any{{"name": "layer_inspect", "inputSchema": map[string]any{"type": "object"}}, {"name": "resource_preview", "inputSchema": map[string]any{"type": "object"}}})
	defer cleanup()
	a.SetLocalBridgeProvider(provider)
	l, e := layers.NewDefaultLibrary()
	if e != nil {
		t.Fatal(e)
	}
	a.SetLayerLibrary(l)
	root := t.TempDir()
	path := filepath.Join(root, "河流线.geojson")
	content := []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"LineString","coordinates":[[112,33],[113,34]]},"properties":{"NAME":"河流"}}]}`)
	if e = os.WriteFile(path, content, 0600); e != nil {
		t.Fatal(e)
	}
	respond := func(preview bool) {
		msg := readInstanceActionWSMessage(t, conn)
		params := msg.Data["params"].(map[string]any)
		output := map[string]any{"success": true, "data": map[string]any{"geometry": "line", "declared_crs": "EPSG:4326", "crs": "EPSG:4326", "available_fields": []string{"NAME"}, "feature_count": 1, "coverage": map[string]any{"bbox": []float64{112, 33, 113, 34}, "regions": []string{"河南省"}}, "scale": map[string]any{"best_from": 100000, "best_to": 1000000}}}
		if preview {
			if msg.Data["tool_name"] != "resource_preview" || params["resource_type"] != "layer" {
				t.Errorf("wrong preview call: %v", msg)
			}
			f, e := os.Create(params["output_path"].(string))
			if e != nil {
				t.Error(e)
			} else {
				png.Encode(f, image.NewRGBA(image.Rect(0, 0, 8, 8)))
				f.Close()
			}
			output = map[string]any{"success": true}
		}
		sendInstanceActionWSMessage(t, conn, instanceActionWSMessage{Type: "tool_result", Data: map[string]any{"request_id": msg.Data["request_id"], "success": true, "output": output}})
	}
	go respond(false)
	rr := acquisitionRequest(t, router, "POST", "/api/layers/import/scan", map[string]any{"path": root, "pack": "测试包"})
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	var draft struct {
		Data struct {
			Entries []layerDraftEntry `json:"entries"`
		} `json:"data"`
	}
	if e = json.Unmarshal(rr.Body.Bytes(), &draft); e != nil || len(draft.Data.Entries) != 1 {
		t.Fatal(rr.Body.String(), e)
	}
	m := draft.Data.Entries[0].Manifest
	if m.Category != "hydro.river_line" || m.LabelField != "NAME" {
		t.Fatal(m)
	}
	go func() { respond(false); respond(true) }()
	rr = acquisitionRequest(t, router, "POST", "/api/layers/import/confirm", map[string]any{"pack": "测试包", "entries": []layers.Manifest{m}})
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	item, e := l.Get(m.ID)
	if e != nil || item.PreviewURL == "" {
		t.Fatal(item, e)
	}
	rr = acquisitionRequest(t, router, "GET", item.PreviewURL, nil)
	if rr.Code != 200 || rr.Header().Get("Content-Type") != "image/png" {
		t.Fatal(rr.Code, rr.Body.String())
	}
	rr = acquisitionRequest(t, router, "DELETE", "/api/layers/"+m.ID, nil)
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	after, e := os.ReadFile(path)
	if e != nil || string(after) != string(content) {
		t.Fatal("original source changed", e)
	}
}
