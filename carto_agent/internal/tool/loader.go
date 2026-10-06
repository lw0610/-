package tool

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/defaults"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/toolcontract"
	"gopkg.in/yaml.v3"
)

const canonicalContractRoot = "tool_contracts/system"

type manifestProbe struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

func LoadDefinitionsFromDirectories(dirs ...string) ([]ToolDefinition, error) {
	definitions := make([]ToolDefinition, 0)
	for _, dir := range dirs {
		loaded, err := loadDefinitionsFromDirectory(dir)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, loaded...)
	}
	return definitions, nil
}

func LoadAuthorityDefinitions(dirs ...string) ([]ToolDefinition, error) {
	definitions, err := LoadDefinitionsFromDirectories(dirs...)
	if err != nil {
		return nil, err
	}
	if len(definitions) > 0 {
		return definitions, nil
	}
	return loadLegacyCanonicalDefinitions()
}

func LoadCanonicalDefinitions() ([]ToolDefinition, error) {
	builtinDir := strings.TrimSpace(config.ResolveBuiltinToolsDir())
	if builtinDir != "" {
		definitions, err := LoadDefinitionsFromDirectories(builtinDir)
		if err != nil {
			return nil, err
		}
		if len(definitions) > 0 {
			return definitions, nil
		}
	}
	return loadLegacyCanonicalDefinitions()
}

func loadLegacyCanonicalDefinitions() ([]ToolDefinition, error) {
	contracts, err := toolcontract.LoadFS(defaults.FS, canonicalContractRoot)
	if err != nil {
		return nil, fmt.Errorf("load legacy canonical tool contracts: %w", err)
	}
	materialized, err := toolcontract.Materialize(contracts)
	if err != nil {
		return nil, fmt.Errorf("materialize legacy canonical tool contracts: %w", err)
	}
	definitions := make([]ToolDefinition, 0, len(materialized))
	for _, item := range materialized {
		publishWhenUnready := item.Runtime == RuntimeIDQGIS || item.Runtime == RuntimeIDCLI
		definitions = append(definitions, ToolDefinition{
			APIVersion: ToolPackageAPIVersionV1,
			Kind:       ToolKindPackage,
			Metadata: ToolMetadata{
				Name:        item.Name,
				DisplayName: item.DisplayName,
				Domain:      item.Domain,
				Runtime:     item.Runtime,
				Backend:     item.Backend,
				Source:      item.Source,
				Tags:        append([]string(nil), item.Tags...),
			},
			Spec: ToolSpec{
				Description: item.Description,
				Parameters:  contractParametersToTool(item.InputSchema.Parameters),
			},
			Policy: ToolPolicy{
				Visibility:          strings.TrimSpace(string(item.Visibility)),
				AuthorizationScopes: append([]string(nil), item.AuthorizationScopes...),
				AgentExposure:       AgentExposureSkillRefOnly,
				PublishWhenUnready:  &publishWhenUnready,
				Runtime:             item.Runtime,
			},
			Execution: ToolExecution{
				Provider:     item.Runtime,
				Binding:      strings.TrimSpace(string(item.Execution.Binding)),
				BackendTool:  item.Execution.BackendTool,
				Handler:      item.Execution.Handler,
				HandlerFile:  item.Execution.HandlerFile,
				HandlerFunc:  item.Execution.HandlerFunc,
				CLI:          contractCLIToTool(item.Execution.CLI),
				ParamMapping: cloneMapAny(item.Execution.ParamMapping),
				Validation:   contractValidationToTool(item.Execution.Validation),
				Output:       contractOutputToTool(item.Execution.Output),
			},
			SourcePath: "embedded:" + item.SourcePath,
		})
	}
	return definitions, nil
}

func loadDefinitionsFromDirectory(dir string) ([]ToolDefinition, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, nil
	}

	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat tool directory %s: %w", dir, err)
	}

	if !info.IsDir() {
		definition, ok, err := loadMaybeDefinitionFile(dir)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		return []ToolDefinition{definition}, nil
	}

	paths := make([]string, 0)
	if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yaml" || ext == ".yml" {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk tool directory %s: %w", dir, err)
	}
	sort.Strings(paths)

	definitions := make([]ToolDefinition, 0, len(paths))
	for _, path := range paths {
		definition, ok, err := loadMaybeDefinitionFile(path)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

func loadMaybeDefinitionFile(path string) (ToolDefinition, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ToolDefinition{}, false, fmt.Errorf("read tool definition %s: %w", path, err)
	}
	if !looksLikeToolManifest(data) {
		return ToolDefinition{}, false, nil
	}
	var definition ToolDefinition
	if err := yaml.Unmarshal(data, &definition); err != nil {
		return ToolDefinition{}, false, fmt.Errorf("parse tool definition %s: %w", path, err)
	}
	definition.SourcePath = path
	definition.normalize()
	if err := definition.Validate(); err != nil {
		return ToolDefinition{}, false, err
	}
	return definition, true, nil
}

func loadDefinitionFile(path string) (ToolDefinition, error) {
	definition, ok, err := loadMaybeDefinitionFile(path)
	if err != nil {
		return ToolDefinition{}, err
	}
	if !ok {
		return ToolDefinition{}, fmt.Errorf("tool definition %s is not a supported tool manifest", path)
	}
	return definition, nil
}

func looksLikeToolManifest(data []byte) bool {
	var probe manifestProbe
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return false
	}
	apiVersion := strings.TrimSpace(probe.APIVersion)
	kind := strings.TrimSpace(probe.Kind)
	switch {
	case apiVersion == ToolAPIVersionV1 || apiVersion == ToolPackageAPIVersionV1:
		return true
	case kind == string(ToolKindDefinition) || kind == string(ToolKindPackage):
		return true
	default:
		return false
	}
}

func contractParametersToTool(params []toolcontract.Parameter) []ToolParameter {
	if len(params) == 0 {
		return nil
	}
	out := make([]ToolParameter, len(params))
	for idx, param := range params {
		out[idx] = ToolParameter{
			Name:        param.Name,
			Type:        param.Type,
			Required:    param.Required,
			Description: param.Description,
			Default:     cloneAny(param.Default),
			Values:      append([]any(nil), param.Values...),
		}
	}
	return out
}

func contractCLIToTool(src *toolcontract.CLIExecution) *CLIExecutionConfig {
	if src == nil {
		return nil
	}
	cloned := &CLIExecutionConfig{
		Command:    src.Command,
		Args:       append([]string(nil), src.Args...),
		WorkingDir: src.WorkingDir,
		Env:        nil,
		Stdin:      src.Stdin,
		Stdout:     src.Stdout,
		Timeout:    src.Timeout,
	}
	if len(src.Env) > 0 {
		cloned.Env = make(map[string]string, len(src.Env))
		for key, value := range src.Env {
			cloned.Env[key] = value
		}
	}
	return cloned
}

func contractValidationToTool(rules []toolcontract.ValidationRule) []ToolValidationRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]ToolValidationRule, len(rules))
	for idx, rule := range rules {
		out[idx] = ToolValidationRule{Rule: rule.Rule, Message: rule.Message}
	}
	return out
}

func contractOutputToTool(src *toolcontract.Output) *ToolOutput {
	if src == nil {
		return nil
	}
	return &ToolOutput{AutoLoad: src.AutoLoad}
}
