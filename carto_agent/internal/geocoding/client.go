// Package geocoding resolves extracted places against the TianDiTu search API.
// Provider coordinates and explicitly marked model estimates have separate provenance.
package geocoding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const DefaultEndpoint = "https://api.tianditu.gov.cn/v2/search"
const documentationURL = "https://lbs.tianditu.gov.cn/server/search2.html"
const candidateLimit = 20

// A shared single-token limiter spaces actual HTTP requests, including retries,
// across resolver instances. Concurrency alone permits bursts that exhaust the
// provider's per-key request allowance.
var providerRequests = rate.NewLimiter(rate.Every(250*time.Millisecond), 1)

// Candidate is an actual provider POI, never a model-generated coordinate.
type Candidate struct {
	CountyCode string  `json:"county_code,omitempty"`
	Name       string  `json:"name"`
	Address    string  `json:"address"`
	Lon        float64 `json:"lon"`
	Lat        float64 `json:"lat"`
	SourceID   string  `json:"source_id,omitempty"`
	TypeName   string  `json:"type_name,omitempty"`
}

type searchResult struct {
	Candidates []Candidate
	Truncated  bool
	AreaOnly   bool
}

type providerError struct{ code string }

type httpStatusError struct {
	code       int
	retryAfter time.Duration
}

func (e *httpStatusError) Error() string { return fmt.Sprintf("天地图 HTTP 状态 %d", e.code) }

func (e *providerError) Error() string {
	return "天地图服务返回错误（代码 " + e.code + "）"
}

func (r *Resolver) search(ctx context.Context, query, admin string) (searchResult, error) {
	result := searchResult{Candidates: []Candidate{}}
	post := map[string]any{"keyWord": query, "start": 0, "count": candidateLimit, "show": "2"}
	if admin != "" {
		post["queryType"], post["specify"] = 12, admin
	} else {
		post["queryType"], post["level"], post["mapBound"] = 7, 5, "73,18,135,54"
	}
	data, _ := json.Marshal(post)
	endpoint := r.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return result, errors.New("天地图服务地址无效")
	}
	q := u.Query()
	q.Set("postStr", string(data))
	q.Set("type", "query")
	q.Set("tk", r.Key)
	u.RawQuery = q.Encode()
	requestCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return result, errors.New("无法创建天地图请求")
	}
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}
	// A redirected URL could expose the server key to a different origin.
	safeClient := *client
	safeClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := safeClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if requestCtx.Err() != nil {
			return result, errors.New("天地图查询超时")
		}
		// net/http errors contain the request URL, including tk. Never return it.
		return result, errors.New("无法连接天地图地名服务")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result, &httpStatusError{code: resp.StatusCode, retryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil || len(body) > 2*1024*1024 {
		return result, errors.New("天地图响应读取失败或过大")
	}
	var raw struct {
		Code   json.RawMessage `json:"code"`
		Status struct {
			Code json.RawMessage `json:"infocode"`
		} `json:"status"`
		ResultType json.RawMessage  `json:"resultType"`
		Count      json.RawMessage  `json:"count"`
		POIs       []map[string]any `json:"pois"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return result, errors.New("天地图响应不是有效 JSON")
	}
	if code := scalar(raw.Code); code != "" && code != "0" && code != "1000" {
		return result, &providerError{safeCode(code)}
	}
	code := scalar(raw.Status.Code)
	if code == "3001" {
		return result, nil
	}
	if code != "1000" {
		return result, &providerError{safeCode(code)}
	}
	if scalar(raw.ResultType) == "3" {
		result.AreaOnly = true
		return result, nil
	}
	if scalar(raw.ResultType) != "1" {
		return result, nil
	}
	count, _ := strconv.Atoi(scalar(raw.Count))
	result.Truncated = count > len(raw.POIs) || len(raw.POIs) > candidateLimit
	seen := map[string]bool{}
	for i, poi := range raw.POIs {
		if i >= candidateLimit {
			break
		}
		parts := strings.Split(str(poi["lonlat"]), ",")
		if len(parts) != 2 {
			continue
		}
		lon, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		lat, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err1 != nil || err2 != nil || !validCoordinate(lon, lat) {
			continue
		}
		name := strings.TrimSpace(str(poi["name"]))
		if name == "" {
			continue
		}
		c := Candidate{Name: name, Address: str(poi["address"]), Lon: lon, Lat: lat, SourceID: str(poi["hotPointID"]), TypeName: str(poi["typeName"]), CountyCode: str(poi["countyCode"])}
		key := fmt.Sprintf("%s|%.8f|%.8f", c.Name, c.Lon, c.Lat)
		if !seen[key] {
			result.Candidates = append(result.Candidates, c)
			seen[key] = true
		}
	}
	return result, nil
}

func (r *Resolver) throttledSearch(ctx context.Context, query, admin string) (searchResult, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if err := providerRequests.Wait(ctx); err != nil {
			if ctx.Err() != nil {
				return searchResult{}, ctx.Err()
			}
			return searchResult{}, errors.New("天地图查询阶段剩余时间不足")
		}
		result, err := r.search(ctx, query, admin)
		var status *httpStatusError
		if attempt != 0 || !errors.As(err, &status) || status.code != http.StatusTooManyRequests {
			return result, err
		}
		delay := max(750*time.Millisecond, status.retryAfter)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return searchResult{}, ctx.Err()
		}
	}
	return searchResult{}, errors.New("天地图请求频率超限")
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		// Keep integer conversion bounded; large advertised waits are cancelled
		// by the enclosing 50-second search-stage deadline.
		if seconds > 3600 {
			seconds = 3600
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil && when.After(now) {
		return when.Sub(now)
	}
	return 0
}

func scalar(raw json.RawMessage) string { return strings.Trim(strings.TrimSpace(string(raw)), "\"") }
func safeCode(code string) string {
	if _, err := strconv.Atoi(code); err == nil && len(code) <= 9 {
		return code
	}
	return "未知"
}
func str(value any) string { s, _ := value.(string); return s }
func validCoordinate(lon, lat float64) bool {
	return !math.IsNaN(lon) && !math.IsNaN(lat) && !math.IsInf(lon, 0) && !math.IsInf(lat, 0) && lon >= -180 && lon <= 180 && lat >= -90 && lat <= 90
}

// SearchCandidates exposes the existing bounded, redacted and rate-limited provider
// search to deterministic acquisition; it never invokes a language model.
func (r *Resolver) SearchCandidates(ctx context.Context, query, admin string) ([]Candidate, error) {
	if strings.TrimSpace(r.Key) == "" {
		return nil, errors.New("未配置天地图服务器端 Key")
	}
	result, err := r.throttledSearch(ctx, query, admin)
	return result.Candidates, err
}
