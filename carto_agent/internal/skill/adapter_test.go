package skill

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/compute"
	"github.com/autocarto/carto_agent/internal/toolsession"
	"github.com/cloudwego/eino/schema"
)

func TestBuildSkillSchemaMapFromParameters(t *testing.T) {
	t.Parallel()

	def := SkillDefinition{
		Metadata: SkillMetadata{Name: "buffer_analysis"},
		Spec: SkillSpec{
			Description: "Create a buffer.",
			Parameters: []SkillParameter{
				{Name: "layer", Type: "string", Required: true, Description: "Layer name"},
				{Name: "distance", Type: "number", Required: true, Description: "Distance"},
				{Name: "dissolve", Type: "boolean", Default: false, Description: "Dissolve features"},
			},
			Examples: []SkillExample{{Description: "Buffer roads", Args: map[string]any{"layer": "roads", "distance": 500}}},
		},
	}

	schemaMap := BuildSkillSchemaMap(def)
	if schemaMap["type"] != "object" {
		t.Fatalf("unexpected schema type: %v", schemaMap["type"])
	}
	if schemaMap["additionalProperties"] != false {
		t.Fatalf("unexpected additionalProperties: %v", schemaMap["additionalProperties"])
	}
	required, ok := schemaMap["required"].([]string)
	if !ok {
		t.Fatalf("expected required to be []string, got %#v", schemaMap["required"])
	}
	if !reflect.DeepEqual(required, []string{"distance", "layer"}) {
		t.Fatalf("unexpected required params: %#v", required)
	}
	properties, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %#v", schemaMap["properties"])
	}
	layerSchema := properties["layer"].(map[string]any)
	if layerSchema["type"] != "string" {
		t.Fatalf("unexpected layer type: %#v", layerSchema["type"])
	}
	distanceSchema := properties["distance"].(map[string]any)
	if distanceSchema["type"] != "number" {
		t.Fatalf("unexpected distance type: %#v", distanceSchema["type"])
	}
	dissolveSchema := properties["dissolve"].(map[string]any)
	if dissolveSchema["type"] != "boolean" {
		t.Fatalf("unexpected dissolve type: %#v", dissolveSchema["type"])
	}
	if dissolveSchema["default"] != false {
		t.Fatalf("unexpected dissolve default: %#v", dissolveSchema["default"])
	}

	info, err := newSkillAdapter(def, nil).Info(context.Background())
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	if info.Name != "buffer_analysis" {
		t.Fatalf("unexpected tool name: %s", info.Name)
	}
	if !strings.Contains(info.Desc, "Create a buffer.") || !strings.Contains(info.Desc, "Examples:") || !strings.Contains(info.Desc, "Buffer roads") {
		t.Fatalf("unexpected tool description: %s", info.Desc)
	}
}

func TestBuildSkillSchemaMapDefaultsToObject(t *testing.T) {
	t.Parallel()

	def := SkillDefinition{
		Metadata: SkillMetadata{Name: "noop"},
		Spec:     SkillSpec{Description: "No params skill."},
	}

	schemaMap := BuildSkillSchemaMap(def)
	if schemaMap["type"] != "object" {
		t.Fatalf("unexpected schema type: %v", schemaMap["type"])
	}
	properties, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %#v", schemaMap["properties"])
	}
	if len(properties) != 0 {
		t.Fatalf("expected empty properties, got %#v", properties)
	}
	if _, exists := schemaMap["required"]; exists {
		t.Fatalf("did not expect required field for empty schema")
	}
	jsonSchema := BuildSkillJSONSchema(def)
	if jsonSchema == nil || strings.TrimSpace(jsonSchema.Type) != "object" {
		t.Fatalf("unexpected json schema: %#v", jsonSchema)
	}
}

