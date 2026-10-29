package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/capability"
	"github.com/autocarto/carto_agent/internal/compute"
)

func TestQGISPartialFailureKeepsStructuredRoutes(t *testing.T) {
	output := map[string]any{
		"success": false,
		"info":    "部分路线绘制失败",
		"data": map[string]any{"routes": map[string]any{
			"count": 2, "failed_count": 1,
			"items": []any{
				map[string]any{"route_id": "route-ok", "success": true, "layer_id": "actual-layer"},
				map[string]any{"route_id": "route-loop", "success": false, "info": "同一贝塞尔段出现回环"},
			},
		}},
	}
	err := toolInvokeErrorFromComputeResponse(&compute.InvokeResponse{Success: false, Error: "部分路线绘制失败", Output: output})
	if strings.Contains(err.Error(), "route-loop") {
		t.Fatal("structured detail must not be concatenated into the error string")
	}
	result, mapErr := (capability.DefaultErrorMapper{}).MapToolError(context.Background(), "route_draw", fmt.Errorf("invoke route_draw: %w", err))
	if mapErr != nil {
		t.Fatal(mapErr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Parts[0].Text), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["success"] != false || payload["output"] == nil {
		t.Fatalf("failure or partial output lost: %s", result.Parts[0].Text)
	}
	items := payload["output"].(map[string]any)["data"].(map[string]any)["routes"].(map[string]any)["items"].([]any)
	if items[0].(map[string]any)["layer_id"] != "actual-layer" || items[1].(map[string]any)["route_id"] != "route-loop" {
		t.Fatalf("successful and failed items must both survive: %v", items)
	}
	if strings.Count(result.Parts[0].Text, "route-loop") != 1 {
		t.Fatalf("detail duplicated: %s", result.Parts[0].Text)
	}
}
