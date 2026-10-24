package flowrun

import (
	"fmt"
	"sync"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/cloudwego/eino/schema"
)

type ExecutorAdapter interface {
	Type() string
	BuildInput(flow.StepPlan, *RunState) ([]*schema.Message, error)
	ToReceipt(flow.StepPlan, string) (Receipt, error)
}

type ExecutorRegistry struct {
	mu       sync.RWMutex
	adapters map[string]ExecutorAdapter
}

func NewExecutorRegistry() *ExecutorRegistry {
	return &ExecutorRegistry{adapters: map[string]ExecutorAdapter{}}
}
func (r *ExecutorRegistry) Register(adapter ExecutorAdapter) error {
	if adapter == nil || adapter.Type() == "" {
		return fmt.Errorf("执行器类型不能为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.adapters == nil {
		r.adapters = map[string]ExecutorAdapter{}
	}
	if _, exists := r.adapters[adapter.Type()]; exists {
		return fmt.Errorf("执行器已注册: %s", adapter.Type())
	}
	r.adapters[adapter.Type()] = adapter
	return nil
}
func (r *ExecutorRegistry) Get(kind string) (ExecutorAdapter, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[kind]
	return a, ok
}
