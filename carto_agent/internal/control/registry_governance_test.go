package control

import (
	"os"
	"path/filepath"
	"testing"

	bridgepkg "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/skill"
)

func TestUnifiedRegistryGovernance(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	toolsDir := filepath.Join(root, "tools")
	skillsDir := filepath.Join(root, "skills")
	writeRegistryToolFixture(t, toolsDir, "buffer", skill.RuntimeIDCLI)
	writeRegistryWorkflowPackageFixture(t, skillsDir, "buffer-workflow", "buffer")

	dynamicRegistry := skill.NewBridgeDynamicRegistry()
	dynamicRegistry.RegisterInstanceTools("instance-a", []bridgepkg.ToolDefinition{{
		Name:        "flash_feature",
		Description: "flash feature",
		Category:    "vector",
		InputSchema: map[string]any{"type": "object"},
	}})

	governance := NewRegistryGovernance(RegistryGovernanceDeps{
		ResolveToolDirs:     func() []string { return []string{toolsDir} },
		ResolveSkillDirs:    func() []string { return []string{skillsDir} },
		DynamicToolRegistry: dynamicRegistry,
	})

	toolRegistry, err := governance.LoadToolRegistry()
	if err != nil {
		t.Fatalf("LoadToolRegistry returned error: %v", err)
	}
	if _, ok := toolRegistry.Get("buffer"); !ok {
		t.Fatal("expected governed tool registry to expose static tool")
	}
	if dynamicDef, ok := toolRegistry.Get("flash_feature"); !ok || !dynamicDef.Metadata.Dynamic {
		t.Fatalf("expected governed tool registry to expose dynamic tool, got %#v ok=%v", dynamicDef, ok)
	}

	skillRegistry, err := governance.LoadSkillRegistry()
	if err != nil {
		t.Fatalf("LoadSkillRegistry returned error: %v", err)
	}
	if _, ok := skillRegistry.Get("flash_feature"); !ok {
		t.Fatal("expected skill registry to inherit governed dynamic tool")
	}

	pkg, ok, err := governance.ResolveWorkflowPackage("buffer-workflow")
	if err != nil {
		t.Fatalf("ResolveWorkflowPackage returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected governed workflow package to resolve")
	}
	if pkg.Workflow == nil || len(pkg.Workflow.Spec.Steps) != 1 || pkg.Workflow.Spec.Steps[0].Tool != "buffer" {
		t.Fatalf("unexpected governed workflow package: %#v", pkg.Workflow)
	}
}

func TestNoDynamicBypass(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	toolsDir := filepath.Join(root, "tools")
	skillsDir := filepath.Join(root, "skills")
	writeRegistryToolFixture(t, toolsDir, "buffer", skill.RuntimeIDCLI)
	writeRegistryCategoryPackageFixture(t, skillsDir, "vector-tools", "buffer")

	governedDynamic := skill.NewBridgeDynamicRegistry()
	governedDynamic.RegisterInstanceTools("instance-a", []bridgepkg.ToolDefinition{{
		Name:        "buffer",
		Description: "buffer",
		Category:    "vector",
		InputSchema: map[string]any{"type": "object"},
	}})

	governance := NewRegistryGovernance(RegistryGovernanceDeps{
		ResolveToolDirs:     func() []string { return []string{toolsDir} },
		ResolveSkillDirs:    func() []string { return []string{skillsDir} },
		DynamicToolRegistry: governedDynamic,
	})

	svc := settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{
		UserSkillsDir: skillsDir,
		UserToolsDir:  toolsDir,
		ResolveRuntimeRegistryGovernance: func() settings.RuntimeRegistryGovernance {
			return governance
		},
	}, nil)

	detail, err := svc.GetSkillPackage("vector-tools")
	if err != nil {
		t.Fatalf("GetSkillPackage returned error: %v", err)
	}
	if len(detail.ToolRefs) != 1 || detail.ToolRefs[0] != "buffer" {
		t.Fatalf("expected governed registry-backed tool refs, got %#v", detail.ToolRefs)
	}
	if len(detail.Tools) != 0 {
		t.Fatalf("expected governed detail to hide hydrated tools, got %#v", detail.Tools)
	}
}

func TestRegistryDegradedWhenProviderMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	toolsDir := filepath.Join(root, "tools")
	skillsDir := filepath.Join(root, "skills")
	writeRegistryToolFixture(t, toolsDir, "buffer", skill.RuntimeIDCLI)
	writeRegistryCategoryPackageFixture(t, skillsDir, "vector-tools", "buffer")

	governance := NewRegistryGovernance(RegistryGovernanceDeps{
		ResolveToolDirs:  func() []string { return []string{toolsDir} },
		ResolveSkillDirs: func() []string { return []string{skillsDir} },
	})

	status, err := governance.EvaluateAvailability(map[string]middlewareclient.ServiceStatus{
		skill.RuntimeIDCLI: {
			ID:            skill.RuntimeIDCLI,
			Status:        "stopped",
			Enabled:       true,
			ProviderCount: 0,
		},
	})
	if err != nil {
		t.Fatalf("EvaluateAvailability returned error: %v", err)
	}
	if status.Status != "degraded" || status.Code != "provider_missing" {
		t.Fatalf("expected provider_missing degraded status, got %#v", status)
	}
}

func TestRegistryReadyForLocalCustomCLIWithoutProvider(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	toolsDir := filepath.Join(root, "tools")
	skillsDir := filepath.Join(root, "skills")
	writeRegistryCustomCLIToolFixture(t, toolsDir, "load_review_images")
	writeRegistryCategoryPackageFixture(t, skillsDir, "quality-feedback", "load_review_images")

	governance := NewRegistryGovernance(RegistryGovernanceDeps{
		ResolveToolDirs:  func() []string { return []string{toolsDir} },
		ResolveSkillDirs: func() []string { return []string{skillsDir} },
	})

	status, err := governance.EvaluateAvailability(map[string]middlewareclient.ServiceStatus{
		skill.RuntimeIDCLI: {
			ID:            skill.RuntimeIDCLI,
			Status:        "stopped",
			Enabled:       true,
			ProviderCount: 0,
		},
	})
	if err != nil {
		t.Fatalf("EvaluateAvailability returned error: %v", err)
	}
	if status.Status != "ready" || status.Code != "" {
		t.Fatalf("expected local custom CLI registry to be ready, got %#v", status)
	}
}

func TestRegistryStillRequiresProviderForMixedCLIRegistry(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	toolsDir := filepath.Join(root, "tools")
	skillsDir := filepath.Join(root, "skills")
	writeRegistryCustomCLIToolFixture(t, toolsDir, "load_review_images")
	writeRegistryToolFixture(t, toolsDir, "buffer", skill.RuntimeIDCLI)
	writeRegistryCategoryPackageFixture(t, skillsDir, "quality-feedback", "load_review_images")
	writeRegistryCategoryPackageFixture(t, skillsDir, "vector-tools", "buffer")

	governance := NewRegistryGovernance(RegistryGovernanceDeps{
		ResolveToolDirs:  func() []string { return []string{toolsDir} },
		ResolveSkillDirs: func() []string { return []string{skillsDir} },
	})

	status, err := governance.EvaluateAvailability(map[string]middlewareclient.ServiceStatus{
		skill.RuntimeIDCLI: {
			ID:            skill.RuntimeIDCLI,
			Status:        "stopped",
			Enabled:       true,
			ProviderCount: 0,
		},
	})
	if err != nil {
		t.Fatalf("EvaluateAvailability returned error: %v", err)
	}
	if status.Status != "degraded" || status.Code != "provider_missing" {
		t.Fatalf("expected provider-backed CLI tool to keep provider requirement, got %#v", status)
	}
}

