package capability

import (
	"context"
	"testing"
	"time"
)

func TestSnapshotStoreRoundTrip(t *testing.T) {
	store := NewSnapshotStore()
	if got := store.Get(); got != nil {
		t.Fatalf("expected nil initial snapshot, got %#v", got)
	}

	expected := &CapabilitySnapshot{ConfigName: "default", Tools: map[string]ToolCapability{}, Packages: map[string]PackageCapability{}, ToolOrder: []string{}, Agents: []AgentCapabilityView{}}
	store.Set(expected)

	got := store.Get()
	if got == nil {
		t.Fatal("expected stored snapshot")
	}
	if got.ConfigName != "default" {
		t.Fatalf("expected config name default, got %q", got.ConfigName)
	}
}

func TestRefresherRefreshStoresBuiltSnapshot(t *testing.T) {
	store := NewSnapshotStore()
	refresher := &Refresher{
		store: store,
		buildFunc: func(ctx context.Context, reason string) (*CapabilitySnapshot, error) {
			return &CapabilitySnapshot{ConfigName: reason, Tools: map[string]ToolCapability{}, Packages: map[string]PackageCapability{}, ToolOrder: []string{}, Agents: []AgentCapabilityView{}}, nil
		},
	}

	snapshot, err := refresher.Refresh(context.Background(), "manual")
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if snapshot == nil || snapshot.ConfigName != "manual" {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if got := store.Get(); got == nil || got.ConfigName != "manual" {
		t.Fatalf("expected store to keep refreshed snapshot, got %#v", got)
	}
}

func TestRefresherRequestRefreshBuildsSnapshotAsync(t *testing.T) {
	store := NewSnapshotStore()
	done := make(chan struct{})
	refresher := &Refresher{
		store:         store,
		debounceDelay: 10 * time.Millisecond,
		buildFunc: func(ctx context.Context, reason string) (*CapabilitySnapshot, error) {
			close(done)
			return &CapabilitySnapshot{ConfigName: reason, Tools: map[string]ToolCapability{}, Packages: map[string]PackageCapability{}, ToolOrder: []string{}, Agents: []AgentCapabilityView{}}, nil
		},
	}

	refresher.RequestRefresh("async")

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected async refresh to run")
	}

	if got := store.Get(); got == nil || got.ConfigName != "async" {
		t.Fatalf("expected async snapshot to be stored, got %#v", got)
	}
}

func TestRefresherRequestRefreshDebouncesBurst(t *testing.T) {
	store := NewSnapshotStore()
	started := make(chan struct{}, 10)
	refresher := &Refresher{
		store:         store,
		debounceDelay: 20 * time.Millisecond,
		buildFunc: func(ctx context.Context, reason string) (*CapabilitySnapshot, error) {
			started <- struct{}{}
			return &CapabilitySnapshot{ConfigName: reason, Tools: map[string]ToolCapability{}, Packages: map[string]PackageCapability{}, ToolOrder: []string{}, Agents: []AgentCapabilityView{}}, nil
		},
	}

	for index := 0; index < 10; index += 1 {
		refresher.RequestRefresh("burst")
	}

	time.Sleep(150 * time.Millisecond)
	close(started)
	count := 0
	for range started {
		count += 1
	}
	if count > 2 {
		t.Fatalf("expected debounce to limit builds to <=2, got %d", count)
	}
	if got := store.Get(); got == nil || got.ConfigName != "burst" {
		t.Fatalf("expected debounced snapshot to be stored, got %#v", got)
	}
}
