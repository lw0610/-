package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	_ "unsafe"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/logger"
	bridgepkg "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
)

//go:linkname testDefaultLogger github.com/autocarto/carto_agent/internal/logger.defaultLogger
var testDefaultLogger *logger.Logger

//go:linkname testLoggerOnce github.com/autocarto/carto_agent/internal/logger.once
var testLoggerOnce sync.Once

func resetDefaultLoggerForTest(t *testing.T) {
	t.Helper()
	if testDefaultLogger != nil {
		_ = testDefaultLogger.Close()
	}
	testDefaultLogger = nil
	testLoggerOnce = sync.Once{}
}

type stubSkillReferenceProvider struct {
	refs          map[string][]config.SkillReference
	exclusiveDirs map[string]string
}

func TestDeprecatedMethodPackagesHiddenFromListsButRemainReadable(t *testing.T) {
	for _, governed := range []bool{false, true} {
		t.Run(fmt.Sprintf("governed=%t", governed), func(t *testing.T) {
			root := t.TempDir()
			writePackageFile(t, root, "old-method", "SKILL.md", "---\nname: old-method\nkind: workflow\ndescription: 旧方法\ndeprecated: true\n---\n方法正文\n")
			writePackageFile(t, root, "current", "SKILL.md", "---\nname: current\nkind: category\ndescription: 新工具包\n---\n方法正文\n")
			cfg := FileSkillServiceConfig{UserSkillsDir: root, GlobalSkillsDir: root, SkillsDir: root}
			if governed {
				governance := &stubRuntimeRegistryGovernance{skillDirs: []string{root}}
				cfg.ResolveRuntimeRegistryGovernance = func() RuntimeRegistryGovernance { return governance }
			}
			svc := NewFileSkillServiceWithConfig(cfg, nil)
			items, err := svc.ListSkillPackages()
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 || items[0].Name != "current" {
				t.Fatalf("deprecated method listed: %#v", items)
			}
			items, err = svc.ListSkillPackagesByScope(SkillPackageScopeGlobal, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 || items[0].Name != "current" {
				t.Fatalf("deprecated method in scoped list: %#v", items)
			}
			detail, err := svc.GetSkillPackage("old-method")
			if err != nil {
				t.Fatal(err)
			}
			if !detail.Deprecated || !detail.FrontMatter.Deprecated || detail.MarkdownBody == "" {
				t.Fatalf("migration detail lost: %#v", detail)
			}
		})
	}
}

type stubRuntimeRegistryGovernance struct {
	dirs                  []string
	skillDirs             []string
	returnEmptySkillReg   bool
	bridgeDynamicRegistry *skill.BridgeDynamicRegistry
	resolvedSkillPkg      skill.SkillPackage
	resolvedSkillOK       bool
	resolvedSkillErr      error
	resolveSkillCalls     []string
	toolLoadCount         *int
}

func (s stubRuntimeRegistryGovernance) LoadToolRegistry() (*toolpkg.Registry, error) {
	if s.toolLoadCount != nil {
		(*s.toolLoadCount)++
	}
	registry := toolpkg.NewRegistry()
	if len(s.dirs) > 0 {
		if err := registry.LoadDirectories(s.dirs...); err != nil {
			return nil, err
		}
	}
	if s.bridgeDynamicRegistry != nil {
		s.bridgeDynamicRegistry.ApplyToRegistry(registry)
	}
	if len(registry.All()) == 0 {
		return nil, nil
	}
	return registry, nil
}

func TestFileSkillServiceListSkillPackagesSharesToolRegistryWithinListing(t *testing.T) {
	skillsDir := t.TempDir()
	for _, name := range []string{"first", "second", "third", "fourth"} {
		writePackageFile(t, skillsDir, name, "SKILL.md", "---\nname: "+name+"\ndescription: test\nkind: workflow\n---\n")
	}
	loads := 0
	governance := &stubRuntimeRegistryGovernance{skillDirs: []string{skillsDir}, toolLoadCount: &loads}
	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		UserSkillsDir:                    skillsDir,
		ResolveRuntimeRegistryGovernance: func() RuntimeRegistryGovernance { return governance },
	}, nil)
	packages, err := svc.ListSkillPackages()
	if err != nil || len(packages) != 4 {
		t.Fatalf("initial listing: packages=%#v err=%v", packages, err)
	}
	// One load builds the governed skill registry; one serves every detail.
	if loads != 2 {
		t.Fatalf("expected two registry loads independent of package count, got %d", loads)
	}
	writePackageFile(t, skillsDir, "first", "config.yaml", "enabled: false\n")
	writePackageFile(t, skillsDir, "new-package", "SKILL.md", "---\nname: new-package\ndescription: new\nkind: workflow\n---\n")
	if err := os.RemoveAll(filepath.Join(skillsDir, "second")); err != nil {
		t.Fatal(err)
	}
	packages, err = svc.ListSkillPackages()
	if err != nil || len(packages) != 4 {
		t.Fatalf("updated listing: packages=%#v err=%v", packages, err)
	}
	if loads != 4 {
		t.Fatalf("expected fresh registries for the next listing, got %d loads", loads)
	}
	seen := make(map[string]SkillPackageSummary)
	for _, pkg := range packages {
		seen[pkg.PackageID] = pkg
	}
	if pkg, ok := seen["first"]; !ok || pkg.Enabled {
		t.Fatalf("updated disabled configuration not observed: %#v", seen)
	}
	if _, ok := seen["new-package"]; !ok {
		t.Fatal("new package not observed")
	}
	if _, ok := seen["second"]; ok {
		t.Fatal("removed package remains visible")
	}
}

