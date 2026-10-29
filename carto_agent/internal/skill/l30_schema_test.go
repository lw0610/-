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
			if p["guide"].(map[string]any)["type"] != "string" || p["deliverables"].(map[string]any)["items"].(map[string]any)["type"] != "string" {
				t.Fatalf("v2 guide/output IDs lost in model schema: %s", raw)
			}
			data, ok := p["data"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
			if !ok {
				t.Fatalf("data items must expose the v2 fields: %s", raw)
			}
			for _, k := range []string{"id", "name", "kind", "source", "ref", "need", "extent", "method", "geometry", "style", "label_field"} {
				if data[k] == nil {
					t.Fatalf("%s absent: %s", k, raw)
				}
			}
			for _, k := range []string{"from", "path", "file_id", "find_id", "choice"} {
				if data[k] != nil {
					t.Fatalf("legacy data field %s still advertised: %s", k, raw)
				}
			}
			source := data["source"].(map[string]any)["enum"].([]any)
			if len(source) != 4 || source[0] != "user" || source[1] != "project" || source[2] != "builtin" || source[3] != "retrieve" {
				t.Fatalf("v2 sources lost: %s", raw)
			}
			need := data["need"].(map[string]any)["properties"].(map[string]any)
			for _, key := range []string{"category", "params", "fields", "description", "region", "level", "year", "indicator", "caliber", "unit", "exclude"} {
				if need[key] == nil {
					t.Fatalf("need.%s absent: %s", key, raw)
				}
			}
			if need["exclude"].(map[string]any)["items"].(map[string]any)["type"] != "string" {
				t.Fatalf("statistical exclusions must expose administrative names/codes: %s", raw)
			}
			extent := data["extent"].(map[string]any)["properties"].(map[string]any)
			if extent["from"].(map[string]any)["type"] != "string" || extent["bbox"].(map[string]any)["items"].(map[string]any)["type"] != "number" {
				t.Fatalf("derived/geographic extent lost: %s", raw)
			}
			stepSchema := p["steps"].(map[string]any)["items"].(map[string]any)
			step, ok := stepSchema["properties"].(map[string]any)
			if !ok {
				t.Fatalf("step items must expose v2 input/output bindings: %s", raw)
			}
			if step["outputs"] != nil || step["uses"] != nil || step["inputs"].(map[string]any)["items"].(map[string]any)["type"] != "string" {
				t.Fatalf("step input bindings do not match v2: %s", raw)
			}
			requiresProducts := false
			for _, value := range stepSchema["required"].([]any) {
				requiresProducts = requiresProducts || value == "produces"
			}
			products := step["produces"].(map[string]any)
			if !requiresProducts || products["minItems"] != float64(1) {
				t.Fatalf("every step must declare at least one output: %s", raw)
			}
			product := products["items"].(map[string]any)["properties"].(map[string]any)
			for _, key := range []string{"id", "name", "kind", "format", "geometry", "fields"} {
				if product[key] == nil {
					t.Fatalf("produces.%s absent: %s", key, raw)
				}
			}
			kind := product["kind"].(map[string]any)["enum"].([]any)
			outputKinds := []string{"layer", "table", "dataset", "file", "layout", "review_initial", "review_final", "text", "confirmation"}
			if len(kind) != len(outputKinds) {
				t.Fatalf("model output kinds differ from the public execution contract: %s", raw)
			}
			for _, outputKind := range outputKinds {
				found := false
				for _, value := range kind {
					found = found || value == outputKind
				}
				if !found {
					t.Fatalf("%s output kind absent from model schema: %s", outputKind, raw)
				}
			}
			for _, outputKind := range []string{"layout", "review_initial", "review_final", "text", "confirmation"} {
				for _, value := range data["kind"].(map[string]any)["enum"].([]any) {
					if value == outputKind {
						t.Fatalf("%s is an output, not an acquisition data kind: %s", outputKind, raw)
					}
				}
			}
			parameters := step["parameters"].(map[string]any)
			if parameters["type"] != "object" || parameters["additionalProperties"].(map[string]any)["type"] != "string" {
				t.Fatalf("executor parameters lost in model schema: %s", raw)
			}
		}
	}
}
