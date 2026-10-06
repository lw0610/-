package discovery

import (
	"testing"
	"time"
)

func TestBuildInstanceServicesUsesAgentBridgeServiceForAgentReverseWS(t *testing.T) {
	now := time.Now()
	services := buildInstanceServices(SourceAgentReverseWS, "running", now)
	if _, ok := services[ServiceNameAgentQGISBridge]; !ok {
		t.Fatalf("expected %s service, got %#v", ServiceNameAgentQGISBridge, services)
	}
	if _, ok := services[ServiceNameQGISBridge]; ok {
		t.Fatalf("unexpected %s service: %#v", ServiceNameQGISBridge, services)
	}
	toolServices := buildInstanceServices(SourceReverseWS, "running", now)
	if _, ok := toolServices[ServiceNameQGISBridge]; !ok {
		t.Fatalf("expected %s service for reverse ws, got %#v", ServiceNameQGISBridge, toolServices)
	}
}
