package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/defaults"
)

func writeTextFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}
	return string(data)
}

func TestSeedAgentConfigsCopiesMissingAndReplacesBootstrapDefault(t *testing.T) {
	targetDir := t.TempDir()

	writeTextFile(t, filepath.Join(targetDir, "default.yaml"), `version: "1.0"
runtime:
  global:
    common_tools:
      exclusive: []
      tool_groups: []
    common_skills: []
    tool_groups: {}
  agents:
    orchestrator:
      display_name: 主控智能体
      description: 负责规划任务并协调工具执行
      is_main: true
      enabled: true
      llm:
        provider: ""
      tool_policy:
        private:
          exclusive: []
          tool_groups: []
        share:
          exclusive: []
          tool_groups: []
      skill_policy:
        private: []
        share: []
      prompts:
        static_instructions: demo
`)

	_, replaced, err := seedAgentConfigsFromEmbedded(targetDir)
	if err != nil {
		t.Fatalf("seedAgentConfigsFromEmbedded failed: %v", err)
	}
	if replaced != 1 {
		t.Fatalf("expected replaced=1, got %d", replaced)
	}

	embeddedDefault, err := defaults.FS.ReadFile("agent_configs/default/agent.yaml")
	if err != nil {
		t.Fatalf("read embedded default failed: %v", err)
	}
	defaultRaw := readTextFile(t, filepath.Join(targetDir, "default", "agent.yaml"))
	if defaultRaw != string(embeddedDefault) {
		t.Fatalf("expected default config replaced by embedded default")
	}
	if _, err := os.Stat(filepath.Join(targetDir, "default.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected legacy default.yaml removed, err=%v", err)
	}

	entries, err := fs.ReadDir(defaults.FS, "agent_configs")
	if err != nil {
		t.Fatalf("read embedded agent configs failed: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(targetDir, entry.Name(), "agent.yaml")); statErr != nil {
			t.Fatalf("expected %s copied, stat err: %v", entry.Name(), statErr)
		}
	}
}

func TestSeedAgentConfigsKeepsCustomizedDefault(t *testing.T) {
	targetDir := t.TempDir()

	customDefault := `version: "1.0"
runtime:
  global:
	    common_skills: []
	    common_category_skills: [project-state]
  agents:
    orchestrator:
      is_main: true
      enabled: true
	      skill_policy:
	        private:
	          category_skills: [vector-selection]
	        share: {}
      prompts: {}
`
	writeTextFile(t, filepath.Join(targetDir, "default.yaml"), customDefault)

	_, replaced, err := seedAgentConfigsFromEmbedded(targetDir)
	if err != nil {
		t.Fatalf("seedAgentConfigsFromEmbedded failed: %v", err)
	}
	if replaced != 0 {
		t.Fatalf("expected replaced=0, got %d", replaced)
	}

	defaultRaw := readTextFile(t, filepath.Join(targetDir, "default", "agent.yaml"))
	if !strings.Contains(defaultRaw, "project-state") {
		t.Fatalf("expected customized default to keep original content, got: %s", defaultRaw)
	}
}

func TestSeedAgentConfigsReplacesUnsupportedLegacyBootstrapDefault(t *testing.T) {
	targetDir := t.TempDir()

	writeTextFile(t, filepath.Join(targetDir, "default.yaml"), `version: "1.0"
build:
  global:
    common_tools:
      exclusive: []
      tool_groups: []
    common_skills: []
    tool_groups: {}
  agents:
    orchestrator:
      is_main: true
      enabled: true
      tool_policy:
        private:
          exclusive: []
          tool_groups: []
        share:
          exclusive: []
          tool_groups: []
      prompts: {}
`)

	_, replaced, err := seedAgentConfigsFromEmbedded(targetDir)
	if err != nil {
		t.Fatalf("seedAgentConfigsFromEmbedded failed: %v", err)
	}
	if replaced != 1 {
		t.Fatalf("expected replaced=1 for unsupported legacy bootstrap config, got %d", replaced)
	}

	embeddedDefault, err := defaults.FS.ReadFile("agent_configs/default/agent.yaml")
	if err != nil {
		t.Fatalf("read embedded default failed: %v", err)
	}
	defaultRaw := readTextFile(t, filepath.Join(targetDir, "default", "agent.yaml"))
	if defaultRaw != string(embeddedDefault) {
		t.Fatalf("expected unsupported legacy default replaced by embedded default")
	}
}

func TestSeedAgentConfigsReplacesLegacyBuiltinDefault(t *testing.T) {
	targetDir := t.TempDir()
	writeTextFile(t, filepath.Join(targetDir, "default.yaml"), `version: "1.0"
meta:
  default_execution_mode: build
  description: 系统默认智能体方案（4+1 优化架构，职责清晰，消除冗余）
  plan_enabled: true
runtime:
  global:
    common_tools:
      exclusive: []
      tool_groups:
        - context_readonly
    common_skills:
      - prompt-refinement
    tool_groups: {}
  agents:
    orchestrator:
      is_main: true
      enabled: true
      tool_policy:
        private:
          exclusive: []
          tool_groups: []
        share:
          exclusive: []
          tool_groups: []
      prompts: {}
`)

	_, replaced, err := seedAgentConfigsFromEmbedded(targetDir)
	if err != nil {
		t.Fatalf("seedAgentConfigsFromEmbedded failed: %v", err)
	}
	if replaced != 1 {
		t.Fatalf("expected replaced=1 for legacy builtin default, got %d", replaced)
	}

	defaultRaw := readTextFile(t, filepath.Join(targetDir, "default", "agent.yaml"))
	if !strings.Contains(defaultRaw, "build-capability-awareness") {
		t.Fatalf("expected default config replaced by new embedded default, got: %s", defaultRaw)
	}
}

func TestSeedAgentConfigsRemovesRetiredEmbeddedProfiles(t *testing.T) {
	targetDir := t.TempDir()
	writeTextFile(t, filepath.Join(targetDir, "high-performance-routing.yaml"), "version: '1.0'\n")
	writeTextFile(t, filepath.Join(targetDir, "pyqgis-scripting.yaml"), "version: '1.0'\n")

	if _, _, err := seedAgentConfigsFromEmbedded(targetDir); err != nil {
		t.Fatalf("seedAgentConfigsFromEmbedded failed: %v", err)
	}

	for _, name := range []string{"high-performance-routing.yaml", "pyqgis-scripting.yaml"} {
		if _, err := os.Stat(filepath.Join(targetDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected retired embedded config %s removed, got err=%v", name, err)
		}
	}
}
