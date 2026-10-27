package runtimecontrol

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

type flowServiceStub struct {
	gotID     string
	gotInputs map[string]any
	mode      string
}

func (s *flowServiceStub) StartFlow(_ context.Context, id string, inputs map[string]any) (map[string]any, error) {
	s.gotID = id
	s.gotInputs = inputs
	s.mode = "start"
	return map[string]any{"accepted": true}, nil
}
func (s *flowServiceStub) PreviewFlow(_ context.Context, id string, inputs map[string]any) (map[string]any, error) {
	s.gotID = id
	s.gotInputs = inputs
	s.mode = "preview"
	return map[string]any{"plan": map[string]any{}}, nil
}
func TestFlowToolsRequireTrustedSessionService(t *testing.T) {
	for _, name := range []string{ToolFlowStart, ToolFlowPreview} {
		if !Handles(name) {
			t.Fatal("missing flow dispatcher")
		}
		if _, err := Invoke(context.Background(), name, map[string]any{"flow_id": "test"}); err == nil {
			t.Fatal("missing trusted context accepted")
		}
		service := &flowServiceStub{}
		ctx := WithFlowService(context.Background(), service)
		if _, err := Invoke(ctx, name, map[string]any{}); err == nil {
			t.Fatal("missing ID accepted")
		}
		if _, err := Invoke(ctx, name, map[string]any{"flow_id": "test", "inputs": "invalid"}); err == nil {
			t.Fatal("non-object inputs accepted")
		}
		inputs := map[string]any{"source": map[string]any{"$ref": "current_user_message"}}
		if _, err := Invoke(ctx, name, map[string]any{"flow_id": " test ", "inputs": inputs}); err != nil {
			t.Fatal(err)
		}
		if service.gotID != "test" || service.gotInputs["source"] == nil {
			t.Fatal("inputs lost")
		}
		if _, err := Invoke(ctx, name, map[string]any{"flow_id": "test"}); err != nil {
			t.Fatal("optional inputs failed", err)
		}
	}
}

func TestFlowToolsPassAcquisitionChoicesButRejectModelSourceOverrides(t *testing.T) {
	for _, name := range []string{ToolFlowStart, ToolFlowPreview} {
		service := &flowServiceStub{}
		ctx := WithFlowService(context.Background(), service)
		for _, choice := range []map[string]any{{"$acquire": true}, {"$fill": true, "path": "/fixture"}} {
			if _, err := Invoke(ctx, name, map[string]any{"flow_id": "test", "inputs": map[string]any{"data": choice}}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(service.gotInputs["data"], choice) {
				t.Fatal("choice changed", service.gotInputs)
			}
		}
		for _, field := range []string{"source", "sources"} {
			choice := map[string]any{"$acquire": true, field: "forbidden"}
			if _, err := Invoke(ctx, name, map[string]any{"flow_id": "test", "inputs": map[string]any{"data": choice}}); err == nil || !strings.Contains(err.Error(), "FLOW_ORCHESTRATOR_SOURCE_FORBIDDEN") {
				t.Fatal("accepted model source override", err)
			}
		}
	}
}
