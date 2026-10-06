package security

import (
	"net/http/httptest"
	"testing"
)

func TestIsAllowedDesktopOrigin(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{name: "tauri scheme", origin: "tauri://localhost", want: true},
		{name: "tauri localhost https", origin: "https://tauri.localhost", want: true},
		{name: "tauri localhost http", origin: "http://tauri.localhost", want: true},
		{name: "localhost dev", origin: "http://localhost:3000", want: true},
		{name: "loopback dev", origin: "http://127.0.0.1:3000", want: true},
		{name: "localhost prefix attack", origin: "http://localhost.evil.test:3000", want: false},
		{name: "loopback prefix attack", origin: "http://127.0.0.1.evil.test:3000", want: false},
		{name: "empty", origin: "", want: false},
		{name: "external", origin: "https://example.com", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAllowedDesktopOrigin(tt.origin); got != tt.want {
				t.Fatalf("IsAllowedDesktopOrigin(%q) = %v, want %v", tt.origin, got, tt.want)
			}
		})
	}
}

func TestIsAllowedLocalWebSocketRequest(t *testing.T) {
	tests := []struct {
		name       string
		origin     string
		remoteAddr string
		want       bool
	}{
		{name: "allowed origin", origin: "http://localhost:3000", remoteAddr: "127.0.0.1:3000", want: true},
		{name: "empty origin on loopback", origin: "", remoteAddr: "127.0.0.1:50201", want: true},
		{name: "empty origin on ipv6 loopback", origin: "", remoteAddr: "[::1]:50201", want: true},
		{name: "empty origin on remote host", origin: "", remoteAddr: "192.168.1.20:50201", want: false},
		{name: "external origin", origin: "https://example.com", remoteAddr: "127.0.0.1:3000", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://127.0.0.1/ws", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if got := IsAllowedLocalWebSocketRequest(req); got != tt.want {
				t.Fatalf("IsAllowedLocalWebSocketRequest(%q, %q) = %v, want %v", tt.origin, tt.remoteAddr, got, tt.want)
			}
		})
	}
}
