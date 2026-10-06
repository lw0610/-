package discovery

import (
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/testutil"
)

func TestResolve_DefaultPortAvailable(t *testing.T) {
	preferred := testutil.ReserveFreePort(t)

	resolver := NewPortResolver("agent")
	got, err := resolver.Resolve(config.PortConfig{
		Default:        preferred,
		Range:          []int{preferred, preferred},
		BindRetryCount: 1,
		BindRetryDelay: 1,
	})
	if err != nil {
		t.Fatalf("resolve preferred port: %v", err)
	}
	if got != preferred {
		t.Fatalf("resolved port mismatch: got=%d want=%d", got, preferred)
	}
}

func TestResolve_DefaultOccupied_FallbackEnabled(t *testing.T) {
	ports, listeners := occupyContiguousPorts(t, 3)
	defer closeListeners(t, listeners)
	closeOneListener(t, listeners, 2)

	autoFallback := true
	resolver := NewPortResolver("agent")
	got, err := resolver.Resolve(config.PortConfig{
		Default:        ports[0],
		Range:          []int{ports[0], ports[2]},
		BindRetryCount: 1,
		BindRetryDelay: 1,
		AutoFallback:   &autoFallback,
	})
	if err != nil {
		t.Fatalf("resolve fallback port: %v", err)
	}
	if got != ports[2] {
		t.Fatalf("fallback port mismatch: got=%d want=%d", got, ports[2])
	}
}

func TestResolve_DefaultOccupied_FallbackDisabled(t *testing.T) {
	ports, listeners := occupyContiguousPorts(t, 1)
	defer closeListeners(t, listeners)

	autoFallback := false
	resolver := NewPortResolver("agent")
	_, err := resolver.Resolve(config.PortConfig{
		Default:        ports[0],
		Range:          []int{ports[0], ports[0]},
		BindRetryCount: 1,
		BindRetryDelay: 1,
		AutoFallback:   &autoFallback,
	})
	if err == nil {
		t.Fatal("expected error when preferred port is occupied and auto fallback is disabled")
	}
	if !strings.Contains(err.Error(), "未启用自动回退") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolve_AllPortsOccupied(t *testing.T) {
	ports, listeners := occupyContiguousPorts(t, 3)
	defer closeListeners(t, listeners)

	autoFallback := true
	resolver := NewPortResolver("agent")
	_, err := resolver.Resolve(config.PortConfig{
		Default:        ports[0],
		Range:          []int{ports[0], ports[2]},
		BindRetryCount: 1,
		BindRetryDelay: 1,
		AutoFallback:   &autoFallback,
	})
	if err == nil {
		t.Fatal("expected error when all ports in range are occupied")
	}
	if !strings.Contains(err.Error(), "未找到可用端口") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolve_InvalidConfig(t *testing.T) {
	t.Run("invalid default uses range when available", func(t *testing.T) {
		ports, listeners := occupyContiguousPorts(t, 2)
		defer closeListeners(t, listeners)
		closeOneListener(t, listeners, 1)

		resolver := NewPortResolver("compute")
		got, err := resolver.Resolve(config.PortConfig{
			Default:        0,
			Range:          []int{ports[0], ports[1]},
			BindRetryCount: 1,
			BindRetryDelay: 1,
		})
		if err != nil {
			t.Fatalf("resolve with invalid default but valid range: %v", err)
		}
		if got != ports[1] {
			t.Fatalf("resolved port mismatch: got=%d want=%d", got, ports[1])
		}
	})

	t.Run("invalid default and invalid range returns error", func(t *testing.T) {
		resolver := NewPortResolver("compute")
		_, err := resolver.Resolve(config.PortConfig{
			Default:        -1,
			Range:          []int{},
			BindRetryCount: 1,
			BindRetryDelay: 1,
		})
		if err == nil {
			t.Fatal("expected error when default and range are both invalid")
		}
	})
}

func TestIsPortAvailable_Free(t *testing.T) {
	port := testutil.ReserveFreePort(t)
	if !IsPortAvailable("", port) {
		t.Fatalf("expected free port %d to be available", port)
	}
}

func TestIsPortAvailable_Occupied(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen random port failed: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	if IsPortAvailable("127.0.0.1", port) {
		t.Fatalf("expected occupied port %d to be unavailable", port)
	}
}

func TestIsPortAvailable_Invalid(t *testing.T) {
	for _, port := range []int{0, -1, 99999} {
		if IsPortAvailable("", port) {
			t.Fatalf("expected invalid port %d to be unavailable", port)
		}
	}
}

func occupyContiguousPorts(t *testing.T, count int) ([]int, []net.Listener) {
	t.Helper()
	for base := 59000; base <= 61000-count; base++ {
		ports := make([]int, 0, count)
		listeners := make([]net.Listener, 0, count)
		ok := true
		for offset := 0; offset < count; offset++ {
			port := base + offset
			listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				ok = false
				break
			}
			ports = append(ports, port)
			listeners = append(listeners, listener)
		}
		if ok {
			return ports, listeners
		}
		closeListeners(t, listeners)
	}
	t.Fatalf("failed to reserve %d contiguous ports", count)
	return nil, nil
}

func closeListeners(t *testing.T, listeners []net.Listener) {
	t.Helper()
	for _, listener := range listeners {
		if listener == nil {
			continue
		}
		if err := listener.Close(); err != nil {
			t.Fatalf("close listener failed: %v", err)
		}
	}
}

func closeOneListener(t *testing.T, listeners []net.Listener, index int) {
	t.Helper()
	if index < 0 || index >= len(listeners) {
		t.Fatalf("listener index out of range: %d", index)
	}
	if listeners[index] == nil {
		return
	}
	if err := listeners[index].Close(); err != nil {
		t.Fatalf("close listener %d failed: %v", index, err)
	}
	listeners[index] = nil
}
