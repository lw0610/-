package extension

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func importFixtureManifest(sources ...string) *Manifest {
	resources := make([]Resource, 0, len(sources))
	for _, source := range sources {
		resources = append(resources, Resource{Type: ResourceSkillPackage, SourceDir: source})
	}
	return &Manifest{APIVersion: APIVersionV2, Kind: KindPackageImport,
		Metadata: Metadata{Name: "test-extension"},
		Spec:     Spec{Resources: resources, Activation: Activation{Scope: "global", EnabledByDefault: true}},
	}
}

func requireFileContent(t *testing.T, path, expected string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != expected {
		t.Fatalf("file %s = %q, %v; want %q", path, got, err, expected)
	}
}

func TestImportManifestConflictSkipsWholeManifestByDefault(t *testing.T) {
	sourceRoot, targetRoot := t.TempDir(), t.TempDir()
	first, second := filepath.Join(sourceRoot, "first"), filepath.Join(sourceRoot, "second")
	writeManifestPackageFixture(t, first, "first")
	writeManifestPackageFixture(t, second, "second")
	existing := filepath.Join(targetRoot, "second", "SKILL.md")
	writeManifestFile(t, existing, "original")
	result, err := ImportManifest(importFixtureManifest(first, second), targetRoot, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Resources) != 0 || len(result.Skipped) != 2 {
		t.Fatalf("expected entire manifest skipped: %#v", result)
	}
	for _, skipped := range result.Skipped {
		if !strings.Contains(skipped.Reason, "整个扩展未导入") {
			t.Fatalf("missing conflict explanation: %#v", skipped)
		}
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "first")); !os.IsNotExist(err) {
		t.Fatalf("first resource must not be partially installed: %v", err)
	}
	requireFileContent(t, existing, "original")
}

func TestImportManifestInvalidSecondResourcePreservesExistingFirst(t *testing.T) {
	source := filepath.Join(t.TempDir(), "first")
	writeManifestPackageFixture(t, source, "first")
	targetRoot := t.TempDir()
	existing := filepath.Join(targetRoot, "first", "SKILL.md")
	writeManifestFile(t, existing, "original")
	_, err := ImportManifestWithOptions(importFixtureManifest(source, filepath.Join(t.TempDir(), "missing")), targetRoot, "", t.TempDir(), ImportOptions{Overwrite: true})
	if err == nil {
		t.Fatal("expected missing second source error")
	}
	requireFileContent(t, existing, "original")
}

func TestImportManifestInvalidSkillContentPreservesInstalledPackage(t *testing.T) {
	for _, fixture := range []struct{ name, file, content string }{
		{"frontmatter", "SKILL.md", "---\nname: [broken\n---\n"},
		{"workflow", "workflow.yaml", "apiVersion: workflow/v1\nkind: unsupported_workflow\nmetadata:\n  name: package\n"},
		{"config", "config.yaml", "enabled: [broken\n"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "package")
			writeManifestPackageFixture(t, source, "package")
			writeManifestFile(t, filepath.Join(source, fixture.file), fixture.content)
			targetRoot := t.TempDir()
			installed := filepath.Join(targetRoot, "package")
			writeManifestFile(t, filepath.Join(installed, "SKILL.md"), "installed original")
			writeManifestFile(t, filepath.Join(installed, "references", "guide.md"), "installed reference")
			_, err := ImportManifestWithOptions(importFixtureManifest(source), targetRoot, "", t.TempDir(), ImportOptions{Overwrite: true})
			if err == nil || (fixture.name != "workflow" && !strings.Contains(err.Error(), "load extension skill package")) || (fixture.name == "workflow" && !strings.Contains(err.Error(), "workflow.yaml")) {
				t.Fatalf("expected content validation failure, got %v", err)
			}
			requireFileContent(t, filepath.Join(installed, "SKILL.md"), "installed original")
			requireFileContent(t, filepath.Join(installed, "references", "guide.md"), "installed reference")
		})
	}
}

func TestImportManifestRejectsSkillNameDirectoryMismatch(t *testing.T) {
	source := filepath.Join(t.TempDir(), "new-directory")
	writeManifestPackageFixture(t, source, "existing-skill")
	targetRoot := t.TempDir()
	existing := filepath.Join(targetRoot, "existing-skill", "SKILL.md")
	writeManifestFile(t, existing, "original skill")
	_, err := ImportManifest(importFixtureManifest(source), targetRoot, "", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "SKILL.md name") {
		t.Fatalf("expected skill directory/name mismatch error, got %v", err)
	}
	requireFileContent(t, existing, "original skill")
	if _, err := os.Stat(filepath.Join(targetRoot, "new-directory")); !os.IsNotExist(err) {
		t.Fatalf("mismatched skill was installed: %v", err)
	}
}

