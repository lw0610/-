package skill

import (
	"fmt"
	"maps"
	"strings"

	"github.com/autocarto/carto_agent/internal/workflow"
)

const SkillAPIVersionV1 = "skill/v1"

const SkillAPIVersionV2 = "skill/v2"

const (
	SkillPackageKindCategory  = "category"
	SkillPackageKindWorkflow  = "workflow"
	SkillPackageKindKnowledge = "knowledge"
)

type SkillKind string

const (
	SkillKindAtomic    SkillKind = "AtomicSkill"
	SkillKindComposite SkillKind = "CompositeSkill"
)

type SkillDefinition struct {
	APIVersion string         `yaml:"apiVersion" json:"apiVersion"`
	Kind       SkillKind      `yaml:"kind" json:"kind"`
	Metadata   SkillMetadata  `yaml:"metadata" json:"metadata"`
	Spec       SkillSpec      `yaml:"spec" json:"spec"`
	Execution  SkillExecution `yaml:"execution" json:"execution"`
	SourcePath string         `yaml:"-" json:"sourcePath,omitempty"`
}

type SkillMetadata struct {
	Name        string   `yaml:"name" json:"name"`
	DisplayName string   `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	Category    string   `yaml:"category,omitempty" json:"category,omitempty"`
	Runtime     string   `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Backend     string   `yaml:"backend,omitempty" json:"backend,omitempty"`
	Source      string   `yaml:"source,omitempty" json:"source,omitempty"`
	Dynamic     bool     `yaml:"dynamic,omitempty" json:"dynamic,omitempty"`
	InstanceIDs []string `yaml:"-" json:"instance_ids,omitempty"`
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
}

type SkillFrontMatter struct {
	Name          string   `yaml:"name" json:"name"`
	Description   string   `yaml:"description" json:"description"`
	Kind          string   `yaml:"kind" json:"kind"`
	Runtime       string   `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Backend       string   `yaml:"backend,omitempty" json:"backend,omitempty"`
	Enabled       *bool    `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Tags          []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	ToolRefs      []string `yaml:"tool_refs,omitempty" json:"tool_refs,omitempty"`
	Tools         []string `yaml:"tools,omitempty" json:"tools,omitempty"`
	RequiresTools []string `yaml:"requires_tools,omitempty" json:"requires_tools,omitempty"`
}

type SkillPackageConfig struct {
	Enabled  *bool          `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Settings map[string]any `yaml:",inline" json:"settings,omitempty"`
}

type SkillPackage struct {
	Name          string               `json:"name"`
	Dir           string               `json:"dir"`
	Kind          string               `json:"kind"`
	FrontMatter   SkillFrontMatter     `json:"front_matter"`
	MarkdownBody  string               `json:"markdown_body"`
	Config        *SkillPackageConfig  `json:"config,omitempty"`
	Tools         []SkillDefinition    `json:"-"`
	ToolRefs      []string             `json:"tool_refs,omitempty"`
	Workflow      *workflow.Definition `json:"workflow,omitempty"`
	RequiresTools []string             `json:"requires_tools,omitempty"`
	Source        string               `json:"source,omitempty"`
	Enabled       bool                 `json:"enabled"`
}

type SkillSpec struct {
	Description string           `yaml:"description" json:"description"`
	Parameters  []SkillParameter `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Examples    []SkillExample   `yaml:"examples,omitempty" json:"examples,omitempty"`
}