func (s stubRuntimeRegistryGovernance) LoadSkillRegistry() (*skill.Registry, error) {
	if s.returnEmptySkillReg {
		toolRegistry, err := s.LoadToolRegistry()
		if err != nil {
			return nil, err
		}
		return skill.NewRegistry(toolRegistry), nil
	}
	if len(s.skillDirs) == 0 {
		return nil, nil
	}
	toolRegistry, err := s.LoadToolRegistry()
	if err != nil {
		return nil, err
	}
	registry := skill.NewRegistry(toolRegistry)
	if len(s.skillDirs) > 0 {
		if err := registry.LoadDirectories(s.skillDirs...); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (s *stubRuntimeRegistryGovernance) ResolveSkillPackage(packageName string) (skill.SkillPackage, bool, error) {
	s.resolveSkillCalls = append(s.resolveSkillCalls, packageName)
	if s.resolvedSkillErr != nil || s.resolvedSkillOK || strings.TrimSpace(s.resolvedSkillPkg.Dir) != "" || strings.TrimSpace(s.resolvedSkillPkg.Name) != "" {
		return s.resolvedSkillPkg, s.resolvedSkillOK, s.resolvedSkillErr
	}
	registry, err := s.LoadSkillRegistry()
	if err != nil {
		return skill.SkillPackage{}, false, err
	}
	if registry == nil {
		return skill.SkillPackage{}, false, nil
	}
	pkg, ok := registry.GetPackage(packageName)
	return pkg, ok, nil
}

func (s stubSkillReferenceProvider) FindSkillReferences(skillName string) []config.SkillReference {
	if s.refs == nil {
		return nil
	}
	items := s.refs[skillName]
	cloned := make([]config.SkillReference, len(items))
	copy(cloned, items)
	return cloned
}

func (s stubSkillReferenceProvider) ListExclusiveSkillDirs() map[string]string {
	if s.exclusiveDirs == nil {
		return nil
	}
	cloned := make(map[string]string, len(s.exclusiveDirs))
	for configName, dir := range s.exclusiveDirs {
		cloned[configName] = dir
	}
	return cloned
}

func TestFileSkillServiceGetSkillPackageIncludesToolsConfigAndReferences(t *testing.T) {
	skillsDir := t.TempDir()
	toolsDir := t.TempDir()
	writePackageFile(t, skillsDir, "demo-package", "SKILL.md", `---
name: demo-package
description: demo package
kind: category
backend: compute
tags: [demo]
tools:
  - buffer
---

# demo
`)
	writePackageFile(t, skillsDir, "demo-package", "config.yaml", "enabled: false\nowner: qa\n")
	writeToolFile(t, toolsDir, filepath.Join("user", "vector-tools", "buffer.yaml"), `apiVersion: tool/v1
kind: Tool
metadata:
  name: buffer
  domain: vector-tools
  runtime: qgis
spec:
  description: buffer
execution:
  backend_tool: buffer
  param_mapping:
    INPUT: '{{ INPUT }}'
`)

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{UserSkillsDir: skillsDir, UserToolsDir: toolsDir, ResolveRuntimeRegistryGovernance: func() RuntimeRegistryGovernance {
		return &stubRuntimeRegistryGovernance{dirs: []string{toolsDir}, skillDirs: []string{skillsDir}}
	}}, stubSkillReferenceProvider{refs: map[string][]config.SkillReference{
		"demo-package": {{ConfigName: "default", AgentName: "planner", Layer: "runtime/private", Source: "skills"}},
	}})

	detail, err := svc.GetSkillPackage("demo-package")
	if err != nil {
		t.Fatalf("GetSkillPackage error: %v", err)
	}
	if detail.PackageID != "demo-package" || detail.Kind != skill.SkillPackageKindCategory {
		t.Fatalf("unexpected package identity: %#v", detail)
	}
	if detail.Enabled {
		t.Fatal("expected config override to disable package")
	}
	if !detail.HasConfig || !detail.HasTools || detail.ToolCount != 1 {
		t.Fatalf("unexpected package flags: %#v", detail.SkillPackageSummary)
	}
	if detail.ToolRefCount != 1 {
		t.Fatalf("expected tool_ref_count=1, got %#v", detail.SkillPackageSummary)
	}
	if len(detail.ToolRefs) != 1 || detail.ToolRefs[0] != "buffer" {
		t.Fatalf("unexpected tool refs: %#v", detail.ToolRefs)
	}
	if len(detail.Tools) != 0 {
		t.Fatalf("expected public skill detail to omit hydrated tools, got %#v", detail.Tools)
	}
	if len(detail.References) != 1 || detail.References[0].AgentName != "planner" {
		t.Fatalf("unexpected references: %#v", detail.References)
	}
	if detail.Config["owner"] != "qa" {
		t.Fatalf("unexpected config payload: %#v", detail.Config)
	}

	legacy, err := svc.GetSkillContent("demo-package")
	if err != nil {
		t.Fatalf("GetSkillContent error: %v", err)
	}
	if legacy["kind"] != skill.SkillPackageKindCategory {
		t.Fatalf("unexpected legacy kind: %#v", legacy["kind"])
	}
	if legacy["tool_ref_count"] != 1 {
		t.Fatalf("unexpected legacy tool_ref_count: %#v", legacy["tool_ref_count"])
	}
	if _, ok := legacy["tools"]; ok {
		t.Fatalf("expected legacy skill detail to omit embedded tools payload, got %#v", legacy["tools"])
	}
}

func TestSkillPackageDetailJSONOmitsEmbeddedToolsField(t *testing.T) {
	detail := SkillPackageDetail{
		SkillPackageSummary: SkillPackageSummary{PackageID: "demo-package", Name: "demo-package"},
		ToolRefs:            []string{"buffer"},
		Tools:               []skill.SkillDefinition{{Metadata: skill.SkillMetadata{Name: "buffer"}}},
	}

	raw, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(raw), `"tools"`) {
		t.Fatalf("expected public skill package detail json to omit embedded tools field, got %s", raw)
	}
}

func TestFileSkillServiceSaveSkillPreservesCategoryFrontMatterAndTools(t *testing.T) {
	skillsDir := t.TempDir()
	toolsDir := t.TempDir()
	writePackageFile(t, skillsDir, "demo-package", "SKILL.md", `---
name: demo-package
description: old description
kind: category
backend: compute
tags: [demo]
tools:
  - buffer
---

# old body
`)
	writeToolFile(t, toolsDir, filepath.Join("user", "vector-tools", "buffer.yaml"), `apiVersion: tool/v1
kind: Tool
metadata:
  name: buffer
  domain: vector-tools
  runtime: qgis
spec:
  description: buffer
execution:
  backend_tool: buffer
  param_mapping:
    INPUT: '{{ INPUT }}'
`)

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{UserSkillsDir: skillsDir, UserToolsDir: toolsDir, ResolveRuntimeRegistryGovernance: func() RuntimeRegistryGovernance {
		return &stubRuntimeRegistryGovernance{dirs: []string{toolsDir}, skillDirs: []string{skillsDir}}
	}}, nil)
	if err := svc.SaveSkill("demo-package", "new description", "# new body"); err != nil {
		t.Fatalf("SaveSkill error: %v", err)
	}

	detail, err := svc.GetSkillPackage("demo-package")
	if err != nil {
		t.Fatalf("GetSkillPackage error: %v", err)
	}
	if detail.Kind != skill.SkillPackageKindCategory || detail.Backend != skill.RuntimeIDCLI {
		t.Fatalf("unexpected preserved metadata: %#v", detail.SkillPackageSummary)
	}
	if detail.Description != "new description" || detail.MarkdownBody != "# new body" {
		t.Fatalf("unexpected saved content: %#v", detail)
	}
	if len(detail.ToolRefs) != 1 || detail.ToolRefs[0] != "buffer" {
		t.Fatalf("expected tool refs preserved, got %#v", detail.ToolRefs)
	}
	if len(detail.Tools) != 0 {
		t.Fatalf("expected public skill detail to omit hydrated tools, got %#v", detail.Tools)
	}
}

func TestFileSkillServiceImportSkillRejectsPackageWithTools(t *testing.T) {
	skillsDir := t.TempDir()
	sourceDir := filepath.Join(t.TempDir(), "source-package")
	writeRawPackageFile(t, filepath.Join(sourceDir, "SKILL.md"), `---
name: imported-category
description: imported package
kind: category
---

# imported
`)
	writeRawPackageFile(t, filepath.Join(sourceDir, "tools", "buffer.yaml"), `apiVersion: skill/v1
kind: AtomicSkill
metadata:
  name: buffer
  backend: compute
spec:
  description: buffer
execution:
  backend_tool: buffer
  param_mapping:
    INPUT: '{{ INPUT }}'
`)

	svc := NewFileSkillService(skillsDir, nil)
	_, err := svc.ImportSkill(sourceDir, false)
	if err == nil {
		t.Fatal("expected ImportSkill to reject category package")
	}
	var unsupportedErr *SkillMutationUnsupportedError
	if !errors.As(err, &unsupportedErr) {
		t.Fatalf("expected SkillMutationUnsupportedError, got %v", err)
	}
}

