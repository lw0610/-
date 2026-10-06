package toolrouter

import (
	"sync"

	"github.com/autocarto/carto_agent/internal/compute"
)

type InteractiveToolDefinition struct {
	Name              string                 `json:"name"`
	Description       string                 `json:"description"`
	Category          string                 `json:"category"`
	InputSchema       map[string]interface{} `json:"inputSchema"`
	Available         bool                   `json:"available,omitempty"`
	UnavailableReason string                 `json:"unavailable_reason,omitempty"`
	InstanceIDs       []string               `json:"instance_ids,omitempty"`
}

type Snapshot struct {
	Interactive []InteractiveToolDefinition
	Compute     []compute.ToolDefinition
}

type ToolAggregator struct {
	mu       sync.RWMutex
	snapshot Snapshot
}

func NewToolAggregator() *ToolAggregator {
	return &ToolAggregator{}
}

func (a *ToolAggregator) UpdateInteractive(tools []InteractiveToolDefinition) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cp := make([]InteractiveToolDefinition, len(tools))
	copy(cp, tools)
	a.snapshot.Interactive = cp
}

func (a *ToolAggregator) UpdateCompute(tools []compute.ToolDefinition) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cp := make([]compute.ToolDefinition, len(tools))
	copy(cp, tools)
	a.snapshot.Compute = cp
}

func (a *ToolAggregator) Snapshot() Snapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := Snapshot{
		Interactive: make([]InteractiveToolDefinition, len(a.snapshot.Interactive)),
		Compute:     make([]compute.ToolDefinition, len(a.snapshot.Compute)),
	}
	copy(out.Interactive, a.snapshot.Interactive)
	copy(out.Compute, a.snapshot.Compute)
	return out
}
