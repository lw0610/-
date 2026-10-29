package acquire

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/networkproxy"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type HTTPAdapter struct {
	Client   *http.Client
	MaxBytes int64
	ProxyURL string
}

func (HTTPAdapter) Kind() string { return "http_file" }
func RedactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "[无效地址]"
	}
	parsed.User = nil
	query := parsed.Query()
	for key := range query {
		switch strings.ToLower(key) {
		case "tk", "key", "token", "access_token", "api_key", "apikey", "secret", "signature", "sig", "authorization":
			query.Set(key, "[REDACTED]")
		}
	}
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String()
}
func (a HTTPAdapter) Fetch(ctx context.Context, r FetchRequest) (FetchResult, error) {
	if r.Source == nil || r.Provide == nil {
		return FetchResult{}, fmt.Errorf("下载来源不完整")
	}
	u, err := url.Parse(r.Provide.URL)
	if err != nil || !datasource.HostAllowed(u, r.Source.Hosts) {
		return FetchResult{}, fmt.Errorf("下载地址未获数据源主机白名单授权")
	}
	max := a.MaxBytes
	if max <= 0 {
		max = 2048 * 1024 * 1024
	}
	if limit := int64(r.Source.Limits.MaxMB) * 1024 * 1024; limit > 0 && limit < max {
		max = limit
	}
	timeout := r.Source.Limits.TimeoutSeconds
	if timeout <= 0 {
		timeout = 900
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	client := http.Client{}
	if a.Client != nil {
		client = *a.Client
	} else {
		proxy := ""
		if r.Source.RoutingMethod() == "osm_extract" {
			proxy = a.ProxyURL
		}
		transport := networkproxy.Transport(proxy)
		defer transport.CloseIdleConnections()
		client.Transport = transport
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("重定向次数超限")
		}
		if !datasource.HostAllowed(req.URL, r.Source.Hosts) {
			return fmt.Errorf("重定向主机未获授权")
		}
		return nil
	}
	var response *http.Response
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return FetchResult{}, fmt.Errorf("下载请求无效")
		}
		req.Header.Set("User-Agent", "AutoCarto/4.0 data acquisition")
		response, err = client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return FetchResult{}, ctx.Err()
			}
			var requestErr *url.Error
			if errors.As(err, &requestErr) {
				err = &url.Error{Op: requestErr.Op, URL: RedactURL(requestErr.URL), Err: requestErr.Err}
			}
			return FetchResult{}, fmt.Errorf("无法下载 %s：%w", RedactURL(u.String()), err)
		}
		if response.StatusCode != 429 && response.StatusCode != 503 {
			break
		}
		wait := time.Duration(attempt+1) * time.Second
		if n, _ := strconv.Atoi(response.Header.Get("Retry-After")); n > 0 && n <= 60 {
			wait = time.Duration(n) * time.Second
		}
		response.Body.Close()
		if attempt == 2 {
			return FetchResult{}, fmt.Errorf("数据源暂时限流，请稍后重试")
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return FetchResult{}, ctx.Err()
		case <-timer.C:
		}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return FetchResult{}, fmt.Errorf("数据源返回 HTTP %d", response.StatusCode)
	}
	if response.ContentLength > max {
		return FetchResult{}, fmt.Errorf("文件超过下载上限 %d MB", max/1024/1024)
	}
	if err = os.MkdirAll(r.WorkDir, 0700); err != nil {
		return FetchResult{}, err
	}
	filename := filepath.Base(u.Path)
	if filename == "." || filename == "/" || filename == "" {
		filename = "download.bin"
	}
	output := filepath.Join(r.WorkDir, filename)
	if _, err = os.Lstat(output); !os.IsNotExist(err) {
		return FetchResult{}, fmt.Errorf("下载目标已存在")
	}
	file, err := os.CreateTemp(r.WorkDir, ".download-")
	if err != nil {
		return FetchResult{}, err
	}
	temp := file.Name()
	defer os.Remove(temp)
	digest := sha256.New()
	writer := io.MultiWriter(file, digest)
	var total int64
	var lastProgress time.Time
	buffer := make([]byte, 128*1024)
	reader := io.LimitReader(response.Body, max+1)
	for {
		if err = ctx.Err(); err != nil {
			break
		}
		var n int
		n, err = reader.Read(buffer)
		if n > 0 {
			total += int64(n)
			if total > max {
				err = fmt.Errorf("文件超过下载上限")
				break
			}
			if _, writeErr := writer.Write(buffer[:n]); writeErr != nil {
				err = writeErr
				break
			}
			if time.Since(lastProgress) >= 250*time.Millisecond {
				emitProgress(r.Progress, "download", total, response.ContentLength)
				lastProgress = time.Now()
			}
		}
		if err != nil {
			break
		}
	}
	if err == io.EOF {
		err = nil
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return FetchResult{}, err
	}
	if response.ContentLength >= 0 && total != response.ContentLength {
		return FetchResult{}, fmt.Errorf("下载文件不完整")
	}
	if err = os.Rename(temp, output); err != nil {
		return FetchResult{}, err
	}
	emitProgress(r.Progress, "download", total, response.ContentLength)
	return FetchResult{Files: []string{output}, Format: fileFormat(output), URLs: []string{RedactURL(response.Request.URL.String())}, RetrievedAt: time.Now().UTC(), Bytes: total, SHA256: map[string]string{output: hex.EncodeToString(digest.Sum(nil))}, Notes: []string{}}, nil
}