func TestFileSkillServiceImportSkillDefaultsToUserWritableDirEvenWithActiveExclusive(t *testing.T) {
	builtinDir := t.TempDir()
	userDir := t.TempDir()
	exclusiveDir := t.TempDir()
	sourceDir := t.TempDir()
	sourceFile := filepath.Join(sourceDir, "import-me.md")
	writeRawPackageFile(t, sourceFile, `---
name: imported-exclusive-skill
description: imported from file
kind: category
tools: [fixture_tool]
---

# imported
`)

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		BuiltinSkillsDir: builtinDir,
		UserSkillsDir:    userDir,
		RuntimeSkillDirs: []string{builtinDir, userDir, exclusiveDir},
		ActiveConfigName: "default",
		ResolveExclusiveSkillDir: func(configName string) string {
			if configName == "default" {
				return exclusiveDir
			}
			return ""
		},
	}, stubSkillReferenceProvider{exclusiveDirs: map[string]string{}})

	result, err := svc.ImportSkill(sourceFile, false)
	if err != nil {
		t.Fatalf("ImportSkill error: %v", err)
	}
	imported, ok := result["imported"].([]map[string]any)
	if !ok || len(imported) != 1 {
		t.Fatalf("unexpected imported payload: %#v", result)
	}
	target, _ := imported[0]["target"].(string)
	if !strings.HasPrefix(target, userDir) {
		t.Fatalf("expected import target under user writable dir, got %q", target)
	}
	if _, err := os.Stat(filepath.Join(userDir, "imported-exclusive-skill", "SKILL.md")); err != nil {
		t.Fatalf("expected imported skill in user writable dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "imported-exclusive-skill", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("expected exclusive dir untouched, stat err=%v", err)
	}
}

func TestFileSkillServiceSaveSkillDefaultsToUserWritableDirBeforeExclusiveDirectoryExists(t *testing.T) {
	builtinDir := t.TempDir()
	userDir := t.TempDir()
	exclusiveDir := filepath.Join(t.TempDir(), "agent_configs", "default", "skills")

	writePackageFile(t, builtinDir, "shared-package", "SKILL.md", `---
name: shared-package
description: defaults package
kind: workflow
---

	# defaults package
`)

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		BuiltinSkillsDir: builtinDir,
		UserSkillsDir:    userDir,
		RuntimeSkillDirs: []string{builtinDir, userDir},
		ActiveConfigName: "default",
		ResolveExclusiveSkillDir: func(configName string) string {
			if configName == "default" {
				return exclusiveDir
			}
			return ""
		},
	}, stubSkillReferenceProvider{exclusiveDirs: map[string]string{}})

	if err := svc.SaveSkill("shared-package", "updated package", "# updated package"); err != nil {
		t.Fatalf("SaveSkill error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(userDir, "shared-package", "SKILL.md")); err != nil {
		t.Fatalf("expected save target in user writable dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "shared-package", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("expected exclusive dir untouched, stat err=%v", err)
	}
}

func TestFileSkillServiceSetSkillPackageEnabledWritesConfig(t *testing.T) {
	userDir := t.TempDir()
	writePackageFile(t, userDir, "demo-package", "SKILL.md", `---
name: demo-package
description: demo package
kind: workflow
---

	# demo
`)

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{UserSkillsDir: userDir}, nil)
	if err := svc.SetSkillPackageEnabled("demo-package", false); err != nil {
		t.Fatalf("SetSkillPackageEnabled error: %v", err)
	}
	detail, err := svc.GetSkillPackage("demo-package")
	if err != nil {
		t.Fatalf("GetSkillPackage error: %v", err)
	}
	if detail.Enabled {
		t.Fatal("expected package to be disabled")
	}
	configRaw, err := os.ReadFile(filepath.Join(userDir, "demo-package", "config.yaml"))
	if err != nil {
		t.Fatalf("read config error: %v", err)
	}
	if string(configRaw) != "enabled: false\n" {
		t.Fatalf("unexpected config content: %q", string(configRaw))
	}
}

func TestFileSkillServiceSaveSkillPreservesExecutionAssetsWhenMaterializingOverride(t *testing.T) {
	builtinDir := t.TempDir()
	userDir := t.TempDir()

	writePackageFile(t, builtinDir, "scripted-package", "SKILL.md", `---
name: scripted-package
description: scripted package
kind: category
runtime: cli
---

# scripted
`)
	writePackageFile(t, builtinDir, "scripted-package", "tools/run.yaml", `apiVersion: skill/v2
kind: AtomicSkill
metadata:
  name: run_scripted
  runtime: cli
spec:
  description: run scripted tool
execution:
  cli:
    command: python
    args:
      - '{{ skill_dir }}/scripts/run.py'
  param_mapping: {}
`)
	writePackageFile(t, builtinDir, "scripted-package", "scripts/run.py", "print('ok')\n")
	writePackageFile(t, builtinDir, "scripted-package", "handlers/handlers.py", "def register_tools(registry):\n    return registry\n")
	writePackageFile(t, builtinDir, "scripted-package", "requirements.txt", "click==8.1.7\n")

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		BuiltinSkillsDir: builtinDir,
		UserSkillsDir:    userDir,
		RuntimeSkillDirs: []string{builtinDir, userDir},
	}, nil)

	if err := svc.SaveSkill("scripted-package", "updated scripted package", "# updated scripted"); err != nil {
		t.Fatalf("SaveSkill error: %v", err)
	}

	for _, rel := range []string{"scripts/run.py", "handlers/handlers.py", "requirements.txt"} {
		if _, err := os.Stat(filepath.Join(userDir, "scripted-package", rel)); err != nil {
			t.Fatalf("expected preserved execution asset %s: %v", rel, err)
		}
	}
}

func TestFileSkillServiceScopedPackageQueriesExposeBuiltinUserExclusiveAndGlobalAliasMetadata(t *testing.T) {
	builtinDir := t.TempDir()
	userDir := t.TempDir()
	exclusiveDir := t.TempDir()
	writePackageFile(t, builtinDir, "builtin-package", "SKILL.md", `---
name: builtin-package
description: builtin package
kind: workflow
---

# builtin
`)
	writePackageFile(t, userDir, "user-package", "SKILL.md", `---
name: user-package
description: user package
kind: workflow
---

# user
`)
	writePackageFile(t, exclusiveDir, "exclusive-package", "SKILL.md", `---
name: exclusive-package
description: exclusive package
kind: workflow
---

	# exclusive
`)

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		BuiltinSkillsDir: builtinDir,
		UserSkillsDir:    userDir,
		RuntimeSkillDirs: []string{builtinDir, userDir, exclusiveDir},
	}, stubSkillReferenceProvider{
		exclusiveDirs: map[string]string{"default": exclusiveDir},
	})

	builtinPackages, err := svc.ListSkillPackagesByScope(SkillPackageScopeBuiltin, "")
	if err != nil {
		t.Fatalf("ListSkillPackagesByScope(builtin) error: %v", err)
	}
	if len(builtinPackages) != 1 || builtinPackages[0].Scope != SkillPackageScopeBuiltin {
		t.Fatalf("unexpected builtin packages: %#v", builtinPackages)
	}

	userPackages, err := svc.ListSkillPackagesByScope(SkillPackageScopeUser, "")
	if err != nil {
		t.Fatalf("ListSkillPackagesByScope(user) error: %v", err)
	}
	if len(userPackages) != 1 || userPackages[0].Scope != SkillPackageScopeUser {
		t.Fatalf("unexpected user packages: %#v", userPackages)
	}

	globalPackages, err := svc.ListSkillPackagesByScope(SkillPackageScopeGlobal, "")
	if err != nil {
		t.Fatalf("ListSkillPackagesByScope(global) error: %v", err)
	}
	if len(globalPackages) != 2 {
		t.Fatalf("expected global alias to include builtin+user packages, got %#v", globalPackages)
	}

	exclusivePackages, err := svc.ListSkillPackagesByScope(SkillPackageScopeExclusive, "default")
	if err != nil {
		t.Fatalf("ListSkillPackagesByScope(exclusive) error: %v", err)
	}
	if len(exclusivePackages) != 1 || exclusivePackages[0].OwnerConfig != "default" {
		t.Fatalf("unexpected exclusive packages: %#v", exclusivePackages)
	}

	detail, err := svc.GetSkillPackageByScope("exclusive-package", SkillPackageScopeExclusive, "default")
	if err != nil {
		t.Fatalf("GetSkillPackageByScope(exclusive) error: %v", err)
	}
	if detail.Scope != SkillPackageScopeExclusive || detail.OwnerConfig != "default" {
		t.Fatalf("unexpected scoped detail: %#v", detail.SkillPackageSummary)
	}
	if err := svc.SetSkillPackageEnabledByScope("exclusive-package", SkillPackageScopeExclusive, "default", false); err != nil {
		t.Fatalf("SetSkillPackageEnabledByScope(exclusive) error: %v", err)
	}
	configRaw, err := os.ReadFile(filepath.Join(exclusiveDir, "exclusive-package", "config.yaml"))
	if err != nil {
		t.Fatalf("read exclusive config error: %v", err)
	}
	if string(configRaw) != "enabled: false\n" {
		t.Fatalf("unexpected exclusive config content: %q", string(configRaw))
	}

	allPackages, err := svc.ListSkillPackagesByScope(SkillPackageScopeAll, "default")
	if err != nil {
		t.Fatalf("ListSkillPackagesByScope(all) error: %v", err)
	}
	if len(allPackages) != 3 {
		t.Fatalf("expected all scope to include builtin+user+exclusive, got %#v", allPackages)
	}
}

