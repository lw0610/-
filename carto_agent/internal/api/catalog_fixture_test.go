package api

import (
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/settings"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func realFlowAPI(t *testing.T) (*API, *config.AgentConfigManager, string, string) {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	project := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	home := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", project)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", filepath.Join(project, "autocarto"))
	t.Setenv("AUTOCARTO_HOME", home)
	configDir := filepath.Join(home, "config", "agent_configs")
	path := filepath.Join(configDir, "compile-scheme", "agent.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	raw := `version: '2.0'
runtime:
  shared_tool_packages: []
  agents:
    main-renamed:
      kind: agent
      display_name: 编排角色
      enabled: true
      is_main: true
      tool_packages: []
    reader-renamed:
      kind: executor
      executor: route_knowledge
      display_name: 知识抽取专家
      enabled: true
      tool_packages: []
    data-renamed:
      kind: agent
      display_name: 数据工程专家
      enabled: true
      tool_packages: [data-loading, statistical-data-tools]
    map-renamed:
      kind: agent
      display_name: 制图专家
      enabled: true
      tool_packages: [cartography-styling, print-layout, route-cartography]
    analysis-renamed:
      kind: agent
      display_name: 空间分析专家
      enabled: true
      tool_packages: [vector-geometry]
    review-renamed:
      kind: agent
      display_name: 质量反馈智能体
      enabled: true
      tool_packages: [quality-feedback]
`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	manager := config.NewAgentConfigManager(configDir)
	assets := filepath.Join(project, "autocarto")
	service := settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{SkillsDir: filepath.Join(assets, "skills", "system"), BuiltinSkillsDir: filepath.Join(assets, "skills", "system"), UserSkillsDir: filepath.Join(home, "skills"), GlobalSkillsDir: filepath.Join(home, "skills"), BuiltinToolsDir: filepath.Join(assets, "tools", "system"), UserToolsDir: filepath.Join(home, "tools"), RuntimeToolDirs: []string{filepath.Join(assets, "tools", "system")}, RuntimeSkillDirs: []string{filepath.Join(assets, "skills", "system")}}, manager)
	a := NewAPI(nil, manager, nil, service, filepath.Join(assets, "skills", "system"), nil)
	return a, manager, project, home
}
