package osmdata

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Prime just the ranking so download tests measure actual query attempts rather
// than the separate, small endpoint health requests.
func primedDownloadClient(t *testing.T, options Options) *client {
	t.Helper()
	c, err := newClient(options)
	if err != nil {
		t.Fatal(err)
	}
	s := c.state()
	s.mu.Lock()
	s.ranked = append([]string{}, c.options.Endpoints...)
	s.health = nil
	s.checkedAt = time.Now()
	for _, endpoint := range s.ranked {
		s.health = append(s.health, Health{URL: endpoint, Available: true})
	}
	s.mu.Unlock()
	return c
}

func TestDownloadOptionsDefaultsAndBounds(t *testing.T) {
	c, err := newClient(Options{})
	if err != nil || c.options.TotalTimeoutSeconds != 600 || c.options.MaxAttempts != 3 {
		t.Fatalf("defaults: %v, %v", c, err)
	}
	for _, options := range []Options{{TotalTimeoutSeconds: -1}, {TotalTimeoutSeconds: 7201}, {MaxAttempts: -1}, {MaxAttempts: 9}} {
		if _, err := newClient(options); err == nil {
			t.Fatalf("accepted invalid options: %+v", options)
		}
	}
	for _, options := range []Options{{TotalTimeoutSeconds: 1, MaxAttempts: 1}, {TotalTimeoutSeconds: 7200, MaxAttempts: 8}} {
		if _, err := newClient(options); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDownloadContinuousSlowStreamOutlivesIdleTimeout(t *testing.T) {
	fastNetwork(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"elements":[`)
		w.(http.Flusher).Flush()
		for i := 0; i < 6; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(250 * time.Millisecond):
				fmt.Fprint(w, " ")
				w.(http.Flusher).Flush()
			}
		}
		fmt.Fprint(w, "]}")
	}))
	defer server.Close()
	c := primedDownloadClient(t, Options{Endpoints: []string{server.URL}, TimeoutSeconds: 1, TotalTimeoutSeconds: 5, HTTPClient: &http.Client{Timeout: 50 * time.Millisecond}})
	start := time.Now()
	if _, endpoint, err := c.query(context.Background(), "x"); err != nil || endpoint != server.URL {
		t.Fatalf("healthy stream aborted: %s, %v", endpoint, err)
	}
	if time.Since(start) <= time.Second {
		t.Fatal("fixture did not exceed the old per-request timeout")
	}
}

func TestDownloadHeaderAndBodyIdleTimeout(t *testing.T) {
	for _, phase := range []string{"waiting", "downloading"} {
		t.Run(phase, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if phase == "downloading" {
					fmt.Fprint(w, "{")
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			defer server.Close()
			c, _ := newClient(Options{TimeoutSeconds: 1})
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			start := time.Now()
			_, err := c.requestRaw(ctx, http.MethodGet, server.URL, "")
			var idle *activityTimeoutError
			if !errors.As(err, &idle) || idle.phase != phase || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("incorrect idle error: %v", err)
			}
			if time.Since(start) > 3*time.Second {
				t.Fatal("idle timeout was not enforced promptly")
			}
		})
	}
}

func TestDownloadTotalAndParentBudget(t *testing.T) {
	fastNetwork(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "{")
		w.(http.Flusher).Flush()
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				fmt.Fprint(w, " ")
				w.(http.Flusher).Flush()
			}
		}
	}))
	defer server.Close()
	for _, tc := range []struct {
		name    string
		total   int
		parent  time.Duration
		maximum time.Duration
	}{{"total", 1, 5 * time.Second, 2 * time.Second}, {"earlier parent", 10, 150 * time.Millisecond, time.Second}} {
		t.Run(tc.name, func(t *testing.T) {
			c := primedDownloadClient(t, Options{Endpoints: []string{server.URL}, TimeoutSeconds: 1, TotalTimeoutSeconds: tc.total})
			ctx, cancel := context.WithTimeout(context.Background(), tc.parent)
			defer cancel()
			start := time.Now()
			_, _, err := c.query(ctx, "x")
			if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > tc.maximum {
				t.Fatalf("budget not respected: %v, elapsed %s", err, time.Since(start))
			}
			var idle *activityTimeoutError
			if errors.As(err, &idle) {
				t.Fatal("continuous stream was reported as idle")
			}
		})
	}
}

func TestDownloadAttemptsDistinctAndProgressMetadata(t *testing.T) {
	fastNetwork(t)
	var calls [5]atomic.Int32
	var endpoints []string
	for i := range calls {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls[i].Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()
		endpoints = append(endpoints, server.URL, server.URL)
	}
	c := primedDownloadClient(t, Options{Endpoints: endpoints, MaxAttempts: 3})
	var mu sync.Mutex
	var events []Progress
	ctx := WithProgressCallback(context.Background(), func(p Progress) {
		mu.Lock()
		events = append(events, p)
		mu.Unlock()
	})
	ctx, finish := beginProgress(ctx, "osm_get_features", "铁路")
	_, _, err := c.query(ctx, "x")
	finish(nil, err)
	if err == nil || !strings.Contains(err.Error(), "3 个不同节点") {
		t.Fatalf("missing attempt limit: %v", err)
	}
	for i := range calls {
		want := int32(0)
		if i < 3 {
			want = 1
		}
		if calls[i].Load() != want {
			t.Fatalf("node %d called %d times; want %d", i, calls[i].Load(), want)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	seen := map[int]bool{}
	for _, p := range events {
		if p.Phase == "waiting" || p.Phase == "retrying" {
			if p.RequestKey == "" || p.Attempt < 1 || p.Attempt > 3 || p.MaxAttempts != 3 {
				t.Fatalf("lost request metadata: %+v", p)
			}
			seen[p.Attempt] = true
		}
	}
	if len(seen) != 3 {
		t.Fatalf("missing attempts: %v", seen)
	}
}

func TestDownloadHealthSkipsPermanentKeepsTransientFallback(t *testing.T) {
	fastNetwork(t)
	var missingCalls, transientCalls atomic.Int32
	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		missingCalls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer missing.Close()
	transient := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if transientCalls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `{"elements":[]}`)
	}))
	defer transient.Close()
	untrusted := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("untrusted TLS endpoint reached handler")
	}))
	untrusted.Config.ErrorLog = log.New(io.Discard, "", 0)
	untrusted.StartTLS()
	defer untrusted.Close()
	c, _ := newClient(Options{Endpoints: []string{"invalid-url", missing.URL, untrusted.URL, transient.URL}, MaxAttempts: 1})
	var waitingEvents atomic.Int32
	ctx := WithProgressCallback(context.Background(), func(p Progress) {
		if p.Phase == "waiting" {
			waitingEvents.Add(1)
		}
	})
	ctx, finish := beginProgress(ctx, "osm_get_features", "铁路")
	_, endpoint, err := c.query(ctx, "x")
	finish(nil, err)
	if err != nil || endpoint != transient.URL || transientCalls.Load() != 2 || missingCalls.Load() != 1 {
		t.Fatalf("fallback failed: %s, %v, transient=%d missing=%d", endpoint, err, transientCalls.Load(), missingCalls.Load())
	}
	if waitingEvents.Load() != 1 {
		t.Fatalf("health probes emitted download progress: %d waiting events", waitingEvents.Load())
	}
	_, health := c.rank(context.Background())
	permanent := 0
	for _, h := range health {
		if h.Permanent {
			permanent++
		}
	}
	if permanent != 3 {
		t.Fatalf("expected 3 permanent nodes, got %+v", health)
	}
}

func TestDownloadNoAvailableNodesAndCancellation(t *testing.T) {
	fastNetwork(t)
	c, _ := newClient(Options{Endpoints: []string{"invalid-url", "ftp://example.invalid/data"}})
	if _, _, err := c.query(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "无可用节点") {
		t.Fatalf("unclear unavailable error: %v", err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		calls.Add(1)
		<-r.Context().Done()
	}))
	defer server.Close()
	c = primedDownloadClient(t, Options{Endpoints: []string{server.URL}, TotalTimeoutSeconds: 10})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := c.query(ctx, "x"); !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("cancelled query sent request: %v, calls=%d", err, calls.Load())
	}
	ctx, cancel = context.WithCancel(context.Background())
	timer := time.AfterFunc(100*time.Millisecond, cancel)
	defer timer.Stop()
	start := time.Now()
	if _, _, err := c.query(ctx, "x"); !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
		t.Fatalf("in-flight cancellation failed: %v", err)
	}
}

func TestDownloadResponseLimitStopsWithoutFailover(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		t.Run(fmt.Sprintf("chunked=%v", chunked), func(t *testing.T) {
			fastNetwork(t)
			var nextCalls atomic.Int32
			big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if chunked {
					w.(http.Flusher).Flush()
				} else {
					w.Header().Set("Content-Length", fmt.Sprint((1<<20)+1))
				}
				fmt.Fprint(w, strings.Repeat("x", (1<<20)+1))
			}))
			defer big.Close()
			next := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalls.Add(1)
				fmt.Fprint(w, `{"elements":[]}`)
			}))
			defer next.Close()
			c := primedDownloadClient(t, Options{Endpoints: []string{big.URL, next.URL}, MaxResponseMB: 1})
			_, _, err := c.query(context.Background(), "x")
			var limit *responseLimitError
			if !errors.As(err, &limit) || nextCalls.Load() != 0 {
				t.Fatalf("size limit retried or missed: %v, calls=%d", err, nextCalls.Load())
			}
		})
	}
}

func TestDownloadWaitingForSharedRankHonoursCancellation(t *testing.T) {
	fastNetwork(t)
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	c, _ := newClient(Options{Endpoints: []string{server.URL}})
	owner, stopOwner := context.WithCancel(context.Background())
	defer stopOwner()
	finished := make(chan struct{})
	go func() { c.rank(owner); close(finished) }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	c.rank(ctx)
	if ctx.Err() == nil || time.Since(start) > time.Second {
		t.Fatal("rank waiting ignored cancellation")
	}
	stopOwner()
	<-finished
}

func TestDownloadCancelledThrottleWaitersDoNotReserveFutureSlots(t *testing.T) {
	fastNetwork(t)
	requestInterval = 100 * time.Millisecond
	if err := throttle(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	overpassThrottle.Lock()
	next := overpassThrottle.next
	overpassThrottle.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	var waiting sync.WaitGroup
	for i := 0; i < 8; i++ {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			if err := throttle(ctx, false); !errors.Is(err, context.Canceled) {
				t.Errorf("expected queued request cancellation, got %v", err)
			}
		}()
	}
	time.Sleep(20 * time.Millisecond)
	cancel()
	waiting.Wait()
	overpassThrottle.Lock()
	afterCancel := overpassThrottle.next
	overpassThrottle.Unlock()
	if !afterCancel.Equal(next) {
		t.Fatalf("cancelled waiters reserved future time: %s => %s", next, afterCancel)
	}
	start := time.Now()
	if err := throttle(context.Background(), false); err != nil || time.Since(start) > 250*time.Millisecond {
		t.Fatalf("new request waited for cancelled reservations: %v, elapsed %s", err, time.Since(start))
	}
}

func TestDownloadRejectedQueryDoesNotPoisonEndpoint(t *testing.T) {
	for _, status := range []int{400, 413, 414, 422} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			fastNetwork(t)
			var calls, nextCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.FormValue("data") == "bad" {
					w.WriteHeader(status)
					return
				}
				fmt.Fprint(w, `{"elements":[]}`)
			}))
			defer server.Close()
			next := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalls.Add(1)
				fmt.Fprint(w, `{"elements":[]}`)
			}))
			defer next.Close()
			c := primedDownloadClient(t, Options{Endpoints: []string{server.URL, next.URL}})
			if _, _, err := c.query(context.Background(), "bad"); err == nil || !strings.Contains(err.Error(), "拒绝本次查询") {
				t.Fatalf("query rejection not explained: %v", err)
			}
			_, health := c.rank(context.Background())
			if !health[0].Available || health[0].Permanent || nextCalls.Load() != 0 {
				t.Fatalf("bad query poisoned node or failed over: %+v, next calls=%d", health, nextCalls.Load())
			}
			if _, endpoint, err := c.query(context.Background(), "valid"); err != nil || endpoint != server.URL || calls.Load() != 2 {
				t.Fatalf("later valid query did not recover: %s, %v, calls=%d", endpoint, err, calls.Load())
			}
		})
	}
}

func TestDownloadPermanentHealthCanBeRefreshed(t *testing.T) {
	for _, refresh := range []string{"explicit", "expiry"} {
		t.Run(refresh, func(t *testing.T) {
			fastNetwork(t)
			var broken atomic.Bool
			broken.Store(true)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if broken.Load() {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				fmt.Fprint(w, `{"elements":[]}`)
			}))
			defer server.Close()
			c, _ := newClient(Options{Endpoints: []string{server.URL}})
			if _, _, err := c.query(context.Background(), "x"); err == nil {
				t.Fatal("broken endpoint accepted")
			}
			broken.Store(false)
			if _, _, err := c.query(context.Background(), "x"); err == nil || calls.Load() != 1 {
				t.Fatalf("permanent failure not cached: %v, calls=%d", err, calls.Load())
			}
			if refresh == "explicit" {
				c.invalidateHealth()
			} else {
				s := c.state()
				s.mu.Lock()
				s.checkedAt = time.Now().Add(-healthCacheTTL - time.Second)
				s.mu.Unlock()
			}
			if _, endpoint, err := c.query(context.Background(), "x"); err != nil || endpoint != server.URL || calls.Load() != 3 {
				t.Fatalf("refreshed endpoint failed: %s, %v, calls=%d", endpoint, err, calls.Load())
			}
		})
	}
}
