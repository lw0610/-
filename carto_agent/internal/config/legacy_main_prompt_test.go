package config

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/defaults"
	"gopkg.in/yaml.v3"
)

func legacyMainPromptFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"legacy_flow_start_prompt_v4.txt":          "5d2a838fbde8d53f4feb057dc9fda35ef8d17fd7da094140d243b441cd0c608e",
		"legacy_flow_start_prompt_acquisition.txt": "8edb9af792990c14080734ebe04f6eded8bad9b97cc5c7fec4ff8f08d1d3b5de",
	}[name]
	if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != want {
		t.Fatalf("fixture must match the verified historical official prompt: %s", got)
	}
	return string(raw)
}

func currentEmbeddedMainForPromptTest(t *testing.T) AgentDefData {
	t.Helper()
	raw, err := defaults.FS.ReadFile("agent_configs/default/agent.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var cfg AgentConfigFile
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.Runtime.Agents["orchestrator"]
}

func samePromptMigrationYAML(t *testing.T, left, right any) bool {
	t.Helper()
	a, err := yaml.Marshal(left)
	if err != nil {
		t.Fatal(err)
	}
	b, err := yaml.Marshal(right)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(a, b)
}

func TestKnownLegacyMainPromptUpgradePreservesConfiguration(t *testing.T) {
	target := currentEmbeddedMainForPromptTest(t)
	for _, file := range []string{"legacy_flow_start_prompt_v4.txt", "legacy_flow_start_prompt_acquisition.txt"} {
		for _, version := range []string{AgentConfigVersionV10, AgentConfigVersionV20} {
			t.Run(file+"/"+version, func(t *testing.T) {
				prompt := legacyMainPromptFixture(t, file)
				main := AgentDefData{Kind: "agent", IsMain: true, Enabled: false, ToolPackages: []string{"charts"}, LLM: AgentLLMConfig{Provider: "custom-provider"}, Prompts: map[string]string{"static_instructions": prompt, "user_prompt_template": "custom {{ user_input }}", "custom": "keep"}}
				worker := AgentDefData{Kind: "agent", Enabled: true, ToolPackages: []string{"file-operations"}, Prompts: map[string]string{"static_instructions": prompt}}
				input := &AgentConfigFile{Version: version, Meta: AgentConfigMeta{Description: "user description", PlanEnabled: false}, Runtime: &RuntimeConfig{SharedToolPackages: []string{"vector-selection"}, Agents: map[string]AgentDefData{"renamed-main": main, "worker": worker}}}
				before, _ := yaml.Marshal(input)
				got, warnings := migrateAgentConfigV1(input, nil)
				updated := got.Runtime.Agents["renamed-main"]
				if updated.Prompts["static_instructions"] != target.Prompts["static_instructions"] {
					t.Fatal("known retired flow_start prompt was not upgraded")
				}
				if !reflect.DeepEqual(updated.ToolPackages, []string{"charts", "data-service"}) || updated.LLM != main.LLM || updated.Enabled != main.Enabled || updated.Prompts["custom"] != "keep" || updated.Prompts["user_prompt_template"] != main.Prompts["user_prompt_template"] {
					t.Fatalf("user configuration changed unexpectedly: %+v", updated)
				}
				if !samePromptMigrationYAML(t, got.Runtime.Agents["worker"], worker) || !reflect.DeepEqual(got.Meta, input.Meta) || !reflect.DeepEqual(got.Runtime.SharedToolPackages, input.Runtime.SharedToolPackages) {
					t.Fatal("non-main role or profile settings changed")
				}
				if len(warnings) != 1 || warnings[0].Code != "LEGACY_MAIN_FLOW_PROMPT_UPGRADED" || warnings[0].Path != "runtime.agents.renamed-main.prompts.static_instructions" {
					t.Fatalf("missing explicit migration reason: %+v", warnings)
				}
				after, _ := yaml.Marshal(input)
				if !bytes.Equal(before, after) {
					t.Fatal("source configuration was mutated")
				}
				second, repeated := migrateAgentConfigV1(got, nil)
				if !reflect.DeepEqual(got, second) || len(repeated) != 0 {
					t.Fatal("prompt upgrade is not idempotent")
				}
			})
		}
	}
}

func TestCustomizedFlowStartPromptIsNotRewritten(t *testing.T) {
	legacy := legacyMainPromptFixture(t, "legacy_flow_start_prompt_v4.txt")
	for _, prompt := range []string{
		"自定义：调用 flow_start，但这是用户自己的规则。",
		legacy + "用户追加规则", "用户前言\n" + legacy,
		" " + legacy, strings.TrimSuffix(legacy, "\n"), strings.ReplaceAll(legacy, "\n", "\r\n"),
	} {
		input := &AgentConfigFile{Version: AgentConfigVersionV20, Runtime: &RuntimeConfig{Agents: map[string]AgentDefData{"main": {Kind: "agent", IsMain: true, ToolPackages: []string{"charts"}, Prompts: map[string]string{"static_instructions": prompt}}}}}
		got, warnings := migrateAgentConfigV1(input, nil)
		if !samePromptMigrationYAML(t, got.Runtime.Agents["main"], input.Runtime.Agents["main"]) || len(warnings) != 0 {
			t.Fatal("custom prompt or package choices were rewritten")
		}
	}
}

func TestKnownLegacyMainPromptV2PreviewDoesNotWriteExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "default", "agent.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	input := &AgentConfigFile{Version: AgentConfigVersionV20, Runtime: &RuntimeConfig{Agents: map[string]AgentDefData{"orchestrator": {Kind: "agent", IsMain: true, Enabled: true, Prompts: map[string]string{"static_instructions": legacyMainPromptFixture(t, "legacy_flow_start_prompt_v4.txt")}}}}}
	raw, _ := yaml.Marshal(input)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	manager := NewAgentConfigManager(dir)
	got, err := manager.GetConfigData("default")
	if err != nil {
		t.Fatal(err)
	}
	if got.Runtime.Agents["orchestrator"].Prompts["static_instructions"] != currentEmbeddedMainForPromptTest(t).Prompts["static_instructions"] || len(got.MigrationWarnings) != 1 {
		t.Fatal("loaded configuration did not receive the in-memory prompt upgrade")
	}
	preview, err := manager.PreviewMigration("default")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Changed || !preview.DryRun || len(preview.Warnings) != 1 || !reflect.DeepEqual(preview.Config, got) {
		t.Fatal("v2 preview did not report its known-prompt migration as a change")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(raw, after) {
		t.Fatal("loading overwrote the user's stored configuration")
	}
	files, _ := os.ReadDir(filepath.Dir(path))
	if len(files) != 1 || files[0].Name() != "agent.yaml" {
		t.Fatal("read-only loading created migration files")
	}
	if err := manager.SaveConfig("", "default", got); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewAgentConfigManager(dir).GetConfigData("default")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Runtime.Agents["orchestrator"].Prompts["static_instructions"] != currentEmbeddedMainForPromptTest(t).Prompts["static_instructions"] || len(reloaded.MigrationWarnings) != 0 {
		t.Fatal("normal save did not persist the upgraded prompt")
	}
	savedPreview, err := NewAgentConfigManager(dir).PreviewMigration("default")
	if err != nil {
		t.Fatal(err)
	}
	if savedPreview.Changed || !savedPreview.DryRun || len(savedPreview.Warnings) != 0 {
		t.Fatal("saved current v2 configuration should not report another migration change")
	}
}
