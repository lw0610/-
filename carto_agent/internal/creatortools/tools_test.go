package creatortools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/settings"
)

func TestCreateSkillPackageUsesRuntimeField(t *testing.T) {
	skillsDir := t.TempDir()
	service := settings.NewFileSkillService(skillsDir, nil)

	result, err := Invoke(context.Background(), ToolCreateSkillPackage, map[string]any{
		"package_id":  "runtime-only-package",
		"description": "runtime based package",
		"kind":        "category",
		"runtime":     "internal",
		"body":        "runtime body",
		"backend":     "cli",
	}, Services{SkillCatalog: service})
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected create skill package result")
	}

	raw, err := os.ReadFile(filepath.Join(skillsDir, "runtime-only-package", "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md failed: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "runtime: internal") {
		t.Fatalf("expected runtime field in SKILL.md, got: %s", content)
	}
	if strings.Contains(content, "backend:") {
		t.Fatalf("expected backend field removed from SKILL.md, got: %s", content)
	}
}

func TestHandlesRecognizesCreatorTools(t *testing.T) {
	if !Handles(ToolListSkillPackages) {
		t.Fatal("expected list skill packages to be handled")
	}
	if !Handles(ToolSaveSkillScript) {
		t.Fatal("expected save skill script to be handled")
	}
	if Handles("unknown_tool") {
		t.Fatal("expected unknown tool to be rejected")
	}
}

func TestSaveSkillScriptWritesScriptIntoPackage(t *testing.T) {
	skillsDir := t.TempDir()
	service := settings.NewFileSkillService(skillsDir, nil)

	if _, err := Invoke(context.Background(), ToolCreateSkillPackage, map[string]any{
		"package_id":  "script-package",
		"description": "script package",
		"kind":        "category",
		"runtime":     "cli",
	}, Services{SkillCatalog: service}); err != nil {
		t.Fatalf("create package failed: %v", err)
	}

	result, err := Invoke(context.Background(), ToolSaveSkillScript, map[string]any{
		"package_id":  "script-package",
		"script_path": "scripts/run.py",
		"content":     "print('ok')\n",
	}, Services{SkillCatalog: service})
	if err != nil {
		t.Fatalf("save skill script failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected save skill script result")
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "script-package", "scripts", "run.py")); err != nil {
		t.Fatalf("expected script file created: %v", err)
	}
}
