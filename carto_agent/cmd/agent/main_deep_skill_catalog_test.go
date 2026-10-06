package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/capability"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/autocarto/carto_agent/internal/compute"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/skill"
)

func TestComputeReadyFromRuntimeSnapshot(t *testing.T) {
	if computeReadyFromRuntimeSnapshot(middlewareclient.RuntimeSnapshot{}) {
		t.Fatal("expected empty snapshot to report compute not ready")
	}
	if !computeReadyFromRuntimeSnapshot(middlewareclient.RuntimeSnapshot{Services: map[string]middlewareclient.ServiceStatus{"cli": {Status: "running"}}}) {
		t.Fatal("expected running cli service to report ready")
	}
	if computeReadyFromRuntimeSnapshot(middlewareclient.RuntimeSnapshot{Services: map[string]middlewareclient.ServiceStatus{"compute": {Ready: true}}}) {
		t.Fatal("did not expect legacy compute alias service to report ready")
	}
}

func TestSummarizeCapabilityShadowDiff(t *testing.T) {
	existing := []middlewareclient.ToolCatalogItem{{Name: "buffer", ProviderID: "cli", Source: "cli", Available: true}}
	snapshot := &capability.CapabilitySnapshot{Tools: map[string]capability.ToolCapability{"buffer": {Name: "buffer", ProviderID: "cli", Source: "cli", Available: true}}, ToolOrder: []string{"buffer"}}
	if diff := summarizeCapabilityShadowDiff(existing, snapshot); diff.Changed {
		t.Fatalf("expected equal catalogs to produce clean diff: %#v", diff)
	}
	snapshot.Tools["buffer"] = capability.ToolCapability{Name: "buffer", ProviderID: "cli", Source: "cli", Available: false, UnavailableReason: "compute_unavailable"}
	if diff := summarizeCapabilityShadowDiff(existing, snapshot); !diff.Changed || !diff.AvailabilityChanged {
		t.Fatalf("expected availability diff, got %#v", diff)
	}
}

func TestShouldUseCapabilitySnapshot(t *testing.T) {
	if shouldUseCapabilitySnapshot("shadow") {
		t.Fatal("expected shadow mode to keep old runtime catalog path")
	}
	if !shouldUseCapabilitySnapshot("cutover") || !shouldUseCapabilitySnapshot("primary") {
		t.Fatal("expected cutover/primary modes to use capability snapshot")
	}
	if !shouldAllowCapabilityLegacyFallback("cutover") || shouldAllowCapabilityLegacyFallback("primary") {
		t.Fatal("expected legacy fallback only in shadow/cutover")
	}
}

func TestRuntimeCatalogItemsFromCapabilityStore(t *testing.T) {
	store := capability.NewSnapshotStore()
	store.Set(&capability.CapabilitySnapshot{Tools: map[string]capability.ToolCapability{"buffer": {Name: "buffer", ProviderID: "cli", Source: "cli", Available: true}}, ToolOrder: []string{"buffer"}})
	if items := runtimeCatalogItemsFromCapabilityStore(store, "shadow", middlewareclient.RuntimeSnapshot{}); len(items) != 0 {
		t.Fatalf("expected shadow mode to ignore snapshot store, got %#v", items)
	}
	items := runtimeCatalogItemsFromCapabilityStore(store, "cutover", middlewareclient.RuntimeSnapshot{})
	if len(items) != 1 || items[0].Name != "buffer" {
		t.Fatalf("unexpected runtime items from snapshot store: %#v", items)
	}
}

