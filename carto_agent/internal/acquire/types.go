package acquire

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"time"
)

type Adapter interface {
	Kind() string
	Fetch(context.Context, FetchRequest) (FetchResult, error)
}
type Extent struct {
	BBox []float64 `json:"bbox,omitempty"`
	Path string    `json:"path,omitempty"`
}
type Progress struct {
	Percent        *float64 `json:"percent,omitempty"`
	BytesPerSecond float64  `json:"bytes_per_second,omitempty"`
	Message        string   `json:"message,omitempty"`

	Stage string `json:"stage"`
	Bytes int64  `json:"bytes"`
	Total int64  `json:"total"`
}
type FetchRequest struct {
	Source   *datasource.Source
	Provide  *datasource.Provide
	Item     flow.AcquisitionItem
	Extent   Extent
	WorkDir  string
	Progress func(Progress)
}
type FetchResult struct {
	LayerDefinition map[string]any    `json:"layer_definition,omitempty"`
	Files           []string          `json:"files"`
	Format          string            `json:"format"`
	Query           string            `json:"query"`
	URLs            []string          `json:"urls"`
	RetrievedAt     time.Time         `json:"retrieved_at"`
	Bytes           int64             `json:"bytes"`
	SHA256          map[string]string `json:"sha256"`
	Notes           []string          `json:"notes"`
}
type ToolCaller func(context.Context, string, map[string]any) (any, error)

// ProcessingError separates a failed local conversion from a failed download.
// Paths contains already published inputs that a repaired run can reuse.
type ProcessingError struct {
	Stage string
	Paths []string
	Err   error
}

func (e *ProcessingError) Error() string {
	return fmt.Sprintf("数据处理失败（%s）：%v", e.Stage, e.Err)
}
func (e *ProcessingError) Unwrap() error         { return e.Err }
func (e *ProcessingError) ToolErrorCode() string { return "DATA_PROCESSING_FAILED" }
func (e *ProcessingError) ToolRetryable() bool   { return false }

func UnwrapToolResult(value any) (map[string]any, error) {
	if text, ok := value.(string); ok {
		if err := json.Unmarshal([]byte(text), &value); err != nil {
			return nil, fmt.Errorf("工具未返回 JSON")
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err = json.Unmarshal(encoded, &payload); err != nil {
		return nil, err
	}
	for depth := 0; depth < 4; depth++ {
		if success, exists := payload["success"].(bool); exists && !success {
			return nil, fmt.Errorf("工具执行失败：%v", payload["error"])
		}
		nested, ok := payload["output"].(map[string]any)
		if !ok {
			nested, ok = payload["result"].(map[string]any)
		}
		if !ok {
			break
		}
		payload = nested
	}
	return payload, nil
}
func emitProgress(fn func(Progress), stage string, bytes, total int64) {
	if fn != nil {
		fn(Progress{Stage: stage, Bytes: bytes, Total: total})
	}
}