func TestImportManifestOverwriteCopiesResourcesAndRemovesRetiredFiles(t *testing.T) {
	source := filepath.Join(t.TempDir(), "complete")
	writeManifestPackageFixture(t, source, "complete")
	writeManifestFile(t, filepath.Join(source, "references", "guide.md"), "reference")
	writeManifestFile(t, filepath.Join(source, "scripts", "runner.py"), "print('test')")
	targetRoot := t.TempDir()
	writeManifestFile(t, filepath.Join(targetRoot, "complete", "retired.txt"), "old")
	result, err := ImportManifestWithOptions(importFixtureManifest(source), targetRoot, "", t.TempDir(), ImportOptions{Overwrite: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Resources) != 1 || len(result.Skipped) != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	requireFileContent(t, filepath.Join(targetRoot, "complete", "references", "guide.md"), "reference")
	requireFileContent(t, filepath.Join(targetRoot, "complete", "scripts", "runner.py"), "print('test')")
	if _, err := os.Stat(filepath.Join(targetRoot, "complete", "retired.txt")); !os.IsNotExist(err) {
		t.Fatalf("retired file remains: %v", err)
	}
	entries, err := os.ReadDir(targetRoot)
	if err != nil || len(entries) != 1 || entries[0].Name() != "complete" {
		t.Fatalf("temporary import entries remain: %v, %v", entries, err)
	}
}

func TestImportManifestRejectsOverlappingSourceAndTarget(t *testing.T) {
	for _, mode := range []string{"same", "target-inside-source", "source-inside-target"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "package")
			targetRoot := root
			if mode == "target-inside-source" {
				targetRoot = filepath.Join(source, "destination")
			}
			if mode == "source-inside-target" {
				source = filepath.Join(root, "package", "nested", "package")
			}
			writeManifestPackageFixture(t, source, "package")
			before, err := os.ReadFile(filepath.Join(source, "SKILL.md"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = ImportManifestWithOptions(importFixtureManifest(source), targetRoot, "", t.TempDir(), ImportOptions{Overwrite: true})
			if err == nil || !strings.Contains(err.Error(), "must not overlap") {
				t.Fatalf("expected overlap rejection, got %v", err)
			}
			requireFileContent(t, filepath.Join(source, "SKILL.md"), string(before))
		})
	}
}

func TestImportManifestRejectsSymlinkSourceAndTarget(t *testing.T) {
	for _, mode := range []string{"source-root", "source-file", "target"} {
		t.Run(mode, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "package")
			writeManifestPackageFixture(t, source, "package")
			targetRoot := t.TempDir()
			other := t.TempDir()
			var link, destination string
			switch mode {
			case "source-root":
				link, destination = filepath.Join(t.TempDir(), "package"), source
				source = link
			case "source-file":
				link, destination = filepath.Join(source, "outside"), other
			case "target":
				link, destination = filepath.Join(targetRoot, "package"), other
			}
			if err := os.Symlink(destination, link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			if _, err := ImportManifestWithOptions(importFixtureManifest(source), targetRoot, "", t.TempDir(), ImportOptions{Overwrite: true}); err == nil {
				t.Fatal("expected symlink rejection")
			}
			if _, err := os.Lstat(link); err != nil {
				t.Fatalf("symlink was modified: %v", err)
			}
		})
	}
}

