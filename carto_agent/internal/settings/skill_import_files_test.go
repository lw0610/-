package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillImportCopiesPackageReferencesAndScripts(t *testing.T) {
	for _, useFile := range []bool{false, true} {
		t.Run(map[bool]string{false: "directory", true: "SKILL.md"}[useFile], func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "source")
			writeRawPackageFile(t, filepath.Join(source, "SKILL.md"), "---\nname: route-guidance\ndescription: route guidance\nkind: category\ntools: [fixture_tool]\n---\nRead references/rules.md.\n")
			files := map[string]string{"references/rules.md": "Keep route order", "scripts/check.py": "print('not executed')", "assets/template.txt": "template"}
			for path, content := range files {
				writeRawPackageFile(t, filepath.Join(source, path), content)
			}
			target := t.TempDir()
			service := NewFileSkillService(target, nil)
			input := source
			if useFile {
				input = filepath.Join(source, "SKILL.md")
			}
			result, err := service.ImportSkill(input, false)
			if err != nil {
				t.Fatal(err)
			}
			items := result["imported"].([]map[string]any)
			if len(items) != 1 || items[0]["scope"] == "" {
				t.Fatalf("expected one import with its destination scope: %#v", result)
			}
			for path, content := range files {
				got, err := os.ReadFile(filepath.Join(target, "route-guidance", path))
				if err != nil || string(got) != content {
					t.Fatalf("missing package file %s: %q, %v", path, got, err)
				}
			}
			packages, err := service.ListSkillPackages()
			if err != nil || len(packages) != 1 {
				t.Fatalf("expected exactly one installed catalog item: %#v, %v", packages, err)
			}
		})
	}
}

func TestSkillImportOverwriteAndValidationPreserveOriginal(t *testing.T) {
	target := t.TempDir()
	service := NewFileSkillService(target, nil)
	source := filepath.Join(t.TempDir(), "package")
	writeRawPackageFile(t, filepath.Join(source, "SKILL.md"), "---\nname: demo\nkind: category\ntools: [fixture_tool]\ndescription: test\n---\nOriginal\n")
	writeRawPackageFile(t, filepath.Join(source, "references/old.md"), "old")
	if _, err := service.ImportSkill(source, false); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(target, "demo", "SKILL.md")
	before, _ := os.ReadFile(installed)
	writeRawPackageFile(t, filepath.Join(source, "SKILL.md"), "---\nname: demo\nkind: category\ntools: [fixture_tool]\ndescription: test\n---\nChanged\n")
	result, err := service.ImportSkill(source, false)
	if err != nil || len(result["imported"].([]map[string]any)) != 0 || len(result["skipped"].([]map[string]any)) != 1 {
		t.Fatalf("expected conflict skipped: %#v, %v", result, err)
	}
	after, _ := os.ReadFile(installed)
	if string(after) != string(before) {
		t.Fatal("unchecked overwrite changed the existing package")
	}
	writeRawPackageFile(t, filepath.Join(source, "workflow.yaml"), "apiVersion: invalid\n")
	if _, err := service.ImportSkill(source, true); err == nil {
		t.Fatal("expected invalid workflow to fail before replacing the package")
	}
	after, _ = os.ReadFile(installed)
	if string(after) != string(before) {
		t.Fatal("failed import changed the existing package")
	}
	if err := os.Remove(filepath.Join(source, "workflow.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ImportSkill(source, true); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(installed)
	if !strings.Contains(string(after), "Changed") {
		t.Fatal("explicit overwrite did not install new content")
	}
}

func TestSkillImportRejectsSelfAndLinkedFiles(t *testing.T) {
	target := t.TempDir()
	service := NewFileSkillService(target, nil)
	existing := filepath.Join(target, "demo")
	writeRawPackageFile(t, filepath.Join(existing, "SKILL.md"), "---\nname: demo\nkind: category\ntools: [fixture_tool]\n---\nOriginal\n")
	if _, err := service.ImportSkill(existing, true); err == nil {
		t.Fatal("expected self import rejected")
	}
	source := filepath.Join(t.TempDir(), "source")
	writeRawPackageFile(t, filepath.Join(source, "SKILL.md"), "---\nname: demo\nkind: category\ntools: [fixture_tool]\n---\nChanged\n")
	if err := os.Symlink(filepath.Join(existing, "SKILL.md"), filepath.Join(source, "external.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ImportSkill(source, true); err == nil {
		t.Fatal("expected linked file rejected")
	}
	got, _ := os.ReadFile(filepath.Join(existing, "SKILL.md"))
	if !strings.Contains(string(got), "Original") {
		t.Fatal("invalid import modified original")
	}
}

func TestSkillImportStandaloneMarkdownKeepsReferences(t *testing.T) {
	target := t.TempDir()
	writeRawPackageFile(t, filepath.Join(target, "demo", "SKILL.md"), "---\nname: demo\nkind: category\ntools: [fixture_tool]\n---\nOriginal\n")
	writeRawPackageFile(t, filepath.Join(target, "demo", "references/rules.md"), "rules")
	source := filepath.Join(t.TempDir(), "demo.md")
	writeRawPackageFile(t, source, "---\nname: demo\nkind: category\ntools: [fixture_tool]\n---\nUpdated standalone text")
	if _, err := NewFileSkillService(target, nil).ImportSkill(source, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(target, "demo", "references/rules.md"))
	if err != nil || string(got) != "rules" {
		t.Fatalf("standalone update lost references: %q %v", got, err)
	}
}

func TestSkillImportDoesNotTreatOtherFilesAsMarkdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "package.zip")
	writeRawPackageFile(t, path, "not markdown")
	if _, err := NewFileSkillService(t.TempDir(), nil).ImportSkill(path, false); err == nil {
		t.Fatal("expected unsupported import format to be rejected")
	}
}

func TestSkillImportRejectsNewMethodOnlyPackagesWithoutMutatingExistingFiles(t *testing.T) {
	for _, kind := range []string{"knowledge", "workflow", "category", ""} {
		t.Run("kind_"+kind, func(t *testing.T) {
			target := t.TempDir()
			installed := filepath.Join(target, "demo", "SKILL.md")
			original := "---\nname: demo\nkind: knowledge\n---\nExisting user method\n"
			writeRawPackageFile(t, installed, original)
			service := NewFileSkillService(target, nil)
			// Already installed user content remains readable during the v1 window.
			if _, err := service.GetSkillPackage("demo"); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(t.TempDir(), "demo.md")
			writeRawPackageFile(t, source, "---\nname: demo\nkind: "+kind+"\nrequires_tools: [open_project]\n---\nreplacement\n")
			if _, err := service.ImportSkill(source, true); err == nil {
				t.Fatal("method-only import must be rejected")
			}
			got, err := os.ReadFile(installed)
			if err != nil || string(got) != original {
				t.Fatal("rejected import modified installed user content", err)
			}
		})
	}
}
