package tool

import (
	"fmt"
	"strings"
)

const ToolAPIVersionV1 = "tool/v1"
const ToolPackageAPIVersionV1 = "tool-package/v1"

type ToolKind string

const ToolKindDefinition ToolKind = "Tool"
const ToolKindPackage ToolKind = "ToolPackage"

const (
	RuntimeIDQGIS     = "qgis"
	RuntimeIDCLI      = "cli"
	RuntimeIDInternal = "internal"
)

const (
	VisibilityPublic   = "public"
	VisibilityHidden   = "hidden"
	VisibilityInternal = "internal"
)

const (
	AgentExposureSkillRefOnly = "skill_ref_only"
	AgentExposureSystem       = "system"
)

type ToolDefinition struct {
	APIVersion string        `yaml:"apiVersion" json:"apiVersion"`
	Kind       ToolKind      `yaml:"kind" json:"kind"`
	Metadata   ToolMetadata  `yaml:"metadata" json:"metadata"`
	Spec       ToolSpec      `yaml:"spec" json:"spec"`
	Policy     ToolPolicy    `yaml:"policy,omitempty" json:"policy,omitempty"`
	Execution  ToolExecution `yaml:"execution" json:"execution"`
	Status     ToolStatus    `yaml:"status_model,omitempty" json:"status_model,omitempty"`
	SourcePath string        `yaml:"-" json:"sourcePath,omitempty"`
}

type ToolMetadata struct {
	Name        string   `yaml:"name" json:"name"`
	DisplayName string   `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	Keywords    []string `yaml:"keywords,omitempty" json:"keywords,omitempty"`
	Domain      string   `yaml:"domain,omitempty" json:"domain,omitempty"`
	Version     string   `yaml:"version,omitempty" json:"version,omitempty"`
	Runtime     string   `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Backend     string   `yaml:"backend,omitempty" json:"backend,omitempty"`
	Source      string   `yaml:"source,omitempty" json:"source,omitempty"`
	Dynamic     bool     `yaml:"dynamic,omitempty" json:"dynamic,omitempty"`
	InstanceIDs []string `yaml:"-" json:"instance_ids,omitempty"`
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
}

type ToolSpec struct {
	Artifacts         []ToolArtifact  `yaml:"artifacts,omitempty" json:"artifacts,omitempty"`
	Description       string          `yaml:"description" json:"description"`
	CapabilitySummary string          `yaml:"capability_summary,omitempty" json:"capability_summary,omitempty"`
	CapabilityDetails string          `yaml:"capability_details,omitempty" json:"capability_details,omitempty"`
	Preconditions     []string        `yaml:"preconditions,omitempty" json:"preconditions,omitempty"`
	SideEffects       []string        `yaml:"side_effects,omitempty" json:"side_effects,omitempty"`
	Parameters        []ToolParameter `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Examples          []ToolExample   `yaml:"examples,omitempty" json:"examples,omitempty"`
	Outputs           *ToolOutputs    `yaml:"outputs,omitempty" json:"outputs,omitempty"`
}

type ToolArtifact struct {
	Field string `yaml:"field" json:"field"`
	Role  string `yaml:"role,omitempty" json:"role,omitempty"`
}

type ToolParameter struct {
	Name            string `yaml:"name" json:"name"`
	Type            string `yaml:"type" json:"type"`
	ResourceType    string `yaml:"resource_type,omitempty" json:"resource_type,omitempty"`
	DefaultResource string `yaml:"default_resource,omitempty" json:"default_resource,omitempty"`
	Required        bool   `yaml:"required,omitempty" json:"required,omitempty"`
	Description     string `yaml:"description,omitempty" json:"description,omitempty"`
	Default         any    `yaml:"default,omitempty" json:"default,omitempty"`
	Values          []any  `yaml:"values,omitempty" json:"values,omitempty"`
	Enum            []any  `yaml:"enum,omitempty" json:"enum,omitempty"`
}

type ToolExample struct {
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Args        map[string]any `yaml:"args,omitempty" json:"args,omitempty"`
}

type ToolExecution struct {
	Provider     string               `yaml:"provider,omitempty" json:"provider,omitempty"`
	Binding      string               `yaml:"binding,omitempty" json:"binding,omitempty"`
	BackendTool  string               `yaml:"backend_tool,omitempty" json:"backend_tool,omitempty"`
	Handler      string               `yaml:"handler,omitempty" json:"handler,omitempty"`
	HandlerFile  string               `yaml:"handler_file,omitempty" json:"handler_file,omitempty"`
	HandlerFunc  string               `yaml:"handler_function,omitempty" json:"handler_function,omitempty"`
	CLI          *CLIExecutionConfig  `yaml:"cli,omitempty" json:"cli,omitempty"`
	ParamMapping map[string]any       `yaml:"param_mapping,omitempty" json:"param_mapping,omitempty"`
	Validation   []ToolValidationRule `yaml:"validation,omitempty" json:"validation,omitempty"`
	Output       *ToolOutput          `yaml:"output,omitempty" json:"output,omitempty"`
	Timeout      string               `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Cancelable   *bool                `yaml:"cancelable,omitempty" json:"cancelable,omitempty"`
	AutoLoad     *bool                `yaml:"auto_load,omitempty" json:"auto_load,omitempty"`
}

