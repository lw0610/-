package geocoding

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderRequestsAreEvenlySpacedAcrossConcurrentResolvers(t *testing.T) {
	var mu sync.Mutex
	var starts []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		_, _ = w.Write([]byte(`{"status":{"infocode":3001}}`))
	}))
	defer server.Close()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := Resolver{Key: "test-secret", Endpoint: server.URL}
			if _, err := r.throttledSearch(context.Background(), "柳林", "罗山县"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(starts) != 4 {
		t.Fatal("requests lost")
	}
	for i := 1; i < len(starts); i++ {
		// Allow scheduler jitter, while rejecting the original instantaneous burst.
		if gap := starts[i].Sub(starts[i-1]); gap < 180*time.Millisecond {
			t.Fatalf("provider request burst: %s", gap)
		}
	}
}

func TestTooManyRequestsRetriesOnceAfterAdvertisedDelay(t *testing.T) {
	var requests atomic.Int32
	var mu sync.Mutex
	var starts []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"status":{"infocode":1000},"resultType":1,"count":1,"pois":[{"name":"柳林","address":"罗山县","lonlat":"114.4,31.7"}]}`))
	}))
	defer server.Close()
	r := Resolver{Key: "test-secret", Endpoint: server.URL}
	result, err := r.throttledSearch(context.Background(), "柳林", "罗山县")
	if err != nil || len(result.Candidates) != 1 || requests.Load() != 2 {
		t.Fatal("rate-limit recovery failed", err)
	}
	if starts[1].Sub(starts[0]) < time.Second {
		t.Fatal("Retry-After ignored")
	}
}

func TestRepeated429StopsAfterSingleRetryAndAuthenticationDoesNotRetry(t *testing.T) {
	for _, code := range []int{http.StatusTooManyRequests, http.StatusUnauthorized} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { count.Add(1); w.WriteHeader(code) }))
			defer server.Close()
			r := Resolver{Key: "test-secret", Endpoint: server.URL}
			if _, err := r.throttledSearch(context.Background(), "柳林", "罗山县"); err == nil {
				t.Fatal("provider error lost")
			}
			want := int32(1)
			if code == http.StatusTooManyRequests {
				want = 2
			}
			if count.Load() != want {
				t.Fatalf("requests %d want %d", count.Load(), want)
			}
		})
	}
}

func TestCancellationInterruptsRateLimitBackoffWithoutRetry(t *testing.T) {
	var count atomic.Int32
	called := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if count.Add(1) == 1 {
			close(called)
		}
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-called; cancel() }()
	r := Resolver{Key: "test-secret", Endpoint: server.URL}
	start := time.Now()
	_, err := r.throttledSearch(ctx, "柳林", "罗山县")
	if !errors.Is(err, context.Canceled) || time.Since(start) > time.Second || count.Load() != 1 {
		t.Fatal("backoff ignored cancellation", err)
	}
}

func TestTownCenterCannotBecomeGazetteerCoordinateForVillage(t *testing.T) {
	town := Candidate{Name: "铁铺镇", Address: "河南省信阳市罗山县铁铺镇", Lon: 114.5, Lat: 31.8, TypeName: "行政区划"}
	p := queryPlan{ModernName: "殷家湾", Province: "河南省", City: "信阳市", County: "罗山县", Township: "铁铺镇"}
	if candidateCoherent(town, p) {
		t.Fatal("parent town center accepted as village coordinate")
	}
	p.ModernName = "铁铺镇"
	if !candidateCoherent(town, p) {
		t.Fatal("actual township target incorrectly rejected")
	}
	server := fixtureServer(t, []map[string]any{poi("铁铺镇", "河南省信阳市罗山县铁铺镇", "114.5,31.8")}, 1)
	defer server.Close()
	e := place("p1", "殷家湾")
	r := Resolver{Key: "test-secret", Endpoint: server.URL, Model: automaticModel(t, func(stage string, _ []map[string]any) {
		if stage == "select" {
			t.Error("upper administrative center reached candidate selector")
		}
	})}
	if err := r.Resolve(context.Background(), graphWith(e), "经过铁铺镇附近的殷家湾"); err != nil {
		t.Fatal(err)
	}
	if e["coordinate_source"] != "llm_estimated" || metadataOf(e)["status"] != "estimated" {
		t.Fatal("town center mislabeled as village gazetteer point", e)
	}
	if metadataOf(e)["reference_candidates"] == nil {
		t.Fatal("upper point lost as estimation reference")
	}
}
