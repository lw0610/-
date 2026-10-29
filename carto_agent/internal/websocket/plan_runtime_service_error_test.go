package websocket

import (
	"context"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/flowrun"
)

func TestReplanStopsBeforeCallingModelForPersistedServiceFailure(t *testing.T) {
	s := &DeepServer{}
	state := &flowrun.RunState{Status: "needs_replan", Cursor: 0, Steps: []flowrun.StepState{{
		Attempts: []flowrun.Attempt{{Verification: flowrun.Verification{Errors: []string{"[NodeRunError] status code: 402, message: Insufficient Balance"}}}},
	}}}
	err := s.replanCartographic(context.Background(), flowSessionContext{}, state)
	if err == nil || !strings.Contains(err.Error(), "Insufficient Balance") {
		t.Fatalf("persisted account error should be returned without constructing another model run: %v", err)
	}
}
