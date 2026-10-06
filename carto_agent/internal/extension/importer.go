package extension

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
)

type ImportedResource struct {
	Type        string
	TargetDir   string
	Scope       string
	OwnerConfig string
	PackageID   string
	SourceDir   string
}

type ImportResult struct {
	TargetDir   string
	Scope       string
	OwnerConfig string
	PackageID   string
	SourceDir   string
	Resources   []ImportedResource
}

func ImportSkillPackage(manifest *Manifest, globalSkillsDir, configBaseDir string) (*ImportResult, error) {
	return ImportManifest(manifest, globalSkillsDir, "", configBaseDir)
}

func ImportManifest(manifest *Manifest, globalSkillsDir, globalToolsDir, configBaseDir string) (*ImportResult, error) {
	if manifest == nil {
		return nil, fmt.Errorf("extension manifest is required")
	}
	resources := manifestResources(manifest)
	if len(resources) == 0 {
		return nil, fmt.Errorf("extension resources are required")
	}

	imported := make([]ImportedResource, 0, len(resources))
	for _, resource := range resources {
		importedResource, err := importResource(manifest, resource, globalSkillsDir, globalToolsDir, configBaseDir)
		if err != nil {
			return nil, err
		}
		imported = append(imported, importedResource)
	}

	result := &ImportResult{
		Scope:       strings.TrimSpace(manifest.Spec.Activation.Scope),
		OwnerConfig: strings.TrimSpace(manifest.Spec.Activation.OwnerConfig),
		Resources:   imported,
	}
	if len(imported) > 0 {
		first := imported[0]
		result.TargetDir = first.TargetDir
		result.PackageID = first.PackageID
		result.SourceDir = first.SourceDir
	}
	return result, nil
}

func manifestResources(manifest *Manifest) []Resource {
	if manifest == nil {
		return nil
	}
	if manifest.APIVersion == APIVersionV1 {
		sourceDir := strings.TrimSpace(manifest.Spec.SourceDir)
		if sourceDir == "" {
			return nil
		}
		return []Resource{{
			Type:      ResourceSkillPackage,
			SourceDir: sourceDir,
		}}
	}
	if len(manifest.Spec.Resources) == 0 {
		return nil
	}
	out := make([]Resource, 0, len(manifest.Spec.Resources))
	for _, resource := range manifest.Spec.Resources {
		if strings.TrimSpace(resource.Type) == "" || strings.TrimSpace(resource.SourceDir) == "" {
			continue
		}
		out = append(out, resource)
	}
	return out
}

func importResource(manifest *Manifest, resource Resource, globalSkillsDir, globalToolsDir, configBaseDir string) (ImportedResource, error) {
	sourceDir := filepath.Clean(strings.TrimSpace(resource.SourceDir))
	if !filepath.IsAbs(sourceDir) {
		return ImportedResource{}, fmt.Errorf("extension resource source_dir must be absolute")
	}
	info, err := os.Stat(sourceDir)
	if err != nil {
		return ImportedResource{}, err
	}
	if !info.IsDir() {
		return ImportedResource{}, fmt.Errorf("extension resource source_dir must be directory: %s", sourceDir)
	}
	packageID := strings.TrimSpace(filepath.Base(sourceDir))
	if packageID == "" {
		return ImportedResource{}, fmt.Errorf("extension resource package_id is empty for %s", sourceDir)
	}
	if err := validateResourceSource(resource, sourceDir); err != nil {
		return ImportedResource{}, err
	}
	targetDir, err := resolveImportTargetDir(manifest, resource, globalSkillsDir, globalToolsDir, configBaseDir, packageID)
	if err != nil {
		return ImportedResource{}, err
	}
	if err := copyDirectory(sourceDir, targetDir); err != nil {
		return ImportedResource{}, err
	}
	return ImportedResource{
		Type:        strings.TrimSpace(resource.Type),
		TargetDir:   targetDir,
		Scope:       strings.TrimSpace(manifest.Spec.Activation.Scope),
		OwnerConfig: strings.TrimSpace(manifest.Spec.Activation.OwnerConfig),
		PackageID:   packageID,
		SourceDir:   sourceDir,
	}, nil
}

func validateResourceSource(resource Resource, sourceDir string) error {
	switch strings.TrimSpace(resource.Type) {
	case ResourceSkillPackage:
		if _, err := os.Stat(filepath.Join(sourceDir, "SKILL.md")); err != nil {
			return fmt.Errorf("extension skill package missing SKILL.md: %w", err)
		}
		return nil
	case ResourceToolPackage:
		definitions, err := toolpkg.LoadDefinitionsFromDirectories(sourceDir)
		if err != nil {
			return fmt.Errorf("load tool package definitions: %w", err)
		}
		if len(definitions) == 0 {
			return fmt.Errorf("extension tool package missing tool manifest: %s", sourceDir)
		}
		return nil
	default:
		return fmt.Errorf("unsupported extension resource type %q", strings.TrimSpace(resource.Type))
	}
}

func resolveImportTargetDir(manifest *Manifest, resource Resource, globalSkillsDir, globalToolsDir, configBaseDir, packageID string) (string, error) {
	scope := strings.TrimSpace(manifest.Spec.Activation.Scope)
	resourceType := strings.TrimSpace(resource.Type)
	switch scope {
	case "global":
		switch resourceType {
		case ResourceSkillPackage:
			if strings.TrimSpace(globalSkillsDir) == "" {
				return "", fmt.Errorf("global skills dir is required")
			}
			return filepath.Join(strings.TrimSpace(globalSkillsDir), packageID), nil
		case ResourceToolPackage:
			targetRoot := config.ResolveGlobalToolsDir(configBaseDir)
			if targetRoot == "" {
				targetRoot = strings.TrimSpace(globalToolsDir)
			}
			if targetRoot == "" {
				return "", fmt.Errorf("global tools dir is required")
			}
			return filepath.Join(targetRoot, packageID), nil
		default:
			return "", fmt.Errorf("unsupported extension resource type %q", resourceType)
		}
	case "exclusive":
		ownerConfig := strings.TrimSpace(manifest.Spec.Activation.OwnerConfig)
		if ownerConfig == "" {
			return "", fmt.Errorf("extension activation.owner_config is required for exclusive scope")
		}
		switch resourceType {
		case ResourceSkillPackage:
			return filepath.Join(config.ResolveExclusiveSkillsDir(configBaseDir, ownerConfig), packageID), nil
		case ResourceToolPackage:
			return filepath.Join(config.ResolveExclusiveToolsDir(configBaseDir, ownerConfig), packageID), nil
		default:
			return "", fmt.Errorf("unsupported extension resource type %q", resourceType)
		}
	default:
		return "", fmt.Errorf("unsupported extension activation.scope %q", scope)
	}
}

func copyDirectory(sourceDir, targetDir string) error {
	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("remove target dir %s: %w", targetDir, err)
	}
	if err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(targetDir, rel)
		if info.IsDir() {
			return os.MkdirAll(destination, info.Mode())
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return copyFile(path, destination, info.Mode())
	}); err != nil {
		return fmt.Errorf("copy skill package directory: %w", err)
	}
	return nil
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer output.Close()
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	return nil
}