func TestImportManifestRejectsNonRegularSourceFile(t *testing.T) {
	source := filepath.Join(t.TempDir(), "package")
	writeManifestPackageFixture(t, source, "package")
	// Bind through a short path for macOS's Unix socket path limit, then move
	// the filesystem entry into the package being validated.
	socketRoot, err := os.MkdirTemp("", "ext-sock-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(socketRoot)
	socketPath := filepath.Join(socketRoot, "socket")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	defer listener.Close()
	if err := os.Rename(socketPath, filepath.Join(source, "socket")); err != nil {
		t.Fatal(err)
	}
	targetRoot := t.TempDir()
	if _, err := ImportManifest(importFixtureManifest(source), targetRoot, "", t.TempDir()); err == nil || !strings.Contains(err.Error(), "non-regular") {
		t.Fatalf("expected non-regular file rejection: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "package")); !os.IsNotExist(err) {
		t.Fatalf("invalid package was installed: %v", err)
	}
}

func TestImportManifestRejectsDuplicateResourceTargets(t *testing.T) {
	first := filepath.Join(t.TempDir(), "same")
	second := filepath.Join(t.TempDir(), "same")
	writeManifestPackageFixture(t, first, "same")
	writeManifestPackageFixture(t, second, "same")
	targetRoot := t.TempDir()
	if _, err := ImportManifestWithOptions(importFixtureManifest(first, second), targetRoot, "", t.TempDir(), ImportOptions{Overwrite: true}); err == nil {
		t.Fatal("duplicate targets accepted")
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "same")); !os.IsNotExist(err) {
		t.Fatalf("duplicate target was partially installed: %v", err)
	}
}

func TestCommitImportsRollsBackFirstAndSecondWhenSecondRenameFails(t *testing.T) {
	root := t.TempDir()
	items := make([]stagedImport, 2)
	for i, name := range []string{"first", "second"} {
		target := filepath.Join(root, name)
		stageRoot := filepath.Join(root, ".stage-"+name)
		stage := filepath.Join(stageRoot, "package")
		writeManifestFile(t, filepath.Join(target, "SKILL.md"), "old-"+name)
		writeManifestFile(t, filepath.Join(stage, "SKILL.md"), "new-"+name)
		items[i] = stagedImport{resource: ImportedResource{TargetDir: target}, stageRoot: stageRoot, stageDir: stage}
	}
	failure := errors.New("simulated second commit failure")
	err := commitImports(items, true, func(from, to string) error {
		if from == items[1].stageDir {
			return failure
		}
		return os.Rename(from, to)
	})
	if !errors.Is(err, failure) {
		t.Fatalf("unexpected commit error: %v", err)
	}
	for _, name := range []string{"first", "second"} {
		requireFileContent(t, filepath.Join(root, name, "SKILL.md"), "old-"+name)
	}
	backups, err := filepath.Glob(filepath.Join(root, ".extension-backup-*"))
	if err != nil || len(backups) != 0 {
		t.Fatalf("rollback left backups: %v, %v", backups, err)
	}
}

func TestManifestRejectsUnsafeNamesAndScopes(t *testing.T) {
	for _, name := range []string{"../outside", `..\outside`, "/absolute", "..", "bad:name", "line\nbreak"} {
		t.Run(name, func(t *testing.T) {
			manifest := importFixtureManifest("/tmp/package")
			manifest.Metadata.Name = name
			if err := validateManifest(*manifest); err == nil {
				t.Fatal("unsafe metadata.name accepted")
			}
			manifest.Metadata.Name = "valid"
			manifest.Spec.Activation = Activation{Scope: "exclusive", OwnerConfig: name}
			if err := validateManifest(*manifest); err == nil {
				t.Fatal("unsafe owner_config accepted")
			}
		})
	}
	for _, scope := range []string{"", "../global", "user", "builtin"} {
		manifest := importFixtureManifest("/tmp/package")
		manifest.Spec.Activation.Scope = scope
		if err := validateManifest(*manifest); err == nil {
			t.Fatalf("unsupported scope %q accepted", scope)
		}
	}
}

func TestRegisterManifestRejectsTraversalAndSymlinkWithoutChangingFiles(t *testing.T) {
	root := t.TempDir()
	manifest := importFixtureManifest("/tmp/package")
	manifest.Metadata.Name = "../outside"
	if _, err := RegisterManifest(manifest, &ImportResult{}, root); err == nil {
		t.Fatal("unsafe name accepted")
	}
	outside := filepath.Join(root, "outside.yaml")
	writeManifestFile(t, outside, "original")
	manifest.Metadata.Name = "safe"
	metadataDir := filepath.Join(root, "extensions")
	if err := os.MkdirAll(metadataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(metadataDir, "safe.yaml")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := RegisterManifest(manifest, &ImportResult{}, root); err == nil {
		t.Fatal("symlink metadata accepted")
	}
	requireFileContent(t, outside, "original")
}

func TestRegisterManifestReplacesMetadataAndCleansTemporaryFile(t *testing.T) {
	root := t.TempDir()
	manifest := importFixtureManifest("/tmp/package")
	writeManifestFile(t, filepath.Join(root, "extensions", manifest.Metadata.Name+".yaml"), "old")
	path, err := RegisterManifest(manifest, &ImportResult{PackageID: "new-package"}, root)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(content), "package_id: new-package") {
		t.Fatalf("metadata was not replaced: %s, %v", content, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != manifest.Metadata.Name+".yaml" {
		t.Fatalf("temporary metadata remains: %v, %v", entries, err)
	}
}

func TestImportManifestRejectsMethodOnlyPackageBeforeAnyResourceIsInstalled(t *testing.T) {
	root, target := t.TempDir(), t.TempDir()
	valid, method := filepath.Join(root, "valid"), filepath.Join(root, "method")
	writeManifestPackageFixture(t, valid, "valid")
	writeManifestFile(t, filepath.Join(method, "SKILL.md"), "---\nname: method\nkind: knowledge\n---\nmethod only\n")
	if _, err := ImportManifest(importFixtureManifest(valid, method), target, "", t.TempDir()); err == nil {
		t.Fatal("manifest must not bypass tool package validation")
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatalf("rejected manifest partially installed resources: %v %v", entries, err)
	}
}
