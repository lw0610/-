package websocket

import (
	"fmt"

	"github.com/autocarto/carto_agent/internal/logger"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	tooldefinition "github.com/autocarto/carto_agent/internal/tool"
	"github.com/cloudwego/eino/schema"
)

func registerToolArtifacts(store *sessionfiles.Store, definition tooldefinition.ToolDefinition, name, agent string, message *schema.Message, options consumeEventsOptions) []error {
	if message == nil || store == nil {
		return nil
	}
	// Read-only tools return input paths, which must not be registered as outputs.
	if len(definition.Spec.Artifacts) == 0 && sessionfiles.ReadsInputPaths(name) {
		return nil
	}
	origin := sessionfiles.Origin{Type: "tool", Tool: name, Label: definition.Metadata.DisplayName, Agent: agent, CallID: message.ToolCallID}
	if origin.Label == "" {
		origin.Label = name
	}
	paths := map[string]string{}
	parts := messageInputPartsPayload(message.UserInputMultiContent)
	if len(definition.Spec.Artifacts) > 0 {
		for _, declaration := range definition.Spec.Artifacts {
			for _, path := range sessionfiles.ResultPaths(message.Content, parts, declaration.Field) {
				paths[path] = declaration.Role
			}
		}
	} else {
		for _, path := range sessionfiles.ResultPaths(message.Content, parts) {
			paths[path] = ""
		}
	}
	var failures []error
	for path, role := range paths {
		opts := []sessionfiles.RegistrationOption{sessionfiles.WithFlow(options.flowRunID, options.flowStepID)}
		if role != "" {
			opts = append(opts, sessionfiles.WithGroup(role))
		}
		if _, err := store.Register(path, origin, opts...); err != nil {
			failure := fmt.Errorf("tool=%s path=%s: %w", name, path, err)
			failures = append(failures, failure)
			logger.Warn("会话产物登记失败: session_id=%s call_id=%s %v", store.SessionID, message.ToolCallID, failure)
		}
	}
	return failures
}
