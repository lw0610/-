package main

import (
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
)

func TestResolveBridgeToolInvokeTimeoutUsesPerformanceConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.Performance.ToolCallTimeout = 45_000

	got := resolveBridgeToolInvokeTimeout(cfg)
	want := 45 * time.Second
	if got != want {
		t.Fatalf("resolveBridgeToolInvokeTimeout mismatch, got=%s want=%s", got, want)
	}
}

func TestResolveBridgeToolInvokeTimeoutFallsBackToLegacyDefault(t *testing.T) {
	got := resolveBridgeToolInvokeTimeout(nil)
	want := 120 * time.Second
	if got != want {
		t.Fatalf("resolveBridgeToolInvokeTimeout fallback mismatch, got=%s want=%s", got, want)
	}
}
