package skill

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type LegacyToolGroupDefinition struct {
	Category    string                 `yaml:"category"`
	DisplayName string                 `yaml:"display_name"`
	Description string                 `yaml:"description"`
	Tools       []LegacyToolDefinition `yaml:"tools"`
}

type LegacyToolDefinition struct {
	Name        string                `yaml:"name"`
	AlgorithmID string                `yaml:"algorithm_id,omitempty"`
	Handler     string                `yaml:"handler,omitempty"`
	DisplayName string                `yaml:"display_name,omitempty"`
	Description string                `yaml:"description,omitempty"`
	Parameters  []LegacyToolParameter `yaml:"parameters,omitempty"`
}

type LegacyToolParameter struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required,omitempty"`
	Description string `yaml:"description,omitempty"`
	Default     any    `yaml:"default,omitempty"`
	Values      []any  `yaml:"values,omitempty"`
}

func LoadLegacyDefinitionFile(path string) ([]SkillDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read legacy definition %s: %w", path, err)
	}
	return MigrateLegacyDefinitionFile(data, path)
}

func MigrateLegacyDefinitionFile(data []byte, sourcePath string) ([]SkillDefinition, error) {
	var legacy LegacyToolGroupDefinition
	if err := yaml.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("parse legacy definition %s: %w", sourcePath, err)
	}
	return ConvertLegacyGroupToSkillDefinitions(legacy, sourcePath)
}

func ConvertLegacyGroupToSkillDefinitions(group LegacyToolGroupDefinition, sourcePath string) ([]SkillDefinition, error) {
	group.normalize()
	definitions := make([]SkillDefinition, 0, len(group.Tools))
	for _, tool := range group.Tools {
		definition, err := convertLegacyToolToSkillDefinition(group, tool, sourcePath)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

func (d *LegacyToolGroupDefinition) normalize() {
	if d == nil {
		return
	}
	d.Category = strings.TrimSpace(d.Category)
	d.DisplayName = strings.TrimSpace(d.DisplayName)
	d.Description = strings.TrimSpace(d.Description)
	for idx := range d.Tools {
		d.Tools[idx].normalize()
	}
}

func (d *LegacyToolDefinition) normalize() {
	if d == nil {
		return
	}
	d.Name = strings.TrimSpace(d.Name)
	d.AlgorithmID = strings.TrimSpace(d.AlgorithmID)
	d.Handler = strings.TrimSpace(d.Handler)
	d.DisplayName = strings.TrimSpace(d.DisplayName)
	d.Description = strings.TrimSpace(d.Description)
	for idx := range d.Parameters {
		d.Parameters[idx].Name = strings.TrimSpace(d.Parameters[idx].Name)
		d.Parameters[idx].Type = strings.TrimSpace(d.Parameters[idx].Type)
		d.Parameters[idx].Description = strings.TrimSpace(d.Parameters[idx].Description)
	}
}

func convertLegacyToolToSkillDefinition(group LegacyToolGroupDefinition, tool LegacyToolDefinition, sourcePath string) (SkillDefinition, error) {
	if tool.Name == "" {
		return SkillDefinition{}, fmt.Errorf("legacy definition %s contains tool without name", sourcePath)
	}
	if tool.AlgorithmID == "" && tool.Handler == "" {
		return SkillDefinition{}, fmt.Errorf("legacy tool %s (%s) missing algorithm_id or handler", tool.Name, sourcePath)
	}

	def := SkillDefinition{
		APIVersion: SkillAPIVersionV1,
		Kind:       SkillKindAtomic,
		Metadata: SkillMetadata{
			Name:        tool.Name,
			DisplayName: firstNonEmpty(tool.DisplayName, tool.Name),
			Category:    group.Category,
			Backend:     legacyToolBackendID(tool),
		},
		Spec: SkillSpec{
			Description: firstNonEmpty(tool.Description, group.Description, tool.DisplayName, tool.Name),
			Parameters:  convertLegacyParameters(tool.Parameters),
		},
		Execution: SkillExecution{
			BackendTool:  tool.Name,
			ParamMapping: buildLegacyParamMapping(tool),
			Output:       buildLegacySkillOutput(tool.Parameters),
		},
		SourcePath: sourcePath,
	}
	def.normalize()
	if err := def.Validate(); err != nil {
		return SkillDefinition{}, err
	}
	return def, nil
}

func legacyToolBackendID(tool LegacyToolDefinition) string {
	return RuntimeIDCLI
}

func convertLegacyParameters(params []LegacyToolParameter) []SkillParameter {
	if len(params) == 0 {
		return nil
	}
	out := make([]SkillParameter, 0, len(params))
	for _, param := range params {
		out = append(out, SkillParameter{
			Name:        param.Name,
			Type:        param.Type,
			Required:    param.Required,
			Description: param.Description,
			Default:     cloneAny(param.Default),
			Values:      cloneLegacyValues(param.Values),
		})
	}
	return out
}

func buildLegacyParamMapping(tool LegacyToolDefinition) map[string]any {
	mapping := make(map[string]any, len(tool.Parameters))
	for _, param := range tool.Parameters {
		if param.Name == "" {
			continue
		}
		mapping[param.Name] = fmt.Sprintf("{{ %s }}", param.Name)
	}
	return mapping
}

func buildLegacySkillOutput(params []LegacyToolParameter) *SkillOutput {
	for _, param := range params {
		switch strings.ToLower(strings.TrimSpace(param.Type)) {
		case "output_layer", "output_raster":
			return &SkillOutput{AutoLoad: true}
		}
	}
	return nil
}

func cloneLegacyValues(values []any) []any {
	if len(values) == 0 {
		return nil
	}
	cloned := cloneAny(values)
	if typed, ok := cloned.([]any); ok {
		return typed
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