func TestRuntimeCatalogItemsFromCapabilityStoreAppliesLiveBridgeAvailability(t *testing.T) {
	store := capability.NewSnapshotStore()
	store.Set(&capability.CapabilitySnapshot{
		Tools: map[string]capability.ToolCapability{
			"open_project": {
				Name:              "open_project",
				Backend:           skill.RuntimeIDQGIS,
				Source:            skill.RuntimeIDQGIS,
				Available:         false,
				UnavailableReason: skill.MissingReasonInteractiveInstanceDisconnected,
			},
		},
		ToolOrder: []string{"open_project"},
	})

	items := runtimeCatalogItemsFromCapabilityStore(store, "primary", middlewareclient.RuntimeSnapshot{
		Instances: middlewareclient.InstancesSnapshot{
			Items: []*discovery.QGISInstance{{ID: "inst-a", Status: "running"}},
		},
	})
	if len(items) != 1 {
		t.Fatalf("unexpected runtime items count: %#v", items)
	}
	if !items[0].Available || items[0].UnavailableReason != "" {
		t.Fatalf("expected live runtime overlay to recover qgis availability, got %#v", items[0])
	}
	if len(items[0].InstanceIDs) != 1 || items[0].InstanceIDs[0] != "inst-a" {
		t.Fatalf("expected running instance id injected, got %#v", items[0].InstanceIDs)
	}
}

