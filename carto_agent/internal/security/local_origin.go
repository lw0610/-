package security

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

func IsAllowedDesktopOrigin(origin string) bool {
	trimmed := strings.TrimSpace(origin)
	if trimmed == "" {
		return false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	hostname := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))

	switch scheme {
	case "tauri":
		return hostname == "localhost"
	case "http":
		return hostname == "localhost" || hostname == "127.0.0.1" || hostname == "tauri.localhost"
	case "https":
		return hostname == "tauri.localhost"
	default:
		return false
	}
}

func IsAllowedLocalWebSocketRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		return IsAllowedDesktopOrigin(origin)
	}
	return isLoopbackRemoteAddr(r.RemoteAddr)
}

func isLoopbackRemoteAddr(remoteAddr string) bool {
	trimmed := strings.TrimSpace(remoteAddr)
	if trimmed == "" {
		return false
	}
	host, _, err := net.SplitHostPort(trimmed)
	if err != nil {
		host = trimmed
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