func TestFileSkillServiceListSkillPackagesSkipsMissingPackageWarnSpam(t *testing.T) {
	resetDefaultLoggerForTest(t)
	t.Cleanup(func() {
		resetDefaultLoggerForTest(t)
	})

	logDir := t.TempDir()
	if err := logger.Init(logDir, logger.DEBUG, "skill-service-test"); err != nil {
		t.Fatalf("init logger: %v", err)
	}

	userDir := t.TempDir()
	writePackageFile(t, userDir, "valid-package", "SKILL.md", `---
name: valid-package
description: valid package
kind: workflow
---

# valid
`)
	if err := os.MkdirAll(filepath.Join(userDir, "agent-management"), 0o755); err != nil {
		t.Fatalf("mkdir non-package dir: %v", err)
	}

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		UserSkillsDir:    userDir,
		RuntimeSkillDirs: []string{userDir},
	}, nil)

	packages, err := svc.ListSkillPackages()
	if err != nil {
		t.Fatalf("ListSkillPackages error: %v", err)
	}
	if len(packages) != 1 || packages[0].PackageID != "valid-package" {
		t.Fatalf("expected only valid package visible, got %#v", packages)
	}

	logRaw, err := os.ReadFile(filepath.Join(logDir, "skill-service-test.log"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	logText := string(logRaw)
	if strings.Contains(logText, "[WARN]") && strings.Contains(logText, "agent-management") {
		t.Fatalf("expected missing package to avoid WARN log spam, got %s", logText)
	}
}

func TestFileSkillServiceListSkillPackagesSkipsScopeDirectoriesWithoutPackageLookupLogs(t *testing.T) {
	resetDefaultLoggerForTest(t)
	t.Cleanup(func() {
		resetDefaultLoggerForTest(t)
	})

	logDir := t.TempDir()
	if err := logger.Init(logDir, logger.DEBUG, "skill-service-scope-test"); err != nil {
		t.Fatalf("init logger: %v", err)
	}

	runtimeRoot := t.TempDir()
	for _, rel := range []string{"system", "user"} {
		if err := os.MkdirAll(filepath.Join(runtimeRoot, rel), 0o755); err != nil {
			t.Fatalf("mkdir scope dir %s: %v", rel, err)
		}
	}

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		UserSkillsDir:    runtimeRoot,
		RuntimeSkillDirs: []string{runtimeRoot},
	}, nil)

	packages, err := svc.ListSkillPackages()
	if err != nil {
		t.Fatalf("ListSkillPackages error: %v", err)
	}
	if len(packages) != 0 {
		t.Fatalf("expected no packages under pure scope root, got %#v", packages)
	}

	logRaw, err := os.ReadFile(filepath.Join(logDir, "skill-service-scope-test.log"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	logText := string(logRaw)
	for _, scopeName := range []string{"system", "user"} {
		if strings.Contains(logText, scopeName) && strings.Contains(logText, "skill package 不存在") {
			t.Fatalf("expected scope directory %s to be skipped before package lookup, got logs: %s", scopeName, logText)
		}
	}
}

func TestFileSkillServiceRefreshesExclusiveDirsAfterInitialization(t *testing.T) {
	globalDir := t.TempDir()
	exclusiveDir := t.TempDir()
	provider := &stubSkillReferenceProvider{exclusiveDirs: map[string]string{}}
	svc := NewFileSkillService(globalDir, provider)

	provider.exclusiveDirs["default"] = exclusiveDir
	writePackageFile(t, exclusiveDir, "late-exclusive-package", "SKILL.md", `---
name: late-exclusive-package
description: exclusive package created later
kind: workflow
---

# late
`)

	exclusivePackages, err := svc.ListSkillPackagesByScope(SkillPackageScopeExclusive, "default")
	if err != nil {
		t.Fatalf("ListSkillPackagesByScope(exclusive) error: %v", err)
	}
	if len(exclusivePackages) != 1 || exclusivePackages[0].PackageID != "late-exclusive-package" {
		t.Fatalf("unexpected late exclusive packages: %#v", exclusivePackages)
	}

	detail, err := svc.GetSkillPackageByScope("late-exclusive-package", SkillPackageScopeExclusive, "default")
	if err != nil {
		t.Fatalf("GetSkillPackageByScope(late-exclusive-package) error: %v", err)
	}
	if detail.OwnerConfig != "default" || detail.Scope != SkillPackageScopeExclusive {
		t.Fatalf("unexpected late exclusive detail: %#v", detail.SkillPackageSummary)
	}
}

func TestFileSkillServiceManagedSkillsDirPrefersDedicatedGlobalDir(t *testing.T) {
	userDir := t.TempDir()
	globalDir := t.TempDir()

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		UserSkillsDir:   userDir,
		GlobalSkillsDir: globalDir,
	}, nil)

	if got := svc.ManagedSkillsDir(); got != globalDir {
		t.Fatalf("ManagedSkillsDir() = %q, want %q", got, globalDir)
	}
	location, err := svc.defaultImportLocation(SkillPackageScopeGlobal, "")
	if err != nil {
		t.Fatalf("defaultImportLocation(global) error: %v", err)
	}
	if location.Scope != SkillPackageScopeGlobal || location.Dir != globalDir {
		t.Fatalf("unexpected global import location: %#v", location)
	}
}

func TestListSkillPackagesByScopeIncludesDedicatedGlobalDir(t *testing.T) {
	builtinDir := t.TempDir()
	userDir := t.TempDir()
	globalDir := t.TempDir()
	exclusiveDir := t.TempDir()
	writePackageFile(t, builtinDir, "builtin-package", "SKILL.md", `---
name: builtin-package
description: builtin package
kind: workflow
---

# builtin
`)
	writePackageFile(t, userDir, "user-package", "SKILL.md", `---
name: user-package
description: user package
kind: workflow
---

# user
`)
	writePackageFile(t, globalDir, "global-package", "SKILL.md", `---
name: global-package
description: global package
kind: workflow
---

# global
`)
	writePackageFile(t, exclusiveDir, "exclusive-package", "SKILL.md", `---
name: exclusive-package
description: exclusive package
kind: workflow
---

# exclusive
`)

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		BuiltinSkillsDir: builtinDir,
		UserSkillsDir:    userDir,
		GlobalSkillsDir:  globalDir,
		RuntimeSkillDirs: []string{builtinDir, userDir, globalDir, exclusiveDir},
	}, stubSkillReferenceProvider{
		exclusiveDirs: map[string]string{"default": exclusiveDir},
	})

	globalPackages, err := svc.ListSkillPackagesByScope(SkillPackageScopeGlobal, "")
	if err != nil {
		t.Fatalf("ListSkillPackagesByScope(global) error: %v", err)
	}
	if len(globalPackages) != 3 {
		t.Fatalf("expected builtin+user+global packages, got %#v", globalPackages)
	}
	seen := map[string]string{}
	for _, item := range globalPackages {
		seen[item.PackageID] = item.Scope
	}
	if seen["builtin-package"] != SkillPackageScopeBuiltin {
		t.Fatalf("expected builtin package in global scope list, got %#v", globalPackages)
	}
	if seen["user-package"] != SkillPackageScopeUser {
		t.Fatalf("expected user package in global scope list, got %#v", globalPackages)
	}
	if seen["global-package"] != SkillPackageScopeGlobal {
		t.Fatalf("expected dedicated global package in global scope list, got %#v", globalPackages)
	}
}

