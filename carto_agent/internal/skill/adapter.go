package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"

	"github.com/autocarto/carto_agent/internal/toolresult"
)

type SkillAdapter struct {
	definition  SkillDefinition
	description string
	inputSchema *jsonschema.Schema
	engine      *Engine
}

func NewSkillAdapter(def SkillDefinition, engine *Engine) einotool.BaseTool {
	return NewEnhancedSkillAdapter(def, engine)
}

func NewEnhancedSkillAdapter(def SkillDefinition, engine *Engine) einotool.EnhancedInvokableTool {
	adapter := newSkillAdapter(def, engine)
	info, _ := adapter.Info(context.Background())
	return toolutils.NewEnhancedTool(info, func(ctx context.Context, args map[string]any) (*schema.ToolResult, error) {
		return adapter.runToolResult(ctx, args)
	})
}

func BuildToolsFromDefinitions(definitions []SkillDefinition, engine *Engine) ([]einotool.BaseTool, error) {
	if engine == nil {
		return nil, fmt.Errorf("skill engine is nil")
	}
	tools := make([]einotool.BaseTool, 0, len(definitions))
	for _, def := range definitions {
		if strings.TrimSpace(def.Metadata.Name) == "" {
			continue
		}
		tools = append(tools, NewSkillAdapter(def, engine))
	}
	return tools, nil
}

func BuildToolsFromRegistry(registry *Registry, engine *Engine) ([]einotool.BaseTool, error) {
	if registry == nil {
		return nil, fmt.Errorf("skill registry is nil")
	}
	return BuildToolsFromDefinitions(registry.Definitions(), engine)
}

func newSkillAdapter(def SkillDefinition, engine *Engine) *SkillAdapter {
	cloned := def.Clone()
	cloned.normalize()
	return &SkillAdapter{
		definition:  cloned,
		description: buildSkillDescription(cloned),
		inputSchema: BuildSkillJSONSchema(cloned),
		engine:      engine,
	}
}

func (a *SkillAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	_ = ctx
	if a == nil {
		return nil, fmt.Errorf("skill adapter is nil")
	}
	return &schema.ToolInfo{
		Name:        a.definition.Metadata.Name,
		Desc:        a.description,
		ParamsOneOf: schema.NewParamsOneOfByJSONSchema(a.inputSchema),
	}, nil
}

func (a *SkillAdapter) runToolResult(ctx context.Context, args map[string]any) (*schema.ToolResult, error) {
	if a == nil || a.engine == nil {
		return nil, fmt.Errorf("skill adapter engine is nil")
	}
	result, err := a.engine.Execute(ctx, a.definition.Metadata.Name, cloneMapAny(args))
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &schema.ToolResult{}, nil
	}
	output, err := toolResultWithMedia(result.Output)
	if err != nil {
		return nil, err
	}
	for _, layer := range result.OutputLayers {
		if strings.TrimSpace(layer.Path) == "" {
			continue
		}
		output = toolresult.AppendFileURL(output, toolresult.LocalFileURL(layer.Path), "", map[string]any{
			"path":      strings.TrimSpace(layer.Path),
			"auto_load": layer.AutoLoad,
		})
	}
	return output, nil
}

const (
	agentMediaKey      = "_agent_media"
	maxAgentImageBytes = 50 * 1024 * 1024
)

// toolResultWithMedia converts the generic _agent_media envelope returned by a
// trusted Python/CLI tool into actual image parts. A local path in JSON is only
// text to the model; this bridge is what makes the image visible to it.
func toolResultWithMedia(output any) (*schema.ToolResult, error) {
	payload, media, err := splitAgentMedia(output)
	if err != nil {
		return nil, err
	}
	result, err := toolresult.JSON(payload)
	if err != nil {
		return toolresult.Text(fmt.Sprintf("%v", payload)), nil
	}
	for index, item := range media {
		mediaType := strings.ToLower(strings.TrimSpace(stringValue(item["type"])))
		if mediaType != "image" {
			return nil, fmt.Errorf("%s[%d] only supports image media", agentMediaKey, index)
		}
		path := strings.TrimSpace(stringValue(item["path"]))
		if path == "" || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%s[%d] image path must be absolute", agentMediaKey, index)
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			return nil, fmt.Errorf("read tool image %q: %w", path, statErr)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("tool image is not a regular file: %s", path)
		}
		if info.Size() > maxAgentImageBytes {
			return nil, fmt.Errorf("tool image exceeds %d bytes: %s", maxAgentImageBytes, path)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("read tool image %q: %w", path, readErr)
		}
		mimeType := strings.ToLower(strings.TrimSpace(http.DetectContentType(data)))
		if separator := strings.IndexByte(mimeType, ';'); separator >= 0 {
			mimeType = mimeType[:separator]
		}
		switch mimeType {
		case "image/png", "image/jpeg", "image/webp":
		default:
			return nil, fmt.Errorf("unsupported tool image type %q: %s", mimeType, path)
		}
		label := strings.TrimSpace(stringValue(item["label"]))
		if label == "" {
			label = "Image: " + filepath.Base(path)
		}
		result = toolresult.AppendText(result, label)
		result = toolresult.AppendImageData(result, data, mimeType)
	}
	return result, nil
}

