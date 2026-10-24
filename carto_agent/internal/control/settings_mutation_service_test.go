package control

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/settings"
)

type stubScopedSkillProvider struct {
	exclusiveDirs map[string]string
}

func (s stubScopedSkillProvider) FindSkillReferences(string) []config.SkillReference { return nil }

func (s stubScopedSkillProvider) ListExclusiveSkillDirs() map[string]string {
	cloned := make(map[string]string, len(s.exclusiveDirs))
	maps.Copy(cloned, s.exclusiveDirs)
	return cloned
}

type stubRuntimeController struct {
	activeRuntimeCalls int
	llmApplyCalls      int
	runtimeRevision    string
	activeConfig       string
}

type stubSnapshotRefresher struct {
	reasons []string
}

func (s *stubSnapshotRefresher) RequestRefresh(reason string) {
	s.reasons = append(s.reasons, reason)
}

func (s *stubRuntimeController) ApplyPlanModeConfig() error { return nil }
func (s *stubRuntimeController) ApplyLLMConfig() error {
	s.llmApplyCalls++
	return nil
}
func (s *stubRuntimeController) ApplyAgentConfig(configName string) (bool, error) {
	return false, nil
}
func (s *stubRuntimeController) ApplyActiveRuntime() error {
	s.activeRuntimeCalls++
	return nil
}
func (s *stubRuntimeController) GetRuntimeRevision() string { return s.runtimeRevision }
func (s *stubRuntimeController) GetRuntimeActiveConfigName() string {
	return s.activeConfig
}
func (s *stubRuntimeController) GetRuntimeModeIssues() map[string]settings.SettingsModeIssue {
	return nil
}

func TestSaveConfigRevision(t *testing.T) {
	tmpDir := t.TempDir()
	defaultPath := filepath.Join(tmpDir, "config.yaml")
	userPath := filepath.Join(tmpDir, "user.yaml")
	if err := os.WriteFile(defaultPath, []byte("llm:\n  active: \"\"\n  providers: []\n"), 0o644); err != nil {
		t.Fatalf("write default config failed: %v", err)
	}

	configService := config.NewConfigService(nil, &config.Config{
		Log: config.LogConfig{Level: "INFO"},
	}, defaultPath, userPath)
	runtime := &stubRuntimeController{runtimeRevision: "runtime-rev-1", activeConfig: "default"}
	service := NewSettingsMutationService(SettingsMutationServiceDeps{
		ConfigService:     configService,
		RuntimeController: runtime,
	})

	result, err := service.SaveConfig("log.level", "DEBUG", "")
	if err != nil {
		t.Fatalf("SaveConfig error: %v", err)
	}
	payload := mutationResultPayload(t, result)
	configRevision, _ := payload["config_revision"].(string)
	if strings.TrimSpace(configRevision) == "" {
		t.Fatalf("expected config_revision in mutation payload, got %#v", payload)
	}
	if configRevision == result.RuntimeRevision {
		t.Fatalf("expected config_revision independent from runtime_revision, got %#v", payload)
	}
	if result.RuntimeRevision != "runtime-rev-1" {
		t.Fatalf("expected runtime revision preserved, got %#v", result)
	}
}

