package skill

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestL30RealToolSchemaReachesModelWithNestedContract(t *testing.T) {
	root := filepath.Join("..", "..", "..", "autocarto", "tools", "system")
	paths := []string{"data-service/data_find.yaml", "runtime-control/plan_propose.yaml", "runtime-control/plan_revise.yaml"}
	files := map[string]string{}
	for _, p := range paths {
		b, e := os.ReadFile(filepath.Join(root, p))
		if e != nil {
			t.Fatal(e)
		}
		files[p] = string(b)
	}
	registry := loadRegistryForTest(t, files)
	for _, name := range []string{"data_find", "plan_propose", "plan_revise"} {
		d, ok := registry.Get(name)
		if !ok {
			t.Fatal(name)
		}
		info, e := newSkillAdapter(d, nil).Info(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		actual, e := info.ParamsOneOf.ToJSONSchema()
		if e != nil {
			t.Fatal(e)
		}
		raw, e := json.Marshal(actual)
		if e != nil {
			t.Fatal(e)
		}
		var m map[string]any
		if e = json.Unmarshal(raw, &m); e != nil {
			t.Fatal(e)
		}
		props := m["properties"].(map[string]any)
		if name == "data_find" {
			need := props["need"].(map[string]any)["properties"].(map[string]any)
			what := need["what"].(map[string]any)
			wp := what["properties"].(map[string]any)
			if what["type"] != "object" || wp["description"] == nil || wp["category"] == nil || wp["list"] != nil || what["anyOf"] == nil || what["oneOf"] != nil {
				t.Fatal(string(raw))
			}
			if props["provided"].(map[string]any)["items"].(map[string]any)["type"] != "string" {
				t.Fatal(string(raw))
			}
		} else {
			p := props["plan"].(map[string]any)["properties"].(map[string]any)
			data := p["data"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
			for _, k := range []string{"geometry", "layer", "layer_name", "style", "kind", "path", "file_id"} {
				if data[k] == nil {
					t.Fatalf("%s absent: %s", k, raw)
				}
			}
			step := p["steps"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
			parameters := step["parameters"].(map[string]any)
			if parameters["type"] != "object" || parameters["additionalProperties"].(map[string]any)["type"] != "string" {
				t.Fatalf("executor parameters lost in model schema: %s", raw)
			}
		}
	}
}