func TestFileSkillServiceAllScopeWithConfigNameOnlyIncludesCurrentExclusivePackages(t *testing.T) {
	globalDir := t.TempDir()
	defaultExclusiveDir := t.TempDir()
	creatorExclusiveDir := t.TempDir()

	writePackageFile(t, globalDir, "global-package", "SKILL.md", `---
name: global-package
description: global package
kind: workflow
---

# global
`)
	writePackageFile(t, defaultExclusiveDir, "default-exclusive", "SKILL.md", `---
name: default-exclusive
description: default exclusive package
kind: workflow
---

# default exclusive
`)
	writePackageFile(t, creatorExclusiveDir, "creator-exclusive", "SKILL.md", `---
name: creator-exclusive
description: creator exclusive package
kind: workflow
---

# creator exclusive
`)

	svc := NewFileSkillService(globalDir, stubSkillReferenceProvider{
		exclusiveDirs: map[string]string{
			"default":       defaultExclusiveDir,
			"skill_creator": creatorExclusiveDir,
		},
	})

	packages, err := svc.ListSkillPackagesByScope(SkillPackageScopeAll, "default")
	if err != nil {
		t.Fatalf("ListSkillPackagesByScope(all, default) error: %v", err)
	}
	if len(packages) != 2 {
		t.Fatalf("expected global + current exclusive only, got %#v", packages)
	}
	got := make([]string, 0, len(packages))
	for _, item := range packages {
		got = append(got, item.PackageID+":"+item.Scope+":"+item.OwnerConfig)
	}
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "global-package:user:") {
		t.Fatalf("expected user-scoped package in result, got %v", got)
	}
	if !strings.Contains(joined, "default-exclusive:exclusive:default") {
		t.Fatalf("expected current config exclusive package in result, got %v", got)
	}
	if strings.Contains(joined, "creator-exclusive") {
		t.Fatalf("expected unrelated exclusive package excluded, got %v", got)
	}
}

func TestFileSkillServiceListSkillPackagesUsesGovernedVisiblePackages(t *testing.T) {
	globalDir := t.TempDir()
	defaultsDir := t.TempDir()
	exclusiveDir := t.TempDir()

	writePackageFile(t, globalDir, "global-only", "SKILL.md", `---
name: global-only
description: global package
kind: workflow
---

# global
`)
	writePackageFile(t, defaultsDir, "defaults-only", "SKILL.md", `---
name: defaults-only
description: defaults package
kind: workflow
---

# defaults
`)
	writePackageFile(t, defaultsDir, "shared-package", "SKILL.md", `---
name: shared-package
description: defaults shared package
kind: workflow
---

# defaults shared
`)
	writePackageFile(t, exclusiveDir, "shared-package", "SKILL.md", `---
name: shared-package
description: exclusive shared package
kind: workflow
---

# exclusive shared
`)
	writePackageFile(t, exclusiveDir, "exclusive-only", "SKILL.md", `---
name: exclusive-only
description: exclusive only package
kind: workflow
---

# exclusive only
`)

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		SkillsDir:        globalDir,
		RuntimeSkillDirs: []string{defaultsDir, globalDir, exclusiveDir},
		ResolveRuntimeRegistryGovernance: func() RuntimeRegistryGovernance {
			return &stubRuntimeRegistryGovernance{skillDirs: []string{globalDir, defaultsDir}}
		},
	}, stubSkillReferenceProvider{exclusiveDirs: map[string]string{"default": exclusiveDir}})

	packages, err := svc.ListSkillPackages()
	if err != nil {
		t.Fatalf("ListSkillPackages error: %v", err)
	}
	if len(packages) != 3 {
		t.Fatalf("expected 3 governed visible packages, got %#v", packages)
	}
	seen := map[string]SkillPackageSummary{}
	for _, item := range packages {
		seen[item.PackageID] = item
	}
	if _, ok := seen["global-only"]; !ok {
		t.Fatalf("expected global-only visible, got %#v", packages)
	}
	if _, ok := seen["defaults-only"]; !ok {
		t.Fatalf("expected defaults-only visible, got %#v", packages)
	}
	if _, ok := seen["exclusive-only"]; ok {
		t.Fatalf("expected ungoverned exclusive-only hidden, got %#v", packages)
	}
	shared, ok := seen["shared-package"]
	if !ok {
		t.Fatalf("expected shared-package visible, got %#v", packages)
	}
	if shared.Description != "defaults shared package" || shared.Scope != SkillPackageScopeBuiltin || shared.OwnerConfig != "" {
		t.Fatalf("expected governed registry package to win, got %#v", shared)
	}
}

func TestFileSkillServiceGetSkillPackageUsesGovernedVisiblePackageLocation(t *testing.T) {
	globalDir := t.TempDir()
	defaultsDir := t.TempDir()
	exclusiveDir := t.TempDir()

	writePackageFile(t, defaultsDir, "shared-package", "SKILL.md", `---
name: shared-package
description: defaults shared package
kind: workflow
---

# defaults shared
`)
	writePackageFile(t, exclusiveDir, "shared-package", "SKILL.md", `---
name: shared-package
description: exclusive shared package
kind: workflow
---

# exclusive shared
`)

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		SkillsDir:        globalDir,
		RuntimeSkillDirs: []string{defaultsDir, globalDir, exclusiveDir},
		ResolveRuntimeRegistryGovernance: func() RuntimeRegistryGovernance {
			return &stubRuntimeRegistryGovernance{skillDirs: []string{defaultsDir}}
		},
	}, stubSkillReferenceProvider{exclusiveDirs: map[string]string{"default": exclusiveDir}})

	detail, err := svc.GetSkillPackage("shared-package")
	if err != nil {
		t.Fatalf("GetSkillPackage(shared-package) error: %v", err)
	}
	if detail.Description != "defaults shared package" || detail.OwnerConfig != "" || detail.Scope != SkillPackageScopeBuiltin {
		t.Fatalf("unexpected governed detail: %#v", detail.SkillPackageSummary)
	}
}

func TestFileSkillServiceGetSkillPackageUsesGovernedPackageResolverWhenRegistryEnumerationIsEmpty(t *testing.T) {
	globalDir := t.TempDir()
	defaultsDir := t.TempDir()

	writePackageFile(t, defaultsDir, "shared-package", "SKILL.md", `---
name: shared-package
description: defaults shared package
kind: workflow
---

# defaults shared
`)

	governance := &stubRuntimeRegistryGovernance{
		resolvedSkillOK: true,
		resolvedSkillPkg: skill.SkillPackage{
			Name: "shared-package",
			Dir:  filepath.Join(defaultsDir, "shared-package"),
		},
	}

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		SkillsDir:        globalDir,
		RuntimeSkillDirs: []string{defaultsDir, globalDir},
		ResolveRuntimeRegistryGovernance: func() RuntimeRegistryGovernance {
			return governance
		},
	}, nil)

	detail, err := svc.GetSkillPackage("shared-package")
	if err != nil {
		t.Fatalf("GetSkillPackage(shared-package) error: %v", err)
	}
	if detail.Description != "defaults shared package" || detail.Scope != SkillPackageScopeBuiltin {
		t.Fatalf("unexpected governed detail: %#v", detail.SkillPackageSummary)
	}
	if len(governance.resolveSkillCalls) != 1 || governance.resolveSkillCalls[0] != "shared-package" {
		t.Fatalf("expected governed package resolver call, got %#v", governance.resolveSkillCalls)
	}
}

