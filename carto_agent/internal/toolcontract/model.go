package toolcontract

import (
	"sort"
	"strings"
)

const (
	ContractAPIVersionV1 = "tool_contract/v1"
	KindToolContract     = "ToolContract"
)

type Visibility string

const (
	VisibilityPublic   Visibility = "public"
	VisibilityHidden   Visibility = "hidden"
	VisibilityInternal Visibility = "internal"
)

type ExecutionBindingKind string

const (
	ExecutionBindingHandler     ExecutionBindingKind = "handler"
	ExecutionBindingBackendTool ExecutionBindingKind = "backend_tool"
	ExecutionBindingCLI         ExecutionBindingKind = "cli"
)

type Contract struct {
	APIVersion string    `yaml:"apiVersion"`
	Kind       string    `yaml:"kind"`
	Metadata   Metadata  `yaml:"metadata"`
	Spec       Spec      `yaml:"spec"`
	Policy     Policy    `yaml:"policy"`
	Execution  Execution `yaml:"execution"`
	SourcePath string    `yaml:"-"`
}

type Metadata struct {
	Name        string   `yaml:"name"`
	DisplayName string   `yaml:"display_name,omitempty"`
	Domain      string   `yaml:"domain,omitempty"`
	Runtime     string   `yaml:"runtime,omitempty"`
	Backend     string   `yaml:"backend,omitempty"`
	Source      string   `yaml:"source,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
}

type Spec struct {
	Description string      `yaml:"description"`
	Parameters  []Parameter `yaml:"parameters,omitempty"`
	Examples    []Example   `yaml:"examples,omitempty"`
}

type Parameter struct {
	// Schema carries the complete JSON Schema for nested objects and arrays.
	Schema      map[string]any `yaml:"schema,omitempty" json:"schema,omitempty"`
	Name        string         `yaml:"name" json:"name"`
	Type        string         `yaml:"type" json:"type"`
	Required    bool           `yaml:"required,omitempty" json:"required,omitempty"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Default     any            `yaml:"default,omitempty" json:"default,omitempty"`
	Values      []any          `yaml:"values,omitempty" json:"values,omitempty"`
}

type Example struct {
	Description string         `yaml:"description,omitempty"`
	Args        map[string]any `yaml:"args,omitempty"`
}

type Policy struct {
	Visibility    Visibility    `yaml:"visibility"`
	Authorization Authorization `yaml:"authorization,omitempty"`
}

type Authorization struct {
	Scopes []string `yaml:"scopes,omitempty"`
}

type Execution struct {
	Binding      ExecutionBindingKind `yaml:"binding"`
	BackendTool  string               `yaml:"backend_tool,omitempty"`
	Handler      string               `yaml:"handler,omitempty"`
	HandlerFile  string               `yaml:"handler_file,omitempty"`
	HandlerFunc  string               `yaml:"handler_function,omitempty"`
	CLI          *CLIExecution        `yaml:"cli,omitempty"`
	ParamMapping map[string]any       `yaml:"param_mapping,omitempty"`
	Validation   []ValidationRule     `yaml:"validation,omitempty"`
	Output       *Output              `yaml:"output,omitempty"`
}

type CLIExecution struct {
	Command    string            `yaml:"command"`
	Args       []string          `yaml:"args,omitempty"`
	WorkingDir string            `yaml:"working_dir,omitempty"`
	Env        map[string]string `yaml:"env,omitempty"`
	Stdin      string            `yaml:"stdin,omitempty"`
	Stdout     string            `yaml:"stdout,omitempty"`
	Timeout    string            `yaml:"timeout,omitempty"`
}

type ValidationRule struct {
	Rule    string `yaml:"rule"`
	Message string `yaml:"message"`
}

type Output struct {
	AutoLoad bool `yaml:"auto_load,omitempty"`
}

type ParameterSchema struct {
	Parameters []Parameter `json:"parameters,omitempty"`
}

type ExecutionDescriptor struct {
	Binding      ExecutionBindingKind
	BackendTool  string
	Handler      string
	HandlerFile  string
	HandlerFunc  string
	CLI          *CLIExecution
	ParamMapping map[string]any
	Validation   []ValidationRule
	Output       *Output
}

type MaterializedTool struct {
	Name                string
	DisplayName         string
	Description         string
	Domain              string
	Runtime             string
	Backend             string
	Source              string
	Tags                []string
	Visibility          Visibility
	AuthorizationScopes []string
	InputSchema         ParameterSchema
	Execution           ExecutionDescriptor
	SourcePath          string
}

type RuntimeState struct {
	Available         bool
	UnavailableReason string
	InstanceIDs       []string
}

type CatalogEntry struct {
	Name              string          `json:"name"`
	Description       string          `json:"description,omitempty"`
	Domain            string          `json:"domain,omitempty"`
	Runtime           string          `json:"runtime,omitempty"`
	Backend           string          `json:"backend,omitempty"`
	InputSchema       ParameterSchema `json:"input_schema,omitempty"`
	Available         bool            `json:"available"`
	UnavailableReason string          `json:"unavailable_reason,omitempty"`
	InstanceIDs       []string        `json:"instance_ids,omitempty"`
}