func TestBatchSetConfigIdempotency(t *testing.T) {
	tmpDir := t.TempDir()
	defaultPath := filepath.Join(tmpDir, "config.yaml")
	userPath := filepath.Join(tmpDir, "user.yaml")
	if err := os.WriteFile(defaultPath, []byte("llm:\n  active: \"\"\n  providers: []\n"), 0o644); err != nil {
		t.Fatalf("write default config failed: %v", err)
	}

	configService := config.NewConfigService(nil, &config.Config{
		Log: config.LogConfig{Level: "INFO"},
	}, defaultPath, userPath)
	service := NewSettingsMutationService(SettingsMutationServiceDeps{
		ConfigService: configService,
	})

	items := []config.ConfigItem{{Path: "log.level", Value: "DEBUG"}}
	first, err := service.BatchSetConfig(items)
	if err != nil {
		t.Fatalf("first BatchSetConfig error: %v", err)
	}
	firstPayload := mutationResultPayload(t, first)
	firstRevision, _ := firstPayload["config_revision"].(string)
	if strings.TrimSpace(firstRevision) == "" {
		t.Fatalf("expected first config_revision, got %#v", firstPayload)
	}
	if idempotent, _ := firstPayload["idempotent"].(bool); idempotent {
		t.Fatalf("first mutation should not be idempotent, got %#v", firstPayload)
	}

	second, err := service.BatchSetConfig(items)
	if err != nil {
		t.Fatalf("second BatchSetConfig error: %v", err)
	}
	secondPayload := mutationResultPayload(t, second)
	if got, _ := secondPayload["config_revision"].(string); got != firstRevision {
		t.Fatalf("duplicate batch should preserve config_revision, got first=%q second=%q payload=%#v", firstRevision, got, secondPayload)
	}
	if idempotent, _ := secondPayload["idempotent"].(bool); !idempotent {
		t.Fatalf("duplicate batch should be marked idempotent, got %#v", secondPayload)
	}
	if persisted, _ := secondPayload["persisted"].(bool); persisted {
		t.Fatalf("duplicate batch should avoid redundant persistence, got %#v", secondPayload)
	}
}

func TestSwitchActiveLLMProviderUsesMutationPath(t *testing.T) {
	tmpDir := t.TempDir()
	defaultPath := filepath.Join(tmpDir, "config.yaml")
	userPath := filepath.Join(tmpDir, "user.yaml")
	if err := os.WriteFile(defaultPath, []byte("llm:\n  active: provider-a\n  providers:\n    - name: provider-a\n      provider: openai\n      model: gpt-4o-mini\n    - name: provider-b\n      provider: openai\n      model: gpt-4.1-mini\n"), 0o644); err != nil {
		t.Fatalf("write default config failed: %v", err)
	}

	configService := config.NewConfigService(nil, &config.Config{
		LLM: config.LLMConfig{
			Active: "provider-a",
			Providers: []config.LLMProviderConfig{
				{Name: "provider-a", Provider: "openai", Model: "gpt-4o-mini"},
				{Name: "provider-b", Provider: "openai", Model: "gpt-4.1-mini"},
			},
		},
	}, defaultPath, userPath)
	runtime := &stubRuntimeController{runtimeRevision: "runtime-rev-llm", activeConfig: "default"}
	service := NewSettingsMutationService(SettingsMutationServiceDeps{
		ConfigService:     configService,
		RuntimeController: runtime,
	})

	result, err := service.SwitchActiveLLMProvider("provider-b")
	if err != nil {
		t.Fatalf("SwitchActiveLLMProvider error: %v", err)
	}
	if runtime.llmApplyCalls != 1 {
		t.Fatalf("expected ApplyLLMConfig once, got %d", runtime.llmApplyCalls)
	}
	if !result.RuntimeApplied {
		t.Fatalf("expected runtime applied, got %#v", result)
	}
	payload := mutationResultPayload(t, result)
	if got, _ := payload["affected_domain"].(string); got != settings.MutationDomainModels {
		t.Fatalf("expected affected_domain=%q, got %#v", settings.MutationDomainModels, got)
	}
	if got, _ := payload["config_revision"].(string); strings.TrimSpace(got) == "" {
		t.Fatalf("expected config_revision in payload, got %#v", payload)
	}
	if got, _ := payload["runtime_revision"].(string); got != "runtime-rev-llm" {
		t.Fatalf("expected runtime_revision preserved, got %#v", payload)
	}
	llmConfigRaw, err := configService.GetConfig("llm", "")
	if err != nil {
		t.Fatalf("GetConfig llm error: %v", err)
	}
	llmConfig, ok := llmConfigRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected llm config map, got %#v", llmConfigRaw)
	}
	if got, _ := llmConfig["active"].(string); got != "provider-b" {
		t.Fatalf("expected active provider switched to provider-b, got %#v", got)
	}
}

