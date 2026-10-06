package capability

import "sync"

type SnapshotStore struct {
	mu       sync.RWMutex
	snapshot *CapabilitySnapshot
}

func NewSnapshotStore() *SnapshotStore {
	return &SnapshotStore{}
}

func (s *SnapshotStore) Get() *CapabilitySnapshot {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot
}

func (s *SnapshotStore) Set(snapshot *CapabilitySnapshot) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = snapshot
}
