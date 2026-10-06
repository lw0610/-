package websocket

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/discovery"
	bridgeprovider "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/protocol"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/gorilla/websocket"
)

func TestSharedSkillCatalogDoesNotExposeBridgeDynamicToolsAsPackage(t *testing.T) {
	registry := discovery.NewInstanceRegistry()
	defer registry.Close()

	provider := bridgeprovider.NewProvider(discovery.ServiceNameQGISBridge, registry)
	bridgeDynamicRegistry := skill.NewBridgeDynamicRegistry()
	bridgeDynamicRegistry.RegisterInstanceTools("instance-a", []bridgeprovider.ToolDefinition{{
		Name:        "flash_feature",
		Description: "flash feature",
		Category:    "vector",
		InputSchema: map[string]any{"type": "object"},
	}})
	provider.SetDynamicToolRegistry(bridgeDynamicRegistry)

	server := NewDeepServer(0, nil)
	server.SetLocalBridgeProvider(provider)
	server.SetSkillsDir(t.TempDir())

	store := server.sharedSkillCatalog()
	if store == nil {
		t.Fatal("expected shared skill catalog")
	}
	packages, err := store.ListSkillPackages()
	if err != nil {
		t.Fatalf("ListSkillPackages error: %v", err)
	}
	for _, pkg := range packages {
		if pkg.PackageID == "_bridge-dynamic" {
			t.Fatalf("expected no virtual bridge package in shared skill catalog, got %#v", packages)
		}
	}
	if _, err := store.GetSkillPackage("_bridge-dynamic"); err == nil {
		t.Fatal("expected _bridge-dynamic lookup to fail once virtual package is removed")
	}
}

func TestSharedSkillCatalogExposesPluginToolRefsWithoutHydratedTools(t *testing.T) {
	pluginRoot := t.TempDir()
	pluginSkillsDir := filepath.Join(pluginRoot, "skills")
	writeWebsocketRuntimeSkillPackage(t, pluginSkillsDir, "plugin-package", "plugin_tool")
	pluginToolsDir := settings.InferSiblingToolsDirFromSkillsDir(pluginSkillsDir)

	server := NewDeepServer(0, nil)
	server.SetSkillsDir(t.TempDir())
	server.SetAgentRebuildConfig(&AgentRebuildConfig{
		PluginSkillsDir: pluginSkillsDir,
		RegistryGovernance: control.NewRegistryGovernance(control.RegistryGovernanceDeps{
			ResolveToolDirs:  func() []string { return []string{pluginToolsDir} },
			ResolveSkillDirs: func() []string { return []string{pluginSkillsDir} },
		}),
	})

	store := server.sharedSkillCatalog()
	if store == nil {
		t.Fatal("expected shared skill catalog")
	}
	detail, err := store.GetSkillPackage("plugin-package")
	if err != nil {
		t.Fatalf("GetSkillPackage error: %v", err)
	}
	if len(detail.ToolRefs) != 1 || detail.ToolRefs[0] != "plugin_tool" {
		t.Fatalf("unexpected tool refs: %#v", detail.ToolRefs)
	}
	if len(detail.Tools) != 0 {
		t.Fatalf("expected shared skill catalog detail to hide hydrated tools, got %#v", detail.Tools)
	}
}

func TestSharedSkillCatalogReturnsWithoutDeadlock(t *testing.T) {
	server := NewDeepServer(0, nil)
	server.SetSkillsDir(t.TempDir())

	done := make(chan control.SkillCatalogStore, 1)
	go func() {
		done <- server.sharedSkillCatalog()
	}()

	select {
	case store := <-done:
		if store == nil {
			t.Fatal("expected shared skill catalog")
		}
	case <-time.After(time.Second):
		t.Fatal("sharedSkillCatalog timed out; possible DeepServer.mu deadlock")
	}
}

