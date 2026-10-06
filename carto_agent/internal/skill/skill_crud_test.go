package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateSkillPackageAndSaveSkill(t *testing.T) {
	baseDir := t.TempDir()
	packagePath, err := CreateSkillPackage(baseDir, PackageCreateRequest{
		PackageID:   "skill-management",
		Description: "管理技能包",
		Kind:        SkillPackageKindCategory,
		Backend:     RuntimeIDInternal,
		Body:        "creator package",
	})
	if err != nil {
		t.Fatalf("CreateSkillPackage failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(packagePath, "SKILL.md")); err != nil {
		t.Fatalf("expected SKILL.md created: %v", err)
	}

	def := SkillDefinition{
		APIVersion: SkillAPIVersionV1,
		Kind:       SkillKindAtomic,
		Metadata: SkillMetadata{
			Name:    "list_skill_packages",
			Backend: RuntimeIDInternal,
		},
		Spec:      SkillSpec{Description: "列出技能包"},
		Execution: SkillExecution{BackendTool: "list_skill_packages", ParamMapping: map[string]any{}},
	}
	path, err := SaveSkillToPackage(baseDir, "skill-management", def)
	if err != nil {
		t.Fatalf("SaveSkillToPackage failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected tool yaml created: %v", err)
	}
}

func TestValidateSkillDefinitionRejectsUnsupportedBackend(t *testing.T) {
	err := ValidateSkillDefinition(SkillDefinition{
		APIVersion: SkillAPIVersionV1,
		Kind:       SkillKindAtomic,
		Metadata: SkillMetadata{
			Name:    "bad_tool",
			Backend: "unknown",
		},
		Spec:      SkillSpec{Description: "bad"},
		Execution: SkillExecution{BackendTool: "bad_tool", ParamMapping: map[string]any{}},
	})
	if err == nil {
		t.Fatal("expected unsupported backend validation error")
	}
}

func TestValidateSkillDefinitionAcceptsQGISRuntime(t *testing.T) {
	err := ValidateSkillDefinition(SkillDefinition{
		APIVersion: SkillAPIVersionV1,
		Kind:       SkillKindAtomic,
		Metadata: SkillMetadata{
			Name:    "legacy_qgis_tool",
			Runtime: RuntimeIDQGIS,
		},
		Spec:      SkillSpec{Description: "legacy"},
		Execution: SkillExecution{BackendTool: "legacy_qgis_tool", ParamMapping: map[string]any{}},
	})
	if err != nil {
		t.Fatalf("expected qgis runtime to be accepted, got %v", err)
	}
}
