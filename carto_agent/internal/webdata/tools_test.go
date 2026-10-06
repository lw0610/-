package webdata

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixtureClient(body, contentType string, status int) *http.Client {
	return &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{contentType}}, Request: r, ContentLength: int64(len(body))}, nil
	})}
}

func TestPublicAddressPolicyAndRedirect(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.169.254", "::1", "::ffff:127.0.0.1", "fc00::1", "100.64.0.1", "0.0.0.0", "198.18.0.1"} {
		if publicIP(netip.MustParseAddr(ip)) {
			t.Errorf("accepted %s", ip)
		}
	}
	if !publicIP(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public address rejected")
	}
	for _, u := range []string{"file:///etc/passwd", "http://localhost/test", "http://127.0.0.1/", "http://user:pass@example.org/", "ftp://example.org/"} {
		if _, err := publicURL(u); err == nil {
			t.Errorf("accepted %s", u)
		}
	}
	r, _ := http.NewRequest("GET", "http://127.0.0.1/", nil)
	if err := publicClient().CheckRedirect(r, nil); err == nil {
		t.Fatal("private redirect accepted")
	}
}

func TestPageReadExtractsTextAndLinks(t *testing.T) {
	result, err := invoke(context.Background(), "web_read_page", map[string]any{"url": "https://example.org/data/"}, Options{Enabled: true}, fixtureClient(`<html><head><script>secretCode()</script></head><body><h1>Population</h1><a href="2025.csv">Download CSV</a></body></html>`, "text/html", 200))
	if err != nil {
		t.Fatal(err)
	}
	r := result.(map[string]any)
	if strings.Contains(r["text"].(string), "secretCode") || !strings.Contains(r["text"].(string), "Population") {
		t.Fatalf("unexpected text %s", r["text"])
	}
	links := r["links"].([]map[string]string)
	if len(links) != 1 || links[0]["url"] != "https://example.org/data/2025.csv" {
		t.Fatalf("links %+v", links)
	}
}

func TestPageReadDecodesChineseLegacyCharset(t *testing.T) {
	result, err := invoke(context.Background(), "web_read_page", map[string]any{"url": "https://example.org"}, Options{Enabled: true}, fixtureClient("<html><body>\xd6\xd0\xce\xc4</body></html>", "text/html; charset=gbk", 200))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.(map[string]any)["text"].(string), "中文") {
		t.Fatalf("wrong charset: %#v", result)
	}
}

func TestDownloadEvidenceAndNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	opts := Options{Enabled: true, OutputDir: dir, MaxDownloadMB: 1}
	args := map[string]any{"url": "https://example.org/table.csv", "filename": "population.csv"}
	var previous string
	for i := 0; i < 2; i++ {
		result, err := invoke(context.Background(), "web_download_file", args, opts, fixtureClient("region,population\nA,42\n", "text/csv", 200))
		if err != nil {
			t.Fatal(err)
		}
		r := result.(map[string]any)
		target := r["path"].(string)
		if target == previous {
			t.Fatal("download overwrote previous file")
		}
		previous = target
		if !strings.HasPrefix(target, dir+string(filepath.Separator)) {
			t.Fatal("escaped output directory")
		}
		if _, err := os.Stat(r["manifest_path"].(string)); err != nil {
			t.Fatal(err)
		}
		if len(r["sha256"].(string)) != 64 || r["source_url"] != args["url"] {
			t.Fatalf("missing provenance: %+v", r)
		}
	}
}

func TestDownloadRejectsLimitsPathsAndDisabled(t *testing.T) {
	opts := Options{Enabled: true, OutputDir: t.TempDir(), MaxDownloadMB: 1}
	for _, filename := range []string{"../escape.csv", "a\\b.csv", "C:bad.csv"} {
		_, err := invoke(context.Background(), "web_download_file", map[string]any{"url": "https://example.org/data", "filename": filename}, opts, fixtureClient("x", "text/plain", 200))
		if err == nil {
			t.Errorf("accepted path %s", filename)
		}
	}
	_, err := invoke(context.Background(), "web_download_file", map[string]any{"url": "https://example.org/data"}, opts, fixtureClient(strings.Repeat("x", (1<<20)+1), "text/plain", 200))
	if err == nil {
		t.Fatal("accepted oversized body")
	}
	_, err = invoke(context.Background(), "web_read_page", map[string]any{"url": "https://example.org"}, Options{}, fixtureClient("x", "text/plain", 200))
	if err == nil {
		t.Fatal("disabled tool executed")
	}
	_, err = invoke(context.Background(), "web_read_page", map[string]any{"url": "https://example.org"}, opts, fixtureClient("Access denied", "text/plain", 403))
	if err == nil {
		t.Fatal("HTTP failure treated as content")
	}
}
