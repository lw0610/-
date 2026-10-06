package control

import (
	"encoding/json"
	"errors"
	"strings"
)

type ErrorEnvelope struct {
	Code          string         `json:"code"`
	Message       string         `json:"message"`
	Origin        string         `json:"origin"`
	Stage         string         `json:"stage"`
	Retryable     bool           `json:"retryable"`
	Degraded      bool           `json:"degraded"`
	UserAction    string         `json:"user_action"`
	DebugContext  map[string]any `json:"debug_context"`
	CorrelationID string         `json:"correlation_id"`
	EvidenceID    string         `json:"evidence_id"`
}

type errorEnvelopeCarrier interface {
	ControlErrorEnvelope() *ErrorEnvelope
}

func NewErrorEnvelope(code, message, origin, stage string) *ErrorEnvelope {
	return (&ErrorEnvelope{
		Code:          strings.TrimSpace(code),
		Message:       strings.TrimSpace(message),
		Origin:        strings.TrimSpace(origin),
		Stage:         strings.TrimSpace(stage),
		UserAction:    "",
		DebugContext:  map[string]any{},
		CorrelationID: "",
		EvidenceID:    "",
	}).Clone()
}

func (e *ErrorEnvelope) Error() string {
	if e == nil {
		return "控制面错误"
	}
	if strings.TrimSpace(e.Message) != "" {
		return strings.TrimSpace(e.Message)
	}
	if strings.TrimSpace(e.Code) != "" {
		return strings.TrimSpace(e.Code)
	}
	return "控制面错误"
}

func (e *ErrorEnvelope) MarshalJSON() ([]byte, error) {
	type alias ErrorEnvelope
	return json.Marshal((*alias)(e.Clone()))
}

func (e *ErrorEnvelope) Clone() *ErrorEnvelope {
	if e == nil {
		return &ErrorEnvelope{DebugContext: map[string]any{}}
	}
	cloned := *e
	cloned.Code = strings.TrimSpace(cloned.Code)
	cloned.Message = strings.TrimSpace(cloned.Message)
	cloned.Origin = strings.TrimSpace(cloned.Origin)
	cloned.Stage = strings.TrimSpace(cloned.Stage)
	cloned.UserAction = strings.TrimSpace(cloned.UserAction)
	cloned.CorrelationID = strings.TrimSpace(cloned.CorrelationID)
	cloned.EvidenceID = strings.TrimSpace(cloned.EvidenceID)
	cloned.DebugContext = cloneDebugContext(e.DebugContext)
	return &cloned
}

func (e *ErrorEnvelope) WithIdentifiers(correlationID, evidenceID string) *ErrorEnvelope {
	cloned := e.Clone()
	if cloned.CorrelationID == "" {
		cloned.CorrelationID = strings.TrimSpace(correlationID)
	}
	if cloned.EvidenceID == "" {
		cloned.EvidenceID = strings.TrimSpace(evidenceID)
	}
	return cloned
}

func (e *ErrorEnvelope) WithDebugContext(debugContext map[string]any) *ErrorEnvelope {
	cloned := e.Clone()
	for key, value := range debugContext {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		cloned.DebugContext[trimmedKey] = value
	}
	return cloned
}

func (e *ErrorEnvelope) ControlErrorEnvelope() *ErrorEnvelope {
	return e.Clone()
}

func AsErrorEnvelope(err error) (*ErrorEnvelope, bool) {
	if err == nil {
		return nil, false
	}
	var carrier errorEnvelopeCarrier
	if errors.As(err, &carrier) {
		if envelope := carrier.ControlErrorEnvelope(); envelope != nil {
			return envelope.Clone(), true
		}
	}
	var envelope *ErrorEnvelope
	if errors.As(err, &envelope) {
		return envelope.Clone(), true
	}
	return nil, false
}

func EnsureErrorEnvelope(err error, fallback *ErrorEnvelope) *ErrorEnvelope {
	if envelope, ok := AsErrorEnvelope(err); ok {
		return envelope
	}
	if fallback != nil {
		envelope := fallback.Clone()
		if envelope.Message == "" && err != nil {
			envelope.Message = strings.TrimSpace(err.Error())
		}
		if err != nil && strings.TrimSpace(err.Error()) != "" {
			if _, exists := envelope.DebugContext["cause"]; !exists {
				envelope.DebugContext["cause"] = strings.TrimSpace(err.Error())
			}
		}
		return envelope
	}
	message := "控制面错误"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = strings.TrimSpace(err.Error())
	}
	envelope := NewErrorEnvelope("CONTROL_ERROR", message, "control", "control_api")
	envelope.DebugContext["cause"] = message
	return envelope
}

func cloneDebugContext(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
