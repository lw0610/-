package main

import (
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/flow"
	cartoplan "github.com/autocarto/carto_agent/internal/plan"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcceptanceUsesV2ReferencesAndProduces(t *testing.T) {
	raw := []byte(`{"id":"fixture","builtin_layers":["省域"],"inputs":{"标题":"验收"},"steps":{"s1":{"actions":[],"outputs":{"成果图":{"$ref":"export.output"}}}}}`)
	var recipe scenario
	if e := json.Unmarshal(raw, &recipe); e != nil {
		t.Fatal(e)
	}
	p, e := fixturePlan(recipe, flow.Environment{})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Data) != 1 || p.Data[0].Source != "builtin" || p.Data[0].Ref != "省域" || len(p.Steps[0].Inputs) != 1 || len(p.Steps[0].Produces) != 1 {
		t.Fatal(p)
	}
	if ds := cartoplan.ValidateV2(p); len(ds) != 0 {
		t.Fatal(ds)
	}
	encoded, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	for _, old := range []string{`"find_id":`, `"choice":`, `"uses":`, `"outputs":`, `"from":"user"`} {
		if strings.Contains(string(encoded), old) {
			t.Fatal("v1 planning field", old, string(encoded))
		}
	}
}
func TestAcceptanceCannotFeedGoldenBasemapDirectly(t *testing.T) {
	root := "../../../autocarto/tests/integration/acceptance"
	raw, e := os.ReadFile(filepath.Join(root, "run.py"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), "AUTOCARTO_GOLDEN_BASEMAP_DIR") {
		t.Fatal("golden bypass returned")
	}
	for _, file := range []string{"route-no-review.json", "route-with-review.json"} {
		raw, e = os.ReadFile(filepath.Join(root, "scenarios", file))
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(raw), "${basemap}") || !strings.Contains(string(raw), "layer_load_builtin") || !strings.Contains(string(raw), `"golden"`) {
			t.Fatal("route acceptance bypassed registered data or golden comparison", file)
		}
		if !strings.Contains(string(raw), "layer_set_symbology") || strings.Contains(string(raw), "layer_load_styles") {
			t.Fatal("route acceptance must exercise v7 parameterized basemap styling", file)
		}
	}
}
