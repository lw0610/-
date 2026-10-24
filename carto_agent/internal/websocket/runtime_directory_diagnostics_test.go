package websocket

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
)

func TestRuntimeDirectoryDiagnosticsSurviveWebSocketRefresh(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("AUTOCARTO_HOME", home)
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_DISABLE_KEYRING", "1")
	cfg := &config.Config{DataDir: filepath.Join(root, "old/data"), Log: config.LogConfig{Dir: filepath.Join(root, "logs")}}
	server := NewDeepServer(0, nil)
	server.configService = config.NewConfigService(nil, cfg, "", filepath.Join(home, "config/config.yaml"))
	serverConn, clientConn, cleanup := newTestWebSocketPair(t)
	defer cleanup()
	server.clients[serverConn] = true
	server.connWriteMu[serverConn] = &sync.Mutex{}
	server.broadcastRuntimeSnapshot(middlewareclient.RuntimeSnapshot{Version: 1})
	server.sendCurrentRuntimeSnapshot(serverConn)
	count := 0
	for _, message := range readMessagesUntilTimeout(t, clientConn) {
		if message.Type != MsgTypeRuntimeSnapshot {
			continue
		}
		count++
		raw, _ := json.Marshal(message.Data["runtime_paths"])
		var paths config.RuntimeDirectoryDiagnostics
		if err := json.Unmarshal(raw, &paths); err != nil {
			t.Fatal(err)
		}
		if paths.RuntimeHome != home || paths.LogFile != filepath.Join(root, "logs/agent.log") || len(paths.Warnings) != 1 {
			t.Fatalf("runtime path diagnostics lost in websocket refresh: %+v", paths)
		}
	}
	if count != 2 {
		t.Fatalf("expected initial and broadcast snapshots, got %d", count)
	}
}
