package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/llm"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
	"github.com/cloudwego/eino/schema"
)

// Exercise the production YAML loader, contract/definition clones, skill adapter,
// and model client. Inspect the HTTP body, not just an intermediate Go schema:
// an unrestricted {} must never become a null schema on its way to the provider.
func TestBuiltinToolSchemasRemainValidInModelRequest(t *testing.T) {
	toolRegistry := toolpkg.NewRegistry()
	if err := toolRegistry.LoadDirectories(repoSamplePath(t, "autocarto/tools/system")); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(toolRegistry)
	if err := registry.LoadDirectories(repoPluginSkillPackagesPath(t)); err != nil {
		t.Fatal(err)
	}
	definitions := registry.Definitions()
	if len(definitions) == 0 {
		t.Fatal("no builtin tools registered")
	}
	infos := make([]*schema.ToolInfo, 0, len(definitions))
	for _, definition := range definitions {
		info, err := newSkillAdapter(definition, nil).Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		infos = append(infos, info)
	}

	type capturedRequest struct {
		body []byte
		err  error
	}
	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		requests <- capturedRequest{body: body, err: err}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"schema-regression","object":"chat.completion","created":1,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	chatModel, err := llm.NewChatModel(&llm.Config{
		Provider: "openai", Model: "test-model", APIKey: "local-test-only", BaseURL: server.URL + "/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	withTools, err := chatModel.WithTools(infos)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := withTools.Generate(ctx, []*schema.Message{schema.UserMessage("schema regression test")}); err != nil {
		t.Fatal(err)
	}
	var captured capturedRequest
	select {
	case captured = <-requests:
	case <-ctx.Done():
		t.Fatal("model client did not send a request")
	}
	if captured.err != nil {
		t.Fatal(captured.err)
	}
	var request struct {
		Tools []struct {
			Function struct {
				Name       string `json:"name"`
				Parameters any    `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(captured.body, &request); err != nil {
		t.Fatal(err)
	}
	if len(request.Tools) != len(infos) {
		t.Fatalf("HTTP request contains %d tools, expected %d", len(request.Tools), len(infos))
	}
	seenPlans := map[string]bool{}
	for _, tool := range request.Tools {
		name, parameters := tool.Function.Name, tool.Function.Parameters
		if name == "plan_propose" || name == "plan_revise" {
			seenPlans[name] = true
		}
		t.Run(name, func(t *testing.T) {
			if err := validateSchemaPositions(parameters, "parameters"); err != nil {
				t.Fatal(err)
			}
			if name == "data_find" {
				what := schemaValueAt(t, parameters, "properties", "need", "properties", "what").(map[string]any)
				if what["anyOf"] == nil || what["oneOf"] != nil {
					t.Fatalf("model request must allow category and description together: %#v", what)
				}
			}
			if name != "plan_propose" && name != "plan_revise" {
				return
			}
			value := schemaValueAt(t, parameters, "properties", "plan", "properties", "facts", "items", "properties", "value")
			switch value := value.(type) {
			case bool:
				if !value {
					t.Fatal("facts.value must accept arbitrary values, not reject all values")
				}
			case map[string]any:
				if len(value) != 0 {
					t.Fatalf("facts.value must remain unrestricted, got %#v", value)
				}
			default:
				t.Fatalf("facts.value is not an unrestricted JSON Schema: %#v", value)
			}
		})
	}
	for _, name := range []string{"plan_propose", "plan_revise"} {
		if !seenPlans[name] {
			t.Errorf("%s is absent from the model request", name)
		}
	}
	t.Logf("inspected HTTP parameters for %d builtin tools", len(request.Tools))
}

func schemaValueAt(t *testing.T, value any, path ...string) any {
	t.Helper()
	for index, key := range path {
		object, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s must be an object, got %#v", strings.Join(path[:index], "."), value)
		}
		var exists bool
		value, exists = object[key]
		if !exists {
			t.Fatalf("schema path %s is absent", strings.Join(path[:index+1], "."))
		}
	}
	return value
}

// Only follow JSON Schema keywords whose values contain schemas. Arbitrary data
// under default, enum, const and examples may legitimately contain null values.
func validateSchemaPositions(value any, path string) error {
	if _, ok := value.(bool); ok {
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return fmt.Errorf("%s: expected object or boolean schema, got %#v", path, value)
	}
	for key, value := range object {
		childPath := path + "." + key
		switch key {
		case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas":
			children, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("%s: expected map of schemas", childPath)
			}
			for name, child := range children {
				if err := validateSchemaPositions(child, childPath+"."+name); err != nil {
					return err
				}
			}
		case "items", "allOf", "anyOf", "oneOf", "prefixItems":
			if children, ok := value.([]any); ok {
				for index, child := range children {
					if err := validateSchemaPositions(child, fmt.Sprintf("%s[%d]", childPath, index)); err != nil {
						return err
					}
				}
			} else if key == "items" {
				if err := validateSchemaPositions(value, childPath); err != nil {
					return err
				}
			} else {
				return fmt.Errorf("%s: expected array of schemas", childPath)
			}
		case "additionalItems", "additionalProperties", "unevaluatedItems", "unevaluatedProperties", "contains", "propertyNames", "not", "if", "then", "else", "contentSchema":
			if err := validateSchemaPositions(value, childPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func TestSchemaPositionValidationDistinguishesSchemasFromData(t *testing.T) {
	for _, raw := range []string{
		`{"properties":{"value":{}}}`,
		`{"properties":{"value":true}}`,
		`{"properties":{"value":{"default":null,"enum":[null,{}],"examples":[{"items":null}],"const":{"properties":{"x":null}}}}}`,
	} {
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		if err := validateSchemaPositions(value, "parameters"); err != nil {
			t.Fatalf("valid schema rejected: %s: %v", raw, err)
		}
	}
	var invalid any
	if err := json.Unmarshal([]byte(`{"properties":{"facts":{"items":{"properties":{"value":null}}}}}`), &invalid); err != nil {
		t.Fatal(err)
	}
	if err := validateSchemaPositions(invalid, "parameters"); err == nil || !strings.Contains(err.Error(), "parameters.properties.facts.items.properties.value") {
		t.Fatalf("invalid nested schema must be identified at its exact path: %v", err)
	}
}
