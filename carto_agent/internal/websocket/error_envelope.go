package websocket

import (
	"strings"

	"github.com/google/uuid"

	"github.com/autocarto/carto_agent/internal/control"
)

func applyWebSocketErrorPayload(respData map[string]any, err error, fallback *control.ErrorEnvelope, debugContext map[string]any) {
	if respData == nil {
		return
	}
	if err == nil {
		delete(respData, "code")
		respData["error"] = ""
		return
	}

	envelope := control.EnsureErrorEnvelope(err, fallback).
		WithIdentifiers(uuid.NewString(), uuid.NewString()).
		WithDebugContext(debugContext)
	respData["code"] = envelope.Code
	respData["error"] = envelope
}

func newWebSocketAvailabilityEnvelope(code, message, origin, stage, userAction string, debugContext map[string]any) *control.ErrorEnvelope {
	envelope := control.NewErrorEnvelope(code, message, origin, stage)
	envelope.Retryable = true
	envelope.Degraded = true
	envelope.UserAction = strings.TrimSpace(userAction)
	return envelope.WithDebugContext(debugContext)
}

func newWebSocketRequestEnvelope(code, message, origin, stage, userAction string, debugContext map[string]any) *control.ErrorEnvelope {
	envelope := control.NewErrorEnvelope(code, message, origin, stage)
	envelope.UserAction = strings.TrimSpace(userAction)
	return envelope.WithDebugContext(debugContext)
}
