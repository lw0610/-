package discovery

import (
	"database/sql"
	"errors"
	"testing"
)

func TestServiceUnregisterOwnedPreservesReplacement(t *testing.T) {
	registry, _ := newTestServiceRegistry(t)
	entry := testServiceEntry("agent", 50011)
	entry.PID = 222
	if err := registry.Register(entry); err != nil {
		t.Fatal(err)
	}
	for _, owner := range [][2]int{{111, 50010}, {111, 50011}, {222, 50010}} {
		if err := registry.UnregisterOwned("agent", owner[0], owner[1]); err != nil {
			t.Fatal(err)
		}
		got, err := registry.GetPort("agent")
		if err != nil || got.PID != 222 || got.Port != 50011 {
			t.Fatalf("replacement registration removed: %+v, %v", got, err)
		}
	}
	if err := registry.UnregisterOwned("agent", 222, 50011); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.GetPort("agent"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("owned registration not removed: %v", err)
	}
	if err := registry.UnregisterOwned("agent", 222, 50011); err != nil {
		t.Fatalf("cleanup must be idempotent: %v", err)
	}
}

func TestServiceHeartbeatOwnedPreservesReplacement(t *testing.T) {
	registry, _ := newTestServiceRegistry(t)
	entry := testServiceEntry("agent", 50011)
	entry.PID = 222
	entry.LastSeen = "2000-01-01T00:00:00Z"
	if err := registry.Register(entry); err != nil {
		t.Fatal(err)
	}
	if err := registry.HeartbeatOwned("agent", 111, 50010); err != nil {
		t.Fatal(err)
	}
	got, err := registry.GetPort("agent")
	if err != nil || got.LastSeen != entry.LastSeen {
		t.Fatalf("old backend refreshed replacement: %+v, %v", got, err)
	}
	if err := registry.HeartbeatOwned("agent", 222, 50011); err != nil {
		t.Fatal(err)
	}
	got, err = registry.GetPort("agent")
	if err != nil || got.LastSeen == entry.LastSeen {
		t.Fatalf("owner failed to refresh registration: %+v, %v", got, err)
	}
}