func mutationResultPayload(t *testing.T, result settings.SettingsMutationResult) map[string]any {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal mutation result failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal mutation result failed: %v", err)
	}
	return payload
}

func TestSettingsMutationServiceToggleSkillPackageRebuildsRuntimeForCategory(t *testing.T) {
	skillsDir := t.TempDir()
	writePackageTestFile(t, filepath.Join(skillsDir, "demo-package", "SKILL.md"), `---
name: demo-package
description: demo package
kind: category
backend: compute
enabled: true
tools:
  - buffer
---

# demo
`)
	writePackageTestFile(t, filepath.Join(skillsDir, "tools", "user", "demo-package", "buffer.yaml"), `apiVersion: tool/v1
kind: Tool
metadata:
  name: buffer
  domain: vector-tools
  runtime: cli
spec:
  description: buffer
  parameters:
    - name: INPUT
      type: string
execution:
  backend_tool: buffer
  param_mapping:
    INPUT: '{{ INPUT }}'
`)

	skillCatalog := settings.NewFileSkillService(skillsDir, nil)
	runtime := &stubRuntimeController{runtimeRevision: "rev-1", activeConfig: "default"}
	service := NewSettingsMutationService(SettingsMutationServiceDeps{
		RuntimeController: runtime,
		SkillCatalog:      skillCatalog,
	})

	result, err := service.ToggleSkillPackage("demo-package", false)
	if err != nil {
		t.Fatalf("ToggleSkillPackage error: %v", err)
	}
	if !result.RuntimeApplied {
		t.Fatalf("expected runtime rebuild applied, got %#v", result)
	}
	if runtime.activeRuntimeCalls != 1 {
		t.Fatalf("expected ApplyActiveRuntime once, got %d", runtime.activeRuntimeCalls)
	}
	if result.RequiredAction != settings.MutationRequiredActionNone {
		t.Fatalf("expected no further action after apply, got %#v", result)
	}
	detail, err := skillCatalog.GetSkillPackage("demo-package")
	if err != nil {
		t.Fatalf("GetSkillPackage error: %v", err)
	}
	if detail.Enabled {
		t.Fatal("expected package to be disabled after toggle")
	}
}

func TestSettingsMutationServiceImportSkillHandlesExtensionManifest(t *testing.T) {
	globalDir := t.TempDir()
	configBaseDir := t.TempDir()
	sourcePackageDir := filepath.Join(t.TempDir(), "demo-extension-package")
	writePackageTestFile(t, filepath.Join(sourcePackageDir, "SKILL.md"), `---
name: demo-extension-package
description: demo package
kind: category
tools: [fixture_tool]
---

# demo
`)
	manifestPath := filepath.Join(t.TempDir(), "extension.yaml")
	writePackageTestFile(t, manifestPath, "apiVersion: extension/v1\nkind: skill-package\nmetadata:\n  name: demo-extension\nspec:\n  source_dir: "+sourcePackageDir+"\n  activation:\n    scope: global\n    owner_config: \"\"\n    enabled_by_default: false\n    rebuild_policy: auto\n")

	skillCatalog := settings.NewFileSkillService(globalDir, stubScopedSkillProvider{exclusiveDirs: map[string]string{"default": filepath.Join(configBaseDir, "agent_configs", "default", "skills")}})
	runtime := &stubRuntimeController{runtimeRevision: "rev-1", activeConfig: "default"}
	service := NewSettingsMutationService(SettingsMutationServiceDeps{
		RuntimeController:  runtime,
		SkillCatalog:       skillCatalog,
		AgentConfigManager: config.NewAgentConfigManager(filepath.Join(configBaseDir, "agent_configs")),
	})

	result, err := service.ImportSkill(manifestPath, false)
	if err != nil {
		t.Fatalf("ImportSkill error: %v", err)
	}
	imported, ok := result["imported"].([]map[string]any)
	if !ok || len(imported) != 1 {
		t.Fatalf("unexpected imported payload: %#v", result)
	}
	if imported[0]["name"] != "demo-extension-package" {
		t.Fatalf("unexpected imported package name: %#v", imported)
	}
	detail, err := skillCatalog.GetSkillPackage("demo-extension-package")
	if err != nil {
		t.Fatalf("GetSkillPackage error: %v", err)
	}
	if detail.Enabled {
		t.Fatal("expected imported extension package to honor enabled_by_default=false")
	}
	if runtime.activeRuntimeCalls != 0 {
		t.Fatalf("expected import without activation rebuild, got %d", runtime.activeRuntimeCalls)
	}
}

