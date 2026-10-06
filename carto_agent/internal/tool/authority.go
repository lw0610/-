package tool

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/toolcontract"
)

type AccessPolicy struct {
	Visibility          string
	AuthorizationScopes []string
}

func (d ToolDefinition) EffectiveRuntime() string {
	d.normalize()
	for _, candidate := range []string{
		d.Policy.Runtime,
		d.Execution.Provider,
		d.Metadata.Runtime,
		d.Metadata.Backend,
	} {
		if strings.TrimSpace(candidate) != "" {
			return NormalizeRuntime(candidate)
		}
	}
	return ""
}

func (d ToolDefinition) EffectiveVisibility() string {
	d.normalize()
	if visibility := strings.TrimSpace(d.Policy.Visibility); visibility != "" {
		return visibility
	}
	if d.EffectiveRuntime() == RuntimeIDInternal {
		return VisibilityInternal
	}
	return VisibilityPublic
}

func (d ToolDefinition) EffectiveAuthorizationScopes() []string {
	d.normalize()
	if len(d.Policy.AuthorizationScopes) > 0 {
		return append([]string(nil), d.Policy.AuthorizationScopes...)
	}
	if d.EffectiveVisibility() == VisibilityPublic {
		return []string{"tool.invoke"}
	}
	return nil
}

func (d ToolDefinition) EffectiveAccessPolicy() AccessPolicy {
	return AccessPolicy{
		Visibility:          d.EffectiveVisibility(),
		AuthorizationScopes: d.EffectiveAuthorizationScopes(),
	}
}

func (d ToolDefinition) ExecutionManifestEntry() toolcontract.ExecutionManifestEntry {
	d.normalize()
	handlerFile := strings.TrimSpace(d.Execution.HandlerFile)
	handlerFunc := strings.TrimSpace(d.Execution.HandlerFunc)
	if handlerFile == "" || handlerFunc == "" {
		derivedFile, derivedFunc := deriveHandlerBinding(d.SourcePath, d.Execution.Handler)
		if handlerFile == "" {
			handlerFile = derivedFile
		}
		if handlerFunc == "" {
			handlerFunc = derivedFunc
		}
	}
	return toolcontract.ExecutionManifestEntry{
		Name:                strings.TrimSpace(d.Metadata.Name),
		Description:         d.EffectiveDescription(),
		Domain:              strings.TrimSpace(d.Metadata.Domain),
		Runtime:             d.EffectiveRuntime(),
		Backend:             strings.TrimSpace(d.Metadata.Backend),
		Parameters:          toolParametersToContract(d.Spec.Parameters),
		Binding:             toolcontract.ExecutionBindingKind(strings.TrimSpace(d.Execution.Binding)),
		BackendTool:         strings.TrimSpace(d.Execution.BackendTool),
		Handler:             strings.TrimSpace(d.Execution.Handler),
		HandlerFile:         handlerFile,
		HandlerFunction:     handlerFunc,
		Visibility:          toolcontract.Visibility(d.EffectiveVisibility()),
		AuthorizationScopes: d.EffectiveAuthorizationScopes(),
		SourcePath:          strings.TrimSpace(d.SourcePath),
	}
}

func BuildExecutionManifestEntries(definitions []ToolDefinition) []toolcontract.ExecutionManifestEntry {
	if len(definitions) == 0 {
		return nil
	}
	sortedDefs := make([]ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if strings.TrimSpace(definition.Metadata.Name) == "" {
			continue
		}
		sortedDefs = append(sortedDefs, definition.Clone())
	}
	sort.Slice(sortedDefs, func(i, j int) bool {
		return strings.TrimSpace(sortedDefs[i].Metadata.Name) < strings.TrimSpace(sortedDefs[j].Metadata.Name)
	})
	manifest := make([]toolcontract.ExecutionManifestEntry, 0, len(sortedDefs))
	for _, definition := range sortedDefs {
		manifest = append(manifest, definition.ExecutionManifestEntry())
	}
	return manifest
}

func BuildAccessPolicyIndex(definitions []ToolDefinition) map[string]AccessPolicy {
	if len(definitions) == 0 {
		return nil
	}
	index := make(map[string]AccessPolicy, len(definitions))
	for _, definition := range definitions {
		name := strings.TrimSpace(definition.Metadata.Name)
		if name == "" {
			continue
		}
		index[name] = definition.EffectiveAccessPolicy()
	}
	return index
}

func LoadAuthorityPolicyIndex(dirs ...string) (map[string]AccessPolicy, error) {
	definitions, err := LoadAuthorityDefinitions(dirs...)
	if err != nil {
		return nil, err
	}
	return BuildAccessPolicyIndex(definitions), nil
}

func deriveHandlerBinding(sourcePath, handler string) (string, string) {
	handler = strings.TrimSpace(handler)
	if !strings.HasPrefix(handler, "handlers.") {
		return "", ""
	}
	parts := strings.Split(handler, ".")
	if len(parts) < 3 {
		return "", ""
	}
	moduleParts := parts[:len(parts)-1]
	functionName := strings.TrimSpace(parts[len(parts)-1])
	if functionName == "" {
		return "", ""
	}
	fileName := filepath.Join(moduleParts...) + ".py"
	sourceDir := filepath.Dir(strings.TrimSpace(sourcePath))
	if sourceDir == "." || sourceDir == "" {
		return fileName, functionName
	}
	return filepath.ToSlash(filepath.Join(sourceDir, fileName)), functionName
}

func toolParametersToContract(params []ToolParameter) []toolcontract.Parameter {
	if len(params) == 0 {
		return nil
	}
	out := make([]toolcontract.Parameter, len(params))
	for idx, param := range params {
		values := param.Values
		if len(values) == 0 {
			values = param.Enum
		}
		out[idx] = toolcontract.Parameter{
			Name:        param.Name,
			Type:        param.Type,
			Required:    param.Required,
			Description: param.Description,
			Default:     cloneAny(param.Default),
			Values:      cloneAnySlice(values),
		}
	}
	return out
}
