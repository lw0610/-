package websocket

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/agentruntime"
	"github.com/autocarto/carto_agent/internal/config"
)

type fakeBuilder struct{}

func (fakeBuilder) Build(context.Context, string, *config.AgentConfigFile) (*agentruntime.Runtime, error) {
	return &agentruntime.Runtime{}, nil
}

func (fakeBuilder) BuildActive(context.Context, *config.AgentConfigManager) (*agentruntime.Runtime, error) {
	return &agentruntime.Runtime{}, nil
}

func TestUpdateAgentToolsInvalidatesRuntimeFactoryBeforeRebuild(t *testing.T) {
	server := NewDeepServer(0, nil)
	server.agentRebuildConfig = &AgentRebuildConfig{}
	server.runtimeFactory = fakeBuilder{}

	newTools := []tool.BaseTool{nil}
	err := server.UpdateAgentTools(newTools)
	if err == nil {
		t.Fatal("expected rebuild error when managers are missing")
	}
	if !strings.Contains(err.Error(), "agentConfigManager 未设置") {
		t.Fatalf("unexpected error: %v", err)
	}
	if server.runtimeFactory != nil {
		t.Fatal("expected runtimeFactory to be invalidated before rebuild")
	}
	if len(server.agentRebuildConfig.Tools) != len(newTools) {
		t.Fatalf("expected %d tools, got %d", len(newTools), len(server.agentRebuildConfig.Tools))
	}
}

func TestBuildRuntimeFactoryUsesPersistedPluginSkillsDir(t *testing.T) {
	provider, _, wsURL, cleanup := setupLocalBridgeWebsocketServer(t)
	defer cleanup()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"?role=plugin", nil)
	if err != nil {
		t.Fatalf("dial plugin websocket failed: %v", err)
	}
	defer conn.Close()

	sendLocalBridgeWSMessage(t, conn, localBridgeWSMessage{
		Type: "instance_connect",
		Data: map[string]any{
			"instance_id":  "inst-build-factory",
			"pid":          2006,
			"qgis_version": "3.40",
			"project_path": "/tmp/build-factory.qgz",
			"skills_dir":   "/tmp/live-plugin/skills",
			"tools":        []map[string]any{{"name": "open_project", "inputSchema": map[string]any{"type": "object"}}},
		},
	})

	waitForLocalBridgeCondition(t, func() bool {
		return provider.HasInstanceConnected("inst-build-factory")
	})

	server := NewDeepServer(0, nil)
	server.SetLocalBridgeProvider(provider)
	server.SetAgentRebuildConfig(&AgentRebuildConfig{
		ChatModel:       &fakeContextCompressionModel{summary: "ok"},
		PluginSkillsDir: "/tmp/persisted-plugin/skills",
		BaseConfig:      &config.Config{},
	})

	factory, err := server.buildRuntimeFactory()
	if err != nil {
		t.Fatalf("buildRuntimeFactory error: %v", err)
	}

	value := reflect.ValueOf(factory)
	if value.Kind() != reflect.Pointer {
		t.Fatalf("unexpected builder type: %T", factory)
	}
	pluginSkillsDir := value.Elem().FieldByName("config").FieldByName("PluginSkillsDir").String()
	if pluginSkillsDir != "/tmp/persisted-plugin/skills" {
		t.Fatalf("expected persisted plugin skills dir, got %q", pluginSkillsDir)
	}
}

func TestAgentRebuildConfigConcurrentAccessRaceFree(t *testing.T) {
	server := NewDeepServer(0, nil)
	server.SetAgentRebuildConfig(&AgentRebuildConfig{
		ChatModel:       &fakeContextCompressionModel{summary: "ok"},
		PluginSkillsDir: "/tmp/initial-plugin/skills",
		BaseConfig:      &config.Config{},
	})

	const iterations = 200
	var wg sync.WaitGroup
	for writerID := 0; writerID < 2; writerID++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				server.SetAgentRebuildConfig(&AgentRebuildConfig{
					ChatModel:       &fakeContextCompressionModel{summary: fmt.Sprintf("writer-%d-%d", writerID, i)},
					PluginSkillsDir: fmt.Sprintf("/tmp/plugin-%d-%d/skills", writerID, i),
					BaseConfig:      &config.Config{},
				})
			}
		}(writerID)
	}

	for readerID := 0; readerID < 4; readerID++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = server.currentPluginSkillsDir()
				_, _ = server.resolveContextCompressionModel()
				_, _ = server.buildRuntimeFactory()
			}
		}()
	}

	wg.Wait()
	if got := server.currentPluginSkillsDir(); !strings.HasSuffix(got, "/skills") {
		t.Fatalf("expected plugin skills dir suffix, got %q", got)
	}
}