type ExecutionManifestEntry struct {
	Name                string               `json:"name"`
	Description         string               `json:"description,omitempty"`
	Domain              string               `json:"domain,omitempty"`
	Runtime             string               `json:"runtime,omitempty"`
	Backend             string               `json:"backend,omitempty"`
	Parameters          []Parameter          `json:"parameters,omitempty"`
	Binding             ExecutionBindingKind `json:"binding,omitempty"`
	BackendTool         string               `json:"backend_tool,omitempty"`
	Handler             string               `json:"handler,omitempty"`
	HandlerFile         string               `json:"handler_file,omitempty"`
	HandlerFunction     string               `json:"handler_function,omitempty"`
	Visibility          Visibility           `json:"visibility,omitempty"`
	AuthorizationScopes []string             `json:"authorization_scopes,omitempty"`
	SourcePath          string               `json:"source_path,omitempty"`
}

func (c *Contract) normalize() {
	if c == nil {
		return
	}
	c.APIVersion = strings.TrimSpace(c.APIVersion)
	c.Kind = strings.TrimSpace(c.Kind)
	c.Metadata.Name = strings.TrimSpace(c.Metadata.Name)
	c.Metadata.DisplayName = strings.TrimSpace(c.Metadata.DisplayName)
	c.Metadata.Domain = strings.TrimSpace(c.Metadata.Domain)
	c.Metadata.Runtime = normalizeLower(c.Metadata.Runtime)
	c.Metadata.Backend = normalizeLower(c.Metadata.Backend)
	c.Metadata.Source = strings.TrimSpace(c.Metadata.Source)
	c.Metadata.Tags = normalizeStringSlice(c.Metadata.Tags, false)
	c.Spec.Description = strings.TrimSpace(c.Spec.Description)
	for idx := range c.Spec.Parameters {
		c.Spec.Parameters[idx].Name = strings.TrimSpace(c.Spec.Parameters[idx].Name)
		c.Spec.Parameters[idx].Type = strings.TrimSpace(c.Spec.Parameters[idx].Type)
		c.Spec.Parameters[idx].Description = strings.TrimSpace(c.Spec.Parameters[idx].Description)
	}
	for idx := range c.Spec.Examples {
		c.Spec.Examples[idx].Description = strings.TrimSpace(c.Spec.Examples[idx].Description)
	}
	c.Policy.Visibility = Visibility(strings.TrimSpace(string(c.Policy.Visibility)))
	c.Policy.Authorization.Scopes = normalizeStringSlice(c.Policy.Authorization.Scopes, true)
	c.Execution.Binding = ExecutionBindingKind(strings.TrimSpace(string(c.Execution.Binding)))
	c.Execution.BackendTool = strings.TrimSpace(c.Execution.BackendTool)
	c.Execution.Handler = strings.TrimSpace(c.Execution.Handler)
	c.Execution.HandlerFile = strings.TrimSpace(c.Execution.HandlerFile)
	c.Execution.HandlerFunc = strings.TrimSpace(c.Execution.HandlerFunc)
	if c.Execution.CLI != nil {
		c.Execution.CLI.Command = strings.TrimSpace(c.Execution.CLI.Command)
		c.Execution.CLI.WorkingDir = strings.TrimSpace(c.Execution.CLI.WorkingDir)
		c.Execution.CLI.Stdin = strings.TrimSpace(c.Execution.CLI.Stdin)
		c.Execution.CLI.Stdout = strings.TrimSpace(c.Execution.CLI.Stdout)
		c.Execution.CLI.Timeout = strings.TrimSpace(c.Execution.CLI.Timeout)
		c.Execution.CLI.Args = normalizeStringSlice(c.Execution.CLI.Args, false)
		if len(c.Execution.CLI.Env) > 0 {
			env := make(map[string]string, len(c.Execution.CLI.Env))
			for key, value := range c.Execution.CLI.Env {
				trimmedKey := strings.TrimSpace(key)
				if trimmedKey == "" {
					continue
				}
				env[trimmedKey] = strings.TrimSpace(value)
			}
			c.Execution.CLI.Env = env
		}
	}
	if len(c.Execution.ParamMapping) > 0 {
		paramMapping := make(map[string]any, len(c.Execution.ParamMapping))
		for key, value := range c.Execution.ParamMapping {
			trimmedKey := strings.TrimSpace(key)
			if trimmedKey == "" {
				continue
			}
			paramMapping[trimmedKey] = value
		}
		c.Execution.ParamMapping = paramMapping
	}
	for idx := range c.Execution.Validation {
		c.Execution.Validation[idx].Rule = strings.TrimSpace(c.Execution.Validation[idx].Rule)
		c.Execution.Validation[idx].Message = strings.TrimSpace(c.Execution.Validation[idx].Message)
	}
	if c.Metadata.Runtime == "" {
		c.Metadata.Runtime = c.Metadata.Backend
	}
	if c.Metadata.Backend == "" {
		c.Metadata.Backend = c.Metadata.Runtime
	}
}

