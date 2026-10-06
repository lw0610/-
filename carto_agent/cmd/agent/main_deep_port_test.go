package main

import (
	"net"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/discovery"
)

func pickFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen random port failed: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close random listener failed: %v", err)
	}
	return port
}

func TestResolveAgentPort_KeepPreferredWhenAvailable(t *testing.T) {
	preferred := pickFreePort(t)
	portCfg := config.PortConfig{
		Default:        preferred,
		Range:          []int{preferred, preferred},
		BindRetryCount: 1,
		BindRetryDelay: 10,
	}
	resolver := discovery.NewPortResolver("agent")
	got, err := resolver.Resolve(portCfg)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if got != preferred {
		t.Fatalf("resolved port mismatch, got=%d want=%d", got, preferred)
	}
}

func TestResolveAgentPort_RespectDisableFallback(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen random port failed: %v", err)
	}
	defer listener.Close()
	occupiedPort := listener.Addr().(*net.TCPAddr).Port

	autoFallback := false
	portCfg := config.PortConfig{
		Default:        occupiedPort,
		Range:          []int{occupiedPort, occupiedPort},
		BindRetryCount: 1,
		BindRetryDelay: 10,
		AutoFallback:   &autoFallback,
	}
	resolver := discovery.NewPortResolver("agent")
	_, resolveErr := resolver.Resolve(portCfg)
	if resolveErr == nil {
		t.Fatalf("expected error when fallback disabled and port occupied")
	}
}

func TestResolveAgentPort_FallbackToRangeWhenPreferredOccupied(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen random port failed: %v", err)
	}
	defer listener.Close()
	occupiedPort := listener.Addr().(*net.TCPAddr).Port
	fallbackPort := pickFreePort(t)

	autoFallback := true
	portCfg := config.PortConfig{
		Default:        occupiedPort,
		Range:          []int{fallbackPort, fallbackPort},
		BindRetryCount: 1,
		BindRetryDelay: 10,
		AutoFallback:   &autoFallback,
	}
	resolver := discovery.NewPortResolver("agent")
	got, resolveErr := resolver.Resolve(portCfg)
	if resolveErr != nil {
		t.Fatalf("Resolve failed: %v", resolveErr)
	}
	if got != fallbackPort {
		t.Fatalf("resolved fallback port mismatch, got=%d want=%d", got, fallbackPort)
	}
}

func TestResolveAgentPort_FallbackWhenPreferredOccupiedByWildcardBind(t *testing.T) {
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen wildcard random port failed: %v", err)
	}
	defer listener.Close()
	occupiedPort := listener.Addr().(*net.TCPAddr).Port
	fallbackPort := pickFreePort(t)

	autoFallback := true
	portCfg := config.PortConfig{
		Default:        occupiedPort,
		Range:          []int{fallbackPort, fallbackPort},
		BindRetryCount: 1,
		BindRetryDelay: 10,
		AutoFallback:   &autoFallback,
	}
	resolver := discovery.NewPortResolver("agent")
	got, resolveErr := resolver.Resolve(portCfg)
	if resolveErr != nil {
		t.Fatalf("Resolve failed: %v", resolveErr)
	}
	if got != fallbackPort {
		t.Fatalf("resolved fallback port mismatch, got=%d want=%d", got, fallbackPort)
	}
}
