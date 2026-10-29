package flowrun

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/autocarto/carto_agent/internal/flow"
)

// ConversationReference preserves source identity without duplicating user text.
func ConversationReference(source string) map[string]any {
	sum := sha256.Sum256([]byte(source))
	return map[string]any{"$ref": "current_user_message", "sha256": hex.EncodeToString(sum[:]), "length": len([]byte(source))}
}
func provided(value any) bool { return flow.InputProvided(value) }
func truthy(value any) bool   { return flow.InputTruthy(value) }

// EffectiveInputs overlays resolved data on the immutable initial bindings.
func EffectiveInputs(run *RunState) map[string]any {
	out := map[string]any{}
	if run == nil {
		return out
	}
	for key, value := range run.Inputs {
		out[key] = value
	}
	for key, value := range run.AcquiredInputs {
		out[key] = value
	}
	return out
}