func TestSettingsMutationServiceImportSkillHandlesExclusiveExtensionManifest(t *testing.T) {
	globalDir := t.TempDir()
	configBaseDir := t.TempDir()
	exclusiveDir := filepath.Join(configBaseDir, "agent_configs", "default", "skills")
	sourcePackageDir := filepath.Join(t.TempDir(), "exclusive-package")
	writePackageTestFile(t, filepath.Join(sourcePackageDir, "SKILL.md"), `---
name: exclusive-package
description: exclusive package
kind: category
tools: [fixture_tool]
---

# exclusive
`)
	manifestPath := filepath.Join(t.TempDir(), "extension.yaml")
	writePackageTestFile(t, manifestPath, "apiVersion: extension/v1\nkind: skill-package\nmetadata:\n  name: exclusive-extension\nspec:\n  source_dir: "+sourcePackageDir+"\n  activation:\n    scope: exclusive\n    owner_config: default\n    enabled_by_default: false\n    rebuild_policy: auto\n")

	skillCatalog := settings.NewFileSkillService(globalDir, stubScopedSkillProvider{exclusiveDirs: map[string]string{"default": exclusiveDir}})
	service := NewSettingsMutationService(SettingsMutationServiceDeps{
		SkillCatalog:       skillCatalog,
		AgentConfigManager: config.NewAgentConfigManager(filepath.Join(configBaseDir, "agent_configs")),
	})

	result, err := service.ImportSkill(manifestPath, false)
	if err != nil {
		t.Fatalf("ImportSkill error: %v", err)
	}
	imported, ok := result["imported"].([]map[string]any)
	if !ok || len(imported) != 1 {
		t.Fatalf("unexpected imported payload: %#v", result)
	}
	if imported[0]["scope"] != "exclusive" || imported[0]["owner_config"] != "default" {
		t.Fatalf("unexpected exclusive import payload: %#v", imported)
	}
	detail, err := skillCatalog.GetSkillPackageByScope("exclusive-package", settings.SkillPackageScopeExclusive, "default")
	if err != nil {
		t.Fatalf("GetSkillPackageByScope error: %v", err)
	}
	if detail.Enabled {
		t.Fatal("expected exclusive imported package to honor enabled_by_default=false")
	}
}