func TestFileSkillServiceFindVisiblePackageLocationUsesGovernedPackageResolverWhenRegistryEnumerationIsEmpty(t *testing.T) {
	globalDir := t.TempDir()
	defaultsDir := t.TempDir()

	writePackageFile(t, defaultsDir, "shared-package", "SKILL.md", `---
name: shared-package
description: defaults shared package
kind: workflow
---

# defaults shared
`)

	governance := &stubRuntimeRegistryGovernance{
		resolvedSkillOK: true,
		resolvedSkillPkg: skill.SkillPackage{
			Name: "shared-package",
			Dir:  filepath.Join(defaultsDir, "shared-package"),
		},
	}

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		SkillsDir:        globalDir,
		RuntimeSkillDirs: []string{defaultsDir, globalDir},
		ResolveRuntimeRegistryGovernance: func() RuntimeRegistryGovernance {
			return governance
		},
	}, nil)

	location, err := svc.findVisiblePackageLocation("shared-package")
	if err != nil {
		t.Fatalf("findVisiblePackageLocation(shared-package) error: %v", err)
	}
	if location.Scope != SkillPackageScopeBuiltin || !samePath(location.Dir, defaultsDir) {
		t.Fatalf("unexpected governed location: %#v", location)
	}
	if len(governance.resolveSkillCalls) != 1 || governance.resolveSkillCalls[0] != "shared-package" {
		t.Fatalf("expected governed package resolver call, got %#v", governance.resolveSkillCalls)
	}
}

func TestFileSkillServiceListSkillPackagesWithGovernanceNilRegistryDoesNotFallbackToOverlay(t *testing.T) {
	globalDir := t.TempDir()
	defaultsDir := t.TempDir()
	exclusiveDir := t.TempDir()

	writePackageFile(t, globalDir, "global-only", "SKILL.md", `---
name: global-only
description: global package
kind: workflow
---

# global
`)
	writePackageFile(t, defaultsDir, "defaults-only", "SKILL.md", `---
name: defaults-only
description: defaults package
kind: workflow
---

# defaults
`)
	writePackageFile(t, exclusiveDir, "exclusive-only", "SKILL.md", `---
name: exclusive-only
description: exclusive package
kind: workflow
---

# exclusive
`)

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		SkillsDir:        globalDir,
		RuntimeSkillDirs: []string{defaultsDir, globalDir, exclusiveDir},
		ResolveRuntimeRegistryGovernance: func() RuntimeRegistryGovernance {
			return &stubRuntimeRegistryGovernance{}
		},
	}, stubSkillReferenceProvider{exclusiveDirs: map[string]string{"default": exclusiveDir}})

	packages, err := svc.ListSkillPackages()
	if err != nil {
		t.Fatalf("ListSkillPackages error: %v", err)
	}
	if len(packages) != 0 {
		t.Fatalf("expected governed-empty package list, got %#v", packages)
	}
}

func TestFileSkillServiceGetSkillPackageWithGovernanceNilRegistryReturnsNotFound(t *testing.T) {
	globalDir := t.TempDir()
	defaultsDir := t.TempDir()
	exclusiveDir := t.TempDir()

	writePackageFile(t, exclusiveDir, "shared-package", "SKILL.md", `---
name: shared-package
description: exclusive shared package
kind: workflow
---

# exclusive shared
`)

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		SkillsDir:        globalDir,
		RuntimeSkillDirs: []string{defaultsDir, globalDir, exclusiveDir},
		ResolveRuntimeRegistryGovernance: func() RuntimeRegistryGovernance {
			return &stubRuntimeRegistryGovernance{}
		},
	}, stubSkillReferenceProvider{exclusiveDirs: map[string]string{"default": exclusiveDir}})

	_, err := svc.GetSkillPackage("shared-package")
	if err == nil {
		t.Fatal("expected governed-nil registry to return not found")
	}
	var notFoundErr *SkillPackageNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("expected SkillPackageNotFoundError, got %v", err)
	}
}

func TestFileSkillServiceFindVisiblePackageLocationWithGovernanceNilRegistryReturnsNotFound(t *testing.T) {
	globalDir := t.TempDir()
	defaultsDir := t.TempDir()
	exclusiveDir := t.TempDir()

	writePackageFile(t, defaultsDir, "shared-package", "SKILL.md", `---
name: shared-package
description: defaults shared package
kind: workflow
---

# defaults shared
`)

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		SkillsDir:        globalDir,
		RuntimeSkillDirs: []string{defaultsDir, globalDir, exclusiveDir},
		ResolveRuntimeRegistryGovernance: func() RuntimeRegistryGovernance {
			return &stubRuntimeRegistryGovernance{}
		},
	}, stubSkillReferenceProvider{exclusiveDirs: map[string]string{"default": exclusiveDir}})

	_, err := svc.findVisiblePackageLocation("shared-package")
	if err == nil {
		t.Fatal("expected governed-nil visible package resolution to return not found")
	}
	var notFoundErr *SkillPackageNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("expected SkillPackageNotFoundError, got %v", err)
	}
}

func TestFileSkillServiceDeleteSkillRemovesWritableOverrideAndRevealsLowerLayer(t *testing.T) {
	globalDir := t.TempDir()
	defaultsDir := t.TempDir()
	exclusiveDir := t.TempDir()

	writePackageFile(t, defaultsDir, "shared-package", "SKILL.md", `---
name: shared-package
description: defaults package
kind: workflow
---

# defaults
`)
	writePackageFile(t, exclusiveDir, "shared-package", "SKILL.md", `---
name: shared-package
description: override package
kind: workflow
---

# override
`)

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		SkillsDir:        globalDir,
		RuntimeSkillDirs: []string{defaultsDir, globalDir, exclusiveDir},
		ActiveConfigName: "default",
	}, stubSkillReferenceProvider{exclusiveDirs: map[string]string{"default": exclusiveDir}})

	if err := svc.DeleteSkill("shared-package"); err != nil {
		t.Fatalf("DeleteSkill error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "shared-package")); !os.IsNotExist(err) {
		t.Fatalf("expected writable override removed, stat err=%v", err)
	}
	detail, err := svc.GetSkillPackage("shared-package")
	if err != nil {
		t.Fatalf("GetSkillPackage after delete error: %v", err)
	}
	if detail.Description != "defaults package" {
		t.Fatalf("expected lower layer to reappear, got %#v", detail.SkillPackageSummary)
	}
}

