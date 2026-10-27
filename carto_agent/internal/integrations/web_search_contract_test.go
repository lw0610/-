package integrations

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
)

func TestWebSearchContractAndRuntimeGate(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	registry := toolpkg.NewRegistry()
	if err := registry.LoadDirectories(filepath.Join(root, "autocarto", "tools", "system", "browser-data-acquisition")); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get("web_search"); !ok {
		t.Fatal("web_search YAML was not loaded")
	}
	if !Handles("web_search") || IntegrationFor("web_search") != "web" {
		t.Fatal("web_search is not dispatched to the web integration")
	}
	if PackageReady([]string{"web_search"}, map[string]bool{"browser": true}) || !PackageReady([]string{"web_search"}, map[string]bool{"web": true}) {
		t.Fatal("web search availability does not use its own integration switch")
	}
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	ctx := skill.WithSessionContext(context.Background(), skill.SessionContext{SessionID: "search-test", ProjectPath: filepath.Join(t.TempDir(), "project.qgz")})
	_, err := Invoke(ctx, "web_search", map[string]any{"query": "test"}, fixedConfig{})
	if err == nil || !strings.Contains(err.Error(), "未启用") {
		t.Fatalf("disabled search reached network or wrong runtime: %v", err)
	}
}
