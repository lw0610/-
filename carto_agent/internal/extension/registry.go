package extension

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type RegisteredMetadata struct {
	APIVersion  string   `yaml:"apiVersion"`
	Kind        string   `yaml:"kind"`
	Metadata    Metadata `yaml:"metadata"`
	ImportState struct {
		PackageID   string                    `yaml:"package_id"`
		Scope       string                    `yaml:"scope"`
		OwnerConfig string                    `yaml:"owner_config,omitempty"`
		SourceDir   string                    `yaml:"source_dir"`
		TargetDir   string                    `yaml:"target_dir"`
		ImportedAt  time.Time                 `yaml:"imported_at"`
		Resources   []RegisteredResourceState `yaml:"resources,omitempty"`
	} `yaml:"import_state"`
}

type RegisteredResourceState struct {
	Type      string `yaml:"type,omitempty"`
	PackageID string `yaml:"package_id,omitempty"`
	SourceDir string `yaml:"source_dir,omitempty"`
	TargetDir string `yaml:"target_dir,omitempty"`
}

// CheckManifestRegistration validates the destination without changing files.
// Call this before importing resources to detect existing records and unusable
// metadata paths while the import can still be skipped without side effects.
func CheckManifestRegistration(manifest *Manifest, configBaseDir string) (path string, exists bool, err error) {
	if manifest == nil {
		return "", false, fmt.Errorf("extension manifest is required")
	}
	if err := validatePathComponent(manifest.Metadata.Name, "metadata.name"); err != nil {
		return "", false, err
	}
	baseDir := strings.TrimSpace(configBaseDir)
	if baseDir == "" {
		return "", false, fmt.Errorf("config base dir is required")
	}
	metadataDir := filepath.Join(baseDir, "extensions")
	metadataDir, err = canonicalTargetPath(metadataDir)
	if err != nil {
		return "", false, err
	}
	metadataPath := filepath.Join(metadataDir, strings.TrimSpace(manifest.Metadata.Name)+".yaml")
	if info, err := os.Lstat(metadataPath); err == nil {
		if !info.Mode().IsRegular() {
			return "", false, fmt.Errorf("extension metadata target must be a regular file: %s", metadataPath)
		}
		return metadataPath, true, nil
	} else if !os.IsNotExist(err) {
		return "", false, err
	}
	return metadataPath, false, nil
}

func RegisterManifest(manifest *Manifest, result *ImportResult, configBaseDir string) (string, error) {
	if result == nil {
		return "", fmt.Errorf("extension manifest and import result are required")
	}
	metadataPath, _, err := CheckManifestRegistration(manifest, configBaseDir)
	if err != nil {
		return "", err
	}
	metadataDir := filepath.Dir(metadataPath)
	if err := os.MkdirAll(metadataDir, 0o755); err != nil {
		return "", err
	}
	record := RegisteredMetadata{
		APIVersion: manifest.APIVersion,
		Kind:       manifest.Kind,
		Metadata:   manifest.Metadata,
	}
	record.ImportState.PackageID = result.PackageID
	record.ImportState.Scope = result.Scope
	record.ImportState.OwnerConfig = result.OwnerConfig
	record.ImportState.SourceDir = result.SourceDir
	record.ImportState.TargetDir = result.TargetDir
	record.ImportState.ImportedAt = time.Now().UTC()
	if len(result.Resources) > 0 {
		record.ImportState.Resources = make([]RegisteredResourceState, 0, len(result.Resources))
		for _, resource := range result.Resources {
			record.ImportState.Resources = append(record.ImportState.Resources, RegisteredResourceState{
				Type:      strings.TrimSpace(resource.Type),
				PackageID: strings.TrimSpace(resource.PackageID),
				SourceDir: strings.TrimSpace(resource.SourceDir),
				TargetDir: strings.TrimSpace(resource.TargetDir),
			})
		}
	}
	raw, err := yaml.Marshal(record)
	if err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(metadataDir, ".extension-metadata-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		return "", err
	}
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return "", err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temp.Name(), metadataPath); err != nil {
		return "", err
	}
	return metadataPath, nil
}
