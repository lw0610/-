package skill

import (
	"sort"
	"strings"
	"sync"

	toolpkg "github.com/autocarto/carto_agent/internal/tool"
)

type Registry struct {
	mu          sync.RWMutex
	packages    map[string]SkillPackage
	definitions map[string]SkillDefinition
	dynamicDefs map[string]SkillDefinition
	toolReg     *toolpkg.Registry
	onChange    []func()
}

func NewRegistry(toolRegs ...*toolpkg.Registry) *Registry {
	var toolReg *toolpkg.Registry
	if len(toolRegs) > 0 {
		toolReg = toolRegs[0]
	}
	return &Registry{
		packages:    make(map[string]SkillPackage),
		definitions: make(map[string]SkillDefinition),
		dynamicDefs: make(map[string]SkillDefinition),
		toolReg:     toolReg,
	}
}

func (r *Registry) LoadDirectories(dirs ...string) error {
	loadedPackages := make(map[string]SkillPackage)
	for _, dir := range dirs {
		packages, err := loadPackagesFromDirectory(strings.TrimSpace(dir))
		if err != nil {
			return err
		}
		for _, pkg := range packages {
			if existing, ok := loadedPackages[pkg.Name]; ok {
				loadedPackages[pkg.Name] = mergeSkillPackage(existing, pkg)
				continue
			}
			loadedPackages[pkg.Name] = pkg.Clone()
		}
	}
	if r.toolReg != nil {
		loadedPackages = hydratePackageToolsFromRegistry(loadedPackages, r.toolReg)
	}
	loadedDefinitions, err := buildToolIndex(loadedPackages)
	if err != nil {
		return err
	}

	r.mu.Lock()
	r.packages = loadedPackages
	r.definitions = loadedDefinitions
	r.mu.Unlock()
	r.notifyChange()
	return nil
}

func (r *Registry) Get(name string) (SkillDefinition, bool) {
	return r.GetTool(name)
}

func (r *Registry) GetTool(name string) (SkillDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	trimmedName := strings.TrimSpace(name)
	definition, ok := r.dynamicDefs[trimmedName]
	if !ok {
		definition, ok = r.definitions[trimmedName]
	}
	if !ok {
		definition, ok = r.lookupReferencedToolUnlocked(trimmedName)
	}
	if !ok && r.toolReg != nil {
		toolDef, toolOK := r.toolReg.Get(trimmedName)
		if toolOK && toolDef.Metadata.Dynamic {
			definition = convertToolDefinition(toolDef)
			ok = true
		}
	}
	if !ok {
		return SkillDefinition{}, false
	}
	return definition.Clone(), true
}

func (r *Registry) Definitions() []SkillDefinition {
	return r.AllDefinitions()
}

func (r *Registry) ToolDefinitions() []SkillDefinition {
	return r.AllDefinitions()
}

func (r *Registry) AllDefinitions() []SkillDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	definitions := make([]SkillDefinition, 0, len(r.definitions)+len(r.dynamicDefs))
	seen := make(map[string]struct{}, len(r.definitions)+len(r.dynamicDefs))
	for _, definition := range r.definitions {
		seen[definition.Metadata.Name] = struct{}{}
		definitions = append(definitions, definition.Clone())
	}
	for _, definition := range r.dynamicDefs {
		seen[definition.Metadata.Name] = struct{}{}
		definitions = append(definitions, definition.Clone())
	}
	if r.toolReg != nil {
		for _, toolDef := range r.toolReg.All() {
			if !toolDef.Metadata.Dynamic {
				continue
			}
			name := strings.TrimSpace(toolDef.Metadata.Name)
			if name == "" {
				continue
			}
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			definitions = append(definitions, convertToolDefinition(toolDef))
		}
		for _, pkg := range r.packages {
			if !pkg.Enabled || len(pkg.ToolRefs) == 0 {
				continue
			}
			for _, toolRef := range pkg.ToolRefs {
				definition, ok := r.lookupReferencedToolUnlocked(toolRef)
				if !ok {
					continue
				}
				name := strings.TrimSpace(definition.Metadata.Name)
				if name == "" {
					continue
				}
				if _, exists := seen[name]; exists {
					continue
				}
				seen[name] = struct{}{}
				definitions = append(definitions, definition.Clone())
			}
		}
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Metadata.Name < definitions[j].Metadata.Name
	})
	return definitions
}

