package api

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/autocarto/carto_agent/internal/control"
)

const (
	correlationIDHeader = "X-Correlation-ID"
	evidenceIDHeader    = "X-Evidence-ID"
)

func writeControlPlaneError(w http.ResponseWriter, r *http.Request, status int, err error, fallback *control.ErrorEnvelope, data any) {
	envelope := control.EnsureErrorEnvelope(err, fallback).WithIdentifiers(requestCorrelationID(r), requestEvidenceID(r))
	w.Header().Set(correlationIDHeader, envelope.CorrelationID)
	w.Header().Set(evidenceIDHeader, envelope.EvidenceID)
	JSON(w, status, Response{
		Success: false,
		Code:    envelope.Code,
		Error:   envelope,
		Data:    data,
	})
}

func newAvailabilityEnvelope(code, message, origin, stage string, debugContext map[string]any) *control.ErrorEnvelope {
	envelope := control.NewErrorEnvelope(code, message, origin, stage)
	envelope.Retryable = true
	envelope.Degraded = true
	envelope.UserAction = "请先修复控制面依赖后重试。"
	return envelope.WithDebugContext(debugContext)
}

func newRequestEnvelope(code, message, origin, stage, userAction string, debugContext map[string]any) *control.ErrorEnvelope {
	envelope := control.NewErrorEnvelope(code, message, origin, stage)
	envelope.UserAction = strings.TrimSpace(userAction)
	return envelope.WithDebugContext(debugContext)
}

func requestCorrelationID(r *http.Request) string {
	if r != nil {
		if value := strings.TrimSpace(r.Header.Get(correlationIDHeader)); value != "" {
			return value
		}
	}
	return uuid.NewString()
}

func requestEvidenceID(r *http.Request) string {
	if r != nil {
		if value := strings.TrimSpace(r.Header.Get(evidenceIDHeader)); value != "" {
			return value
		}
	}
	return uuid.NewString()
}