func TestSettingsMutationServiceImportSkillHandlesExtensionV2ToolAndSkillResources(t *testing.T) {
	globalDir := t.TempDir()
	configBaseDir := t.TempDir()
	sourceSkillDir := filepath.Join(t.TempDir(), "hydrology-skill")
	sourceToolDir := filepath.Join(t.TempDir(), "hydrology-tools")
	writePackageTestFile(t, filepath.Join(sourceSkillDir, "SKILL.md"), `---
name: hydrology-skill
description: hydrology package
kind: category
backend: qgis
tool_refs:
  - stream_order
---

# hydrology
`)
	writePackageTestFile(t, filepath.Join(sourceToolDir, "analysis", "stream_order", "tool.yaml"), `apiVersion: tool-package/v1
kind: ToolPackage
metadata:
  name: stream_order
  domain: analysis
  runtime: qgis
spec:
  description: stream order
  parameters:
    - name: input
      type: string
execution:
  provider: qgis
  binding: handler
  handler: handlers.analysis.stream_order
`)
	manifestPath := filepath.Join(t.TempDir(), "extension.yaml")
	writePackageTestFile(t, manifestPath, "apiVersion: extension/v2\nkind: PackageImport\nmetadata:\n  name: hydrology-pack\nspec:\n  resources:\n    - type: tool_package\n      source_dir: "+sourceToolDir+"\n    - type: skill_package\n      source_dir: "+sourceSkillDir+"\n  activation:\n    scope: global\n    owner_config: \"\"\n    enabled_by_default: true\n    rebuild_policy: immediate\n")

	skillCatalog := settings.NewFileSkillService(globalDir, stubScopedSkillProvider{exclusiveDirs: map[string]string{"default": filepath.Join(configBaseDir, "agent_configs", "default", "skills")}})
	runtime := &stubRuntimeController{runtimeRevision: "rev-1", activeConfig: "default"}
	service := NewSettingsMutationService(SettingsMutationServiceDeps{
		RuntimeController:  runtime,
		SkillCatalog:       skillCatalog,
		AgentConfigManager: config.NewAgentConfigManager(filepath.Join(configBaseDir, "agent_configs")),
	})

	result, err := service.ImportSkill(manifestPath, false)
	if err != nil {
		t.Fatalf("ImportSkill error: %v", err)
	}
	imported, ok := result["imported"].([]map[string]any)
	if !ok || len(imported) != 2 {
		t.Fatalf("unexpected imported payload: %#v", result)
	}
	seen := map[string]string{}
	for _, item := range imported {
		name, _ := item["name"].(string)
		resourceType, _ := item["type"].(string)
		seen[name] = resourceType
	}
	if seen["hydrology-tools"] != "tool_package" || seen["hydrology-skill"] != "skill_package" {
		t.Fatalf("unexpected imported resources: %#v", imported)
	}
	if _, err := os.Stat(filepath.Join(config.ResolveGlobalToolsDir(configBaseDir), "hydrology-tools", "analysis", "stream_order", "tool.yaml")); err != nil {
		t.Fatalf("expected imported global tool resource: %v", err)
	}
	detail, err := skillCatalog.GetSkillPackage("hydrology-skill")
	if err != nil {
		t.Fatalf("GetSkillPackage error: %v", err)
	}
	if !detail.Enabled {
		t.Fatal("expected imported v2 skill package enabled by default")
	}
	mutation, ok := result["mutation"].(settings.SettingsMutationResult)
	if !ok {
		t.Fatalf("expected mutation payload, got %#v", result["mutation"])
	}
	if !mutation.RuntimeApplied || runtime.activeRuntimeCalls != 1 {
		t.Fatalf("expected runtime rebuild for v2 extension import, mutation=%#v calls=%d", mutation, runtime.activeRuntimeCalls)
	}
}