func (r *Registry) RegisterDynamic(defs []SkillDefinition) {
	if r == nil || len(defs) == 0 {
		return
	}
	changed := false
	r.mu.Lock()
	for _, def := range defs {
		name := strings.TrimSpace(def.Metadata.Name)
		if name == "" {
			continue
		}
		cloned := def.Clone()
		cloned.normalize()
		r.dynamicDefs[name] = cloned
		changed = true
	}
	r.mu.Unlock()
	if changed {
		r.notifyChange()
	}
}

func (r *Registry) UnregisterDynamic(names []string) {
	if r == nil || len(names) == 0 {
		return
	}
	changed := false
	r.mu.Lock()
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := r.dynamicDefs[trimmed]; ok {
			delete(r.dynamicDefs, trimmed)
			changed = true
		}
	}
	r.mu.Unlock()
	if changed {
		r.notifyChange()
	}
}

func (r *Registry) OnChange(fn func()) {
	if r == nil || fn == nil {
		return
	}
	r.mu.Lock()
	r.onChange = append(r.onChange, fn)
	r.mu.Unlock()
}

func (r *Registry) GetPackage(name string) (SkillPackage, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	trimmedName := strings.TrimSpace(name)
	pkg, ok := r.packages[trimmedName]
	if !ok {
		return SkillPackage{}, false
	}
	return pkg.publicView(), true
}

func (r *Registry) Packages() []SkillPackage {
	r.mu.RLock()
	defer r.mu.RUnlock()
	packages := make([]SkillPackage, 0, len(r.packages))
	for _, pkg := range r.packages {
		packages = append(packages, pkg.publicView())
	}
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].Name < packages[j].Name
	})
	return packages
}

func (r *Registry) CategoryPackages() []SkillPackage {
	return r.packagesByKind(SkillPackageKindCategory)
}

func (r *Registry) WorkflowPackages() []SkillPackage {
	return r.packagesByKind(SkillPackageKindWorkflow)
}

func (r *Registry) ToolsForPackages(packageNames []string) []SkillDefinition {
	selected := make(map[string]struct{}, len(packageNames))
	for _, packageName := range packageNames {
		trimmed := strings.TrimSpace(packageName)
		if trimmed != "" {
			selected[trimmed] = struct{}{}
		}
	}
	if len(selected) == 0 {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	definitions := make([]SkillDefinition, 0)
	seen := make(map[string]struct{})
	for packageName := range selected {
		pkg, ok := r.packages[packageName]
		if !ok || !pkg.Enabled {
			continue
		}
		if len(pkg.ToolRefs) > 0 && r.toolReg != nil {
			for _, toolRef := range pkg.ToolRefs {
				toolDef, ok := r.toolReg.Get(toolRef)
				if !ok {
					continue
				}
				name := strings.TrimSpace(toolDef.Metadata.Name)
				if name == "" {
					continue
				}
				if _, exists := seen[name]; exists {
					continue
				}
				seen[name] = struct{}{}
				definitions = append(definitions, convertToolDefinition(toolDef))
			}
			continue
		}
		for _, definition := range pkg.Tools {
			name := strings.TrimSpace(definition.Metadata.Name)
			if name == "" {
				continue
			}
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			definitions = append(definitions, definition.Clone())
		}
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Metadata.Name < definitions[j].Metadata.Name
	})
	return definitions
}

func (r *Registry) packagesByKind(kind string) []SkillPackage {
	r.mu.RLock()
	defer r.mu.RUnlock()
	packages := make([]SkillPackage, 0)
	for _, pkg := range r.packages {
		if pkg.Kind == kind {
			packages = append(packages, pkg.publicView())
		}
	}
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].Name < packages[j].Name
	})
	return packages
}

func buildToolIndex(packages map[string]SkillPackage) (map[string]SkillDefinition, error) {
	if len(packages) == 0 {
		return make(map[string]SkillDefinition), nil
	}
	packageNames := make([]string, 0, len(packages))
	for packageName := range packages {
		packageNames = append(packageNames, packageName)
	}
	sort.Strings(packageNames)

	definitions := make(map[string]SkillDefinition)
	for _, packageName := range packageNames {
		pkg := packages[packageName]
		if !pkg.Enabled {
			continue
		}
		for _, definition := range pkg.Tools {
			toolName := strings.TrimSpace(definition.Metadata.Name)
			if toolName == "" {
				continue
			}
			if _, ok := definitions[toolName]; ok {
				continue
			}
			definitions[toolName] = definition.Clone()
		}
	}
	return definitions, nil
}

