package extension

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFileParsesSkillPackageManifest(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "extension.yaml")
	writeManifestFile(t, path, `apiVersion: extension/v1
kind: skill-package
metadata:
  name: demo-extension
  display_name: Demo Extension
  version: 1.0.0
spec:
  source_dir: /tmp/demo-package
  activation:
    scope: global
    owner_config: ""
    enabled_by_default: false
    rebuild_policy: auto
`)

	manifest, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	if manifest.APIVersion != APIVersionV1 || manifest.Kind != KindSkillPackage {
		t.Fatalf("unexpected manifest header: %#v", manifest)
	}
	if manifest.Metadata.Name != "demo-extension" || manifest.Spec.SourceDir != "/tmp/demo-package" {
		t.Fatalf("unexpected manifest payload: %#v", manifest)
	}
	if manifest.Spec.Activation.Scope != "global" || manifest.Spec.Activation.RebuildPolicy != "auto" {
		t.Fatalf("unexpected activation payload: %#v", manifest.Spec.Activation)
	}
}

func TestLoadFileRejectsUnsupportedManifestKind(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "extension.yaml")
	writeManifestFile(t, path, `apiVersion: extension/v1
kind: openapi
metadata:
  name: bad-extension
spec:
  source_dir: /tmp/demo-package
  activation:
    scope: global
    enabled_by_default: false
    rebuild_policy: auto
`)

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected unsupported manifest kind error")
	}
	if err.Error() != `unsupported extension kind "openapi"` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFileRejectsMissingSourceDir(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "extension.yaml")
	writeManifestFile(t, path, `apiVersion: extension/v1
kind: skill-package
metadata:
  name: bad-extension
spec:
  activation:
    scope: global
    enabled_by_default: false
    rebuild_policy: auto
`)

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected missing source_dir error")
	}
	if err.Error() != `extension spec.source_dir is required` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFileParsesPackageImportManifest(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "extension.yaml")
	writeManifestFile(t, path, `apiVersion: extension/v2
kind: PackageImport
metadata:
  name: hydrology-pack
spec:
  resources:
    - type: tool_package
      source_dir: /tmp/hydrology-tools
    - type: skill_package
      source_dir: /tmp/hydrology-skill
  activation:
    scope: exclusive
    owner_config: default
    enabled_by_default: true
    rebuild_policy: immediate
`)

	manifest, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	if manifest.APIVersion != APIVersionV2 || manifest.Kind != KindPackageImport {
		t.Fatalf("unexpected manifest header: %#v", manifest)
	}
	if len(manifest.Spec.Resources) != 2 {
		t.Fatalf("expected two resources, got %#v", manifest.Spec.Resources)
	}
	if manifest.Spec.Resources[0].Type != ResourceToolPackage || manifest.Spec.Resources[1].Type != ResourceSkillPackage {
		t.Fatalf("unexpected resources: %#v", manifest.Spec.Resources)
	}
	if manifest.Spec.Activation.Scope != "exclusive" || manifest.Spec.Activation.OwnerConfig != "default" {
		t.Fatalf("unexpected activation payload: %#v", manifest.Spec.Activation)
	}
}

func TestLoadFileRejectsMissingResourcesForV2(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "extension.yaml")
	writeManifestFile(t, path, `apiVersion: extension/v2
kind: PackageImport
metadata:
  name: bad-pack
spec:
  activation:
    scope: global
`)

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected missing resources error")
	}
	if err.Error() != `extension spec.resources is required` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeManifestFile(t *testing.T, path, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir manifest dir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write manifest file failed: %v", err)
	}
}
