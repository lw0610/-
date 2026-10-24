package extension

import (
	"fmt"
	"os"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

type manifestProbe struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

func LoadFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	manifest.SourcePath = strings.TrimSpace(path)
	normalizeManifest(&manifest)
	if err := validateManifest(manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func LooksLikeExtensionManifest(path string) (bool, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return false, err
	}
	var probe manifestProbe
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return false, err
	}
	apiVersion := strings.TrimSpace(probe.APIVersion)
	kind := strings.TrimSpace(probe.Kind)
	return apiVersion == APIVersionV1 || apiVersion == APIVersionV2 || kind == KindSkillPackage || kind == KindPackageImport, nil
}

func normalizeManifest(manifest *Manifest) {
	if manifest == nil {
		return
	}
	manifest.APIVersion = strings.TrimSpace(manifest.APIVersion)
	manifest.Kind = strings.TrimSpace(manifest.Kind)
	manifest.Metadata.Name = strings.TrimSpace(manifest.Metadata.Name)
	manifest.Metadata.DisplayName = strings.TrimSpace(manifest.Metadata.DisplayName)
	manifest.Metadata.Version = strings.TrimSpace(manifest.Metadata.Version)
	manifest.Spec.SourceDir = strings.TrimSpace(manifest.Spec.SourceDir)
	for idx := range manifest.Spec.Resources {
		manifest.Spec.Resources[idx].Type = strings.TrimSpace(manifest.Spec.Resources[idx].Type)
		manifest.Spec.Resources[idx].SourceDir = strings.TrimSpace(manifest.Spec.Resources[idx].SourceDir)
	}
	manifest.Spec.Activation.Scope = strings.TrimSpace(manifest.Spec.Activation.Scope)
	manifest.Spec.Activation.OwnerConfig = strings.TrimSpace(manifest.Spec.Activation.OwnerConfig)
	manifest.Spec.Activation.RebuildPolicy = strings.TrimSpace(manifest.Spec.Activation.RebuildPolicy)
}

func validateManifest(manifest Manifest) error {
	if manifest.APIVersion != APIVersionV1 && manifest.APIVersion != APIVersionV2 {
		return fmt.Errorf("unsupported extension apiVersion %q", manifest.APIVersion)
	}
	switch manifest.APIVersion {
	case APIVersionV1:
		if manifest.Kind != KindSkillPackage {
			return fmt.Errorf("unsupported extension kind %q", manifest.Kind)
		}
		if strings.TrimSpace(manifest.Spec.SourceDir) == "" {
			return fmt.Errorf("extension spec.source_dir is required")
		}
	case APIVersionV2:
		if manifest.Kind != KindPackageImport {
			return fmt.Errorf("unsupported extension kind %q", manifest.Kind)
		}
		if len(manifest.Spec.Resources) == 0 {
			return fmt.Errorf("extension spec.resources is required")
		}
		for _, resource := range manifest.Spec.Resources {
			switch strings.TrimSpace(resource.Type) {
			case ResourceSkillPackage, ResourceToolPackage:
			default:
				return fmt.Errorf("unsupported extension resource type %q", strings.TrimSpace(resource.Type))
			}
			if strings.TrimSpace(resource.SourceDir) == "" {
				return fmt.Errorf("extension resource source_dir is required")
			}
		}
	}
	if err := validatePathComponent(manifest.Metadata.Name, "metadata.name"); err != nil {
		return err
	}
	switch manifest.Spec.Activation.Scope {
	case "global":
	case "exclusive":
		if strings.TrimSpace(manifest.Spec.Activation.OwnerConfig) == "" {
			return fmt.Errorf("extension activation.owner_config is required for exclusive scope")
		}
	default:
		return fmt.Errorf("unsupported extension activation.scope %q", manifest.Spec.Activation.Scope)
	}
	if manifest.Spec.Activation.OwnerConfig != "" {
		if err := validatePathComponent(manifest.Spec.Activation.OwnerConfig, "activation.owner_config"); err != nil {
			return err
		}
	}
	return nil
}

// Names become directory or file components on both Unix and Windows.
func validatePathComponent(value, field string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("extension %s is required", field)
	}
	if value == "." || value == ".." || strings.ContainsAny(value, `/\\:`) ||
		strings.TrimSpace(value) != value || strings.HasSuffix(value, ".") {
		return fmt.Errorf("extension %s must be a safe path component", field)
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return fmt.Errorf("extension %s must be a safe path component", field)
		}
	}
	return nil
}
