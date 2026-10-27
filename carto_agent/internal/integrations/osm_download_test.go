package integrations

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/osmdata"
	"github.com/autocarto/carto_agent/internal/skill"
)

func TestEnabledOSMDownloadsInNewSessionsWithoutConfirmation(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	var calls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"elements":[{"type":"node","id":1,"lat":29,"lon":113,"tags":{"waterway":"river"}}]}`)
	}))
	defer endpoint.Close()
	settings := config.IntegrationsConfig{OSM: config.OSMConfig{
		Enabled: true, Endpoints: []string{endpoint.URL}, TimeoutSeconds: 2, TotalTimeoutSeconds: 10, MaxAttempts: 1,
	}}
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"osm_get_features", map[string]any{"extents": []string{"bbox:28,112,30,114"}, "tags": []string{"waterway=river"}, "output": "river.geojson"}},
		{"osm_get_objects", map[string]any{"ids": []string{"N1"}, "output": "object.geojson"}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			if !Handles(tc.tool) {
				t.Fatalf("%s not dispatched", tc.tool)
			}
			ctx := skill.WithSessionContext(context.Background(), skill.SessionContext{
				SessionID:   "new-" + strings.ReplaceAll(tc.tool, "_", "-"),
				ProjectPath: filepath.Join(t.TempDir(), "project.qgz"),
			})
			value, err := Invoke(ctx, tc.tool, tc.args, fixedConfig{settings})
			if err != nil {
				t.Fatalf("enabled new-session download interrupted or failed: %v", err)
			}
			result, ok := value.(*osmdata.Result)
			if !ok || result == nil || !result.Success || result.FeatureCount != 1 || len(result.ArtifactPaths) == 0 {
				t.Fatalf("download did not complete directly: %+v", value)
			}
			for _, path := range result.ArtifactPaths {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("missing download artifact: %v", err)
				}
			}

			// A completed download never grants permission after the setting is
			// disabled. This must fail before contacting an endpoint.
			disabled := settings
			disabled.OSM.Enabled = false
			before := calls.Load()
			if _, err := Invoke(ctx, tc.tool, tc.args, fixedConfig{disabled}); err == nil || !strings.Contains(err.Error(), "启用 OSM") {
				t.Fatalf("disabled download was not rejected: %v", err)
			}
			if calls.Load() != before {
				t.Fatal("disabled download contacted OSM")
			}
		})
	}
}