func hydratePackageToolsFromRegistry(packages map[string]SkillPackage, toolReg *toolpkg.Registry) map[string]SkillPackage {
	if len(packages) == 0 || toolReg == nil {
		return packages
	}
	hydrated := make(map[string]SkillPackage, len(packages))
	for name, pkg := range packages {
		cloned := pkg.Clone()
		if len(cloned.ToolRefs) > 0 {
			cloned.Tools = make([]SkillDefinition, 0, len(cloned.ToolRefs))
			seen := make(map[string]struct{}, len(cloned.ToolRefs))
			for _, toolRef := range cloned.ToolRefs {
				trimmed := strings.TrimSpace(toolRef)
				if trimmed == "" {
					continue
				}
				if _, exists := seen[trimmed]; exists {
					continue
				}
				toolDef, ok := toolReg.Get(trimmed)
				if !ok {
					continue
				}
				seen[trimmed] = struct{}{}
				cloned.Tools = append(cloned.Tools, convertToolDefinition(toolDef))
			}
		}
		hydrated[name] = cloned
	}
	return hydrated
}

func mergeSkillPackage(base SkillPackage, override SkillPackage) SkillPackage {
	merged := base.Clone()
	override = override.Clone()
	merged.Name = override.Name
	merged.Dir = override.Dir
	merged.Kind = override.Kind
	merged.FrontMatter = override.FrontMatter
	merged.MarkdownBody = override.MarkdownBody
	merged.Config = override.Config
	merged.RequiresTools = append([]string(nil), override.RequiresTools...)
	if len(override.ToolRefs) > 0 {
		merged.ToolRefs = append([]string(nil), override.ToolRefs...)
	}
	merged.Source = override.Source
	merged.Enabled = override.Enabled

	toolsByName := make(map[string]SkillDefinition, len(base.Tools)+len(override.Tools))
	for _, definition := range base.Tools {
		toolName := strings.TrimSpace(definition.Metadata.Name)
		if toolName == "" {
			continue
		}
		toolsByName[toolName] = definition.Clone()
	}
	for _, definition := range override.Tools {
		toolName := strings.TrimSpace(definition.Metadata.Name)
		if toolName == "" {
			continue
		}
		toolsByName[toolName] = definition.Clone()
	}
	toolNames := make([]string, 0, len(toolsByName))
	for toolName := range toolsByName {
		toolNames = append(toolNames, toolName)
	}
	sort.Strings(toolNames)
	merged.Tools = make([]SkillDefinition, 0, len(toolNames))
	for _, toolName := range toolNames {
		merged.Tools = append(merged.Tools, toolsByName[toolName])
	}
	return merged
}

func (r *Registry) lookupReferencedToolUnlocked(name string) (SkillDefinition, bool) {
	if r == nil || r.toolReg == nil {
		return SkillDefinition{}, false
	}
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return SkillDefinition{}, false
	}
	for _, pkg := range r.packages {
		if !pkg.Enabled || len(pkg.ToolRefs) == 0 {
			continue
		}
		for _, toolRef := range pkg.ToolRefs {
			if strings.TrimSpace(toolRef) != trimmedName {
				continue
			}
			toolDef, ok := r.toolReg.Get(trimmedName)
			if !ok {
				return SkillDefinition{}, false
			}
			return convertToolDefinition(toolDef), true
		}
	}
	return SkillDefinition{}, false
}