func TestFileSkillServiceSaveSkillCreatesWritableOverrideForReadonlyCategoryPackage(t *testing.T) {
	globalDir := t.TempDir()
	defaultsDir := t.TempDir()
	exclusiveDir := t.TempDir()
	builtinToolsDir := t.TempDir()

	writePackageFile(t, defaultsDir, "shared-package", "SKILL.md", `---
name: shared-package
description: defaults package
kind: category
backend: compute
tools:
  - buffer
---

# defaults package
`)
	writeToolFile(t, builtinToolsDir, filepath.Join("system", "vector-tools", "buffer.yaml"), `apiVersion: tool/v1
kind: Tool
metadata:
  name: buffer
  domain: vector-tools
  runtime: qgis
spec:
  description: buffer
execution:
  backend_tool: buffer
  param_mapping:
    INPUT: '{{ INPUT }}'
`)

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		SkillsDir:        globalDir,
		BuiltinToolsDir:  builtinToolsDir,
		RuntimeSkillDirs: []string{defaultsDir, globalDir, exclusiveDir},
		ResolveRuntimeRegistryGovernance: func() RuntimeRegistryGovernance {
			return &stubRuntimeRegistryGovernance{dirs: []string{builtinToolsDir}, skillDirs: []string{defaultsDir, globalDir, exclusiveDir}}
		},
		ActiveConfigName: "default",
	}, stubSkillReferenceProvider{exclusiveDirs: map[string]string{"default": exclusiveDir}})

	if err := svc.SaveSkill("shared-package", "updated package", "# updated package"); err != nil {
		t.Fatalf("SaveSkill error: %v", err)
	}
	detail, err := svc.GetSkillPackage("shared-package")
	if err != nil {
		t.Fatalf("GetSkillPackage error: %v", err)
	}
	if detail.Description != "updated package" || detail.MarkdownBody != "# updated package" {
		t.Fatalf("unexpected updated package detail: %#v", detail)
	}
	if !detail.HasTools || detail.ToolCount != 1 || len(detail.ToolRefs) != 1 || detail.ToolRefs[0] != "buffer" {
		t.Fatalf("expected readonly package tools preserved in writable override, got %#v", detail)
	}
	if len(detail.Tools) != 0 {
		t.Fatalf("expected readonly package detail to hide hydrated tools, got %#v", detail.Tools)
	}
	if !strings.Contains(readPackageFile(t, filepath.Join(globalDir, "shared-package", "SKILL.md")), "tools:") {
		t.Fatalf("expected writable override to preserve tool refs in SKILL.md")
	}
	if _, err := os.Stat(filepath.Join(globalDir, "shared-package", "tools")); !os.IsNotExist(err) {
		t.Fatalf("expected final architecture override to avoid materializing legacy tools dir, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "shared-package", "tools")); !os.IsNotExist(err) {
		t.Fatalf("expected exclusive dir untouched, stat err=%v", err)
	}
}

func TestFileSkillServiceToggleReadonlyPackageCreatesWritableOverride(t *testing.T) {
	globalDir := t.TempDir()
	defaultsDir := t.TempDir()
	exclusiveDir := t.TempDir()

	writePackageFile(t, defaultsDir, "workflow-package", "SKILL.md", `---
name: workflow-package
description: defaults workflow
kind: workflow
---

# workflow package
`)
	writePackageFile(t, defaultsDir, "workflow-package", "workflow.yaml", `apiVersion: workflow/v1
kind: serial_workflow
metadata:
  name: workflow-package
spec:
  steps: []
`)

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		SkillsDir:        globalDir,
		RuntimeSkillDirs: []string{defaultsDir, globalDir, exclusiveDir},
		ActiveConfigName: "default",
	}, stubSkillReferenceProvider{exclusiveDirs: map[string]string{"default": exclusiveDir}})

	if err := svc.SetSkillPackageEnabled("workflow-package", false); err != nil {
		t.Fatalf("SetSkillPackageEnabled error: %v", err)
	}
	detail, err := svc.GetSkillPackage("workflow-package")
	if err != nil {
		t.Fatalf("GetSkillPackage error: %v", err)
	}
	if detail.Enabled {
		t.Fatalf("expected readonly package disable to be overridden, got %#v", detail)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "workflow-package", "workflow.yaml")); err != nil {
		t.Fatalf("expected workflow definition copied into writable override: %v", err)
	}
	configRaw, err := os.ReadFile(filepath.Join(globalDir, "workflow-package", "config.yaml"))
	if err != nil {
		t.Fatalf("expected writable override config written: %v", err)
	}
	if string(configRaw) != "enabled: false\n" {
		t.Fatalf("unexpected override config content: %q", string(configRaw))
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "workflow-package", "workflow.yaml")); !os.IsNotExist(err) {
		t.Fatalf("expected exclusive dir untouched, stat err=%v", err)
	}
}

func TestFileSkillServiceSaveSkillWithScopeCreatesExclusiveOverrideForReadonlyCategoryPackage(t *testing.T) {
	globalDir := t.TempDir()
	defaultsDir := t.TempDir()
	exclusiveDir := t.TempDir()
	builtinToolsDir := t.TempDir()

	writePackageFile(t, defaultsDir, "shared-package", "SKILL.md", `---
name: shared-package
description: defaults package
kind: category
backend: compute
tools:
  - buffer
---

# defaults package
`)
	writeToolFile(t, builtinToolsDir, filepath.Join("system", "vector-tools", "buffer.yaml"), `apiVersion: tool/v1
kind: Tool
metadata:
  name: buffer
  domain: vector-tools
  runtime: qgis
spec:
  description: buffer
execution:
  backend_tool: buffer
  param_mapping:
    INPUT: '{{ INPUT }}'
`)

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		SkillsDir:        globalDir,
		BuiltinToolsDir:  builtinToolsDir,
		RuntimeSkillDirs: []string{defaultsDir, globalDir, exclusiveDir},
		ResolveRuntimeRegistryGovernance: func() RuntimeRegistryGovernance {
			return &stubRuntimeRegistryGovernance{dirs: []string{builtinToolsDir}, skillDirs: []string{defaultsDir, globalDir, exclusiveDir}}
		},
		ActiveConfigName: "default",
	}, stubSkillReferenceProvider{exclusiveDirs: map[string]string{"default": exclusiveDir}})

	if err := svc.SaveSkillWithScope("shared-package", SkillPackageScopeExclusive, "default", "updated package", "# updated package"); err != nil {
		t.Fatalf("SaveSkillWithScope error: %v", err)
	}
	detail, err := svc.GetSkillPackage("shared-package")
	if err != nil {
		t.Fatalf("GetSkillPackage error: %v", err)
	}
	if detail.Description != "updated package" || detail.OwnerConfig != "default" || detail.Scope != SkillPackageScopeExclusive {
		t.Fatalf("unexpected scoped override detail: %#v", detail)
	}
	if !detail.HasTools || len(detail.ToolRefs) != 1 || detail.ToolRefs[0] != "buffer" {
		t.Fatalf("expected scoped override to preserve tool refs, got %#v", detail)
	}
	if len(detail.Tools) != 0 {
		t.Fatalf("expected scoped override detail to hide hydrated tools, got %#v", detail.Tools)
	}
	if !strings.Contains(readPackageFile(t, filepath.Join(exclusiveDir, "shared-package", "SKILL.md")), "tools:") {
		t.Fatalf("expected scoped override to preserve tool refs in SKILL.md")
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "shared-package", "tools")); !os.IsNotExist(err) {
		t.Fatalf("expected final architecture override to avoid materializing legacy tools dir, stat err=%v", err)
	}
}