func TestSettingsMutationServiceImportSkillRebuildsRuntimeForEnabledCategoryExtension(t *testing.T) {
	globalDir := t.TempDir()
	configBaseDir := t.TempDir()
	sourcePackageDir := filepath.Join(t.TempDir(), "category-package")
	writePackageTestFile(t, filepath.Join(sourcePackageDir, "SKILL.md"), `---
name: category-package
description: category package
kind: category
backend: compute
tools:
  - buffer
---

# category
`)
	writePackageTestFile(t, filepath.Join(sourcePackageDir, "tools", "user", "category-package", "buffer.yaml"), `apiVersion: tool/v1
kind: Tool
metadata:
  name: buffer
  domain: vector-tools
  runtime: cli
spec:
  description: buffer
  parameters:
    - name: INPUT
      type: string
execution:
  backend_tool: buffer
  param_mapping:
    INPUT: '{{ INPUT }}'
`)
	manifestPath := filepath.Join(t.TempDir(), "extension.yaml")
	writePackageTestFile(t, manifestPath, "apiVersion: extension/v1\nkind: skill-package\nmetadata:\n  name: category-extension\nspec:\n  source_dir: "+sourcePackageDir+"\n  activation:\n    scope: global\n    owner_config: \"\"\n    enabled_by_default: true\n    rebuild_policy: auto\n")

	skillCatalog := settings.NewFileSkillService(globalDir, stubScopedSkillProvider{exclusiveDirs: map[string]string{"default": filepath.Join(configBaseDir, "agent_configs", "default", "skills")}})
	runtime := &stubRuntimeController{runtimeRevision: "rev-1", activeConfig: "default"}
	service := NewSettingsMutationService(SettingsMutationServiceDeps{
		RuntimeController:  runtime,
		SkillCatalog:       skillCatalog,
		AgentConfigManager: config.NewAgentConfigManager(filepath.Join(configBaseDir, "agent_configs")),
	})

	result, err := service.ImportSkill(manifestPath, false)
	if err != nil {
		t.Fatalf("ImportSkill error: %v", err)
	}
	mutation, ok := result["mutation"].(settings.SettingsMutationResult)
	if !ok {
		t.Fatalf("expected mutation payload, got %#v", result["mutation"])
	}
	if !mutation.RuntimeApplied || runtime.activeRuntimeCalls != 1 {
		t.Fatalf("expected runtime rebuild for enabled category extension, mutation=%#v calls=%d", mutation, runtime.activeRuntimeCalls)
	}
	detail, err := skillCatalog.GetSkillPackage("category-package")
	if err != nil {
		t.Fatalf("GetSkillPackage error: %v", err)
	}
	if !detail.Enabled {
		t.Fatal("expected category extension package to remain enabled by default")
	}
}

func TestSettingsMutationServiceImportSkillRegistersExtensionMetadataAndRequestsSnapshotRefresh(t *testing.T) {
	globalDir := t.TempDir()
	configBaseDir := t.TempDir()
	sourcePackageDir := filepath.Join(t.TempDir(), "demo-extension-package")
	writePackageTestFile(t, filepath.Join(sourcePackageDir, "SKILL.md"), `---
name: demo-extension-package
description: demo package
kind: category
tools: [fixture_tool]
---

# demo
`)
	manifestPath := filepath.Join(t.TempDir(), "extension.yaml")
	writePackageTestFile(t, manifestPath, "apiVersion: extension/v1\nkind: skill-package\nmetadata:\n  name: demo-extension\n  display_name: Demo Extension\n  version: 1.0.0\nspec:\n  source_dir: "+sourcePackageDir+"\n  activation:\n    scope: global\n    owner_config: \"\"\n    enabled_by_default: false\n    rebuild_policy: auto\n")
	refresher := &stubSnapshotRefresher{}
	skillCatalog := settings.NewFileSkillService(globalDir, stubScopedSkillProvider{exclusiveDirs: map[string]string{"default": filepath.Join(configBaseDir, "agent_configs", "default", "skills")}})
	service := NewSettingsMutationService(SettingsMutationServiceDeps{
		SkillCatalog:       skillCatalog,
		AgentConfigManager: config.NewAgentConfigManager(filepath.Join(configBaseDir, "agent_configs")),
		SnapshotRefresher:  refresher,
	})

	_, err := service.ImportSkill(manifestPath, false)
	if err != nil {
		t.Fatalf("ImportSkill error: %v", err)
	}
	metadataPath := filepath.Join(configBaseDir, "extensions", "demo-extension.yaml")
	if _, err := os.Stat(metadataPath); err != nil {
		t.Fatalf("expected extension metadata file: %v", err)
	}
	if len(refresher.reasons) != 1 || refresher.reasons[0] != "extension_import:demo-extension" {
		t.Fatalf("unexpected snapshot refresh reasons: %#v", refresher.reasons)
	}
}