func convertToolDefinition(def toolpkg.ToolDefinition) SkillDefinition {
	backendTool := strings.TrimSpace(def.Execution.BackendTool)
	// QGIS resolves its algorithm/handler from the authority manifest by public
	// tool name. Backend algorithm IDs are not remote tool registry keys.
	if def.EffectiveRuntime() == toolpkg.RuntimeIDQGIS || backendTool == "" && hasToolHandlerBinding(def.Execution) {
		backendTool = strings.TrimSpace(def.Metadata.Name)
	}
	return SkillDefinition{
		APIVersion: SkillAPIVersionV2,
		Kind:       SkillKindAtomic,
		Metadata: SkillMetadata{
			Name:        strings.TrimSpace(def.Metadata.Name),
			DisplayName: strings.TrimSpace(def.Metadata.DisplayName),
			Keywords:    append([]string(nil), def.Metadata.Keywords...),
			Category:    strings.TrimSpace(def.Metadata.Domain),
			Runtime:     NormalizeRuntime(def.Metadata.Runtime),
			Backend:     NormalizeRuntime(def.Metadata.Backend),
			Source:      strings.TrimSpace(def.Metadata.Source),
			Dynamic:     def.Metadata.Dynamic,
			InstanceIDs: append([]string(nil), def.Metadata.InstanceIDs...),
			Tags:        append([]string(nil), def.Metadata.Tags...),
		},
		Spec: SkillSpec{
			Description: strings.TrimSpace(def.Spec.Description),
			Parameters:  convertToolParameters(def.Spec.Parameters),
			Examples:    convertToolExamples(def.Spec.Examples),
		},
		Execution: SkillExecution{
			BackendTool:  backendTool,
			CLI:          convertToolCLIExecution(def.Execution.CLI),
			ParamMapping: cloneMapAny(def.Execution.ParamMapping),
			Validation:   convertToolValidation(def.Execution.Validation),
			Output:       convertToolOutput(def.Execution.Output),
		},
		SourcePath: def.SourcePath,
	}
}

func hasToolHandlerBinding(execution toolpkg.ToolExecution) bool {
	return strings.TrimSpace(execution.Handler) != "" ||
		strings.TrimSpace(execution.HandlerFile) != "" ||
		strings.TrimSpace(execution.HandlerFunc) != ""
}

func convertToolCLIExecution(cli *toolpkg.CLIExecutionConfig) *CLIExecutionConfig {
	if cli == nil {
		return nil
	}
	converted := &CLIExecutionConfig{
		Command:    cli.Command,
		Args:       append([]string(nil), cli.Args...),
		WorkingDir: cli.WorkingDir,
		Stdin:      cli.Stdin,
		Stdout:     cli.Stdout,
		Timeout:    cli.Timeout,
	}
	if len(cli.Env) > 0 {
		converted.Env = make(map[string]string, len(cli.Env))
		for key, value := range cli.Env {
			converted.Env[key] = value
		}
	}
	return converted
}

func convertToolParameters(params []toolpkg.ToolParameter) []SkillParameter {
	if len(params) == 0 {
		return nil
	}
	converted := make([]SkillParameter, len(params))
	for idx, param := range params {
		converted[idx] = SkillParameter{
			Schema:          cloneMapAny(param.Schema),
			Name:            param.Name,
			Type:            param.Type,
			ResourceType:    param.ResourceType,
			DefaultResource: param.DefaultResource,
			Required:        param.Required,
			Description:     param.Description,
			Default:         cloneAny(param.Default),
		}
		if len(param.Values) > 0 {
			if values, ok := cloneAny(param.Values).([]any); ok {
				converted[idx].Values = values
			}
		}
	}
	return converted
}

func convertToolExamples(examples []toolpkg.ToolExample) []SkillExample {
	if len(examples) == 0 {
		return nil
	}
	converted := make([]SkillExample, len(examples))
	for idx, example := range examples {
		converted[idx] = SkillExample{
			Description: example.Description,
			Args:        cloneMapAny(example.Args),
		}
	}
	return converted
}

func convertToolValidation(rules []toolpkg.ToolValidationRule) []SkillValidationRule {
	if len(rules) == 0 {
		return nil
	}
	converted := make([]SkillValidationRule, len(rules))
	for idx, rule := range rules {
		converted[idx] = SkillValidationRule{Rule: rule.Rule, Message: rule.Message}
	}
	return converted
}

func convertToolOutput(output *toolpkg.ToolOutput) *SkillOutput {
	if output == nil {
		return nil
	}
	return &SkillOutput{AutoLoad: output.AutoLoad}
}

func (r *Registry) notifyChange() {
	if r == nil {
		return
	}
	r.mu.RLock()
	callbacks := append([]func(){}, r.onChange...)
	r.mu.RUnlock()
	for _, callback := range callbacks {
		callback()
	}
}
