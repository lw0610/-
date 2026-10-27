package osmdata

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

var DefaultEndpoints = []string{"https://lz4.overpass-api.de/api/interpreter", "https://z.overpass-api.de/api/interpreter", "https://overpass-api.de/api/interpreter", "https://overpass.osm.jp/api/interpreter", "https://overpass.kumi.systems/api/interpreter", "https://overpass.private.coffee/api/interpreter", "https://overpass.openstreetmap.fr/api/interpreter", "https://maps.mail.ru/osm/tools/overpass/api/interpreter"}

const userAgent = "AutoCarto/4.0 (local GIS application; OpenStreetMap data acquisition)"
const healthCacheTTL = 5 * time.Minute

type Options struct {
	Enabled                                                  bool
	Authorized                                               bool
	Endpoints                                                []string
	NominatimURL, OSMAPIURL, ProxyURL, OutputDir, ProjectDir string
	TimeoutSeconds, MaxResponseMB                            int
	TotalTimeoutSeconds, MaxAttempts                         int
	HTTPClient                                               *http.Client
}
type client struct {
	options Options
	http    *http.Client
	key     string
}
type endpointState struct {
	mu        sync.Mutex
	ranked    []string
	health    []Health
	loading   chan struct{}
	checkedAt time.Time
}
type Health struct {
	URL       string `json:"url"`
	Available bool   `json:"available"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
	Permanent bool   `json:"permanent,omitempty"`
}

var states sync.Map
var overpassThrottle, nominatimThrottle struct {
	sync.Mutex
	next time.Time
}
var requestInterval = 1100 * time.Millisecond
var backoffs = []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}

func pause(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func throttle(ctx context.Context, nominatim bool) error {
	t := &overpassThrottle
	if nominatim {
		t = &nominatimThrottle
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		t.Lock()
		now := time.Now()
		if !now.Before(t.next) {
			// Only requests actually admitted consume a slot. Cancelled health
			// probes must not leave a queue of future reservations behind.
			t.next = now.Add(requestInterval)
			t.Unlock()
			return nil
		}
		wait := t.next.Sub(now)
		t.Unlock()
		if err := pause(ctx, wait); err != nil {
			return err
		}
	}
}
func newClient(options Options) (*client, error) {
	if options.TimeoutSeconds == 0 {
		options.TimeoutSeconds = 120
	}
	if options.MaxResponseMB == 0 {
		options.MaxResponseMB = 200
	}
	if options.TotalTimeoutSeconds == 0 {
		options.TotalTimeoutSeconds = 600
	}
	if options.MaxAttempts == 0 {
		options.MaxAttempts = 3
	}
	if options.TimeoutSeconds < 1 || options.TimeoutSeconds > 3600 || options.MaxResponseMB < 1 || options.MaxResponseMB > 200 || options.TotalTimeoutSeconds < 1 || options.TotalTimeoutSeconds > 7200 || options.MaxAttempts < 1 || options.MaxAttempts > 8 {
		return nil, fmt.Errorf("OSM 超时或体量限制无效")
	}
	if len(options.Endpoints) == 0 {
		options.Endpoints = append([]string{}, DefaultEndpoints...)
	}
	endpoints := make([]string, 0, len(options.Endpoints))
	seen := make(map[string]bool)
	for _, ep := range options.Endpoints {
		ep = strings.TrimSpace(ep)
		if !seen[ep] {
			endpoints = append(endpoints, ep)
			seen[ep] = true
		}
	}
	options.Endpoints = endpoints
	if options.NominatimURL == "" {
		options.NominatimURL = "https://nominatim.openstreetmap.org/search"
	}
	if options.OSMAPIURL == "" {
		options.OSMAPIURL = "https://api.openstreetmap.org/api/0.6"
	}
	hc := options.HTTPClient
	if hc == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if options.ProxyURL != "" {
			u, e := url.Parse(options.ProxyURL)
			if e != nil {
				return nil, e
			}
			transport.Proxy = http.ProxyURL(u)
		}
		hc = &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 && req.URL.Host != via[0].URL.Host {
				return &permanentEndpointError{"OSM 服务重定向到不同主机"}
			}
			if len(via) > 3 {
				return &permanentEndpointError{"OSM 服务重定向次数过多"}
			}
			return nil
		}}
	} else {
		// The request context and activity timer below own the timeout policy.
		// A client-wide timeout would incorrectly abort a healthy slow stream.
		copy := *hc
		copy.Timeout = 0
		hc = &copy
	}
	return &client{options, hc, strings.Join(options.Endpoints, "|") + options.ProxyURL}, nil
}
func (c *client) request(ctx context.Context, method, address, body string, nom bool) ([]byte, error) {
	if err := throttle(ctx, nom); err != nil {
		return nil, err
	}
	return c.requestRaw(ctx, method, address, body)
}
func (c *client) requestRaw(ctx context.Context, method, address, body string) ([]byte, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return nil, &permanentEndpointError{"OSM 节点地址无效，须为不含账号信息的 HTTP(S) 地址"}
	}
	requestCtx, cancel := context.WithCancelCause(ctx)
	watchdog := newActivityWatchdog(time.Duration(c.options.TimeoutSeconds)*time.Second, cancel)
	defer func() { watchdog.stop(); cancel(nil) }()
	req, err := http.NewRequestWithContext(requestCtx, method, address, strings.NewReader(body))
	if err != nil {
		return nil, &permanentEndpointError{fmt.Sprintf("OSM 请求地址无效: %v", err)}
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	reportProgress(ctx, Progress{Phase: "waiting", Endpoint: address, Message: "等待 OSM 服务响应"})
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, requestError(requestCtx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &httpStatusError{resp.StatusCode}
	}
	watchdog.touch("downloading")
	limit := int64(c.options.MaxResponseMB) << 20
	if resp.ContentLength > limit {
		return nil, &responseLimitError{c.options.MaxResponseMB}
	}
	total := resp.ContentLength
	if total < 0 {
		total = 0
	}
	reportProgress(ctx, Progress{Phase: "downloading", Endpoint: address, TotalBytes: total, Message: "正在接收 OSM 数据"})
	reader := io.LimitReader(resp.Body, limit+1)
	buffer := make([]byte, 64*1024)
	var data []byte
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			watchdog.touch("downloading")
			data = append(data, buffer[:n]...)
			if int64(len(data)) > limit {
				return nil, &responseLimitError{c.options.MaxResponseMB}
			}
			reportProgress(ctx, Progress{Phase: "downloading", Endpoint: address, CompletedBytes: int64(len(data)), TotalBytes: total, Message: "正在接收 OSM 数据"})
		}
		if readErr != nil {
			if cause := context.Cause(requestCtx); cause != nil {
				return nil, cause
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, requestError(requestCtx, readErr)
		}
	}
	trim := strings.TrimSpace(string(data))
	if strings.HasPrefix(trim, "<") {
		return nil, fmt.Errorf("OSM 服务返回 HTML 错误页")
	}
	return data, nil
}

type permanentEndpointError struct{ message string }

func (e *permanentEndpointError) Error() string { return e.message }

type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string { return fmt.Sprintf("OSM 服务 HTTP %d", e.code) }

type responseLimitError struct{ megabytes int }

func (e *responseLimitError) Error() string {
	return fmt.Sprintf("OSM 响应超过 %d MB，请缩小范围", e.megabytes)
}

func permanentEndpointFailure(err error) bool {
	var invalid *permanentEndpointError
	var status *httpStatusError
	var hostname x509.HostnameError
	var authority x509.UnknownAuthorityError
	var certificate x509.CertificateInvalidError
	var roots x509.SystemRootsError
	if errors.As(err, &invalid) || errors.As(err, &hostname) || errors.As(err, &authority) || errors.As(err, &certificate) || errors.As(err, &roots) {
		return true
	}
	if errors.As(err, &status) {
		switch status.code {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone:
			return true
		}
	}
	return false
}

func rejectedRequestFailure(err error) bool {
	var status *httpStatusError
	if errors.As(err, &status) {
		switch status.code {
		case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong, http.StatusUnprocessableEntity:
			return true
		}
	}
	return false
}

type activityTimeoutError struct {
	phase    string
	duration time.Duration
}

func (e *activityTimeoutError) Error() string {
	if e.phase == "waiting" {
		return fmt.Sprintf("等待 OSM 响应头超过 %s", e.duration)
	}
	return fmt.Sprintf("接收 OSM 数据连续 %s 没有新数据", e.duration)
}
func (e *activityTimeoutError) Unwrap() error { return context.DeadlineExceeded }

// A callback already scheduled by AfterFunc may race a successful read. Checking
// the last activity under the same mutex prevents a stale callback cancelling a
// stream whose idle deadline has just been extended.
type activityWatchdog struct {
	mu       sync.Mutex
	duration time.Duration
	last     time.Time
	phase    string
	timer    *time.Timer
	stopped  bool
	cancel   context.CancelCauseFunc
}

func newActivityWatchdog(d time.Duration, cancel context.CancelCauseFunc) *activityWatchdog {
	w := &activityWatchdog{duration: d, last: time.Now(), phase: "waiting", cancel: cancel}
	w.mu.Lock()
	w.timer = time.AfterFunc(d, w.expire)
	w.mu.Unlock()
	return w
}
func (w *activityWatchdog) expire() {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	if remaining := w.duration - time.Since(w.last); remaining > 0 {
		w.timer.Reset(remaining)
		w.mu.Unlock()
		return
	}
	w.stopped = true
	err := &activityTimeoutError{w.phase, w.duration}
	w.mu.Unlock()
	w.cancel(err)
}
func (w *activityWatchdog) touch(phase string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.stopped {
		w.last, w.phase = time.Now(), phase
		w.timer.Reset(w.duration)
	}
}
func (w *activityWatchdog) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
	w.timer.Stop()
}
func requestError(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return err
}
func (c *client) post(ctx context.Context, endpoint, query string) (Response, error) {
	data, err := c.request(ctx, http.MethodPost, endpoint, url.Values{"data": {query}}.Encode(), false)
	if err != nil {
		return Response{}, err
	}
	var result Response
	if err = json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("OSM JSON 无效: %w", err)
	}
	if result.Remark != "" {
		return result, fmt.Errorf("OSM 范围失败: %s", result.Remark)
	}
	return result, nil
}
func (c *client) state() *endpointState {
	v, _ := states.LoadOrStore(c.key, &endpointState{})
	return v.(*endpointState)
}

func (c *client) invalidateHealth() {
	state := c.state()
	state.mu.Lock()
	state.ranked, state.health, state.checkedAt = nil, nil, time.Time{}
	state.mu.Unlock()
}

func (c *client) rank(ctx context.Context) ([]string, []Health) {
	state := c.state()
	for {
		state.mu.Lock()
		if len(state.ranked) > 0 && time.Since(state.checkedAt) < healthCacheTTL {
			ranked, health := append([]string{}, state.ranked...), append([]Health{}, state.health...)
			state.mu.Unlock()
			return ranked, health
		}
		if state.loading == nil {
			state.loading = make(chan struct{})
			state.mu.Unlock()
			break
		}
		loading := state.loading
		state.mu.Unlock()
		// Another query may already be checking these nodes. Its network wait
		// must not hold the mutex or prevent this query honouring its deadline.
		select {
		case <-loading:
		case <-ctx.Done():
			return nil, nil
		}
	}
	ctx = withoutProgress(ctx)
	ch := make(chan Health, len(c.options.Endpoints))
	for _, ep := range c.options.Endpoints {
		go func(ep string) {
			err := throttle(ctx, false)
			probe, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			start := time.Now()
			if err == nil {
				var raw []byte
				raw, err = c.requestRaw(probe, http.MethodPost, ep, url.Values{"data": {"[out:json][timeout:5];node(1);out skel;"}}.Encode())
				if err == nil {
					var response Response
					err = json.Unmarshal(raw, &response)
					if response.Remark != "" {
						err = fmt.Errorf("%s", response.Remark)
					}
				}
			}
			h := Health{URL: ep, Available: err == nil, LatencyMS: time.Since(start).Milliseconds()}
			if err != nil {
				h.Error = err.Error()
				h.Permanent = permanentEndpointFailure(err)
			}
			ch <- h
		}(ep)
	}
	health := make([]Health, 0, len(c.options.Endpoints))
	for range c.options.Endpoints {
		health = append(health, <-ch)
	}
	sort.SliceStable(health, func(i, j int) bool {
		if health[i].Available != health[j].Available {
			return health[i].Available
		}
		return health[i].LatencyMS < health[j].LatencyMS
	})
	ranked := make([]string, 0, len(health))
	for _, h := range health {
		ranked = append(ranked, h.URL)
	}
	state.mu.Lock()
	// A cancelled health check is not evidence that all nodes are unhealthy.
	if ctx.Err() == nil {
		state.ranked, state.health = ranked, health
		state.checkedAt = time.Now()
	}
	close(state.loading)
	state.loading = nil
	state.mu.Unlock()
	return ranked, health
}
func (c *client) query(ctx context.Context, q string) (Response, string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.options.TotalTimeoutSeconds)*time.Second)
	defer cancel()
	eps, health := c.rank(ctx)
	if err := ctx.Err(); err != nil {
		return Response{}, "", fmt.Errorf("OSM 查询在节点检查期间结束: %w", err)
	}
	healthByURL := make(map[string]Health, len(health))
	for _, h := range health {
		healthByURL[h.URL] = h
	}
	// A failed small probe can be transient. Keep these nodes as bounded
	// fallbacks, but avoid certificates, invalid addresses and permanent 4xx.
	available := make([]string, 0, len(eps))
	var skipped []string
	for _, ep := range eps {
		if h := healthByURL[ep]; h.Permanent {
			skipped = append(skipped, fmt.Sprintf("%s: %s", ep, h.Error))
			continue
		}
		available = append(available, ep)
	}
	if len(available) == 0 {
		return Response{}, "", fmt.Errorf("OSM 无可用节点（节点地址、证书或访问权限检查失败）: %s", strings.Join(skipped, "; "))
	}
	attemptLimit := min(c.options.MaxAttempts, len(available))
	var last error
	for i, ep := range available[:attemptLimit] {
		if err := ctx.Err(); err != nil {
			return Response{}, "", fmt.Errorf("OSM 查询已结束: %w", err)
		}
		requestCtx := withProgressMetadata(ctx, Progress{Endpoint: ep, Attempt: i + 1, MaxAttempts: attemptLimit})
		reportProgress(requestCtx, Progress{Phase: "waiting", Message: "等待 OSM 服务响应"})
		result, err := c.post(requestCtx, ep, q)
		if ctx.Err() != nil {
			return Response{}, "", fmt.Errorf("OSM 查询已结束: %w", ctx.Err())
		}
		if err == nil {
			state := c.state()
			state.mu.Lock()
			state.ranked = []string{ep}
			for _, other := range eps {
				if other != ep {
					state.ranked = append(state.ranked, other)
				}
			}
			state.mu.Unlock()
			return result, ep, nil
		}
		last = err
		var tooLarge *responseLimitError
		if errors.As(err, &tooLarge) {
			return Response{}, "", err
		}
		if rejectedRequestFailure(err) {
			return Response{}, "", fmt.Errorf("OSM 服务拒绝本次查询，请调整查询或缩小范围: %w", err)
		}
		if permanentEndpointFailure(err) {
			state := c.state()
			state.mu.Lock()
			for j := range state.health {
				if state.health[j].URL == ep {
					state.health[j].Available, state.health[j].Permanent = false, true
					state.health[j].Error = err.Error()
				}
			}
			state.mu.Unlock()
		}
		if i+1 < attemptLimit {
			reportProgress(requestCtx, Progress{Phase: "retrying", Message: err.Error()})
			if len(backoffs) > 0 {
				if err := pause(ctx, backoffs[min(i, len(backoffs)-1)]); err != nil {
					return Response{}, "", fmt.Errorf("OSM 查询在切换节点时结束: %w", err)
				}
			}
		}
	}
	// Failed ordering should not pin subsequent independent queries to stale
	// health results. This call still attempts each candidate only once.
	c.invalidateHealth()
	return Response{}, "", fmt.Errorf("OSM 查询未完成，已尝试 %d 个不同节点（最多 %d 个）: %w", attemptLimit, c.options.MaxAttempts, last)
}
func (c *client) full(ctx context.Context, r Ref) (Element, error) {
	path := fmt.Sprintf("%s/%s/%d/full.json", strings.TrimRight(c.options.OSMAPIURL, "/"), r.Type, r.ID)
	if r.Type == "node" {
		path = fmt.Sprintf("%s/node/%d.json", strings.TrimRight(c.options.OSMAPIURL, "/"), r.ID)
	}
	data, err := c.request(ctx, "GET", path, "", false)
	if err != nil {
		return Element{}, err
	}
	var response Response
	if err = json.Unmarshal(data, &response); err != nil {
		return Element{}, err
	}
	return HydrateFull(response.Elements, r)
}
