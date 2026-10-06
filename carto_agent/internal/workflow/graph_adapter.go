package workflow

import (
	"fmt"
	"strings"
)

type SerialPlan struct {
	Name  string
	Steps []CompiledStep
}

type CompiledStep struct {
	ID        string
	ToolName  string
	Arguments map[string]any
}

func CompileSerialPlan(definition *Definition, inputs map[string]any) (*SerialPlan, error) {
	if definition == nil {
		return nil, fmt.Errorf("workflow definition is required")
	}
	for inputName, input := range definition.Spec.Inputs {
		if !input.Required {
			continue
		}
		if _, ok := inputs[inputName]; !ok {
			return nil, fmt.Errorf("workflow input %q is required", inputName)
		}
	}
	steps := make([]CompiledStep, 0, len(definition.Spec.Steps))
	for _, step := range definition.Spec.Steps {
		compiledArgs, err := resolveStepArguments(step.With, inputs)
		if err != nil {
			return nil, fmt.Errorf("compile workflow step %q: %w", step.ID, err)
		}
		steps = append(steps, CompiledStep{
			ID:        step.ID,
			ToolName:  step.Tool,
			Arguments: compiledArgs,
		})
	}
	return &SerialPlan{Name: definition.Metadata.Name, Steps: steps}, nil
}

func resolveStepArguments(args map[string]any, inputs map[string]any) (map[string]any, error) {
	if len(args) == 0 {
		return map[string]any{}, nil
	}
	resolved := make(map[string]any, len(args))
	for key, value := range args {
		next, err := resolveValue(value, inputs)
		if err != nil {
			return nil, err
		}
		resolved[key] = next
	}
	return resolved, nil
}

func resolveValue(value any, inputs map[string]any) (any, error) {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if !allowedInputTemplatePattern.MatchString(trimmed) {
			return typed, nil
		}
		key := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "{{"), "}}"))
		key = strings.TrimPrefix(key, "inputs.")
		resolved, ok := inputs[key]
		if !ok {
			return nil, fmt.Errorf("workflow input %q is required", key)
		}
		return resolved, nil
	case map[string]any:
		return resolveStepArguments(typed, inputs)
	case []any:
		resolved := make([]any, len(typed))
		for index, item := range typed {
			next, err := resolveValue(item, inputs)
			if err != nil {
				return nil, err
			}
			resolved[index] = next
		}
		return resolved, nil
	default:
		return typed, nil
	}
}
