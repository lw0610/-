package plantools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/capability"
	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"

	"github.com/autocarto/carto_agent/internal/config"
)

func TestHandles(t *testing.T) {
	if !Handles("query_build_capabilities") {
		t.Fatal("expected Handles to return true for query_build_capabilities")
	}
	if !Handles("  query_build_capabilities  ") {
		t.Fatal("expected Handles to trim and match")
	}
	if Handles("list_agent_configs") {
		t.Fatal("expected Handles to return false for unrelated tool")
	}
	if Handles("") {
		t.Fatal("expected Handles to return false for empty string")
	}
}

func TestQueryBuildCapabilities(t *testing.T) {
	rootDir := t.TempDir()
	pluginRoot := filepath.Join(rootDir, "autocarto")
	configBaseDir := filepath.Join(rootDir, ".autocarto", "config")
	globalSkillsDir := filepath.Join(pluginRoot, "skills", "system")
	exclusiveSkillsDir := filepath.Join(pluginRoot, "skills", "user")
	for _, dir := range []string{globalSkillsDir, exclusiveSkillsDir, filepath.Join(pluginRoot, "tools", "system"), filepath.Join(pluginRoot, "tools", "user")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", dir, err)
		}
	}
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })
	writeTestPackage(t, globalSkillsDir, "global-category", "get_layers_info")
	writeTestPackage(t, exclusiveSkillsDir, "exclusive-category", "query_stats")

	activeConfig := &config.AgentConfigFile{Runtime: &config.RuntimeConfig{Global: config.GlobalAgentConfig{SkillPackages: []string{"global-category"}}, Agents: map[string]config.AgentDefData{"orchestrator": {DisplayName: "编排智能体", Description: "运行主智能体", IsMain: true, Enabled: true, SkillPackages: []string{"exclusive-category"}, Prompts: map[string]string{"static_instructions": "run", "user_prompt_template": "{{ user_input }}"}}}}}

	baseTools := []einotool.BaseTool{
		makeFakeTool("get_layers_info", "获取图层信息"),
		makeFakeTool("query_stats", "查询统计"),
	}

	services := Services{
		ActiveConfigName: "default",
		ActiveConfig:     activeConfig,
		SkillsDir:        globalSkillsDir,
		BuiltinSkillsDir: globalSkillsDir,
		UserSkillsDir:    exclusiveSkillsDir,
		ConfigBaseDir:    configBaseDir,
		BaseTools:        baseTools,
	}

	result, err := Invoke(context.Background(), "query_build_capabilities", map[string]any{"agent_name": "orchestrator"}, services)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var payload struct {
		ConfigName string `json:"config_name"`
		Agents     []struct {
			AgentName     string   `json:"agent_name"`
			SkillPackages []string `json:"skill_packages"`
			Tools         []struct {
				Name      string `json:"name"`
				PackageID string `json:"package_id"`
			} `json:"tools"`
		} `json:"agents"`
		TotalTools int `json:"total_tools"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if payload.ConfigName != "default" || payload.TotalTools < 2 {
		t.Fatalf("unexpected payload header: config=%s total=%d", payload.ConfigName, payload.TotalTools)
	}
	if len(payload.Agents) != 1 || payload.Agents[0].AgentName != "orchestrator" {
		t.Fatalf("unexpected agents: %#v", payload.Agents)
	}
	if len(payload.Agents[0].Tools) < 2 {
		t.Fatalf("unexpected tools count: %d", len(payload.Agents[0].Tools))
	}
	packageIDs := map[string]string{}
	for _, item := range payload.Agents[0].Tools {
		packageIDs[item.Name] = item.PackageID
	}
	if packageIDs["get_layers_info"] != "global-category" || packageIDs["query_stats"] != "exclusive-category" {
		t.Fatalf("unexpected package mapping: %#v", packageIDs)
	}
}

func TestNewQueryBuildCapabilitiesTool(t *testing.T) {
	rootDir := t.TempDir()
	pluginRoot := filepath.Join(rootDir, "autocarto")
	configBaseDir := filepath.Join(rootDir, ".autocarto", "config")
	globalSkillsDir := filepath.Join(pluginRoot, "skills", "system")
	exclusiveSkillsDir := filepath.Join(pluginRoot, "skills", "user")
	for _, dir := range []string{globalSkillsDir, exclusiveSkillsDir, filepath.Join(pluginRoot, "tools", "system"), filepath.Join(pluginRoot, "tools", "user")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s failed: %v", dir, err)
		}
	}
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })
	writeTestPackage(t, globalSkillsDir, "global-category", "get_layers_info")
	writeTestPackage(t, exclusiveSkillsDir, "exclusive-category", "query_stats")

	activeConfig := &config.AgentConfigFile{Runtime: &config.RuntimeConfig{Global: config.GlobalAgentConfig{SkillPackages: []string{"global-category"}}, Agents: map[string]config.AgentDefData{"orchestrator": {DisplayName: "编排智能体", Description: "运行主智能体", IsMain: true, Enabled: true, SkillPackages: []string{"exclusive-category"}, Prompts: map[string]string{"static_instructions": "run", "user_prompt_template": "{{ user_input }}"}}}}}
	services := Services{
		ActiveConfigName: "default",
		ActiveConfig:     activeConfig,
		SkillsDir:        globalSkillsDir,
		BuiltinSkillsDir: globalSkillsDir,
		UserSkillsDir:    exclusiveSkillsDir,
		ConfigBaseDir:    configBaseDir,
		BaseTools: []einotool.BaseTool{
			makeFakeTool("get_layers_info", "获取图层信息"),
			makeFakeTool("query_stats", "查询统计"),
		},
	}

	queryTool := NewQueryBuildCapabilitiesTool(services)
	enhanced, ok := queryTool.(einotool.EnhancedInvokableTool)
	if !ok {
		t.Fatalf("expected enhanced invokable tool, got %T", queryTool)
	}

	result, err := enhanced.InvokableRun(context.Background(), &schema.ToolArgument{Text: `{"agent_name":"orchestrator"}`})
	if err != nil {
		t.Fatalf("InvokableRun failed: %v", err)
	}
	if result == nil || len(result.Parts) == 0 {
		t.Fatal("expected tool result payload")
	}

	var payload struct {
		ConfigName string `json:"config_name"`
		Agents     []struct {
			AgentName string `json:"agent_name"`
			Tools     []struct {
				Name      string `json:"name"`
				PackageID string `json:"package_id"`
			} `json:"tools"`
		} `json:"agents"`
		TotalTools int `json:"total_tools"`
	}
	if err := json.Unmarshal([]byte(result.Parts[0].Text), &payload); err != nil {
		t.Fatalf("unmarshal tool result failed: %v", err)
	}
	if payload.ConfigName != "default" || payload.TotalTools < 2 {
		t.Fatalf("unexpected payload header: %#v", payload)
	}
	if len(payload.Agents) != 1 || payload.Agents[0].AgentName != "orchestrator" {
		t.Fatalf("unexpected agent payload: %#v", payload.Agents)
	}
	if len(payload.Agents[0].Tools) < 2 {
		t.Fatalf("unexpected tools payload: %#v", payload.Agents[0].Tools)
	}
	packageIDs := map[string]string{}
	for _, item := range payload.Agents[0].Tools {
		packageIDs[item.Name] = item.PackageID
	}
	if packageIDs["get_layers_info"] != "global-category" || packageIDs["query_stats"] != "exclusive-category" {
		t.Fatalf("unexpected package mapping: %#v", packageIDs)
	}
}

func TestQueryBuildCapabilitiesUsesPluginSkillsDirOverride(t *testing.T) {
	configBaseDir := t.TempDir()
	globalSkillsDir := filepath.Join(configBaseDir, "skills", "system")
	pluginSkillsDir := filepath.Join(t.TempDir(), "plugin-skills")
	writeTestPackage(t, pluginSkillsDir, "plugin-category", "plugin_query_tool")

	agentYAML := `version: '1.0'
meta:
  default_execution_mode: build
  plan_enabled: true
runtime:
  global:
    skill_packages:
      - plugin-category
  agents:
    orchestrator:
      display_name: 编排智能体
      description: 运行主智能体
      is_main: true
      enabled: true
      prompts:
        static_instructions: run
        user_prompt_template: "{{ user_input }}"
`
	agentConfigDir := filepath.Join(configBaseDir, "agent_configs")
	writeTestFile(t, filepath.Join(agentConfigDir, "default", "agent.yaml"), agentYAML)
	writeTestFile(t, filepath.Join(agentConfigDir, ".active_agent_config"), "default")

	mgr := config.NewAgentConfigManager(agentConfigDir)
	services := Services{
		AgentConfigManager: mgr,
		SkillsDir:          globalSkillsDir,
		PluginSkillsDir:    pluginSkillsDir,
		ConfigBaseDir:      configBaseDir,
		BaseTools: []einotool.BaseTool{
			makeFakeTool("plugin_query_tool", "插件工具"),
		},
	}

	result, err := Invoke(context.Background(), "query_build_capabilities", map[string]any{"agent_name": "orchestrator"}, services)
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var payload struct {
		Agents []struct {
			Tools []struct {
				Name      string `json:"name"`
				PackageID string `json:"package_id"`
			} `json:"tools"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(payload.Agents) != 1 || len(payload.Agents[0].Tools) == 0 {
		t.Fatalf("unexpected agents payload: %#v", payload.Agents)
	}
	packageIDs := map[string]string{}
	for _, item := range payload.Agents[0].Tools {
		packageIDs[item.Name] = item.PackageID
	}
	if packageIDs["plugin_query_tool"] != "plugin-category" {
		t.Fatalf("expected plugin tool in payload, got %#v", payload.Agents[0].Tools)
	}
	if _, ok := packageIDs["get_project_info"]; ok {
		t.Fatalf("did not expect unrelated builtin tool when only plugin package is selected, got %#v", payload.Agents[0].Tools)
	}
}