type SkillParameter struct {
	Name        string `yaml:"name" json:"name"`
	Type        string `yaml:"type" json:"type"`
	Required    bool   `yaml:"required,omitempty" json:"required,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Default     any    `yaml:"default,omitempty" json:"default,omitempty"`
	Values      []any  `yaml:"values,omitempty" json:"values,omitempty"`
}

type SkillExample struct {
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Args        map[string]any `yaml:"args,omitempty" json:"args,omitempty"`
}

type SkillExecution struct {
	BackendTool  string                `yaml:"backend_tool" json:"backend_tool"`
	CLI          *CLIExecutionConfig   `yaml:"cli,omitempty" json:"cli,omitempty"`
	ParamMapping map[string]any        `yaml:"param_mapping" json:"param_mapping"`
	Validation   []SkillValidationRule `yaml:"validation,omitempty" json:"validation,omitempty"`
	Output       *SkillOutput          `yaml:"output,omitempty" json:"output,omitempty"`
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

type SkillValidationRule struct {
	Rule    string `yaml:"rule" json:"rule"`
	Message string `yaml:"message" json:"message"`
}

type SkillOutput struct {
	AutoLoad bool `yaml:"auto_load,omitempty" json:"auto_load,omitempty"`
}

func (d *SkillDefinition) normalize() {
	if d == nil {
		return
	}
	d.APIVersion = strings.TrimSpace(d.APIVersion)
	d.Kind = SkillKind(strings.TrimSpace(string(d.Kind)))
	d.Metadata.Name = strings.TrimSpace(d.Metadata.Name)
	d.Metadata.DisplayName = strings.TrimSpace(d.Metadata.DisplayName)
	d.Metadata.Category = strings.TrimSpace(d.Metadata.Category)
	d.Metadata.Runtime = NormalizeRuntime(d.Metadata.Runtime)
	d.Metadata.Backend = NormalizeRuntime(d.Metadata.Backend)
	d.Metadata.Source = strings.TrimSpace(d.Metadata.Source)
	if d.Metadata.Runtime == "" {
		d.Metadata.Runtime = d.Metadata.Backend
	}
	if d.Metadata.Backend == "" {
		d.Metadata.Backend = d.Metadata.Runtime
	}
	if len(d.Metadata.Tags) > 0 {
		tags := make([]string, 0, len(d.Metadata.Tags))
		for _, tag := range d.Metadata.Tags {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				tags = append(tags, tag)
			}
		}
		d.Metadata.Tags = tags
	}
	d.Metadata.InstanceIDs = normalizeStringSlice(d.Metadata.InstanceIDs)
	d.Spec.Description = strings.TrimSpace(d.Spec.Description)
	for idx := range d.Spec.Parameters {
		d.Spec.Parameters[idx].Name = strings.TrimSpace(d.Spec.Parameters[idx].Name)
		d.Spec.Parameters[idx].Type = strings.TrimSpace(d.Spec.Parameters[idx].Type)
		d.Spec.Parameters[idx].Description = strings.TrimSpace(d.Spec.Parameters[idx].Description)
	}
	for idx := range d.Spec.Examples {
		d.Spec.Examples[idx].Description = strings.TrimSpace(d.Spec.Examples[idx].Description)
	}
	d.Execution.BackendTool = strings.TrimSpace(d.Execution.BackendTool)
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
			paramMapping[strings.TrimSpace(key)] = value
		}
		d.Execution.ParamMapping = paramMapping
	}
	for idx := range d.Execution.Validation {
		d.Execution.Validation[idx].Rule = strings.TrimSpace(d.Execution.Validation[idx].Rule)
		d.Execution.Validation[idx].Message = strings.TrimSpace(d.Execution.Validation[idx].Message)
	}
}

func (f *SkillFrontMatter) normalize() {
	if f == nil {
		return
	}
	f.Name = strings.TrimSpace(f.Name)
	f.Description = strings.TrimSpace(f.Description)
	f.Kind = strings.ToLower(strings.TrimSpace(f.Kind))
	f.Runtime = NormalizeRuntime(f.Runtime)
	f.Backend = NormalizeRuntime(f.Backend)
	if f.Runtime == "" {
		f.Runtime = f.Backend
	}
	if f.Backend == "" {
		f.Backend = f.Runtime
	}
	f.Tags = normalizeStringSlice(f.Tags)
	f.ToolRefs = normalizeStringSlice(f.ToolRefs)
	f.Tools = normalizeStringSlice(f.Tools)
	f.RequiresTools = normalizeStringSlice(f.RequiresTools)
}

func (c *SkillPackageConfig) normalize() {
	if c == nil {
		return
	}
	if len(c.Settings) > 0 {
		normalized := make(map[string]any, len(c.Settings))
		for key, value := range c.Settings {
			trimmed := strings.TrimSpace(key)
			if trimmed == "" {
				continue
			}
			normalized[trimmed] = value
		}
		c.Settings = normalized
	}
}

func (p *SkillPackage) normalize() {
	if p == nil {
		return
	}
	p.Name = strings.TrimSpace(p.Name)
	p.Dir = strings.TrimSpace(p.Dir)
	p.Kind = strings.ToLower(strings.TrimSpace(p.Kind))
	p.MarkdownBody = strings.TrimSpace(p.MarkdownBody)
	p.Source = strings.TrimSpace(p.Source)
	p.FrontMatter.normalize()
	if p.Config != nil {
		p.Config.normalize()
	}
	if p.Name == "" {
		p.Name = p.FrontMatter.Name
	}
	if p.Name == "" && p.Dir != "" {
		p.Name = strings.TrimSpace(filepathBase(p.Dir))
	}
	p.RequiresTools = normalizeStringSlice(p.FrontMatter.RequiresTools)
	explicitToolRefs := normalizeStringSlice(p.FrontMatter.ToolRefs)
	if len(explicitToolRefs) == 0 {
		explicitToolRefs = normalizeStringSlice(p.FrontMatter.Tools)
	}
	p.ToolRefs = explicitToolRefs
	if len(p.ToolRefs) == 0 {
		p.ToolRefs = append([]string(nil), p.RequiresTools...)
	}
	p.Kind = normalizeSkillPackageKind(p.Kind, p.FrontMatter.Kind, p.Name, len(explicitToolRefs) > 0)
	p.Enabled = effectiveSkillPackageEnabled(p.FrontMatter, p.Config)
	p.FrontMatter.Name = p.Name
	p.FrontMatter.Kind = p.Kind
	p.FrontMatter.Tags = normalizeStringSlice(p.FrontMatter.Tags)
	p.FrontMatter.ToolRefs = append([]string(nil), p.ToolRefs...)
	p.FrontMatter.Tools = append([]string(nil), p.ToolRefs...)
	p.FrontMatter.RequiresTools = append([]string(nil), p.RequiresTools...)
	for idx := range p.Tools {
		p.Tools[idx].normalize()
	}
}

func (d SkillDefinition) Clone() SkillDefinition {
	cloned := d
	cloned.Metadata.InstanceIDs = append([]string(nil), d.Metadata.InstanceIDs...)
	cloned.Metadata.Tags = append([]string(nil), d.Metadata.Tags...)
	if len(d.Spec.Parameters) > 0 {
		cloned.Spec.Parameters = make([]SkillParameter, len(d.Spec.Parameters))
		for idx, param := range d.Spec.Parameters {
			cloned.Spec.Parameters[idx] = param
			if len(param.Values) > 0 {
				values := cloneAny(param.Values)
				if typed, ok := values.([]any); ok {
					cloned.Spec.Parameters[idx].Values = typed
				}
			}
		}
	}
	if len(d.Spec.Examples) > 0 {
		cloned.Spec.Examples = make([]SkillExample, len(d.Spec.Examples))
		for idx, example := range d.Spec.Examples {
			cloned.Spec.Examples[idx] = SkillExample{
				Description: example.Description,
				Args:        cloneMapAny(example.Args),
			}
		}
	}
	cloned.Execution.ParamMapping = cloneMapAny(d.Execution.ParamMapping)
	cloned.Execution.Validation = append([]SkillValidationRule(nil), d.Execution.Validation...)
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
	return cloned
}

func (p SkillPackage) Clone() SkillPackage {
	cloned := p
	cloned.FrontMatter.Tags = append([]string(nil), p.FrontMatter.Tags...)
	cloned.FrontMatter.ToolRefs = append([]string(nil), p.FrontMatter.ToolRefs...)
	cloned.FrontMatter.Tools = append([]string(nil), p.FrontMatter.Tools...)
	cloned.FrontMatter.RequiresTools = append([]string(nil), p.FrontMatter.RequiresTools...)
	cloned.ToolRefs = append([]string(nil), p.ToolRefs...)
	cloned.RequiresTools = append([]string(nil), p.RequiresTools...)
	if len(p.Tools) > 0 {
		cloned.Tools = make([]SkillDefinition, len(p.Tools))
		for idx, tool := range p.Tools {
			cloned.Tools[idx] = tool.Clone()
		}
	}
	if p.Config != nil {
		config := *p.Config
		config.Settings = cloneMapAny(p.Config.Settings)
		cloned.Config = &config
	}
	if p.Workflow != nil {
		workflowDef := *p.Workflow
		workflowDef.Spec.Inputs = cloneWorkflowInputs(p.Workflow.Spec.Inputs)
		workflowDef.Spec.Steps = cloneWorkflowSteps(p.Workflow.Spec.Steps)
		cloned.Workflow = &workflowDef
	}
	return cloned
}

func (p SkillPackage) publicView() SkillPackage {
	cloned := p.Clone()
	cloned.Tools = nil
	return cloned
}

func cloneWorkflowInputs(src map[string]workflow.Input) map[string]workflow.Input {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]workflow.Input, len(src))
	maps.Copy(out, src)
	return out
}

func cloneWorkflowSteps(src []workflow.Step) []workflow.Step {
	if len(src) == 0 {
		return nil
	}
	out := make([]workflow.Step, len(src))
	for idx, step := range src {
		out[idx] = step
		out[idx].With = cloneMapAny(step.With)
	}
	return out
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
		target = "skill definition"
	}
	if source := strings.TrimSpace(e.SourcePath); source != "" {
		target = fmt.Sprintf("%s (%s)", target, source)
	}
	return fmt.Sprintf("invalid %s: %s", target, strings.Join(e.Problems, "; "))
}

func (d SkillDefinition) Validate() error {
	d.normalize()
	problems := make([]string, 0)
	apiVersion := d.APIVersion
	if apiVersion == "" {
		problems = append(problems, "apiVersion is required")
	} else if apiVersion != SkillAPIVersionV1 && apiVersion != SkillAPIVersionV2 {
		problems = append(problems, fmt.Sprintf("apiVersion must be %q or %q", SkillAPIVersionV1, SkillAPIVersionV2))
	}

	kind := d.Kind
	if kind == "" {
		problems = append(problems, "kind is required")
	} else if kind != SkillKindAtomic {
		problems = append(problems, fmt.Sprintf("kind must be %q", SkillKindAtomic))
	}

	name := d.Metadata.Name
	if name == "" {
		problems = append(problems, "metadata.name is required")
	}
	if d.Metadata.Backend == "" && d.Metadata.Runtime == "" {
		problems = append(problems, "metadata.runtime is required")
	}
	if d.Spec.Description == "" {
		problems = append(problems, "spec.description is required")
	}
	if d.Execution.BackendTool == "" && (d.Execution.CLI == nil || d.Execution.CLI.Command == "") {
		problems = append(problems, "execution.backend_tool is required")
	}
	if len(d.Execution.ParamMapping) > 0 {
		for key := range d.Execution.ParamMapping {
			if key == "" {
				problems = append(problems, "execution.param_mapping contains an empty key")
			}
		}
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

func normalizeSkillPackageKind(current string, frontMatter string, name string, hasTools bool) string {
	kind := strings.ToLower(strings.TrimSpace(current))
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(frontMatter))
	}
	if kind != "" {
		return kind
	}
	if strings.EqualFold(strings.TrimSpace(name), "prompt-refinement") {
		return SkillPackageKindKnowledge
	}
	if hasTools {
		return SkillPackageKindCategory
	}
	return SkillPackageKindWorkflow
}

func effectiveSkillPackageEnabled(frontMatter SkillFrontMatter, cfg *SkillPackageConfig) bool {
	if cfg != nil && cfg.Enabled != nil {
		return *cfg.Enabled
	}
	if frontMatter.Enabled != nil {
		return *frontMatter.Enabled
	}
	return true
}

func filepathBase(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimRight(trimmed, "/\\")
	idx := strings.LastIndexAny(trimmed, `/\\`)
	if idx < 0 {
		return trimmed
	}
	return trimmed[idx+1:]
}
