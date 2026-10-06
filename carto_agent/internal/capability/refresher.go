package capability

import (
	"context"
	"sync"
	"time"
)

const defaultRefreshDebounce = 200 * time.Millisecond

type Refresher struct {
	store         *SnapshotStore
	buildFunc     func(ctx context.Context, reason string) (*CapabilitySnapshot, error)
	debounceDelay time.Duration

	mu            sync.Mutex
	timer         *time.Timer
	pendingReason string
}

func NewRefresher(store *SnapshotStore, buildFunc func(ctx context.Context, reason string) (*CapabilitySnapshot, error)) *Refresher {
	return &Refresher{
		store:     store,
		buildFunc: buildFunc,
	}
}

func (r *Refresher) Refresh(ctx context.Context, reason string) (*CapabilitySnapshot, error) {
	if r == nil || r.buildFunc == nil {
		return nil, nil
	}
	snapshot, err := r.buildFunc(ctx, reason)
	if err != nil {
		return nil, err
	}
	if r.store != nil {
		r.store.Set(snapshot)
	}
	return snapshot, nil
}

func (r *Refresher) RequestRefresh(reason string) {
	if r == nil || r.buildFunc == nil {
		return
	}
	r.mu.Lock()
	r.pendingReason = reason
	delay := r.debounceDelay
	if delay <= 0 {
		delay = defaultRefreshDebounce
	}
	if r.timer != nil {
		r.timer.Reset(delay)
		r.mu.Unlock()
		return
	}
	r.timer = time.AfterFunc(delay, func() {
		r.runPendingRefresh()
	})
	r.mu.Unlock()
}

func (r *Refresher) runPendingRefresh() {
	if r == nil {
		return
	}
	r.mu.Lock()
	reason := r.pendingReason
	r.pendingReason = ""
	r.timer = nil
	r.mu.Unlock()
	_, _ = r.Refresh(context.Background(), reason)
}