type CLIExecutionConfig struct {
	Command    string            `yaml:"command" json:"command"`
	Args       []string          `yaml:"args,omitempty" json:"args,omitempty"`
	WorkingDir string            `yaml:"working_dir,omitempty" json:"working_dir,omitempty"`
	Env        map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Stdin      string            `yaml:"stdin,omitempty" json:"stdin,omitempty"`
	Stdout     string            `yaml:"stdout,omitempty" json:"stdout,omitempty"`
	Timeout    string            `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

type ToolValidationRule struct {
	Rule    string `yaml:"rule" json:"rule"`
	Message string `yaml:"message" json:"message"`
}

type ToolOutput struct {
	AutoLoad bool `yaml:"auto_load,omitempty" json:"auto_load,omitempty"`
}

type ToolOutputs struct {
	SuccessShape string `yaml:"success_shape,omitempty" json:"success_shape,omitempty"`
}

type ToolPolicy struct {
	Visibility          string            `yaml:"visibility,omitempty" json:"visibility,omitempty"`
	Authorization       ToolAuthorization `yaml:"authorization,omitempty" json:"authorization,omitempty"`
	AuthorizationScopes []string          `yaml:"authorization_scopes,omitempty" json:"authorization_scopes,omitempty"`
	AgentExposure       string            `yaml:"agent_exposure,omitempty" json:"agent_exposure,omitempty"`
	PublishWhenUnready  *bool             `yaml:"publish_when_unready,omitempty" json:"publish_when_unready,omitempty"`
	Runtime             string            `yaml:"runtime,omitempty" json:"runtime,omitempty"`
}

type ToolAuthorization struct {
	Scopes []string `yaml:"scopes,omitempty" json:"scopes,omitempty"`
}

type ToolStatus struct {
	ReadinessFrom string `yaml:"readiness_from,omitempty" json:"readiness_from,omitempty"`
}

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
		target = "tool definition"
	}
	if source := strings.TrimSpace(e.SourcePath); source != "" {
		target = fmt.Sprintf("%s (%s)", target, source)
	}
	return fmt.Sprintf("invalid %s: %s", target, strings.Join(e.Problems, "; "))
}

// NormalizeRuntime 规范化 runtime 标识；`bridge` / `compute` 仅作为私有兼容别名输入消化，
// 不再代表任何公开 schema、catalog 或 README 受支持语义。
func NormalizeRuntime(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", RuntimeIDQGIS, RuntimeIDCLI, RuntimeIDInternal:
		return strings.ToLower(strings.TrimSpace(raw))
	case "bridge":
		return RuntimeIDQGIS
	case "compute":
		return RuntimeIDCLI
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func (d *ToolDefinition) normalize() {
	if d == nil {
		return
	}
	d.APIVersion = strings.TrimSpace(d.APIVersion)
	d.Kind = ToolKind(strings.TrimSpace(string(d.Kind)))
	d.Metadata.Name = strings.TrimSpace(d.Metadata.Name)
	d.Metadata.DisplayName = strings.TrimSpace(d.Metadata.DisplayName)
	d.Metadata.Keywords = normalizeStringSlice(d.Metadata.Keywords)
	d.Metadata.Domain = strings.TrimSpace(d.Metadata.Domain)
	d.Metadata.Version = strings.TrimSpace(d.Metadata.Version)
	d.Metadata.Runtime = NormalizeRuntime(d.Metadata.Runtime)
	d.Metadata.Backend = NormalizeRuntime(d.Metadata.Backend)
	d.Metadata.Source = strings.TrimSpace(d.Metadata.Source)
	d.Metadata.InstanceIDs = normalizeStringSlice(d.Metadata.InstanceIDs)
	d.Metadata.Tags = normalizeStringSlice(d.Metadata.Tags)

	d.Spec.Description = strings.TrimSpace(d.Spec.Description)
	d.Spec.CapabilitySummary = strings.TrimSpace(d.Spec.CapabilitySummary)
	d.Spec.CapabilityDetails = strings.TrimSpace(d.Spec.CapabilityDetails)
	d.Spec.Preconditions = normalizeStringSlice(d.Spec.Preconditions)
	d.Spec.SideEffects = normalizeStringSlice(d.Spec.SideEffects)
	for idx := range d.Spec.Parameters {
		d.Spec.Parameters[idx].Name = strings.TrimSpace(d.Spec.Parameters[idx].Name)
		d.Spec.Parameters[idx].Type = strings.TrimSpace(d.Spec.Parameters[idx].Type)
		d.Spec.Parameters[idx].Description = strings.TrimSpace(d.Spec.Parameters[idx].Description)
		if len(d.Spec.Parameters[idx].Values) == 0 && len(d.Spec.Parameters[idx].Enum) > 0 {
			d.Spec.Parameters[idx].Values = cloneAnySlice(d.Spec.Parameters[idx].Enum)
		}
	}
	for idx := range d.Spec.Examples {
		d.Spec.Examples[idx].Description = strings.TrimSpace(d.Spec.Examples[idx].Description)
	}

	d.Policy.Visibility = strings.ToLower(strings.TrimSpace(d.Policy.Visibility))
	d.Policy.Authorization.Scopes = normalizeStringSlice(d.Policy.Authorization.Scopes)
	d.Policy.AuthorizationScopes = normalizeStringSlice(append(append([]string(nil), d.Policy.AuthorizationScopes...), d.Policy.Authorization.Scopes...))
	d.Policy.AgentExposure = strings.ToLower(strings.TrimSpace(d.Policy.AgentExposure))
	d.Policy.Runtime = NormalizeRuntime(d.Policy.Runtime)

	d.Execution.Provider = NormalizeRuntime(d.Execution.Provider)
	d.Execution.Binding = strings.ToLower(strings.TrimSpace(d.Execution.Binding))
	d.Execution.BackendTool = strings.TrimSpace(d.Execution.BackendTool)
	d.Execution.Handler = strings.TrimSpace(d.Execution.Handler)
	d.Execution.HandlerFile = strings.TrimSpace(d.Execution.HandlerFile)
	d.Execution.HandlerFunc = strings.TrimSpace(d.Execution.HandlerFunc)
	d.Execution.Timeout = strings.TrimSpace(d.Execution.Timeout)
	if d.Execution.CLI != nil {
		d.Execution.CLI.Command = strings.TrimSpace(d.Execution.CLI.Command)
		d.Execution.CLI.WorkingDir = strings.TrimSpace(d.Execution.CLI.WorkingDir)
		d.Execution.CLI.Stdin = strings.ToLower(strings.TrimSpace(d.Execution.CLI.Stdin))
		d.Execution.CLI.Stdout = strings.ToLower(strings.TrimSpace(d.Execution.CLI.Stdout))
		d.Execution.CLI.Timeout = strings.TrimSpace(d.Execution.CLI.Timeout)
		if len(d.Execution.CLI.Args) > 0 {
			args := make([]string, 0, len(d.Execution.CLI.Args))
			for _, arg := range d.Execution.CLI.Args {
				args = append(args, strings.TrimSpace(arg))
			}
			d.Execution.CLI.Args = args
		}
		if len(d.Execution.CLI.Env) > 0 {
			env := make(map[string]string, len(d.Execution.CLI.Env))
			for key, value := range d.Execution.CLI.Env {
				trimmedKey := strings.TrimSpace(key)
				if trimmedKey == "" {
					continue
				}
				env[trimmedKey] = strings.TrimSpace(value)
			}
			d.Execution.CLI.Env = env
		}
	}
	if len(d.Execution.ParamMapping) > 0 {
		paramMapping := make(map[string]any, len(d.Execution.ParamMapping))
		for key, value := range d.Execution.ParamMapping {
			trimmedKey := strings.TrimSpace(key)
			if trimmedKey == "" {
				continue
			}
			paramMapping[trimmedKey] = value
		}
		d.Execution.ParamMapping = paramMapping
	}
	for idx := range d.Execution.Validation {
		d.Execution.Validation[idx].Rule = strings.TrimSpace(d.Execution.Validation[idx].Rule)
		d.Execution.Validation[idx].Message = strings.TrimSpace(d.Execution.Validation[idx].Message)
	}
	if d.Execution.Output != nil && d.Execution.AutoLoad == nil {
		autoLoad := d.Execution.Output.AutoLoad
		d.Execution.AutoLoad = &autoLoad
	}
	d.Status.ReadinessFrom = strings.ToLower(strings.TrimSpace(d.Status.ReadinessFrom))

	if d.Policy.Runtime == "" {
		d.Policy.Runtime = d.Metadata.Runtime
	}
	if d.Policy.Runtime == "" {
		d.Policy.Runtime = d.Metadata.Backend
	}
	if d.Execution.Provider == "" {
		d.Execution.Provider = d.Policy.Runtime
	}
	if d.Execution.Provider == "" {
		d.Execution.Provider = d.Metadata.Runtime
	}
	if d.Execution.Provider == "" {
		d.Execution.Provider = d.Metadata.Backend
	}
	if d.Metadata.Runtime == "" {
		d.Metadata.Runtime = d.Policy.Runtime
	}
	if d.Metadata.Runtime == "" {
		d.Metadata.Runtime = d.Execution.Provider
	}
	if d.Metadata.Backend == "" {
		d.Metadata.Backend = d.Metadata.Runtime
	}
	if d.Execution.Binding == "" {
		d.Execution.Binding = inferExecutionBinding(d.Execution)
	}
}

func (d ToolDefinition) Clone() ToolDefinition {
	cloned := d
	cloned.Spec.Artifacts = append([]ToolArtifact(nil), d.Spec.Artifacts...)
	cloned.Metadata.Keywords = append([]string(nil), d.Metadata.Keywords...)
	cloned.Metadata.InstanceIDs = append([]string(nil), d.Metadata.InstanceIDs...)
	cloned.Metadata.Tags = append([]string(nil), d.Metadata.Tags...)
	if len(d.Spec.Preconditions) > 0 {
		cloned.Spec.Preconditions = append([]string(nil), d.Spec.Preconditions...)
	}
	if len(d.Spec.SideEffects) > 0 {
		cloned.Spec.SideEffects = append([]string(nil), d.Spec.SideEffects...)
	}
	if len(d.Spec.Parameters) > 0 {
		cloned.Spec.Parameters = make([]ToolParameter, len(d.Spec.Parameters))
		for idx, param := range d.Spec.Parameters {
			cloned.Spec.Parameters[idx] = param
			if len(param.Values) > 0 {
				cloned.Spec.Parameters[idx].Values = cloneAnySlice(param.Values)
			}
			if len(param.Enum) > 0 {
				cloned.Spec.Parameters[idx].Enum = cloneAnySlice(param.Enum)
			}
		}
	}
	if len(d.Spec.Examples) > 0 {
		cloned.Spec.Examples = make([]ToolExample, len(d.Spec.Examples))
		for idx, example := range d.Spec.Examples {
			cloned.Spec.Examples[idx] = ToolExample{
				Description: example.Description,
				Args:        cloneMapAny(example.Args),
			}
		}
	}
	if d.Spec.Outputs != nil {
		outputs := *d.Spec.Outputs
		cloned.Spec.Outputs = &outputs
	}
	cloned.Policy.Authorization.Scopes = append([]string(nil), d.Policy.Authorization.Scopes...)
	cloned.Policy.AuthorizationScopes = append([]string(nil), d.Policy.AuthorizationScopes...)
	cloned.Execution.ParamMapping = cloneMapAny(d.Execution.ParamMapping)
	cloned.Execution.Validation = append([]ToolValidationRule(nil), d.Execution.Validation...)
	if d.Execution.CLI != nil {
		cli := *d.Execution.CLI
		cli.Args = append([]string(nil), d.Execution.CLI.Args...)
		if len(d.Execution.CLI.Env) > 0 {
			cli.Env = make(map[string]string, len(d.Execution.CLI.Env))
			for key, value := range d.Execution.CLI.Env {
				cli.Env[key] = value
			}
		}
		cloned.Execution.CLI = &cli
	}
	if d.Execution.Output != nil {
		output := *d.Execution.Output
		cloned.Execution.Output = &output
	}
	if d.Execution.Cancelable != nil {
		cancelable := *d.Execution.Cancelable
		cloned.Execution.Cancelable = &cancelable
	}
	if d.Execution.AutoLoad != nil {
		autoLoad := *d.Execution.AutoLoad
		cloned.Execution.AutoLoad = &autoLoad
	}
	if d.Policy.PublishWhenUnready != nil {
		publishWhenUnready := *d.Policy.PublishWhenUnready
		cloned.Policy.PublishWhenUnready = &publishWhenUnready
	}
	return cloned
}

func (d ToolDefinition) Validate() error {
	d.normalize()
	problems := make([]string, 0)
	apiVersion := d.APIVersion
	if apiVersion == "" {
		problems = append(problems, "apiVersion is required")
	} else if apiVersion != ToolAPIVersionV1 && apiVersion != ToolPackageAPIVersionV1 {
		problems = append(problems, fmt.Sprintf("apiVersion must be %q or %q", ToolAPIVersionV1, ToolPackageAPIVersionV1))
	}

	kind := d.Kind
	if kind == "" {
		problems = append(problems, "kind is required")
	} else if kind != ToolKindDefinition && kind != ToolKindPackage {
		problems = append(problems, fmt.Sprintf("kind must be %q or %q", ToolKindDefinition, ToolKindPackage))
	}

	name := d.Metadata.Name
	if name == "" {
		problems = append(problems, "metadata.name is required")
	}
	if d.Metadata.Backend == "" && d.Metadata.Runtime == "" && d.Policy.Runtime == "" && d.Execution.Provider == "" {
		problems = append(problems, "metadata.runtime is required")
	}
	if d.EffectiveDescription() == "" {
		problems = append(problems, "spec.description is required")
	}
	hasHandlerFile := d.Execution.HandlerFile != ""
	hasHandlerFunc := d.Execution.HandlerFunc != ""
	if d.Execution.BackendTool == "" && d.Execution.Handler == "" && !hasHandlerFile && (d.Execution.CLI == nil || d.Execution.CLI.Command == "") {
		problems = append(problems, "execution.backend_tool, execution.handler, or execution.handler_file is required")
	}
	if hasHandlerFile && !hasHandlerFunc {
		problems = append(problems, "execution.handler_function is required when execution.handler_file is set")
	}
	if len(d.Execution.ParamMapping) > 0 {
		for key := range d.Execution.ParamMapping {
			if key == "" {
				problems = append(problems, "execution.param_mapping contains an empty key")
			}
		}
	}
	if d.Policy.Visibility != "" && d.Policy.Visibility != VisibilityPublic && d.Policy.Visibility != VisibilityHidden && d.Policy.Visibility != VisibilityInternal {
		problems = append(problems, fmt.Sprintf("policy.visibility must be one of public, hidden, internal (got %q)", d.Policy.Visibility))
	}
	if d.Policy.AgentExposure != "" && d.Policy.AgentExposure != AgentExposureSkillRefOnly && d.Policy.AgentExposure != AgentExposureSystem {
		problems = append(problems, fmt.Sprintf("policy.agent_exposure must be one of %q, %q", AgentExposureSkillRefOnly, AgentExposureSystem))
	}

	seenParameters := make(map[string]struct{}, len(d.Spec.Parameters))
	for idx, param := range d.Spec.Parameters {
		fieldPrefix := fmt.Sprintf("spec.parameters[%d]", idx)
		paramName := param.Name
		if paramName == "" {
			problems = append(problems, fieldPrefix+".name is required")
		} else {
			if _, exists := seenParameters[paramName]; exists {
				problems = append(problems, fmt.Sprintf("duplicate parameter name %q", paramName))
			} else {
				seenParameters[paramName] = struct{}{}
			}
		}
		if param.Type == "" {
			problems = append(problems, fieldPrefix+".type is required")
		}
		if err := ValidateResourceParameter(param.Type, param.ResourceType, param.DefaultResource); err != nil {
			problems = append(problems, fieldPrefix+": "+err.Error())
		}
	}

	for idx, rule := range d.Execution.Validation {
		fieldPrefix := fmt.Sprintf("execution.validation[%d]", idx)
		if rule.Rule == "" {
			problems = append(problems, fieldPrefix+".rule is required")
		}
		if rule.Message == "" {
			problems = append(problems, fieldPrefix+".message is required")
		}
	}

	if len(problems) > 0 {
		return &ValidationError{
			Name:       name,
			SourcePath: d.SourcePath,
			Problems:   problems,
		}
	}

	return nil
}

func (d ToolDefinition) EffectiveDescription() string {
	d.normalize()
	for _, candidate := range []string{
		d.Spec.Description,
		d.Spec.CapabilitySummary,
		d.Spec.CapabilityDetails,
		d.Metadata.DisplayName,
	} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}

func inferExecutionBinding(execution ToolExecution) string {
	switch {
	case strings.TrimSpace(execution.Binding) != "":
		return strings.ToLower(strings.TrimSpace(execution.Binding))
	case strings.TrimSpace(execution.Handler) != "" || strings.TrimSpace(execution.HandlerFile) != "":
		return "handler"
	case strings.TrimSpace(execution.BackendTool) != "":
		return "backend_tool"
	case execution.CLI != nil && strings.TrimSpace(execution.CLI.Command) != "":
		return "cli"
	default:
		return ""
	}
}

func cloneMapAny(src map[string]any) map[string]any {
	if len(src) == 0 {
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

func cloneAnySlice(values []any) []any {
	if len(values) == 0 {
		return nil
	}
	cloned := cloneAny(values)
	if typed, ok := cloned.([]any); ok {
		return typed
	}
	return nil
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}
