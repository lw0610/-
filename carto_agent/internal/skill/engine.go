package skill

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type skillDefinitionContextKey struct{}

func WithSkillDefinition(ctx context.Context, definition SkillDefinition) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	cloned := definition.Clone()
	cloned.normalize()
	return context.WithValue(ctx, skillDefinitionContextKey{}, &cloned)
}

func SkillDefinitionFromContext(ctx context.Context) *SkillDefinition {
	if ctx == nil {
		return nil
	}
	definition, _ := ctx.Value(skillDefinitionContextKey{}).(*SkillDefinition)
	if definition == nil {
		return nil
	}
	cloned := definition.Clone()
	cloned.normalize()
	return &cloned
}

type ExecutionResult struct {
	SkillName    string         `json:"skill_name"`
	BackendID    string         `json:"backend_id"`
	ToolName     string         `json:"tool_name"`
	Params       map[string]any `json:"params,omitempty"`
	Output       any            `json:"output,omitempty"`
	OutputLayers []OutputLayer  `json:"output_layers,omitempty"`
}

type Engine struct {
	registry *Registry

	mu              sync.RWMutex
	backends        map[string]ToolBackend
	autoLoadHandler func(context.Context, map[string]any, []OutputLayer)
}

func NewEngine(registry *Registry, backends ...ToolBackend) *Engine {
	engine := &Engine{
		registry: registry,
		backends: make(map[string]ToolBackend),
	}
	for _, backend := range backends {
		engine.RegisterBackend(backend)
	}
	return engine
}

func (e *Engine) RegisterBackend(backend ToolBackend) {
	if e == nil || backend == nil || strings.TrimSpace(backend.ID()) == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.backends[strings.TrimSpace(backend.ID())] = backend
}

func (e *Engine) SetAutoLoadHandler(handler func(context.Context, map[string]any, []OutputLayer)) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.autoLoadHandler = handler
}

func (e *Engine) Execute(ctx context.Context, skillName string, args map[string]any) (*ExecutionResult, error) {
	if e == nil || e.registry == nil {
		return nil, fmt.Errorf("skill engine registry is nil")
	}

	definition, ok := e.registry.Get(skillName)
	if !ok {
		return nil, fmt.Errorf("skill %s not found", strings.TrimSpace(skillName))
	}
	if definition.Kind != SkillKindAtomic {
		return nil, fmt.Errorf("skill %s kind %s is not executable", definition.Metadata.Name, definition.Kind)
	}

	session := SessionContextFromContext(ctx)
	preparedArgs := applyParameterDefaults(definition, args)
	preparedArgs = applyExecutionOutputDefaults(definition, preparedArgs, session)
	if err := validateExecutionArgs(definition, preparedArgs, session); err != nil {
		return nil, err
	}

	resolvedParams, err := resolveParamMapping(definition.Execution.ParamMapping, preparedArgs, definition.Spec.Parameters, session, definition.SourcePath)
	if err != nil {
		return nil, err
	}

	runtimeID := definition.Metadata.Runtime
	if runtimeID == "" {
		runtimeID = definition.Metadata.Backend
	}
	backend, ok := e.lookupBackend(runtimeID)
	if !ok {
		return nil, fmt.Errorf("skill runtime %s is not registered", runtimeID)
	}

	execCtx := WithSkillDefinition(ctx, definition)
	backendResult, err := backend.Execute(execCtx, definition.Execution.BackendTool, resolvedParams)
	if err != nil {
		return nil, err
	}
	if backendResult == nil {
		return nil, fmt.Errorf("backend %s returned nil result", backend.ID())
	}
	if !backendResult.Success {
		if backendResult.Error != "" {
			return nil, fmt.Errorf("skill %s execute failed: %s", definition.Metadata.Name, backendResult.Error)
		}
		return nil, fmt.Errorf("skill %s execute failed", definition.Metadata.Name)
	}

	e.triggerAutoLoad(ctx, definition, resolvedParams, backendResult.OutputLayers)

	return &ExecutionResult{
		SkillName:    definition.Metadata.Name,
		BackendID:    backend.ID(),
		ToolName:     definition.Execution.BackendTool,
		Params:       cloneMapAny(resolvedParams),
		Output:       cloneAny(backendResult.Output),
		OutputLayers: cloneOutputLayers(backendResult.OutputLayers),
	}, nil
}

func (e *Engine) triggerAutoLoad(ctx context.Context, definition SkillDefinition, params map[string]any, layers []OutputLayer) {
	if e == nil || len(layers) == 0 {
		return
	}
	if definition.Execution.Output == nil || !definition.Execution.Output.AutoLoad {
		return
	}
	e.mu.RLock()
	handler := e.autoLoadHandler
	e.mu.RUnlock()
	if handler == nil {
		return
	}
	handler(ctx, cloneMapAny(params), cloneOutputLayers(layers))
}

func (e *Engine) lookupBackend(id string) (ToolBackend, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	normalizedID := NormalizeRuntime(id)
	backend, ok := e.backends[normalizedID]
	if ok {
		return backend, true
	}
	if normalizedID == RuntimeIDQGIS {
		backend, ok = e.backends[RuntimeIDCLI]
	}
	return backend, ok
}
