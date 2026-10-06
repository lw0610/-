package testutil

import (
	"net"
	"testing"
)

// ReserveFreePort 返回一个当前可用的本地回环端口，仅用于测试。
func ReserveFreePort(tb testing.TB) int {
	tb.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("listen free port failed: %v", err)
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		tb.Fatalf("unexpected listener addr type: %T", listener.Addr())
	}
	return addr.Port
}