func TestSkillAdapterRunToolResultViaEngine(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	registry := loadRegistryForTest(t, map[string]string{
		"vector/buffer_analysis.yaml": `
apiVersion: tool/v1
kind: Tool
metadata:
  name: buffer_analysis
  domain: vector
  runtime: qgis
spec:
  description: Create a buffer.
  parameters:
    - name: layer
      type: string
      required: true
    - name: distance
      type: number
      required: true
execution:
  backend_tool: buffer
  param_mapping:
    INPUT: "{{ resolve_layer(layer) }}"
    DISTANCE: "{{ distance }}"
    OUTPUT: "{{ auto_output_path('buffer') }}"
`,
	})

	def, ok := registry.Get("buffer_analysis")
	if !ok {
		t.Fatal("expected buffer_analysis definition")
	}
	computeInvoker := &fakeComputeInvoker{
		resp: &compute.InvokeResponse{
			Success:      true,
			Output:       map[string]any{"status": "ok"},
			OutputLayers: []compute.OutputLayer{{Path: filepath.Join(projectDir, "buffer.gpkg"), AutoLoad: true}},
		},
	}
	engine := NewEngine(registry, NewComputeBackend(computeInvoker))
	adapter := newSkillAdapter(def, engine)
	ctx := toolsession.WithContext(context.Background(), "session-7", filepath.Join(projectDir, "demo.qgz"), "")
	ctx = WithSessionContext(ctx, SessionContext{
		ProjectID: "project-7",
		Layers:    []SessionLayer{{Name: "roads", Path: filepath.Join(projectDir, "roads.gpkg")}},
	})

	result, err := adapter.runToolResult(ctx, map[string]any{"layer": "roads", "distance": 25})
	if err != nil {
		t.Fatalf("runToolResult() error = %v", err)
	}
	if computeInvoker.callCount != 1 {
		t.Fatalf("expected engine to invoke compute backend once, got %d", computeInvoker.callCount)
	}
	if computeInvoker.lastReq.ProjectID != "project-7" {
		t.Fatalf("unexpected project id: %s", computeInvoker.lastReq.ProjectID)
	}
	if len(result.Parts) != 2 {
		t.Fatalf("expected 2 tool result parts, got %d", len(result.Parts))
	}
	if result.Parts[0].Type != schema.ToolPartTypeText {
		t.Fatalf("unexpected first part type: %v", result.Parts[0].Type)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Parts[0].Text), &payload); err != nil {
		t.Fatalf("invalid JSON payload: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if result.Parts[1].Type != schema.ToolPartTypeFile || result.Parts[1].File == nil || result.Parts[1].File.URL == nil {
		t.Fatalf("unexpected second part: %#v", result.Parts[1])
	}
	if !strings.HasPrefix(*result.Parts[1].File.URL, "file://") {
		t.Fatalf("unexpected file URL: %s", *result.Parts[1].File.URL)
	}
	if result.Parts[1].File.Extra["path"] != filepath.Join(projectDir, "buffer.gpkg") {
		t.Fatalf("unexpected file extra path: %#v", result.Parts[1].File.Extra)
	}
}

func TestSkillAdapterKeepsProjectLayerIdentityWithoutFileURL(t *testing.T) {
	registry := loadRegistryForTest(t, map[string]string{"vector/prepare.yaml": `
apiVersion: tool/v1
kind: Tool
metadata:
  name: prepare
  domain: vector
  runtime: qgis
spec:
  description: Prepare a project layer.
execution:
  backend_tool: prepare
`})
	def, _ := registry.Get("prepare")
	var primary any = "专题图"
	engine := NewEngine(registry, NewQGISBackend(func(context.Context, string, map[string]any) (*compute.InvokeResponse, error) {
		return &compute.InvokeResponse{Success: true, Output: primary, OutputLayers: []compute.OutputLayer{{
			LayerID: "thematic-123", Name: "专题图", ReferenceType: "project_layer", Provider: "memory",
			FeatureCount: 18, Fields: []string{"name", "population"},
		}}}, nil
	}, nil, nil))
	adapter := newSkillAdapter(def, engine)
	result, err := adapter.runToolResult(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Parts) != 2 || result.Parts[1].Type != schema.ToolPartTypeText ||
		!strings.Contains(result.Parts[1].Text, `"layer_id":"thematic-123"`) ||
		!strings.Contains(result.Parts[1].Text, `"population"`) {
		t.Fatalf("model must receive the project layer reference as text: %#v", result)
	}
	primary = map[string]any{"layer_id": "thematic-123", "name": "专题图"}
	result, err = adapter.runToolResult(context.Background(), nil)
	if err != nil || len(result.Parts) != 1 || result.Parts[0].Type != schema.ToolPartTypeText {
		t.Fatalf("do not duplicate structured identity or fabricate file URLs: %#v, %v", result, err)
	}
}

