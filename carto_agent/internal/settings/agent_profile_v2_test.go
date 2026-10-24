package settings

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
)

func TestAgentProfileV2RoundTripPreservesExecutorFlowsAndWarnings(t *testing.T) {
	input := &config.AgentConfigData{Version: "2.0", Flows: &config.FlowConfig{Enabled: []string{}}, Runtime: &config.RuntimeConfig{SharedToolPackages: []string{"vector-selection"}, LegacyUnresolvedPackages: []string{"old-shared"}, Agents: map[string]config.AgentDefData{"custom-id": {Kind: "executor", Executor: "route_knowledge", FlowOnly: true, ToolPackages: []string{}, LegacyUnresolvedPackages: []string{"unknown"}, Enabled: true, Prompts: map[string]string{"static_instructions": "fallback"}}}}, Plan: &config.PlanConfig{MaxIterations: 70, Global: config.GlobalAgentConfig{ToolPackages: []string{"charts"}}, Agents: map[string]config.PlanAgentDefData{"planner": {Kind: "agent", Mode: "readonly_plus", ToolPackages: []string{"data-loading"}}}}}
	profile := BuildAgentProfileView("example", input, nil)
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AgentProfileView
	if err = json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	got := ExpandAgentProfileView(decoded)
	if got.Version != "2.0" || got.Flows == nil || len(got.Flows.Enabled) != 0 {
		t.Fatalf("flow semantics lost: %+v", got)
	}
	a := got.Runtime.Agents["custom-id"]
	if a.Kind != "executor" || a.Executor != "route_knowledge" || !a.FlowOnly || !reflect.DeepEqual(a.LegacyUnresolvedPackages, []string{"unknown"}) {
		t.Fatalf("role fields lost: %+v", a)
	}
	if !reflect.DeepEqual(got.Runtime.SharedToolPackages, input.Runtime.SharedToolPackages) || !reflect.DeepEqual(got.Plan.Global.ToolPackages, input.Plan.Global.ToolPackages) || !reflect.DeepEqual(got.Plan.Agents["planner"].ToolPackages, input.Plan.Agents["planner"].ToolPackages) {
		t.Fatal("package fields lost")
	}
	// WebSocket uses the direct structured configuration decoder.
	raw, _ = json.Marshal(got)
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)
	ws, err := DecodeAgentConfigData(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ws.Runtime, got.Runtime) {
		t.Fatal("WebSocket config decode lost v2 fields")
	}
}