func TestInvokeUnknownTool(t *testing.T) {
	_, err := Invoke(context.Background(), "unknown_tool", nil, Services{})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestQueryBuildCapabilitiesPrefersSnapshotStoreWhenAvailable(t *testing.T) {
	store := capability.NewSnapshotStore()
	store.Set(&capability.CapabilitySnapshot{
		ConfigName: "default",
		Agents: []capability.AgentCapabilityView{{
			AgentName:     "orchestrator",
			DisplayName:   "编排智能体",
			Description:   "运行主智能体",
			Enabled:       true,
			SkillPackages: []string{"analysis-global"},
			Tools: []capability.ToolRef{{
				Name:        "buffer",
				Description: "缓冲区分析",
				Package:     capability.PackageRef{PackageID: "analysis-global"},
			}},
		}},
	})

	result, err := Invoke(context.Background(), "query_build_capabilities", map[string]any{"agent_name": "orchestrator"}, Services{
		SnapshotStore: store,
		SnapshotMode:  "cutover",
	})
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var payload struct {
		ConfigName string `json:"config_name"`
		Agents     []struct {
			AgentName string `json:"agent_name"`
			Tools     []struct {
				Name      string `json:"name"`
				PackageID string `json:"package_id"`
			} `json:"tools"`
		} `json:"agents"`
		TotalTools int `json:"total_tools"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if payload.ConfigName != "default" || payload.TotalTools != 1 {
		t.Fatalf("unexpected snapshot payload header: %#v", payload)
	}
	if len(payload.Agents) != 1 || len(payload.Agents[0].Tools) != 1 || payload.Agents[0].Tools[0].PackageID != "analysis-global" {
		t.Fatalf("unexpected snapshot payload: %#v", payload)
	}
}

func TestQueryBuildCapabilitiesPrimaryModeDoesNotFallbackWithoutSnapshot(t *testing.T) {
	result, err := Invoke(context.Background(), "query_build_capabilities", nil, Services{
		SnapshotStore: capability.NewSnapshotStore(),
		SnapshotMode:  "primary",
	})
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if string(raw) != `{"agents":[],"config_name":"","total_tools":0}` {
		t.Fatalf("unexpected primary fallback payload: %s", string(raw))
	}
}

func writeTestPackage(t *testing.T, rootDir, packageName, toolName string) {
	t.Helper()
	writeTestFile(t, filepath.Join(rootDir, packageName, "SKILL.md"),
		"---\nname: "+packageName+"\ndescription: test package\nkind: category\ntools:\n  - "+toolName+"\n---\n\n# test\n")
}

func writeTestFile(t *testing.T, path, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}

func makeFakeTool(name, desc string) einotool.BaseTool {
	info := &schema.ToolInfo{Name: name, Desc: desc}
	return toolutils.NewEnhancedTool(info, func(ctx context.Context, args map[string]any) (*schema.ToolResult, error) {
		return &schema.ToolResult{}, nil
	})
}
