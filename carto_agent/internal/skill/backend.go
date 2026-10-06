package skill

import (
	"context"
	"strings"
)

const (
	RuntimeIDQGIS     = "qgis"
	RuntimeIDCLI      = "cli"
	RuntimeIDInternal = "internal"
)

const (
	BackendIDCompute  = RuntimeIDCLI
	BackendIDInternal = RuntimeIDInternal
)

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

type OutputLayer struct {
	Path     string `json:"path,omitempty"`
	AutoLoad bool   `json:"auto_load,omitempty"`
}

type BackendResult struct {
	Success      bool          `json:"success"`
	Output       any           `json:"output,omitempty"`
	Error        string        `json:"error,omitempty"`
	OutputLayers []OutputLayer `json:"output_layers,omitempty"`
}

type ToolBackend interface {
	ID() string
	Execute(ctx context.Context, toolName string, args map[string]any) (*BackendResult, error)
}

func cloneOutputLayers(src []OutputLayer) []OutputLayer {
	if len(src) == 0 {
		return nil
	}
	out := make([]OutputLayer, len(src))
	copy(out, src)
	return out
}
