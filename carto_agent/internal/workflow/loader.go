package workflow

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var allowedInputTemplatePattern = regexp.MustCompile(`^\{\{\s*inputs\.[a-zA-Z_][a-zA-Z0-9_]*\s*\}\}$`)

func LoadFile(path string) (*Definition, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	var definition Definition
	if err := yaml.Unmarshal(data, &definition); err != nil {
		return nil, err
	}
	definition.SourcePath = strings.TrimSpace(path)
	normalizeDefinition(&definition)
	if err := validateDefinition(definition); err != nil {
		return nil, err
	}
	return &definition, nil
}

func normalizeDefinition(definition *Definition) {
	if definition == nil {
		return
	}
	definition.APIVersion = strings.TrimSpace(definition.APIVersion)
	definition.Kind = strings.TrimSpace(definition.Kind)
	definition.Metadata.Name = strings.TrimSpace(definition.Metadata.Name)
	if len(definition.Spec.Inputs) == 0 {
		definition.Spec.Inputs = map[string]Input{}
	}
	for key, input := range definition.Spec.Inputs {
		trimmedKey := strings.TrimSpace(key)
		input.Type = strings.TrimSpace(input.Type)
		delete(definition.Spec.Inputs, key)
		definition.Spec.Inputs[trimmedKey] = input
	}
	for index := range definition.Spec.Steps {
		definition.Spec.Steps[index].ID = strings.TrimSpace(definition.Spec.Steps[index].ID)
		definition.Spec.Steps[index].Type = strings.TrimSpace(definition.Spec.Steps[index].Type)
		definition.Spec.Steps[index].Tool = strings.TrimSpace(definition.Spec.Steps[index].Tool)
		if definition.Spec.Steps[index].With == nil {
			definition.Spec.Steps[index].With = map[string]any{}
		}
	}
}

func validateDefinition(definition Definition) error {
	if definition.APIVersion != APIVersionV1 {
		return fmt.Errorf("unsupported workflow apiVersion %q", definition.APIVersion)
	}
	if definition.Kind != KindSerialWorkflow {
		return fmt.Errorf("unsupported workflow kind %q", definition.Kind)
	}
	if strings.TrimSpace(definition.Metadata.Name) == "" {
		return fmt.Errorf("workflow metadata.name is required")
	}
	for _, step := range definition.Spec.Steps {
		if step.Type != StepTypeTool {
			return fmt.Errorf("workflow step %q has unsupported type %q", step.ID, step.Type)
		}
		if strings.TrimSpace(step.ID) == "" {
			return fmt.Errorf("workflow step id is required")
		}
		if strings.TrimSpace(step.Tool) == "" {
			return fmt.Errorf("workflow step %q missing tool", step.ID)
		}
		if err := validateStepTemplates(step); err != nil {
			return err
		}
	}
	return nil
}

func validateStepTemplates(step Step) error {
	for _, value := range step.With {
		if err := validateTemplateValue(step.ID, value); err != nil {
			return err
		}
	}
	return nil
}

func validateTemplateValue(stepID string, value any) error {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if strings.Contains(trimmed, "{{") && !allowedInputTemplatePattern.MatchString(trimmed) {
			return fmt.Errorf("workflow step %q has unsupported template %q", stepID, typed)
		}
	case map[string]any:
		for _, item := range typed {
			if err := validateTemplateValue(stepID, item); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range typed {
			if err := validateTemplateValue(stepID, item); err != nil {
				return err
			}
		}
	}
	return nil
}