func TestSettingsMutationServiceImportSkillRejectsInvalidExtensionManifest(t *testing.T) {
	globalDir := t.TempDir()
	configBaseDir := t.TempDir()
	manifestPath := filepath.Join(t.TempDir(), "extension.yaml")
	writePackageTestFile(t, manifestPath, "apiVersion: extension/v1\nkind: skill-package\nmetadata:\n  name: broken-extension\nspec:\n  activation:\n    scope: global\n")

	skillCatalog := settings.NewFileSkillService(globalDir, stubScopedSkillProvider{exclusiveDirs: map[string]string{"default": filepath.Join(configBaseDir, "agent_configs", "default", "skills")}})
	service := NewSettingsMutationService(SettingsMutationServiceDeps{
		SkillCatalog:       skillCatalog,
		AgentConfigManager: config.NewAgentConfigManager(filepath.Join(configBaseDir, "agent_configs")),
	})

	_, err := service.ImportSkill(manifestPath, false)
	if err == nil {
		t.Fatal("expected invalid extension manifest error")
	}
	if !strings.Contains(err.Error(), "extension spec.source_dir is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSettingsMutationServiceSaveSkillWithScopeWritesExclusiveTarget(t *testing.T) {
	globalDir := t.TempDir()
	exclusiveDir := t.TempDir()
	skillCatalog := settings.NewFileSkillService(globalDir, stubScopedSkillProvider{exclusiveDirs: map[string]string{"default": exclusiveDir}})
	service := NewSettingsMutationService(SettingsMutationServiceDeps{SkillCatalog: skillCatalog})

	result, err := service.SaveSkillWithScope("exclusive-package", settings.SkillPackageScopeExclusive, "default", "exclusive package", "# exclusive")
	if err != nil {
		t.Fatalf("SaveSkillWithScope error: %v", err)
	}
	if len(result.AffectedPaths) != 1 || result.AffectedPaths[0] != buildSkillAffectedPath("exclusive-package") {
		t.Fatalf("unexpected mutation result: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "exclusive-package", "SKILL.md")); err != nil {
		t.Fatalf("expected scoped save target in exclusive dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "exclusive-package", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("expected global dir untouched, stat err=%v", err)
	}
}

func TestSettingsMutationServiceSaveSkillDefaultsToGlobalTargetWithExclusiveOverlayPresent(t *testing.T) {
	globalDir := t.TempDir()
	exclusiveDir := t.TempDir()
	skillCatalog := settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{
		SkillsDir:        globalDir,
		RuntimeSkillDirs: []string{globalDir, exclusiveDir},
		ActiveConfigName: "default",
		ResolveExclusiveSkillDir: func(configName string) string {
			if configName == "default" {
				return exclusiveDir
			}
			return ""
		},
	}, stubScopedSkillProvider{exclusiveDirs: map[string]string{"default": exclusiveDir}})
	service := NewSettingsMutationService(SettingsMutationServiceDeps{SkillCatalog: skillCatalog})

	result, err := service.SaveSkill("global-default-package", "global default package", "# global default")
	if err != nil {
		t.Fatalf("SaveSkill error: %v", err)
	}
	if len(result.AffectedPaths) != 1 || result.AffectedPaths[0] != buildSkillAffectedPath("global-default-package") {
		t.Fatalf("unexpected mutation result: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "global-default-package", "SKILL.md")); err != nil {
		t.Fatalf("expected default save target in global dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "global-default-package", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("expected exclusive dir untouched by default save, stat err=%v", err)
	}
}

func TestSettingsMutationServiceDeleteSkillWithScopeRemovesExclusiveOverrideOnly(t *testing.T) {
	globalDir := t.TempDir()
	exclusiveDir := t.TempDir()
	writePackageTestFile(t, filepath.Join(globalDir, "shared-package", "SKILL.md"), `---
name: shared-package
description: global package
kind: workflow
---

# global
`)
	writePackageTestFile(t, filepath.Join(exclusiveDir, "shared-package", "SKILL.md"), `---
name: shared-package
description: exclusive package
kind: workflow
---

# exclusive
`)
	skillCatalog := settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{
		SkillsDir:        globalDir,
		RuntimeSkillDirs: []string{globalDir, exclusiveDir},
	}, stubScopedSkillProvider{exclusiveDirs: map[string]string{"default": exclusiveDir}})
	service := NewSettingsMutationService(SettingsMutationServiceDeps{SkillCatalog: skillCatalog})

	_, err := service.DeleteSkillWithScope("shared-package", settings.SkillPackageScopeExclusive, "default")
	if err != nil {
		t.Fatalf("DeleteSkillWithScope error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "shared-package")); !os.IsNotExist(err) {
		t.Fatalf("expected exclusive override removed, stat err=%v", err)
	}
	detail, err := skillCatalog.GetSkillPackage("shared-package")
	if err != nil {
		t.Fatalf("GetSkillPackage error: %v", err)
	}
	if detail.Description != "global package" {
		t.Fatalf("expected lower layer to remain visible, got %#v", detail)
	}
}

func TestSettingsMutationServiceImportSkillWithScopeWritesExclusiveTarget(t *testing.T) {
	globalDir := t.TempDir()
	exclusiveDir := t.TempDir()
	sourcePath := filepath.Join(t.TempDir(), "exclusive-import.md")
	writePackageTestFile(t, sourcePath, `---
name: imported-exclusive-package
description: imported package
kind: category
tools: [fixture_tool]
---

# imported
`)
	skillCatalog := settings.NewFileSkillService(globalDir, stubScopedSkillProvider{exclusiveDirs: map[string]string{"default": exclusiveDir}})
	service := NewSettingsMutationService(SettingsMutationServiceDeps{SkillCatalog: skillCatalog})

	result, err := service.ImportSkillWithScope(sourcePath, false, settings.SkillPackageScopeExclusive, "default")
	if err != nil {
		t.Fatalf("ImportSkillWithScope error: %v", err)
	}
	imported, ok := result["imported"].([]map[string]any)
	if !ok || len(imported) != 1 {
		t.Fatalf("unexpected import payload: %#v", result)
	}
	target, _ := imported[0]["target"].(string)
	if !strings.HasPrefix(target, exclusiveDir) {
		t.Fatalf("expected scoped import target under exclusive dir, got %q", target)
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "imported-exclusive-package", "SKILL.md")); err != nil {
		t.Fatalf("expected imported package in exclusive dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "imported-exclusive-package", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("expected global dir untouched, stat err=%v", err)
	}
}

func TestSettingsMutationServiceImportSkillDefaultsToGlobalTargetWithExclusiveOverlayPresent(t *testing.T) {
	globalDir := t.TempDir()
	exclusiveDir := t.TempDir()
	sourcePath := filepath.Join(t.TempDir(), "global-import.md")
	writePackageTestFile(t, sourcePath, `---
name: imported-global-package
description: imported package
kind: category
tools: [fixture_tool]
---

# imported
`)
	skillCatalog := settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{
		SkillsDir:        globalDir,
		RuntimeSkillDirs: []string{globalDir, exclusiveDir},
		ActiveConfigName: "default",
		ResolveExclusiveSkillDir: func(configName string) string {
			if configName == "default" {
				return exclusiveDir
			}
			return ""
		},
	}, stubScopedSkillProvider{exclusiveDirs: map[string]string{"default": exclusiveDir}})
	service := NewSettingsMutationService(SettingsMutationServiceDeps{SkillCatalog: skillCatalog})

	result, err := service.ImportSkill(sourcePath, false)
	if err != nil {
		t.Fatalf("ImportSkill error: %v", err)
	}
	imported, ok := result["imported"].([]map[string]any)
	if !ok || len(imported) != 1 {
		t.Fatalf("unexpected import payload: %#v", result)
	}
	target, _ := imported[0]["target"].(string)
	if !strings.HasPrefix(target, globalDir) {
		t.Fatalf("expected default import target under global dir, got %q", target)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "imported-global-package", "SKILL.md")); err != nil {
		t.Fatalf("expected imported package in global dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "imported-global-package", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("expected exclusive dir untouched by default import, stat err=%v", err)
	}
}

func writePackageTestFile(t *testing.T, path string, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir error: %v", err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write file error: %v", err)
	}
}
