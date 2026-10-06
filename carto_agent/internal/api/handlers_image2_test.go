package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/mux"

	"github.com/autocarto/carto_agent/internal/config"
)

type image2RoundTripFunc func(*http.Request) (*http.Response, error)

func (fn image2RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func newImage2APITestRouter(t *testing.T, baseURL string) (*mux.Router, *config.ConfigService, string) {
	t.Helper()

	tmpDir := t.TempDir()
	defaultPath := filepath.Join(tmpDir, "default.yaml")
	userPath := filepath.Join(tmpDir, "user.yaml")
	if err := os.WriteFile(defaultPath, []byte("image2:\n  enabled: true\n  base_url: https://api.openai.com/v1\n"), 0o644); err != nil {
		t.Fatalf("write default config failed: %v", err)
	}
	enabled := true
	configService := config.NewConfigService(nil, &config.Config{
		Image2: config.Image2Config{
			Enabled: &enabled,
			BaseURL: baseURL,
		},
	}, defaultPath, userPath)
	handler := NewAPI(configService, nil, nil, nil, "", nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)
	return router, configService, userPath
}

func performImage2APIRequest(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AutoCarto-Contract-Version", "v1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestImage2ConnectionUsesCurrentOrSavedBaseURLWithoutGeneratingImage(t *testing.T) {
	tests := []struct {
		name         string
		savedBaseURL string
		requestBody  string
		wantURL      string
	}{
		{
			name:         "current frontend input wins",
			savedBaseURL: "https://saved-image2.example.com/v1",
			requestBody:  `{"base_url":" https://current-image2.example.com/openai/v1/ ","api_key":"sk-image2-test"}`,
			wantURL:      "https://current-image2.example.com/openai/v1/models",
		},
		{
			name:         "saved URL is fallback",
			savedBaseURL: "https://saved-image2.example.com/v1/",
			requestBody:  `{"api_key":"sk-image2-test"}`,
			wantURL:      "https://saved-image2.example.com/v1/models",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router, _, _ := newImage2APITestRouter(t, test.savedBaseURL)

			previousClient := image2HTTPClient
			t.Cleanup(func() { image2HTTPClient = previousClient })
			var receivedURL string
			var authorization string
			image2HTTPClient = &http.Client{Transport: image2RoundTripFunc(func(req *http.Request) (*http.Response, error) {
				receivedURL = req.URL.String()
				authorization = req.Header.Get("Authorization")
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(`{"id":"gpt-image-2"}`)),
					Request:    req,
				}, nil
			})}

			recorder := performImage2APIRequest(
				router,
				http.MethodPost,
				"/api/image2/test",
				test.requestBody,
			)

			if recorder.Code != http.StatusOK {
				t.Fatalf("unexpected status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if receivedURL != test.wantURL {
				t.Fatalf("connection test used %q, want %q", receivedURL, test.wantURL)
			}
			if authorization != "Bearer sk-image2-test" {
				t.Fatalf("unexpected authorization header %q", authorization)
			}
		})
	}
}

func TestImage2ConfigPUTPersistsBaseURLAndGETReturnsIt(t *testing.T) {
	router, _, userPath := newImage2APITestRouter(t, config.Image2DefaultBaseURL)
	wantBaseURL := "https://image2.example.com/openai/v1"

	putRecorder := performImage2APIRequest(
		router,
		http.MethodPut,
		"/api/image2/config",
		`{"enabled":true,"base_url":" https://image2.example.com/openai/v1/ ","api_key":""}`,
	)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected PUT status=%d body=%s", putRecorder.Code, putRecorder.Body.String())
	}

	persisted, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatalf("read persisted config failed: %v", err)
	}
	if !bytes.Contains(persisted, []byte("base_url: "+wantBaseURL)) {
		t.Fatalf("persisted config does not contain normalized Image 2 base_url:\n%s", persisted)
	}

	getRecorder := performImage2APIRequest(router, http.MethodGet, "/api/image2/config", "")
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected GET status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}
	var response struct {
		Success bool                    `json:"success"`
		Data    config.Image2ConfigView `json:"data"`
	}
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode GET response failed: %v", err)
	}
	if !response.Success || response.Data.BaseURL != wantBaseURL {
		t.Fatalf("GET did not return persisted Image 2 base_url: %#v", response)
	}
	if response.Data.Model != config.Image2ModelName {
		t.Fatalf("Image 2 model must remain fixed, got %q", response.Data.Model)
	}
}

func TestImage2HandlersRejectInvalidBaseURL(t *testing.T) {
	router, configService, _ := newImage2APITestRouter(t, config.Image2DefaultBaseURL)
	previousClient := image2HTTPClient
	t.Cleanup(func() { image2HTTPClient = previousClient })
	transportCalled := false
	image2HTTPClient = &http.Client{Transport: image2RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		transportCalled = true
		return nil, nil
	})}

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "save",
			method: http.MethodPut,
			path:   "/api/image2/config",
			body:   `{"enabled":true,"base_url":"file:///tmp/image2","api_key":""}`,
		},
		{
			name:   "connection test",
			method: http.MethodPost,
			path:   "/api/image2/test",
			body:   `{"base_url":"file:///tmp/image2","api_key":"sk-image2-test"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := performImage2APIRequest(router, test.method, test.path, test.body)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("unexpected status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
	if transportCalled {
		t.Fatal("invalid Image 2 base_url must be rejected before an outbound request")
	}
	view, err := configService.GetImage2Config()
	if err != nil {
		t.Fatalf("GetImage2Config failed: %v", err)
	}
	if view.BaseURL != config.Image2DefaultBaseURL {
		t.Fatalf("invalid PUT changed Image 2 base_url to %q", view.BaseURL)
	}
}