func TestFileSkillServiceDeleteReadonlyVisiblePackageReturnsUnsupportedError(t *testing.T) {
	globalDir := t.TempDir()
	defaultsDir := t.TempDir()

	writePackageFile(t, defaultsDir, "readonly-package", "SKILL.md", `---
name: readonly-package
description: readonly package
kind: workflow
---

# readonly
`)

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		SkillsDir:        globalDir,
		RuntimeSkillDirs: []string{defaultsDir, globalDir},
	}, nil)

	err := svc.DeleteSkill("readonly-package")
	if err == nil {
		t.Fatal("expected DeleteSkill to reject readonly visible package")
	}
	var unsupportedErr *SkillMutationUnsupportedError
	if !errors.As(err, &unsupportedErr) {
		t.Fatalf("expected SkillMutationUnsupportedError, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(defaultsDir, "readonly-package", "SKILL.md")); statErr != nil {
		t.Fatalf("expected readonly source to remain intact: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(globalDir, "readonly-package")); !os.IsNotExist(statErr) {
		t.Fatalf("expected DeleteSkill not to materialize writable override, stat err=%v", statErr)
	}
}

func TestFileSkillServiceTypesDoNotExposeBridgeDynamicAuthoritySurface(t *testing.T) {
	if _, ok := reflect.TypeFor[FileSkillServiceConfig]().FieldByName("ResolveBridgeDynamicRegistry"); ok {
		t.Fatal("expected FileSkillServiceConfig to stop exposing ResolveBridgeDynamicRegistry")
	}
	if _, ok := reflect.TypeFor[FileSkillService]().FieldByName("ResolveBridgeDynamicRegistry"); ok {
		t.Fatal("expected FileSkillService to stop exposing ResolveBridgeDynamicRegistry")
	}
}

func TestFileSkillServiceDoesNotExposeBridgeDynamicToolsAsVirtualPackageThroughGovernance(t *testing.T) {
	bridgeDynamicRegistry := skill.NewBridgeDynamicRegistry()
	bridgeDynamicRegistry.RegisterInstanceTools("instance-a", []bridgepkg.ToolDefinition{{
		Name:        "flash_feature",
		Description: "Flash feature.",
		Category:    "vector",
		InputSchema: map[string]any{"type": "object"},
	}})

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		UserSkillsDir: t.TempDir(),
		UserToolsDir:  t.TempDir(),
		ResolveRuntimeRegistryGovernance: func() RuntimeRegistryGovernance {
			return &stubRuntimeRegistryGovernance{bridgeDynamicRegistry: bridgeDynamicRegistry}
		},
	}, nil)

	packages, err := svc.ListSkillPackages()
	if err != nil {
		t.Fatalf("ListSkillPackages error: %v", err)
	}
	if len(packages) != 0 {
		t.Fatalf("expected dynamic bridge tools to stay out of package list, got %#v", packages)
	}

	_, err = svc.GetSkillPackage("_bridge-dynamic")
	if err == nil {
		t.Fatal("expected _bridge-dynamic to behave like a missing package when only dynamic bridge tools exist")
	}
	var notFoundErr *SkillPackageNotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("expected SkillPackageNotFoundError, got %v", err)
	}
}

func TestFileSkillServiceGetSkillPackageUsesGovernedRegistryWhenBridgeDynamicPresent(t *testing.T) {
	skillsDir := t.TempDir()
	toolsDir := t.TempDir()
	writePackageFile(t, skillsDir, "demo-package", "SKILL.md", `---
name: demo-package
description: demo package
kind: category
tools:
  - buffer
---

# demo
`)
	writeToolFile(t, toolsDir, filepath.Join("user", "vector-tools", "buffer.yaml"), `apiVersion: tool/v1
kind: Tool
metadata:
  name: buffer
  domain: vector-tools
  runtime: qgis
spec:
  description: buffer
execution:
  backend_tool: buffer
  param_mapping:
    INPUT: '{{ INPUT }}'
`)

	bridgeDynamicRegistry := skill.NewBridgeDynamicRegistry()
	bridgeDynamicRegistry.RegisterInstanceTools("instance-a", []bridgepkg.ToolDefinition{{
		Name:        "flash_feature",
		Description: "Flash feature.",
		Category:    "vector",
		InputSchema: map[string]any{"type": "object"},
	}})

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		UserSkillsDir: skillsDir,
		UserToolsDir:  toolsDir,
		ResolveRuntimeRegistryGovernance: func() RuntimeRegistryGovernance {
			return &stubRuntimeRegistryGovernance{dirs: []string{toolsDir}, skillDirs: []string{skillsDir}, bridgeDynamicRegistry: bridgeDynamicRegistry}
		},
	}, nil)

	detail, err := svc.GetSkillPackage("demo-package")
	if err != nil {
		t.Fatalf("GetSkillPackage error: %v", err)
	}
	if detail.PackageID != "demo-package" {
		t.Fatalf("expected filesystem package id, got %#v", detail.SkillPackageSummary)
	}
	if detail.Name != "demo-package" || detail.Description != "demo package" {
		t.Fatalf("expected filesystem package metadata, got %#v", detail.SkillPackageSummary)
	}
	if len(detail.ToolRefs) != 1 || detail.ToolRefs[0] != "buffer" {
		t.Fatalf("expected filesystem tool refs preserved, got %#v", detail.ToolRefs)
	}
	if len(detail.FrontMatter.Tools) != 1 || detail.FrontMatter.Tools[0] != "buffer" {
		t.Fatalf("expected filesystem frontmatter tools preserved, got %#v", detail.FrontMatter.Tools)
	}
	if len(detail.Tools) != 0 {
		t.Fatalf("expected filesystem skill detail to hide hydrated tools, got %#v", detail.Tools)
	}
}

func TestFileSkillServiceListSkillPackagesUsesGovernedRegistryWhenBridgeDynamicPresent(t *testing.T) {
	skillsDir := t.TempDir()
	toolsDir := t.TempDir()
	writePackageFile(t, skillsDir, "demo-package", "SKILL.md", `---
name: demo-package
description: demo package
kind: category
tools:
  - buffer
---

# demo
`)
	writeToolFile(t, toolsDir, filepath.Join("user", "vector-tools", "buffer.yaml"), `apiVersion: tool/v1
kind: Tool
metadata:
  name: buffer
  domain: vector-tools
  runtime: qgis
spec:
  description: buffer
execution:
  backend_tool: buffer
  param_mapping:
    INPUT: '{{ INPUT }}'
`)

	bridgeDynamicRegistry := skill.NewBridgeDynamicRegistry()
	bridgeDynamicRegistry.RegisterInstanceTools("instance-a", []bridgepkg.ToolDefinition{{
		Name:        "flash_feature",
		Description: "Flash feature.",
		Category:    "vector",
		InputSchema: map[string]any{"type": "object"},
	}})

	svc := NewFileSkillServiceWithConfig(FileSkillServiceConfig{
		UserSkillsDir: skillsDir,
		UserToolsDir:  toolsDir,
		ResolveRuntimeRegistryGovernance: func() RuntimeRegistryGovernance {
			return &stubRuntimeRegistryGovernance{dirs: []string{toolsDir}, skillDirs: []string{skillsDir}, bridgeDynamicRegistry: bridgeDynamicRegistry}
		},
	}, nil)

	packages, err := svc.ListSkillPackages()
	if err != nil {
		t.Fatalf("ListSkillPackages error: %v", err)
	}
	if len(packages) != 1 {
		t.Fatalf("expected only filesystem package in list, got %#v", packages)
	}

	byID := make(map[string]SkillPackageSummary, len(packages))
	for _, item := range packages {
		byID[item.PackageID] = item
	}

	detail, ok := byID["demo-package"]
	if !ok {
		t.Fatalf("expected filesystem package in list, got %#v", packages)
	}
	if detail.Name != "demo-package" || detail.Description != "demo package" {
		t.Fatalf("expected filesystem package metadata preserved, got %#v", detail)
	}
	if detail.ToolRefCount != 1 || detail.ToolCount != 1 || !detail.HasTools {
		t.Fatalf("expected filesystem package tool counts preserved, got %#v", detail)
	}
}

func writePackageFile(t *testing.T, skillsDir, packageName, relativePath, raw string) {
	t.Helper()
	fullPath := filepath.Join(skillsDir, packageName, relativePath)
	writeRawPackageFile(t, fullPath, raw)
}

func writeRawPackageFile(t *testing.T, path, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir error: %v", err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write file error: %v", err)
	}
}

func writeToolFile(t *testing.T, toolsDir, relativePath, raw string) {
	t.Helper()
	fullPath := filepath.Join(toolsDir, relativePath)
	writeRawPackageFile(t, fullPath, raw)
}

func readPackageFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file error: %v", err)
	}
	return string(raw)
}