func TestSharedSkillCatalogReadyDescriptorMatchesPackagedSmokeContract(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.April, 9, 10, 0, 0, 0, time.UTC)
	descriptor, err := control.BuildDiscoveryDescriptor(middlewareclient.RuntimeSnapshot{
		Version:                  11,
		Timestamp:                now.Add(-5 * time.Second),
		RuntimeRevision:          "runtime-rev-11",
		ToolCatalogRevision:      "catalog-rev-11",
		ToolSchemaRevision:       "schema-rev-11",
		ToolAvailabilityRevision: "availability-rev-11",
		ServicePhase:             "ready",
		ActiveConfig:             "default",
		Instances: middlewareclient.InstancesSnapshot{
			ActiveID:      "qgis-1",
			InstanceCount: 1,
			RunningCount:  1,
		},
	}, discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        50321,
		Protocol:    "websocket",
		Status:      "running",
	}, now, 30*time.Second)
	if err != nil {
		t.Fatalf("BuildDiscoveryDescriptor returned error: %v", err)
	}
	if descriptor.Version != protocol.ContractVersionV1 {
		t.Fatalf("unexpected descriptor version: %q", descriptor.Version)
	}
	if descriptor.Readiness != control.DiscoveryReadinessReady {
		t.Fatalf("unexpected descriptor readiness: %q", descriptor.Readiness)
	}
	if descriptor.Degraded {
		t.Fatal("expected ready descriptor degraded=false")
	}
	if descriptor.BackendURL != "http://127.0.0.1:50321" {
		t.Fatalf("unexpected backend_url: %q", descriptor.BackendURL)
	}
	if descriptor.WSURL != "ws://127.0.0.1:50321/ws" {
		t.Fatalf("unexpected ws_url: %q", descriptor.WSURL)
	}
	if descriptor.RuntimeRevision != "runtime-rev-11" {
		t.Fatalf("unexpected runtime revision: %q", descriptor.RuntimeRevision)
	}
	if descriptor.ActiveInstanceID != "qgis-1" {
		t.Fatalf("unexpected active instance id: %q", descriptor.ActiveInstanceID)
	}
}

func TestSharedSkillCatalogListsBuiltinAndPluginPackagesWhenPluginConnected(t *testing.T) {
	provider, wsServer, wsURL, cleanup := setupLocalBridgeWebsocketServer(t)
	defer cleanup()
	provider.SetDynamicToolRegistry(skill.NewBridgeDynamicRegistry())

	builtinRoot := filepath.Join(t.TempDir(), "builtin")
	userRoot := filepath.Join(t.TempDir(), "user")
	pluginRoot := t.TempDir()
	pluginSkillsDir := filepath.Join(pluginRoot, "skills")
	writeWebsocketRuntimeSkillPackage(t, builtinRoot, "builtin-package", "builtin_tool")
	writeWebsocketRuntimeSkillPackage(t, pluginSkillsDir, "plugin-package", "plugin_tool")

	wsServer.SetSkillDirs(builtinRoot, userRoot)
	wsServer.SetAgentRebuildConfig(&AgentRebuildConfig{
		RegistryGovernance: control.NewRegistryGovernance(control.RegistryGovernanceDeps{
			ResolveToolDirs: func() []string {
				return config.ResolveRuntimeToolDirsWithPlugin(
					settings.InferSiblingToolsDirFromSkillsDir(wsServer.currentPluginSkillsDir()),
					settings.InferSiblingToolsDirFromSkillsDir(builtinRoot),
					settings.InferUserToolsDirFromSkillsDir(userRoot),
					"",
					"",
				)
			},
			ResolveSkillDirs: wsServer.currentRuntimeSkillDirs,
		}),
	})

	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"?role=plugin", nil)
	if err != nil {
		t.Fatalf("dial plugin websocket failed: %v", err)
	}
	defer conn.Close()

	sendLocalBridgeWSMessage(t, conn, localBridgeWSMessage{
		Type: "instance_connect",
		Data: map[string]any{
			"instance_id":  "inst-shared-skill-catalog",
			"pid":          3010,
			"qgis_version": "3.40",
			"project_path": "/tmp/shared-skill-catalog.qgz",
			"skills_dir":   pluginSkillsDir,
			"tools":        []map[string]any{{"name": "open_project", "inputSchema": map[string]any{"type": "object"}}},
		},
	})

	waitForLocalBridgeCondition(t, func() bool {
		return provider.HasInstanceConnected("inst-shared-skill-catalog")
	})

	store := wsServer.sharedSkillCatalog()
	if store == nil {
		t.Fatal("expected shared skill catalog")
	}
	packages, err := store.ListSkillPackages()
	if err != nil {
		t.Fatalf("ListSkillPackages error: %v", err)
	}

	seen := make(map[string]bool, len(packages))
	for _, pkg := range packages {
		seen[pkg.PackageID] = true
	}
	for _, packageID := range []string{"builtin-package", "plugin-package"} {
		if !seen[packageID] {
			t.Fatalf("expected package %s after plugin connect, got %#v", packageID, packages)
		}
	}
	if seen["_bridge-dynamic"] {
		t.Fatalf("expected no _bridge-dynamic package after plugin connect, got %#v", packages)
	}
}