func TestNewRuntimeRegistryGovernanceResolvesGlobalSkillPackage(t *testing.T) {
	configBaseDir := t.TempDir()
	globalSkillsDir := config.ResolveGlobalSkillsDir(configBaseDir)
	globalToolsDir := config.ResolveGlobalToolsDir(configBaseDir)
	packageID := "canvas-navigation-runtime-check"
	toolName := "canvas_zoom_to_extent_runtime_check"
	packageDir := filepath.Join(globalSkillsDir, packageID)
	toolDir := filepath.Join(globalToolsDir, packageID)

	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatalf("mkdir package dir failed: %v", err)
	}
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatalf("mkdir tool dir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte(`---
name: `+packageID+`
description: `+packageID+`
kind: category
tools:
  - `+toolName+`
---

# `+packageID+`
`), 0o644); err != nil {
		t.Fatalf("write SKILL.md failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(toolDir, toolName+".yaml"), []byte(`apiVersion: tool/v1
kind: Tool
metadata:
  name: `+toolName+`
  domain: `+packageID+`
  runtime: qgis
spec:
  description: zoom canvas
execution:
  backend_tool: `+toolName+`
  param_mapping:
    INPUT: '{{ layer }}'
`), 0o644); err != nil {
		t.Fatalf("write tool definition failed: %v", err)
	}

	governance := newRuntimeRegistryGovernance(
		&config.Config{ConfigBaseDir: configBaseDir},
		nil,
		"",
		"",
		config.NewAgentConfigManager(filepath.Join(configBaseDir, "agent_configs")),
		nil,
	)
	if governance == nil {
		t.Fatal("expected registry governance")
	}

	pkg, ok, err := governance.ResolveSkillPackage(packageID)
	if err != nil {
		t.Fatalf("ResolveSkillPackage returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected global skill package to be governed")
	}
	if filepath.Clean(pkg.Dir) != filepath.Clean(packageDir) {
		t.Fatalf("unexpected governed package dir: got=%s want=%s", pkg.Dir, packageDir)
	}
}

func TestBuildLocalRuntimeSnapshotIncludesAgentManagedCompute(t *testing.T) {
	snapshot := buildLocalRuntimeSnapshot(&compute.LocalRuntime{
		PythonPath: "/usr/bin/python3",
		WorkingDir: "/tmp",
	})
	service, ok := snapshot.Services["cli"]
	if !ok {
		t.Fatal("expected cli service in local snapshot")
	}
	if service.Source != "sidecar_local_cli" || !service.Ready {
		t.Fatalf("unexpected compute service snapshot: %#v", service)
	}
}

func TestBuildRuntimeToolsUsesSkillEngineForComputeAndCatalogForInteractive(t *testing.T) {
	configBaseDir := t.TempDir()
	useSkillEngine := true
	tools, err := buildRuntimeTools(context.Background(), &config.Config{ConfigBaseDir: configBaseDir, Agent: config.AgentConfig{UseSkillEngine: &useSkillEngine}}, skill.BuildOptions{
		ComputeClient: newFakeCLIComputeClient(t, t.TempDir(), `{"tools":[]}`, `{"success":true,"output":{"ok":true},"error":"","output_layers":[]}`),
		QGISInvoker: func(context.Context, string, map[string]any) (any, error) {
			return map[string]any{"success": true}, nil
		},
	}, "", "", "", "")
	if err != nil {
		t.Fatalf("buildRuntimeTools failed: %v", err)
	}
	if len(tools) < 20 {
		t.Fatalf("expected skill-engine tool set larger than middleware-only path, got %d", len(tools))
	}
	names := collectToolNames(t, tools)
	if countToolName(names, "assign_skill_packages") != 1 {
		t.Fatalf("expected exactly one assign_skill_packages tool, got names: %v", names)
	}
	if countToolName(names, "get_layers_info") != 1 {
		t.Fatalf("expected exactly one get_layers_info tool, got names: %v", names)
	}
}

func TestBuildRuntimeToolsUsesRegistryWhenSkillEngineDisabled(t *testing.T) {
	configBaseDir := t.TempDir()
	useSkillEngine := false
	tools, err := buildRuntimeTools(context.Background(), &config.Config{ConfigBaseDir: configBaseDir, Agent: config.AgentConfig{UseSkillEngine: &useSkillEngine}}, skill.BuildOptions{
		ComputeClient: newFakeCLIComputeClient(t, t.TempDir(), `{"tools":[]}`, `{"success":true,"output":{"ok":true},"error":"","output_layers":[]}`),
		QGISInvoker: func(context.Context, string, map[string]any) (any, error) {
			return map[string]any{"success": true}, nil
		},
	}, "", "", "", "")
	if err != nil {
		t.Fatalf("buildRuntimeTools failed: %v", err)
	}
	if len(tools) < 20 {
		t.Fatalf("expected registry-backed tool set even when skill engine flag is false, got %d", len(tools))
	}
	names := collectToolNames(t, tools)
	if countToolName(names, "assign_skill_packages") != 1 {
		t.Fatalf("expected exactly one assign_skill_packages tool, got names: %v", names)
	}
	if countToolName(names, "get_layers_info") != 1 {
		t.Fatalf("expected exactly one get_layers_info tool, got names: %v", names)
	}
}

func TestBuildRuntimeToolsIncludesExplicitPluginSkillsDir(t *testing.T) {
	configBaseDir := t.TempDir()
	pluginRoot := t.TempDir()
	pluginSkillsDir := filepath.Join(pluginRoot, "skills")
	writeRuntimeSkillPackage(t, pluginSkillsDir, "plugin-only", "plugin_only_tool")
	useSkillEngine := true

	tools, err := buildRuntimeTools(
		context.Background(),
		&config.Config{ConfigBaseDir: configBaseDir, Agent: config.AgentConfig{UseSkillEngine: &useSkillEngine}},
		skill.BuildOptions{
			ComputeClient: newFakeCLIComputeClient(t, t.TempDir(), `{"tools":[]}`, `{"success":true,"output":{"ok":true},"error":"","output_layers":[]}`),
			QGISInvoker: func(context.Context, string, map[string]any) (any, error) {
				return map[string]any{"success": true}, nil
			},
		},
		pluginSkillsDir,
		"",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("buildRuntimeTools failed: %v", err)
	}
	names := collectToolNames(t, tools)
	if countToolName(names, "plugin_only_tool") != 1 {
		t.Fatalf("expected explicit plugin skill tool, got names: %v", names)
	}
}

func TestBuildRuntimeSkillConfigProjectionIncludesExplicitPluginSkillsDir(t *testing.T) {
	configBaseDir := t.TempDir()
	pluginSkillsDir := t.TempDir()
	writeRuntimeSkillPackage(t, pluginSkillsDir, "plugin-category", "plugin_only_tool")
	activeConfig := &config.AgentConfigFile{
		Runtime: &config.RuntimeConfig{
			Global: config.GlobalAgentConfig{SkillPackages: []string{"plugin-category"}},
			Agents: map[string]config.AgentDefData{
				"orchestrator": {DisplayName: "编排智能体", Description: "测试", Enabled: true},
			},
		},
	}

	runtimeSkillConfig := buildRuntimeSkillConfigProjection(activeConfig, pluginSkillsDir, "", "", configBaseDir, "default")
	if runtimeSkillConfig == nil {
		t.Fatal("expected runtime skill config")
	}
	allowed := runtimeSkillConfig.GetAgentAllowedTools("orchestrator")
	if len(allowed) != 1 || allowed[0] != "plugin_only_tool" {
		t.Fatalf("expected plugin_only_tool from explicit plugin dir, got %v", allowed)
	}
}

func TestBuildRuntimeToolsIncludesExclusiveSkillsForActiveConfig(t *testing.T) {
	rootDir := t.TempDir()
	pluginRoot := filepath.Join(rootDir, "autocarto")
	configBaseDir := filepath.Join(rootDir, ".autocarto", "config")
	for _, dir := range []string{
		filepath.Join(pluginRoot, "skills", "system"),
		filepath.Join(pluginRoot, "skills", "user"),
		filepath.Join(pluginRoot, "tools", "system"),
		filepath.Join(pluginRoot, "tools", "user"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", dir, err)
		}
	}
	// An inherited desktop asset root intentionally overrides cwd discovery.
	// Isolate that setting and exercise the actual per-config exclusive paths.
	t.Setenv("AUTOCARTO_ASSETS_ROOT", pluginRoot)
	exclusiveSkillsDir := config.ResolveExclusiveSkillsDir(configBaseDir, "creator")
	exclusiveToolsDir := config.ResolveExclusiveToolsDir(configBaseDir, "creator")
	writeTextFile(t, filepath.Join(exclusiveSkillsDir, "exclusive-category", "SKILL.md"), `---
name: exclusive-category
description: exclusive test package
kind: category
tools:
  - exclusive_tool
---

# Exclusive category
`)
	writeTextFile(t, filepath.Join(exclusiveToolsDir, "exclusive-category", "exclusive_tool.yaml"), `apiVersion: tool/v1
kind: Tool
metadata:
  name: exclusive_tool
  domain: exclusive-category
  runtime: qgis
spec:
  description: exclusive tool
execution:
  backend_tool: exclusive_tool
  param_mapping:
    INPUT: '{{ layer }}'
`)
	useSkillEngine := true

	tools, err := buildRuntimeTools(
		context.Background(),
		&config.Config{ConfigBaseDir: configBaseDir, Agent: config.AgentConfig{UseSkillEngine: &useSkillEngine}},
		skill.BuildOptions{
			ComputeClient: newFakeCLIComputeClient(t, t.TempDir(), `{"tools":[]}`, `{"success":true,"output":{"ok":true},"error":"","output_layers":[]}`),
			InternalInvoker: func(context.Context, string, map[string]any) (any, error) {
				return map[string]any{"success": true}, nil
			},
		},
		"",
		"",
		"",
		"creator",
	)
	if err != nil {
		t.Fatalf("buildRuntimeTools failed: %v", err)
	}
	names := collectToolNames(t, tools)
	if countToolName(names, "exclusive_tool") != 1 {
		t.Fatalf("expected exclusive_tool from active config skills dir, got names: %v", names)
	}
	otherTools, err := buildRuntimeTools(
		context.Background(),
		&config.Config{ConfigBaseDir: configBaseDir, Agent: config.AgentConfig{UseSkillEngine: &useSkillEngine}},
		skill.BuildOptions{
			ComputeClient: newFakeCLIComputeClient(t, t.TempDir(), `{"tools":[]}`, `{"success":true,"output":{"ok":true},"error":"","output_layers":[]}`),
		},
		"", "", "", "other",
	)
	if err != nil {
		t.Fatalf("buildRuntimeTools for other config failed: %v", err)
	}
	if countToolName(collectToolNames(t, otherTools), "exclusive_tool") != 0 {
		t.Fatal("exclusive_tool leaked into a different active config")
	}
}

func TestBuildRuntimeToolCatalogItemsUsesGeneratedSkillPackages(t *testing.T) {
	configBaseDir := t.TempDir()

	items, err := buildRuntimeToolCatalogItems(context.Background(), &config.Config{ConfigBaseDir: configBaseDir}, nil, "", "")
	if err != nil {
		t.Fatalf("buildRuntimeToolCatalogItems failed: %v", err)
	}
	if len(items) < 30 {
		t.Fatalf("expected generated runtime tool catalog items, got %d", len(items))
	}
	if item := findToolCatalogItem(t, items, "assign_skill_packages"); item.Name != "assign_skill_packages" {
		t.Fatalf("unexpected creator catalog identity: %#v", item)
	}
	if item := findToolCatalogItem(t, items, "get_layers_info"); item.Name != "get_layers_info" {
		t.Fatalf("unexpected analysis catalog identity: %#v", item)
	}
}

func TestBuildRuntimeToolCatalogItemsUsesLocalComputeAvailability(t *testing.T) {
	configBaseDir := t.TempDir()

	items, err := buildRuntimeToolCatalogItems(context.Background(), &config.Config{ConfigBaseDir: configBaseDir}, newFakeCLIComputeClient(t, t.TempDir(), `{"tools":[{"name":"buffer","available":false,"backend":"qgis_process","unavailable_reason":"qgis_not_ready"}]}`, `{"success":true,"output":{"ok":true},"error":"","output_layers":[]}`), "", "")
	if err != nil {
		t.Fatalf("buildRuntimeToolCatalogItems failed: %v", err)
	}
	if len(items) < 30 {
		t.Fatalf("expected generated runtime tool catalog items, got %d", len(items))
	}
	if item := findToolCatalogItem(t, items, "assign_skill_packages"); item.Name != "assign_skill_packages" {
		t.Fatalf("unexpected creator catalog identity: %#v", item)
	}
	if item := findToolCatalogItem(t, items, "get_layers_info"); item.Name != "get_layers_info" {
		t.Fatalf("unexpected analysis catalog identity: %#v", item)
	}
}

func findToolCatalogItem(t *testing.T, items []middlewareclient.ToolCatalogItem, name string) middlewareclient.ToolCatalogItem {
	t.Helper()
	for _, item := range items {
		if item.Name == name {
			return item
		}
	}
	t.Fatalf("tool catalog item %s not found", name)
	return middlewareclient.ToolCatalogItem{}
}

func countToolName(names []string, target string) int {
	count := 0
	for _, name := range names {
		if name == target {
			count++
		}
	}
	return count
}

func collectToolNames(t *testing.T, tools []einotool.BaseTool) []string {
	t.Helper()
	result := make([]string, 0, len(tools))
	for _, baseTool := range tools {
		provider, ok := baseTool.(interface {
			Info(context.Context) (*schema.ToolInfo, error)
		})
		if !ok {
			t.Fatalf("tool does not expose Info: %T", baseTool)
		}
		info, err := provider.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info failed: %v", err)
		}
		result = append(result, info.Name)
	}
	return result
}

func writeRuntimeSkillPackage(t *testing.T, root string, packageName string, toolName string) {
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

func newFakeCLIComputeClient(t *testing.T, root string, listToolsJSON string, invokeJSON string) *compute.Client {
	t.Helper()
	invoker := &fakeCatalogComputeInvoker{}
	if err := json.Unmarshal([]byte(listToolsJSON), &struct {
		Tools *[]map[string]any `json:"tools"`
	}{Tools: &invoker.tools}); err != nil {
		t.Fatalf("decode fake list tools json failed: %v", err)
	}
	if err := json.Unmarshal([]byte(invokeJSON), &invoker.invoke); err != nil {
		t.Fatalf("decode fake invoke json failed: %v", err)
	}
	return compute.NewWSClient(invoker, "", 0)
}

type fakeCatalogComputeInvoker struct {
	tools  []map[string]any
	invoke compute.InvokeResponse
}

func (f *fakeCatalogComputeInvoker) ListInstanceTools(context.Context, string) ([]map[string]any, error) {
	return f.tools, nil
}

func (f *fakeCatalogComputeInvoker) InvokeTool(context.Context, string, map[string]any) (*compute.InvokeResponse, error) {
	resp := f.invoke
	return &resp, nil
}
