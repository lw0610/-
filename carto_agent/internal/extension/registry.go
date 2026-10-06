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

func RegisterManifest(manifest *Manifest, result *ImportResult, configBaseDir string) (string, error) {
	if manifest == nil || result == nil {
		return "", fmt.Errorf("extension manifest and import result are required")
	}
	baseDir := strings.TrimSpace(configBaseDir)
	if baseDir == "" {
		return "", fmt.Errorf("config base dir is required")
	}
	metadataDir := filepath.Join(baseDir, "extensions")
	if err := os.MkdirAll(metadataDir, 0o755); err != nil {
		return "", err
	}
	metadataPath := filepath.Join(metadataDir, strings.TrimSpace(manifest.Metadata.Name)+".yaml")
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
	if err := os.WriteFile(metadataPath, raw, 0o644); err != nil {
		return "", err
	}
	return metadataPath, nil
}