func TestSkillAdapterConvertsCLIMediaEnvelopeToImageParts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	originalPath := filepath.Join(root, "original.png")
	candidatePath := filepath.Join(root, "candidate.png")
	if err := os.WriteFile(originalPath, append([]byte("\x89PNG\r\n\x1a\n"), []byte("original")...), 0o644); err != nil {
		t.Fatalf("write original image: %v", err)
	}
	if err := os.WriteFile(candidatePath, append([]byte("\x89PNG\r\n\x1a\n"), []byte("candidate")...), 0o644); err != nil {
		t.Fatalf("write candidate image: %v", err)
	}
	registry := loadRegistryForTest(t, map[string]string{
		"quality/generate_candidate.yaml": `
apiVersion: tool/v1
kind: Tool
metadata:
  name: generate_candidate
  domain: quality
  runtime: qgis
spec:
  description: Generate a visual candidate.
execution:
  backend_tool: generate_candidate
`,
	})
	def, ok := registry.Get("generate_candidate")
	if !ok {
		t.Fatal("expected generate_candidate definition")
	}
	engine := NewEngine(registry, NewQGISBackend(
		func(context.Context, string, map[string]any) (*compute.InvokeResponse, error) {
			return &compute.InvokeResponse{Success: true, Output: map[string]any{
				"candidate_image_path": candidatePath,
				agentMediaKey: []any{
					map[string]any{"type": "image", "path": originalPath, "label": "original"},
					map[string]any{"type": "image", "path": candidatePath, "label": "candidate"},
				},
			}}, nil
		},
		nil,
		nil,
	))
	adapter := newSkillAdapter(def, engine)

	result, err := adapter.runToolResult(context.Background(), nil)
	if err != nil {
		t.Fatalf("runToolResult() error = %v", err)
	}
	if len(result.Parts) != 5 {
		t.Fatalf("expected metadata plus two labeled images, got %#v", result.Parts)
	}
	if result.Parts[0].Type != schema.ToolPartTypeText || !strings.Contains(result.Parts[0].Text, candidatePath) || strings.Contains(result.Parts[0].Text, agentMediaKey) {
		t.Fatalf("unexpected text part: %#v", result.Parts[0])
	}
	if result.Parts[1].Text != "original" || result.Parts[2].Type != schema.ToolPartTypeImage || result.Parts[2].Image == nil {
		t.Fatalf("original image was not attached: %#v", result.Parts[1:3])
	}
	if result.Parts[3].Text != "candidate" || result.Parts[4].Type != schema.ToolPartTypeImage || result.Parts[4].Image == nil {
		t.Fatalf("candidate image was not attached: %#v", result.Parts[3:5])
	}
	if result.Parts[2].Image.MIMEType != "image/png" || result.Parts[4].Image.MIMEType != "image/png" {
		t.Fatalf("unexpected image MIME types: %#v", result.Parts)
	}
}

func TestSkillAdapterKeepsQGISContractPayloadAtTopLevel(t *testing.T) {
	t.Parallel()

	registry := loadRegistryForTest(t, map[string]string{
		"knowledge/marching_route_get_extraction_contract.yaml": `
apiVersion: tool/v1
kind: Tool
metadata:
  name: marching_route_get_extraction_contract
  domain: marching-route-knowledge
  runtime: qgis
spec:
  description: Return the extraction contract.
  parameters:
    - name: source_text
      type: string
      required: true
execution:
  backend_tool: marching_route_get_extraction_contract
  param_mapping:
    source_text: "{{ source_text }}"
`,
	})
	def, ok := registry.Get("marching_route_get_extraction_contract")
	if !ok {
		t.Fatal("expected extraction contract definition")
	}
	engine := NewEngine(
		registry,
		NewQGISBackend(
			func(context.Context, string, map[string]any) (*compute.InvokeResponse, error) {
				return &compute.InvokeResponse{
					Success: true,
					Output: map[string]any{
						"schema_version": "1.0",
						"contract_id":    "tkg-v1-sha256:test",
						"prompt":         "extract",
						"json_schema":    map[string]any{"type": "object"},
					},
				}, nil
			},
			nil,
			stubQGISRuntimeInspector{
				providerID:   "qgis_bridge",
				activeID:     "inst-1",
				connectedIDs: []string{"inst-1"},
			},
		),
	)
	adapter := newSkillAdapter(def, engine)
	result, err := adapter.runToolResult(
		context.Background(),
		map[string]any{"source_text": "路线原文"},
	)
	if err != nil {
		t.Fatalf("runToolResult() error = %v", err)
	}
	if len(result.Parts) != 1 || result.Parts[0].Type != schema.ToolPartTypeText {
		t.Fatalf("unexpected tool result: %#v", result)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Parts[0].Text), &payload); err != nil {
		t.Fatalf("invalid JSON payload: %v", err)
	}
	if payload["contract_id"] != "tkg-v1-sha256:test" || payload["prompt"] != "extract" {
		t.Fatalf("unexpected top-level contract payload: %#v", payload)
	}
	if _, nested := payload["output"]; nested {
		t.Fatalf("did not expect nested output wrapper: %#v", payload)
	}
}
