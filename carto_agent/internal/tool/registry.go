package tool

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Registry struct {
	mu          sync.RWMutex
	definitions map[string]ToolDefinition
	dynamicDefs map[string]ToolDefinition
	onChange    []func()
}

func NewRegistry() *Registry {
	return &Registry{
		definitions: make(map[string]ToolDefinition),
		dynamicDefs: make(map[string]ToolDefinition),
	}
}

func (r *Registry) LoadDefinitions(definitions []ToolDefinition) error {
	loadedDefinitions := make(map[string]ToolDefinition, len(definitions))
	for _, definition := range definitions {
		cloned := definition.Clone()
		cloned.normalize()
		if err := cloned.Validate(); err != nil {
			return err
		}
		name := strings.TrimSpace(cloned.Metadata.Name)
		if name == "" {
			continue
		}
		if existing, ok := loadedDefinitions[name]; ok {
			return fmt.Errorf("duplicate tool %s found in %s and %s", name, existing.SourcePath, cloned.SourcePath)
		}
		loadedDefinitions[name] = cloned
	}

	r.mu.Lock()
	r.definitions = loadedDefinitions
	r.mu.Unlock()
	r.notifyChange()
	return nil
}

func (r *Registry) LoadDirectories(dirs ...string) error {
	definitions, err := LoadDefinitionsFromDirectories(dirs...)
	if err != nil {
		return err
	}
	return r.LoadDefinitions(definitions)
}

func (r *Registry) Get(name string) (ToolDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	trimmedName := strings.TrimSpace(name)
	definition, ok := r.dynamicDefs[trimmedName]
	if !ok {
		definition, ok = r.definitions[trimmedName]
	}
	if !ok {
		return ToolDefinition{}, false
	}
	return definition.Clone(), true
}

func (r *Registry) All() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	definitions := make([]ToolDefinition, 0, len(r.definitions)+len(r.dynamicDefs))
	for _, definition := range r.definitions {
		definitions = append(definitions, definition.Clone())
	}
	for _, definition := range r.dynamicDefs {
		definitions = append(definitions, definition.Clone())
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Metadata.Name < definitions[j].Metadata.Name
	})
	return definitions
}

func (r *Registry) RegisterDynamic(defs []ToolDefinition) {
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
