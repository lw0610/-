package extension

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/skill"
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
	Skipped     []SkippedResource
}

type ImportOptions struct {
	Overwrite bool
}

type SkippedResource struct {
	ImportedResource
	Reason string
}

type stagedImport struct {
	resource  ImportedResource
	stageRoot string
	stageDir  string
	backupDir string
	installed bool
}

var importMu sync.Mutex

func ImportSkillPackage(manifest *Manifest, globalSkillsDir, configBaseDir string) (*ImportResult, error) {
	return ImportManifest(manifest, globalSkillsDir, "", configBaseDir)
}

func ImportManifest(manifest *Manifest, globalSkillsDir, globalToolsDir, configBaseDir string) (*ImportResult, error) {
	return ImportManifestWithOptions(manifest, globalSkillsDir, globalToolsDir, configBaseDir, ImportOptions{})
}

// ImportManifestWithOptions validates every resource before staging any copies,
// then commits with filesystem renames so failures can restore old data.
func ImportManifestWithOptions(manifest *Manifest, globalSkillsDir, globalToolsDir, configBaseDir string, options ImportOptions) (*ImportResult, error) {
	importMu.Lock()
	defer importMu.Unlock()
	if manifest == nil {
		return nil, fmt.Errorf("extension manifest is required")
	}
	validated := *manifest
	validated.Spec.Resources = append([]Resource(nil), manifest.Spec.Resources...)
	normalizeManifest(&validated)
	if err := validateManifest(validated); err != nil {
		return nil, err
	}
	manifest = &validated
	resources := manifestResources(manifest)
	if len(resources) == 0 {
		return nil, fmt.Errorf("extension resources are required")
	}

	planned := make([]ImportedResource, 0, len(resources))
	for _, resource := range resources {
		importedResource, err := planResource(manifest, resource, globalSkillsDir, globalToolsDir, configBaseDir)
		if err != nil {
			return nil, err
		}
		planned = append(planned, importedResource)
	}
	result := &ImportResult{
		Scope:       strings.TrimSpace(manifest.Spec.Activation.Scope),
		OwnerConfig: strings.TrimSpace(manifest.Spec.Activation.OwnerConfig),
		Resources:   []ImportedResource{},
		Skipped:     []SkippedResource{},
	}
	physical := append([]ImportedResource(nil), planned...)
	for i := range physical {
		var err error
		physical[i].SourceDir, err = filepath.EvalSymlinks(physical[i].SourceDir)
		if err != nil {
			return nil, err
		}
		physical[i].TargetDir, err = canonicalTargetPath(physical[i].TargetDir)
		if err != nil {
			return nil, err
		}
	}
	for i, resource := range physical {
		for j, other := range physical {
			if pathsOverlap(resource.SourceDir, other.TargetDir) {
				return nil, fmt.Errorf("extension source and target directories must not overlap: %s and %s", resource.SourceDir, other.TargetDir)
			}
			if i < j && pathsOverlap(resource.TargetDir, other.TargetDir) {
				return nil, fmt.Errorf("extension target directories must not overlap: %s and %s", resource.TargetDir, other.TargetDir)
			}
		}
	}
	if !options.Overwrite {
		for _, resource := range planned {
			if _, err := os.Lstat(resource.TargetDir); err == nil {
				reason := fmt.Sprintf("目标技能包或工具包已存在（%s）；未启用覆盖，整个扩展未导入", resource.PackageID)
				for _, skipped := range planned {
					result.Skipped = append(result.Skipped, SkippedResource{ImportedResource: skipped, Reason: reason})
				}
				return result, nil
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
	}
	staged := make([]stagedImport, 0, len(planned))
	defer func() {
		for _, item := range staged {
			if item.stageRoot != "" {
				_ = os.RemoveAll(item.stageRoot)
			}
		}
	}()
	for _, resource := range planned {
		parent := filepath.Dir(resource.TargetDir)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return nil, err
		}
		// Keep temporary packages outside the asset root: tool discovery is
		// recursive and would otherwise publish staged/backup tool definitions.
		stageRoot, err := os.MkdirTemp(filepath.Dir(parent), ".extension-import-*")
		if err != nil {
			return nil, err
		}
		stageDir := filepath.Join(stageRoot, "package")
		staged = append(staged, stagedImport{resource: resource, stageRoot: stageRoot, stageDir: stageDir})
		if err := copyDirectory(resource.SourceDir, stageDir); err != nil {
			return nil, err
		}
	}
	if err := commitImports(staged, options.Overwrite, os.Rename); err != nil {
		return nil, err
	}
	result.Resources = planned
	if len(planned) > 0 {
		first := planned[0]
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

func planResource(manifest *Manifest, resource Resource, globalSkillsDir, globalToolsDir, configBaseDir string) (ImportedResource, error) {
	sourceDir := filepath.Clean(strings.TrimSpace(resource.SourceDir))
	if !filepath.IsAbs(sourceDir) {
		return ImportedResource{}, fmt.Errorf("extension resource source_dir must be absolute")
	}
	info, err := os.Lstat(sourceDir)
	if err != nil {
		return ImportedResource{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ImportedResource{}, fmt.Errorf("extension resource source_dir must be directory: %s", sourceDir)
	}
	packageID := strings.TrimSpace(filepath.Base(sourceDir))
	if err := validatePathComponent(packageID, "resource package_id"); err != nil {
		return ImportedResource{}, err
	}
	if err := validateSourceTree(sourceDir); err != nil {
		return ImportedResource{}, err
	}
	if err := validateResourceSource(resource, sourceDir); err != nil {
		return ImportedResource{}, err
	}
	targetDir, err := resolveImportTargetDir(manifest, resource, globalSkillsDir, globalToolsDir, configBaseDir, packageID)
	if err != nil {
		return ImportedResource{}, err
	}
	_, err = canonicalTargetPath(targetDir)
	if err != nil {
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
		info, err := os.Stat(filepath.Join(sourceDir, "SKILL.md"))
		if err != nil {
			return fmt.Errorf("extension skill package missing SKILL.md: %w", err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("extension SKILL.md must be a regular file")
		}
		// Parse the same metadata/config that runtime discovery will
		// load before replacing any usable installed package.
		registry := skill.NewRegistry()
		if err := registry.LoadDirectories(sourceDir); err != nil {
			return fmt.Errorf("load extension skill package: %w", err)
		}
		packages := registry.Packages()
		if len(packages) != 1 || packages[0].Name != filepath.Base(sourceDir) {
			return fmt.Errorf("请将技能目录命名与 SKILL.md name 保持一致: %s", sourceDir)
		}
		if err := skill.ValidateToolPackageImport(packages[0]); err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(sourceDir, "workflow.yaml")); err == nil {
			return fmt.Errorf("串行 workflow.yaml 已下线，请使用 FLOW.md 流程包")
		} else if !os.IsNotExist(err) {
			return err
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
		if strings.TrimSpace(configBaseDir) == "" {
			return "", fmt.Errorf("config base dir is required for exclusive scope")
		}
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
			return os.MkdirAll(destination, 0o755)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("extension source contains a symlink or non-regular file: %s", path)
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
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}

func validateSourceTree(sourceDir string) error {
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("extension source contains a symlink or non-regular file: %s", path)
		}
		return nil
	})
}

// Resolve existing ancestors (including macOS /var) without accepting a target
// package symlink. Missing descendants remain within the resolved parent.
func canonicalTargetPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if info, err := os.Lstat(abs); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("extension target must be a real directory: %s", abs)
		}
		return filepath.EvalSymlinks(abs)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	parent, err := canonicalTargetPath(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(abs)), nil
}

func pathsOverlap(left, right string) bool {
	within := func(root, child string) bool {
		rel, err := filepath.Rel(root, child)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return within(left, right) || within(right, left)
}

func commitImports(items []stagedImport, overwrite bool, rename func(string, string) error) error {
	rollback := func(cause error) error {
		for i := len(items) - 1; i >= 0; i-- {
			item := &items[i]
			if item.installed {
				if err := rename(item.resource.TargetDir, item.stageDir); err != nil {
					cause = errors.Join(cause, fmt.Errorf("rollback new package %s: %w", item.resource.TargetDir, err))
					continue
				}
			}
			if item.backupDir != "" {
				if err := rename(filepath.Join(item.backupDir, "package"), item.resource.TargetDir); err != nil {
					cause = errors.Join(cause, fmt.Errorf("restore package failed; original retained at %s: %w", item.backupDir, err))
				} else {
					_ = os.Remove(item.backupDir)
				}
			}
		}
		return cause
	}
	for i := range items {
		item := &items[i]
		if info, err := os.Lstat(item.resource.TargetDir); err == nil {
			if !overwrite || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return rollback(fmt.Errorf("extension target changed or already exists: %s", item.resource.TargetDir))
			}
			backupDir, err := os.MkdirTemp(filepath.Dir(filepath.Dir(item.resource.TargetDir)), ".extension-backup-*")
			if err != nil {
				return rollback(err)
			}
			if err := rename(item.resource.TargetDir, filepath.Join(backupDir, "package")); err != nil {
				_ = os.Remove(backupDir)
				return rollback(err)
			}
			item.backupDir = backupDir
		} else if !os.IsNotExist(err) {
			return rollback(err)
		}
		if err := rename(item.stageDir, item.resource.TargetDir); err != nil {
			return rollback(err)
		}
		item.installed = true
	}
	for _, item := range items {
		if item.backupDir != "" {
			_ = os.RemoveAll(item.backupDir)
		}
	}
	return nil
}
