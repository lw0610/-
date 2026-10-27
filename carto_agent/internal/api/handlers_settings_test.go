package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSettingsWorkspaceEndpoint(t *testing.T) {
	router, _ := buildToolTestRouter(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/settings/workspace", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/settings/workspace status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	if !resp.Success {
		t.Fatalf("workspace response success=false: %+v", resp)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected workspace data type: %T", resp.Data)
	}
	tasks, ok := data["tasks"].([]any)
	if !ok {
		t.Fatalf("unexpected tasks payload: %#v", data["tasks"])
	}
	if len(tasks) != 8 {
		t.Fatalf("expected 8 tasks, got=%d payload=%#v", len(tasks), tasks)
	}
	firstTask, ok := tasks[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected first task type: %T", tasks[0])
	}
	if got := firstTask["key"]; got != "system" {
		t.Fatalf("unexpected first task key: %#v", got)
	}
	if got := firstTask["href"]; got != "/settings/system" {
		t.Fatalf("unexpected first task href: %#v", got)
	}
	firstRoutes, ok := firstTask["routes"].([]any)
	if !ok {
		t.Fatalf("unexpected first task routes: %#v", firstTask["routes"])
	}
	if len(firstRoutes) != 1 || firstRoutes[0] != "/settings/system" {
		t.Fatalf("unexpected first task routes payload: %#v", firstRoutes)
	}
	if rawChildren, ok := firstTask["children"]; ok {
		children, ok := rawChildren.([]any)
		if !ok {
			t.Fatalf("unexpected first task children payload: %#v", rawChildren)
		}
		if len(children) != 0 {
			t.Fatalf("expected simplified top-level nav without children, got=%d payload=%#v", len(children), children)
		}
	}
	var toolsTask map[string]any
	var toolCenterTask map[string]any
	var acquisitionTask map[string]any
	var servicesTask map[string]any
	var diagnosticsTask map[string]any
	for _, item := range tasks {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch record["key"] {
		case "tools":
			toolsTask = record
		case "tool-center":
			toolCenterTask = record
		case "acquisition":
			acquisitionTask = record
		case "services":
			servicesTask = record
		case "diagnostics":
			diagnosticsTask = record
		}
	}
	if acquisitionTask == nil || toolsTask == nil || toolCenterTask == nil || servicesTask == nil || diagnosticsTask == nil {
		t.Fatalf("expected tools/tool-center/services/diagnostics task entries, payload=%#v", tasks)
	}
	toolsRoutes, ok := toolsTask["routes"].([]any)
	if !ok {
		t.Fatalf("unexpected tools routes payload: %#v", toolsTask["routes"])
	}
	toolCenterRoutes, ok := toolCenterTask["routes"].([]any)
	if !ok {
		t.Fatalf("unexpected tool-center routes payload: %#v", toolCenterTask["routes"])
	}
	servicesRoutes, ok := servicesTask["routes"].([]any)
	if !ok {
		t.Fatalf("unexpected services routes payload: %#v", servicesTask["routes"])
	}
	diagnosticsRoutes, ok := diagnosticsTask["routes"].([]any)
	if !ok {
		t.Fatalf("unexpected diagnostics routes payload: %#v", diagnosticsTask["routes"])
	}
	containsRoute := func(routes []any, target string) bool {
		for _, item := range routes {
			if item == target {
				return true
			}
		}
		return false
	}
	if acquisitionTask["href"] != "/settings/acquisition" {
		t.Fatalf("incorrect acquisition route: %+v", acquisitionTask)
	}
	if !containsRoute(toolsRoutes, "/settings/tools") {
		t.Fatalf("expected tools routes to include /settings/tools, got=%#v", toolsRoutes)
	}
	if !containsRoute(toolCenterRoutes, "/tool-center") {
		t.Fatalf("expected tool-center routes to include /tool-center, got=%#v", toolCenterRoutes)
	}
	if !containsRoute(servicesRoutes, "/settings/services") {
		t.Fatalf("expected services routes to include /settings/services, got=%#v", servicesRoutes)
	}
	if !containsRoute(diagnosticsRoutes, "/diagnostics") {
		t.Fatalf("expected diagnostics routes to include /diagnostics, got=%#v", diagnosticsRoutes)
	}
	if hidden, _ := toolCenterTask["hidden"].(bool); !hidden {
		t.Fatalf("expected tool-center task hidden=true, got=%#v", toolCenterTask["hidden"])
	}
	if hidden, _ := diagnosticsTask["hidden"].(bool); !hidden {
		t.Fatalf("expected diagnostics task hidden=true, got=%#v", diagnosticsTask["hidden"])
	}

	descriptors, ok := data["descriptors"].([]any)
	if !ok {
		t.Fatalf("unexpected descriptors payload: %#v", data["descriptors"])
	}
	if len(descriptors) < 10 {
		t.Fatalf("expected descriptors payload, got=%d", len(descriptors))
	}
	foundServicesDescriptor := false
	foundAboutDescriptor := false
	foundToolsDescriptor := false
	for _, item := range descriptors {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch record["id"] {
		case "tools-skill-packages":
			foundToolsDescriptor = record["path"] == "/settings/tools"
		case "services-runtime":
			foundServicesDescriptor = record["path"] == "/settings/services"
		case "about-app":
			foundAboutDescriptor = record["path"] == "/settings/about"
		}
	}
	if !foundToolsDescriptor || !foundServicesDescriptor || !foundAboutDescriptor {
		t.Fatalf("expected tools/services/about descriptors in workspace payload")
	}
}
