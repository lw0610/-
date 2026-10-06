package capability

import (
	"context"
	"errors"
	"strings"

	"github.com/autocarto/carto_agent/internal/toolresult"
	"github.com/cloudwego/eino/schema"
)

type DefaultErrorMapper struct{}

type toolErrorPayload struct {
	Success             bool           `json:"success"`
	Error               string         `json:"error"`
	Code                string         `json:"code,omitempty"`
	Retryable           bool           `json:"retryable,omitempty"`
	ProviderDiagnostics map[string]any `json:"provider_diagnostics,omitempty"`
}

func (DefaultErrorMapper) MapToolError(_ context.Context, _ string, err error) (*schema.ToolResult, error) {
	message := "unknown tool error"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = strings.TrimSpace(err.Error())
	}
	payload := toolErrorPayload{Success: false, Error: message}
	var coded interface {
		ToolErrorCode() string
		ToolRetryable() bool
		ToolProviderDiagnostics() map[string]any
	}
	if errors.As(err, &coded) {
		payload.Code = strings.TrimSpace(coded.ToolErrorCode())
		payload.Retryable = coded.ToolRetryable()
		payload.ProviderDiagnostics = coded.ToolProviderDiagnostics()
	}
	return toolresult.JSON(payload)
}
