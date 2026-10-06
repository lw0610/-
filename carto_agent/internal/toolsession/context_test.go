package toolsession

import (
	"context"
	"testing"
)

func TestWithContextStoresTrimmedValues(t *testing.T) {
	ctx := WithContext(context.Background(), " sid ", " /tmp/demo.qgz ", " inst-1 ")

	if got := SessionIDFromContext(ctx); got != "sid" {
		t.Fatalf("unexpected session id: %q", got)
	}
	if got := ProjectPathFromContext(ctx); got != "/tmp/demo.qgz" {
		t.Fatalf("unexpected project path: %q", got)
	}
	if got := InstanceIDFromContext(ctx); got != "inst-1" {
		t.Fatalf("unexpected instance id: %q", got)
	}
}

func TestContextReadersHandleNilAndEmpty(t *testing.T) {
	if got := SessionIDFromContext(nil); got != "" {
		t.Fatalf("expected empty session id for nil context, got %q", got)
	}

	ctx := WithContext(context.Background(), "", "", "")
	if got := SessionIDFromContext(ctx); got != "" {
		t.Fatalf("expected empty session id, got %q", got)
	}
	if got := ProjectPathFromContext(ctx); got != "" {
		t.Fatalf("expected empty project path, got %q", got)
	}
	if got := InstanceIDFromContext(ctx); got != "" {
		t.Fatalf("expected empty instance id, got %q", got)
	}
}