func TestRegistryDegradedWhenRuntimeUnavailable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	toolsDir := filepath.Join(root, "tools")
	skillsDir := filepath.Join(root, "skills")
	writeRegistryToolFixture(t, toolsDir, "flash_feature", skill.RuntimeIDQGIS)
	writeRegistryCategoryPackageFixture(t, skillsDir, "vector-tools", "flash_feature")

	governance := NewRegistryGovernance(RegistryGovernanceDeps{
		ResolveToolDirs:  func() []string { return []string{toolsDir} },
		ResolveSkillDirs: func() []string { return []string{skillsDir} },
	})

	status, err := governance.EvaluateAvailability(map[string]middlewareclient.ServiceStatus{})
	if err != nil {
		t.Fatalf("EvaluateAvailability returned error: %v", err)
	}
	if status.Status != "degraded" || status.Code != "unsupported_runtime" {
		t.Fatalf("expected unsupported_runtime degraded status, got %#v", status)
	}
}

func writeRegistryToolFixture(t *testing.T, rootDir, toolName, runtimeID string) {
	t.Helper()
	toolDir := filepath.Join(rootDir, "vector-tools")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatalf("mkdir tool dir: %v", err)
	}
	toolYAML := "apiVersion: tool/v1\nkind: Tool\nmetadata:\n  name: " + toolName + "\n  runtime: " + runtimeID + "\nspec:\n  description: " + toolName + "\nexecution:\n  backend_tool: " + toolName + "\n"
	if err := os.WriteFile(filepath.Join(toolDir, toolName+".yaml"), []byte(toolYAML), 0o644); err != nil {
		t.Fatalf("write tool fixture: %v", err)
	}
}

func writeRegistryCustomCLIToolFixture(t *testing.T, rootDir, toolName string) {
	t.Helper()
	toolDir := filepath.Join(rootDir, "quality-feedback")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatalf("mkdir tool dir: %v", err)
	}
	toolYAML := "apiVersion: tool/v1\nkind: Tool\nmetadata:\n  name: " + toolName + "\n  runtime: cli\nspec:\n  description: " + toolName + "\nexecution:\n  cli:\n    command: python3\n    args:\n      - review.py\n    stdin: json\n    stdout: json\n"
	if err := os.WriteFile(filepath.Join(toolDir, toolName+".yaml"), []byte(toolYAML), 0o644); err != nil {
		t.Fatalf("write custom CLI tool fixture: %v", err)
	}
}

func writeRegistryCategoryPackageFixture(t *testing.T, rootDir, packageName, toolName string) {
	t.Helper()
	packageDir := filepath.Join(rootDir, packageName)
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatalf("mkdir package dir: %v", err)
	}
	skillMarkdown := "---\nname: " + packageName + "\ndescription: " + packageName + "\nkind: category\ntools:\n  - " + toolName + "\n---\n\n# " + packageName + "\n"
	if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte(skillMarkdown), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

func writeRegistryWorkflowPackageFixture(t *testing.T, rootDir, packageName, toolName string) {
	t.Helper()
	packageDir := filepath.Join(rootDir, packageName)
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatalf("mkdir workflow dir: %v", err)
	}
	skillMarkdown := "---\nname: " + packageName + "\ndescription: " + packageName + "\nkind: workflow\n---\n\n# " + packageName + "\n"
	if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte(skillMarkdown), 0o644); err != nil {
		t.Fatalf("write workflow SKILL.md: %v", err)
	}
	workflowYAML := "apiVersion: workflow/v1\nkind: serial_workflow\nmetadata:\n  name: " + packageName + "\nspec:\n  steps:\n    - id: step_1\n      type: tool\n      tool: " + toolName + "\n      with: {}\n"
	if err := os.WriteFile(filepath.Join(packageDir, "workflow.yaml"), []byte(workflowYAML), 0o644); err != nil {
		t.Fatalf("write workflow.yaml: %v", err)
	}
}
