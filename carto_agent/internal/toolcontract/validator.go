package toolcontract

import (
	"fmt"
	"strings"
)

type ValidationError struct {
	Name       string
	SourcePath string
	Problems   []string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	target := strings.TrimSpace(e.Name)
	if target == "" {
		target = "tool contract"
	}
	if source := strings.TrimSpace(e.SourcePath); source != "" {
		target = fmt.Sprintf("%s (%s)", target, source)
	}
	return fmt.Sprintf("invalid %s: %s", target, strings.Join(e.Problems, "; "))
}

func ValidateContract(contract Contract) error {
	contract.normalize()
	problems := make([]string, 0)
	if contract.APIVersion == "" {
		problems = append(problems, "apiVersion is required")
	} else if contract.APIVersion != ContractAPIVersionV1 {
		problems = append(problems, fmt.Sprintf("apiVersion must be %q", ContractAPIVersionV1))
	}
	if contract.Kind == "" {
		problems = append(problems, "kind is required")
	} else if contract.Kind != KindToolContract {
		problems = append(problems, fmt.Sprintf("kind must be %q", KindToolContract))
	}
	if contract.Metadata.Name == "" {
		problems = append(problems, "metadata.name is required")
	}
	if contract.Metadata.Runtime == "" && contract.Metadata.Backend == "" {
		problems = append(problems, "metadata.runtime is required")
	}
	if contract.Spec.Description == "" {
		problems = append(problems, "spec.description is required")
	}
	switch contract.Policy.Visibility {
	case VisibilityPublic, VisibilityHidden, VisibilityInternal:
	case "":
		problems = append(problems, "policy.visibility is required")
	default:
		problems = append(problems, "policy.visibility must be one of public|hidden|internal")
	}
	switch contract.Execution.Binding {
	case ExecutionBindingHandler:
		hasHandlerPath := contract.Execution.Handler != ""
		hasHandlerFile := contract.Execution.HandlerFile != ""
		hasHandlerFunc := contract.Execution.HandlerFunc != ""
		if !hasHandlerPath && !hasHandlerFile {
			problems = append(problems, "execution.handler or execution.handler_file is required when execution.binding=handler")
		}
		if hasHandlerFile && !hasHandlerFunc {
			problems = append(problems, "execution.handler_function is required when execution.handler_file is set")
		}
	case ExecutionBindingBackendTool:
		if contract.Execution.BackendTool == "" {
			problems = append(problems, "execution.backend_tool is required when execution.binding=backend_tool")
		}
	case ExecutionBindingCLI:
		if contract.Execution.CLI == nil || strings.TrimSpace(contract.Execution.CLI.Command) == "" {
			problems = append(problems, "execution.cli.command is required when execution.binding=cli")
		}
	case "":
		problems = append(problems, "execution.binding is required")
	default:
		problems = append(problems, "execution.binding is invalid")
	}
	for idx, rule := range contract.Execution.Validation {
		prefix := fmt.Sprintf("execution.validation[%d]", idx)
		if rule.Rule == "" {
			problems = append(problems, prefix+".rule is required")
		}
		if rule.Message == "" {
			problems = append(problems, prefix+".message is required")
		}
	}
	seenParameters := make(map[string]struct{}, len(contract.Spec.Parameters))
	for idx, param := range contract.Spec.Parameters {
		prefix := fmt.Sprintf("spec.parameters[%d]", idx)
		if param.Name == "" {
			problems = append(problems, prefix+".name is required")
		} else {
			if _, exists := seenParameters[param.Name]; exists {
				problems = append(problems, fmt.Sprintf("duplicate parameter name %q", param.Name))
			} else {
				seenParameters[param.Name] = struct{}{}
			}
		}
		if param.Type == "" {
			problems = append(problems, prefix+".type is required")
		}
	}
	if len(problems) > 0 {
		return &ValidationError{
			Name:       contract.Metadata.Name,
			SourcePath: contract.SourcePath,
			Problems:   problems,
		}
	}
	return nil
}

func ValidateContracts(contracts []Contract) error {
	seen := make(map[string]string, len(contracts))
	for _, contract := range contracts {
		if err := ValidateContract(contract); err != nil {
			return err
		}
		name := strings.TrimSpace(contract.Metadata.Name)
		if name == "" {
			continue
		}
		if existing, ok := seen[name]; ok {
			return fmt.Errorf("duplicate tool name %q found in %s and %s", name, existing, contract.SourcePath)
		}
		seen[name] = contract.SourcePath
	}
	return nil
}
