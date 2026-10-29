package bridge

import "strings"

const (
	ToolErrorCodeNotReady         = "TOOL_NOT_READY"
	ToolErrorCodeProviderMissing  = "TOOL_PROVIDER_MISSING"
	ToolErrorCodeInstanceRequired = "TOOL_INSTANCE_REQUIRED"
)

type ToolInvokeError struct {
	Code                string
	Message             string
	Retryable           bool
	ProviderDiagnostics map[string]any
	Output              any
}

// ToolOutput retains a handler's structured partial result on execution failure.
func (e *ToolInvokeError) ToolOutput() any {
	if e == nil {
		return nil
	}
	return e.Output
}

func (e *ToolInvokeError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return strings.TrimSpace(e.Message)
	}
	return strings.TrimSpace(e.Code)
}

func (e *ToolInvokeError) ToolErrorCode() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.Code)
}

func (e *ToolInvokeError) ToolRetryable() bool {
	if e == nil {
		return false
	}
	return e.Retryable
}

func (e *ToolInvokeError) ToolProviderDiagnostics() map[string]any {
	if e == nil || len(e.ProviderDiagnostics) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(e.ProviderDiagnostics))
	for key, value := range e.ProviderDiagnostics {
		cloned[key] = value
	}
	return cloned
}

func newToolInvokeError(code, message string, retryable bool, diagnostics map[string]any) error {
	return &ToolInvokeError{
		Code:                strings.TrimSpace(code),
		Message:             strings.TrimSpace(message),
		Retryable:           retryable,
		ProviderDiagnostics: cloneDiagnosticsMap(diagnostics),
	}
}

func cloneDiagnosticsMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