func splitAgentMedia(output any) (any, []map[string]any, error) {
	payload, ok := output.(map[string]any)
	if !ok {
		return output, nil, nil
	}
	rawMedia, exists := payload[agentMediaKey]
	if !exists {
		return output, nil, nil
	}
	items, ok := rawMedia.([]any)
	if !ok {
		return nil, nil, fmt.Errorf("%s must be an array", agentMediaKey)
	}
	media := make([]map[string]any, 0, len(items))
	for index, rawItem := range items {
		item, itemOK := rawItem.(map[string]any)
		if !itemOK {
			return nil, nil, fmt.Errorf("%s[%d] must be an object", agentMediaKey, index)
		}
		media = append(media, item)
	}
	cleaned := make(map[string]any, len(payload)-1)
	for key, value := range payload {
		if key != agentMediaKey {
			cleaned[key] = value
		}
	}
	return cleaned, media, nil
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprintf("%v", value)
}

func BuildSkillJSONSchema(def SkillDefinition) *jsonschema.Schema {
	return toJSONSchema(BuildSkillSchemaMap(def))
}

func BuildSkillSchemaMap(def SkillDefinition) map[string]any {
	def.normalize()
	properties := make(map[string]any, len(def.Spec.Parameters))
	required := make([]string, 0, len(def.Spec.Parameters))
	for _, param := range def.Spec.Parameters {
		if strings.TrimSpace(param.Name) == "" {
			continue
		}
		property := map[string]any{
			"type": skillParameterJSONSchemaType(param.Type, param.Values),
		}
		if description := strings.TrimSpace(param.Description); description != "" {
			property["description"] = description
		}
		if param.Default != nil {
			property["default"] = cloneAny(param.Default)
		}
		if len(param.Values) > 0 {
			property["enum"] = cloneAny(param.Values)
		}
		properties[param.Name] = property
		if param.Required {
			required = append(required, param.Name)
		}
	}
	sort.Strings(required)
	schemaMap := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schemaMap["required"] = required
	}
	return schemaMap
}

func buildSkillDescription(def SkillDefinition) string {
	base := strings.TrimSpace(def.Spec.Description)
	if base == "" {
		base = strings.TrimSpace(def.Metadata.DisplayName)
	}
	if len(def.Spec.Examples) == 0 {
		return base
	}
	parts := []string{base, "Examples:"}
	for _, example := range def.Spec.Examples {
		line := strings.TrimSpace(example.Description)
		if payload, err := json.Marshal(example.Args); err == nil && string(payload) != "null" {
			if line != "" {
				line += ": "
			}
			line += string(payload)
		}
		if line == "" {
			continue
		}
		parts = append(parts, "- "+line)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func skillParameterJSONSchemaType(rawType string, values []any) string {
	switch strings.ToLower(strings.TrimSpace(rawType)) {
	case "number", "float", "double":
		return "number"
	case "integer", "int":
		return "integer"
	case "boolean", "bool":
		return "boolean"
	case "enum":
		return inferEnumJSONSchemaType(values)
	case "array", "list":
		return "array"
	case "object", "map", "dict":
		return "object"
	default:
		return "string"
	}
}

func inferEnumJSONSchemaType(values []any) string {
	if len(values) == 0 {
		return "string"
	}
	switch values[0].(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "integer"
	case float32, float64:
		return "number"
	case bool:
		return "boolean"
	default:
		return "string"
	}
}