func TestCurrentBridgeDynamicRegistryConcurrentAccessRaceFree(t *testing.T) {
	registry := discovery.NewInstanceRegistry()
	defer registry.Close()

	providerA := bridgeprovider.NewProvider(discovery.ServiceNameQGISBridge, registry)
	providerA.SetDynamicToolRegistry(skill.NewBridgeDynamicRegistry())
	providerB := bridgeprovider.NewProvider(discovery.ServiceNameQGISBridge, registry)
	providerB.SetDynamicToolRegistry(skill.NewBridgeDynamicRegistry())

	server := NewDeepServer(0, nil)
	server.SetLocalBridgeProvider(providerA)

	const iterations = 500
	var wg sync.WaitGroup
	errCh := make(chan error, 4)

	writer := func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if i%2 == 0 {
				server.SetLocalBridgeProvider(providerA)
			} else {
				server.SetLocalBridgeProvider(providerB)
			}
		}
	}

	reader := func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if server.currentBridgeDynamicRegistry() == nil {
				select {
				case errCh <- fmt.Errorf("expected bridge dynamic registry"):
				default:
				}
				return
			}
		}
	}

	wg.Add(1)
	go writer()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go reader()
	}
	wg.Wait()
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
}

func writeWebsocketRuntimeSkillPackage(t *testing.T, root string, packageName string, toolName string) {
	t.Helper()
	packageDir := filepath.Join(root, packageName)
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatalf("mkdir runtime skill package failed: %v", err)
	}
	skillMarkdown := strings.TrimSpace(`---
name: `+packageName+`
description: plugin test package
kind: category
tools:
  - `+toolName+`
---

# `+packageName) + "\n"
	if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte(skillMarkdown), 0o644); err != nil {
		t.Fatalf("write skill markdown failed: %v", err)
	}
	toolBaseDir := filepath.Join(filepath.Dir(root), "tools")
	baseName := filepath.Base(root)
	parentDir := filepath.Dir(root)
	if (strings.EqualFold(baseName, "system") || strings.EqualFold(baseName, "user")) && strings.EqualFold(filepath.Base(parentDir), "skills") {
		toolBaseDir = filepath.Join(filepath.Dir(parentDir), "tools", baseName)
	} else if strings.EqualFold(baseName, "skills") {
		toolBaseDir = filepath.Join(filepath.Dir(root), "tools", "system")
	}
	toolDir := filepath.Join(toolBaseDir, packageName)
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatalf("mkdir runtime tool package failed: %v", err)
	}
	toolDefinition := strings.TrimSpace(`apiVersion: tool/v1
kind: Tool
metadata:
  name: `+toolName+`
  domain: `+packageName+`
  runtime: qgis
spec:
  description: plugin tool
execution:
  backend_tool: `+toolName+`
  param_mapping:
    INPUT: '{{ layer }}'`) + "\n"
	if err := os.WriteFile(filepath.Join(toolDir, toolName+".yaml"), []byte(toolDefinition), 0o644); err != nil {
		t.Fatalf("write tool definition failed: %v", err)
	}
}
