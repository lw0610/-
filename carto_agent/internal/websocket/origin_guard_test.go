package websocket

import (
	"net/http/httptest"
	"testing"
)

func TestDeepServerUpgraderRejectsExternalOrigin(t *testing.T) {
	req := httptest.NewRequest("GET", "http://127.0.0.1/ws", nil)
	req.RemoteAddr = "127.0.0.1:50201"
	req.Header.Set("Origin", "https://example.com")
	if upgrader.CheckOrigin == nil {
		t.Fatal("expected websocket upgrader CheckOrigin to be configured")
	}
	if upgrader.CheckOrigin(req) {
		t.Fatal("expected external origin to be rejected")
	}
}