func Materialize(contracts []Contract) ([]MaterializedTool, error) {
	if err := ValidateContracts(contracts); err != nil {
		return nil, err
	}
	tools := make([]MaterializedTool, 0, len(contracts))
	for _, contract := range contracts {
		tools = append(tools, MaterializedTool{
			Name:                contract.Metadata.Name,
			DisplayName:         contract.Metadata.DisplayName,
			Description:         contract.Spec.Description,
			Domain:              contract.Metadata.Domain,
			Runtime:             contract.Metadata.Runtime,
			Backend:             contract.Metadata.Backend,
			Source:              contract.Metadata.Source,
			Tags:                append([]string(nil), contract.Metadata.Tags...),
			Visibility:          contract.Policy.Visibility,
			AuthorizationScopes: append([]string(nil), contract.Policy.Authorization.Scopes...),
			InputSchema:         ParameterSchema{Parameters: cloneParameters(contract.Spec.Parameters)},
			Execution: ExecutionDescriptor{
				Binding:      contract.Execution.Binding,
				BackendTool:  contract.Execution.BackendTool,
				Handler:      contract.Execution.Handler,
				HandlerFile:  contract.Execution.HandlerFile,
				HandlerFunc:  contract.Execution.HandlerFunc,
				CLI:          cloneCLI(contract.Execution.CLI),
				ParamMapping: cloneMapAny(contract.Execution.ParamMapping),
				Validation:   append([]ValidationRule(nil), contract.Execution.Validation...),
				Output:       cloneOutput(contract.Execution.Output),
			},
			SourcePath: contract.SourcePath,
		})
	}
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})
	return tools, nil
}

func (m MaterializedTool) PublicCatalogEntry(state RuntimeState) (CatalogEntry, bool) {
	if m.Visibility != VisibilityPublic {
		return CatalogEntry{}, false
	}
	instanceIDs := append([]string(nil), state.InstanceIDs...)
	sort.Strings(instanceIDs)
	return CatalogEntry{
		Name:              m.Name,
		Description:       m.Description,
		Domain:            m.Domain,
		Runtime:           m.Runtime,
		Backend:           m.Backend,
		InputSchema:       ParameterSchema{Parameters: cloneParameters(m.InputSchema.Parameters)},
		Available:         state.Available,
		UnavailableReason: strings.TrimSpace(state.UnavailableReason),
		InstanceIDs:       instanceIDs,
	}, true
}

func (m MaterializedTool) ExecutionManifestEntry() ExecutionManifestEntry {
	return ExecutionManifestEntry{
		Name:                m.Name,
		Description:         m.Description,
		Domain:              m.Domain,
		Runtime:             m.Runtime,
		Backend:             m.Backend,
		Parameters:          cloneParameters(m.InputSchema.Parameters),
		Binding:             m.Execution.Binding,
		BackendTool:         m.Execution.BackendTool,
		Handler:             m.Execution.Handler,
		HandlerFile:         m.Execution.HandlerFile,
		HandlerFunction:     m.Execution.HandlerFunc,
		Visibility:          m.Visibility,
		AuthorizationScopes: append([]string(nil), m.AuthorizationScopes...),
		SourcePath:          m.SourcePath,
	}
}

func cloneParameters(src []Parameter) []Parameter {
	if len(src) == 0 {
		return nil
	}
	out := make([]Parameter, len(src))
	copy(out, src)
	for idx := range src {
		out[idx].Schema = cloneMapAny(src[idx].Schema)
		if len(src[idx].Values) > 0 {
			out[idx].Values = append([]any(nil), src[idx].Values...)
		}
	}
	return out
}

func cloneCLI(src *CLIExecution) *CLIExecution {
	if src == nil {
		return nil
	}
	cloned := *src
	cloned.Args = append([]string(nil), src.Args...)
	if len(src.Env) > 0 {
		cloned.Env = make(map[string]string, len(src.Env))
		for key, value := range src.Env {
			cloned.Env[key] = value
		}
	}
	return &cloned
}

func cloneOutput(src *Output) *Output {
	if src == nil {
		return nil
	}
	cloned := *src
	return &cloned
}

func cloneMapAny(src map[string]any) map[string]any {
	// Empty objects are valid JSON Schemas; preserve {} instead of emitting null.
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMapAny(typed)
	case []any:
		out := make([]any, len(typed))
		for idx, item := range typed {
			out[idx] = cloneAny(item)
		}
		return out
	default:
		return typed
	}
}

func normalizeStringSlice(values []string, dedupe bool) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if dedupe {
			if _, exists := seen[trimmed]; exists {
				continue
			}
			seen[trimmed] = struct{}{}
		}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	if dedupe {
		sort.Strings(result)
	}
	return result
}

func normalizeLower(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
