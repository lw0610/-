package websocket

import (
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/runtimepersist"
)

func TestSetAgentRebuildConfigConfiguresPersistentCheckpointStore(t *testing.T) {
	server := NewDeepServer(0, nil)
	pluginRoot := filepath.Join(t.TempDir(), "autocarto")
	baseDir := filepath.Join(pluginRoot, "config")

	server.SetAgentRebuildConfig(&AgentRebuildConfig{
		BaseConfig: &config.Config{
			ConfigBaseDir: baseDir,
		},
	})

	want := filepath.Join(pluginRoot, "data", "runtime", "checkpoint_store.db")
	if server.checkpointDBPath != runtimepersist.ResolveCheckpointStorePath(baseDir) {
		t.Fatalf("unexpected checkpoint db path helper mismatch: %s", server.checkpointDBPath)
	}
	if server.checkpointDBPath != want {
		t.Fatalf("unexpected checkpoint db path: got=%s want=%s", server.checkpointDBPath, want)
	}
	if _, ok := server.checkpointStore.(*runtimepersist.SQLiteCheckpointStore); !ok {
		t.Fatalf("expected sqlite checkpoint store, got %T", server.checkpointStore)
	}
}
