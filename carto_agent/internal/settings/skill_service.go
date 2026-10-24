package settings

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/governance"
	"github.com/autocarto/carto_agent/internal/logger"
	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
)

type SkillReferenceProvider interface {
	FindSkillReferences(skillName string) []config.SkillReference
}

type SkillReferencedError struct {
	SkillName   string
	References  []config.SkillReference
	Description string
}

func (e *SkillReferencedError) Error() string {
	if e == nil {
		return "skill 正在被引用"
	}
	if strings.TrimSpace(e.Description) != "" {
		return strings.TrimSpace(e.Description)
	}
	items := make([]string, 0, len(e.References))
	for _, ref := range e.References {
		items = append(items, fmt.Sprintf("%s/%s/%s (%s)", ref.ConfigName, ref.AgentName, ref.Layer, ref.Source))
	}
	if len(items) == 0 {
		return fmt.Sprintf("skill 正在被引用，无法删除: %s", strings.TrimSpace(e.SkillName))
	}
	return fmt.Sprintf("skill 正在被引用，无法删除: %s", strings.Join(items, ", "))
}

type SkillMutationUnsupportedError struct {
	Operation string
	Target    string
	Reason    string
}

func (e *SkillMutationUnsupportedError) Error() string {
	if e == nil {
		return "不支持的技能包操作"
	}
	if strings.TrimSpace(e.Reason) != "" {
		return strings.TrimSpace(e.Reason)
	}
	if strings.TrimSpace(e.Target) == "" {
		return fmt.Sprintf("操作 %s 不支持当前技能包", strings.TrimSpace(e.Operation))
	}
	return fmt.Sprintf("技能包 %s 不支持操作 %s", strings.TrimSpace(e.Target), strings.TrimSpace(e.Operation))
}

type SkillPackageNotFoundError struct {
	PackageID string
}

func (e *SkillPackageNotFoundError) Error() string {
	if e == nil {
		return "skill package 不存在"
	}
	return fmt.Sprintf("skill package 不存在: %s", strings.TrimSpace(e.PackageID))
}

const (
	SkillPackageScopeBuiltin   = "builtin"
	SkillPackageScopeUser      = "user"
	SkillPackageScopeGlobal    = "global"
	SkillPackageScopeExclusive = "exclusive"
	SkillPackageScopeAll       = "all"
)

type SkillPackageSummary struct {
	PackageID     string   `json:"package_id"`
	Name          string   `json:"name"`
	DisplayName   string   `json:"display_name,omitempty"`
	Domain        string   `json:"domain,omitempty"`
	System        bool     `json:"system"`
	Hidden        bool     `json:"hidden"`
	AutoMount     string   `json:"auto_mount"`
	Deprecated    bool     `json:"deprecated,omitempty"`
	Description   string   `json:"description,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	Backend       string   `json:"backend,omitempty"`
	Source        string   `json:"source,omitempty"`
	Enabled       bool     `json:"enabled"`
	Tags          []string `json:"tags,omitempty"`
	RequiresTools []string `json:"requires_tools,omitempty"`
	ToolCount     int      `json:"tool_count,omitempty"`
	ToolRefCount  int      `json:"tool_ref_count,omitempty"`
	HasTools      bool     `json:"has_tools,omitempty"`
	HasConfig     bool     `json:"has_config,omitempty"`
	Path          string   `json:"path,omitempty"`
	SkillPath     string   `json:"skill_path,omitempty"`
	UpdatedAt     string   `json:"updated_at,omitempty"`
	SizeBytes     int64    `json:"size_bytes,omitempty"`
	Scope         string   `json:"scope,omitempty"`
	OwnerConfig   string   `json:"owner_config,omitempty"`
}

type SkillPackageDetail struct {
	SkillPackageSummary
	FrontMatter  skill.SkillFrontMatter  `json:"front_matter"`
	MarkdownBody string                  `json:"markdown_body,omitempty"`
	ToolRefs     []string                `json:"tool_refs,omitempty"`
	Tools        []skill.SkillDefinition `json:"-"`
	Config       map[string]any          `json:"config,omitempty"`
	References   []config.SkillReference `json:"references,omitempty"`
}

func (d SkillPackageDetail) Summary() SkillPackageSummary {
	return d.SkillPackageSummary
}

type skillPackageFilesystemMeta struct {
	UpdatedAt time.Time
	SizeBytes int64
	HasConfig bool
}

type skillPackageLocation struct {
	Scope       string
	OwnerConfig string
	Dir         string
}

type FileSkillServiceConfig struct {
	SkillsDir                        string
	BuiltinSkillsDir                 string
	UserSkillsDir                    string
	GlobalSkillsDir                  string
	BuiltinToolsDir                  string
	UserToolsDir                     string
	ResolveRuntimeRegistryGovernance func() RuntimeRegistryGovernance
	RuntimeToolDirs                  []string
	ResolveRuntimeToolDirs           func() []string
	RuntimeSkillDirs                 []string
	ResolveRuntimeSkillDirs          func() []string
	ActiveConfigName                 string
	ResolveActiveConfigName          func() string
	ResolveExclusiveSkillDir         func(configName string) string
}

type exclusiveSkillDirProvider interface {
	ListExclusiveSkillDirs() map[string]string
}

type FileSkillService struct {
	SkillsDir                        string
	BuiltinSkillsDir                 string
	UserSkillsDir                    string
	GlobalSkillsDir                  string
	BuiltinToolsDir                  string
	UserToolsDir                     string
	ResolveRuntimeRegistryGovernance func() RuntimeRegistryGovernance
	RuntimeToolDirs                  []string
	ResolveRuntimeToolDirs           func() []string
	ExclusiveSkillsDirs              map[string]string
	ReferenceProvider                SkillReferenceProvider
	RuntimeSkillDirs                 []string
	ResolveRuntimeSkillDirs          func() []string
	ActiveConfigName                 string
	ResolveActiveConfigName          func() string
	ResolveExclusiveSkillDir         func(configName string) string
}

type RuntimeRegistryGovernance = governance.GovernedRegistry

func NewFileSkillService(skillsDir string, provider SkillReferenceProvider) *FileSkillService {
	trimmedSkillsDir := strings.TrimSpace(skillsDir)
	userToolsDir := InferUserToolsDirFromSkillsDir(trimmedSkillsDir)
	return NewFileSkillServiceWithConfig(FileSkillServiceConfig{UserSkillsDir: skillsDir, GlobalSkillsDir: skillsDir, SkillsDir: skillsDir, UserToolsDir: userToolsDir}, provider)
}

func InferUserToolsDirFromSkillsDir(skillsDir string) string {
	trimmedSkillsDir := strings.TrimSpace(skillsDir)
	if trimmedSkillsDir == "" {
		return ""
	}
	skillsPath := filepath.Clean(trimmedSkillsDir)
	baseDir := skillsPath
	baseName := filepath.Base(skillsPath)
	parentDir := filepath.Dir(skillsPath)
	if (strings.EqualFold(baseName, "system") || strings.EqualFold(baseName, "user") || strings.EqualFold(baseName, "global")) && strings.EqualFold(filepath.Base(parentDir), "skills") {
		baseDir = filepath.Dir(parentDir)
		return filepath.Join(baseDir, "tools", strings.ToLower(baseName))
	}
	return filepath.Join(baseDir, "tools", "user")
}

func InferSiblingToolsDirFromSkillsDir(skillsDir string) string {
	trimmedSkillsDir := strings.TrimSpace(skillsDir)
	if trimmedSkillsDir == "" {
		return ""
	}
	skillsPath := filepath.Clean(trimmedSkillsDir)
	baseDir := skillsPath
	baseName := filepath.Base(skillsPath)
	parentDir := filepath.Dir(skillsPath)
	if (strings.EqualFold(baseName, "system") || strings.EqualFold(baseName, "user") || strings.EqualFold(baseName, "global")) && strings.EqualFold(filepath.Base(parentDir), "skills") {
		baseDir = filepath.Dir(parentDir)
		return filepath.Join(baseDir, "tools", strings.ToLower(baseName))
	}
	if strings.EqualFold(baseName, "skills") {
		return filepath.Join(filepath.Dir(skillsPath), "tools", "system")
	}
	return filepath.Join(filepath.Dir(skillsPath), "tools")
}

func NewFileSkillServiceWithConfig(cfg FileSkillServiceConfig, provider SkillReferenceProvider) *FileSkillService {
	builtinSkillsDir := strings.TrimSpace(cfg.BuiltinSkillsDir)
	userSkillsDir := strings.TrimSpace(cfg.UserSkillsDir)
	globalSkillsDir := strings.TrimSpace(cfg.GlobalSkillsDir)
	legacySkillsDir := strings.TrimSpace(cfg.SkillsDir)
	if userSkillsDir == "" {
		userSkillsDir = legacySkillsDir
	}
	if globalSkillsDir == "" {
		globalSkillsDir = legacySkillsDir
	}
	managedSkillsDir := globalSkillsDir
	if managedSkillsDir == "" {
		managedSkillsDir = userSkillsDir
	}
	service := &FileSkillService{
		SkillsDir:                        managedSkillsDir,
		BuiltinSkillsDir:                 builtinSkillsDir,
		UserSkillsDir:                    userSkillsDir,
		GlobalSkillsDir:                  globalSkillsDir,
		BuiltinToolsDir:                  strings.TrimSpace(cfg.BuiltinToolsDir),
		UserToolsDir:                     strings.TrimSpace(cfg.UserToolsDir),
		ResolveRuntimeRegistryGovernance: cfg.ResolveRuntimeRegistryGovernance,
		RuntimeToolDirs:                  normalizePaths(cfg.RuntimeToolDirs),
		ResolveRuntimeToolDirs:           cfg.ResolveRuntimeToolDirs,
		ExclusiveSkillsDirs:              map[string]string{},
		ReferenceProvider:                provider,
		RuntimeSkillDirs:                 normalizePaths(cfg.RuntimeSkillDirs),
		ResolveRuntimeSkillDirs:          cfg.ResolveRuntimeSkillDirs,
		ActiveConfigName:                 strings.TrimSpace(cfg.ActiveConfigName),
		ResolveActiveConfigName:          cfg.ResolveActiveConfigName,
		ResolveExclusiveSkillDir:         cfg.ResolveExclusiveSkillDir,
	}
	if provider != nil {
		value := reflect.ValueOf(provider)
		if value.Kind() == reflect.Pointer && value.IsNil() {
			return service
		}
		if lister, ok := provider.(exclusiveSkillDirProvider); ok {
			for configName, dir := range lister.ListExclusiveSkillDirs() {
				trimmedName := strings.TrimSpace(configName)
				trimmedDir := strings.TrimSpace(dir)
				if trimmedName == "" || trimmedDir == "" {
					continue
				}
				service.ExclusiveSkillsDirs[trimmedName] = trimmedDir
			}
		}
	}
	return service
}

func (s *FileSkillService) ManagedSkillsDir() string {
	if s == nil {
		return ""
	}
	if strings.TrimSpace(s.GlobalSkillsDir) != "" {
		return strings.TrimSpace(s.GlobalSkillsDir)
	}
	return strings.TrimSpace(s.UserSkillsDir)
}

func (s *FileSkillService) ManagedToolsDir() string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(s.UserToolsDir)
}

// SkillCatalogWatchDirs exposes the current filesystem inputs for cheap watcher
// invalidation. It does not load registries or cache the active overlay paths.
func (s *FileSkillService) SkillCatalogWatchDirs() []string {
	return normalizePaths(append(s.currentRuntimeSkillDirs(), s.currentRuntimeToolDirs()...))
}

func (s *FileSkillService) ensureSkillsDir() error {
	if s == nil {
		return fmt.Errorf("skills 目录未初始化")
	}
	if len(s.currentRuntimeSkillDirs()) == 0 && strings.TrimSpace(s.UserSkillsDir) == "" && strings.TrimSpace(s.GlobalSkillsDir) == "" {
		return fmt.Errorf("skills 目录未初始化")
	}
	return nil
}

func (s *FileSkillService) ListSkillPackages() ([]SkillPackageSummary, error) {
	if err := s.ensureSkillsDir(); err != nil {
		return nil, err
	}
	if registry, governed, err := s.loadRuntimeSkillRegistry(); err != nil {
		return nil, err
	} else if governed {
		if registry == nil {
			return nil, nil
		}
		// Reuse one tool registry for this listing instead of rebuilding it for
		// every package. It remains local so subsequent listings see file edits.
		toolRegistry, err := s.loadRuntimeToolRegistry()
		if err != nil {
			return nil, err
		}
		visible := make(map[string]SkillPackageSummary)
		for _, pkg := range registry.Packages() {
			detail, ok, loadErr := s.loadGovernedSkillPackageDetailWithRegistry(pkg, toolRegistry)
			if loadErr != nil {
				logger.Warn("读取 governed 技能包失败 %s: %v", pkg.Name, loadErr)
				continue
			}
			if !ok || detail.Deprecated {
				continue
			}
			visible[detail.PackageID] = detail.Summary()
		}
		packages := make([]SkillPackageSummary, 0, len(visible))
		for _, item := range visible {
			packages = append(packages, item)
		}
		sort.Slice(packages, func(i, j int) bool {
			left := strings.TrimSpace(packages[i].Name)
			right := strings.TrimSpace(packages[j].Name)
			if left != right {
				return left < right
			}
			if packages[i].Scope != packages[j].Scope {
				return packages[i].Scope < packages[j].Scope
			}
			if packages[i].OwnerConfig != packages[j].OwnerConfig {
				return packages[i].OwnerConfig < packages[j].OwnerConfig
			}
			return packages[i].PackageID < packages[j].PackageID
		})
		return packages, nil
	}
	locations := s.runtimeLocations()
	if len(locations) == 0 {
		return nil, nil
	}
	visible := make(map[string]SkillPackageSummary)
	for _, location := range locations {
		entries, err := os.ReadDir(location.Dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("读取 skills 目录失败: %w", err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			isPackageDir, checkErr := isSkillPackageDir(filepath.Join(location.Dir, entry.Name()))
			if checkErr != nil {
				logger.Warn("检查技能包目录失败 %s: %v", entry.Name(), checkErr)
				continue
			}
			if !isPackageDir {
				continue
			}
			detail, loadErr := s.loadSkillPackageDetailAt(entry.Name(), location)
			if loadErr != nil {
				var notFoundErr *SkillPackageNotFoundError
				if errors.As(loadErr, &notFoundErr) {
					logger.Debug("跳过非技能包目录 %s: %v", entry.Name(), loadErr)
					continue
				}
				logger.Warn("读取技能包失败 %s: %v", entry.Name(), loadErr)
				continue
			}
			if !detail.Deprecated {
				visible[detail.PackageID] = detail.Summary()
			}
		}
	}
	packages := make([]SkillPackageSummary, 0, len(visible))
	for _, item := range visible {
		packages = append(packages, item)
	}
	sort.Slice(packages, func(i, j int) bool {
		left := strings.TrimSpace(packages[i].Name)
		right := strings.TrimSpace(packages[j].Name)
		if left != right {
			return left < right
		}
		if packages[i].Scope != packages[j].Scope {
			return packages[i].Scope < packages[j].Scope
		}
		if packages[i].OwnerConfig != packages[j].OwnerConfig {
			return packages[i].OwnerConfig < packages[j].OwnerConfig
		}
		return packages[i].PackageID < packages[j].PackageID
	})
	return packages, nil
}

func (s *FileSkillService) ListSkillPackagesByScope(scope, configName string) ([]SkillPackageSummary, error) {
	if err := s.ensureSkillsDir(); err != nil {
		return nil, err
	}
	locations := s.listLocations(scope, configName)
	packages := make([]SkillPackageSummary, 0)
	for _, location := range locations {
		entries, err := os.ReadDir(location.Dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("读取 skills 目录失败: %w", err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			isPackageDir, checkErr := isSkillPackageDir(filepath.Join(location.Dir, entry.Name()))
			if checkErr != nil {
				logger.Warn("检查技能包目录失败 %s: %v", entry.Name(), checkErr)
				continue
			}
			if !isPackageDir {
				continue
			}
			detail, loadErr := s.loadSkillPackageDetailAt(entry.Name(), location)
			if loadErr != nil {
				var notFoundErr *SkillPackageNotFoundError
				if errors.As(loadErr, &notFoundErr) {
					logger.Debug("跳过非技能包目录 %s: %v", entry.Name(), loadErr)
					continue
				}
				logger.Warn("读取技能包失败 %s: %v", entry.Name(), loadErr)
				continue
			}
			if !detail.Deprecated {
				packages = append(packages, detail.Summary())
			}
		}
	}
	sort.Slice(packages, func(i, j int) bool {
		left := strings.TrimSpace(packages[i].Name)
		right := strings.TrimSpace(packages[j].Name)
		if left != right {
			return left < right
		}
		if packages[i].Scope != packages[j].Scope {
			return packages[i].Scope < packages[j].Scope
		}
		if packages[i].OwnerConfig != packages[j].OwnerConfig {
			return packages[i].OwnerConfig < packages[j].OwnerConfig
		}
		return packages[i].PackageID < packages[j].PackageID
	})
	return packages, nil
}

func isSkillPackageDir(packageDir string) (bool, error) {
	skillPath := filepath.Join(strings.TrimSpace(packageDir), "SKILL.md")
	if strings.TrimSpace(packageDir) == "" {
		return false, nil
	}
	if _, err := os.Stat(skillPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *FileSkillService) GetSkillPackage(skillName string) (SkillPackageDetail, error) {
	if err := s.ensureSkillsDir(); err != nil {
		return SkillPackageDetail{}, err
	}
	if pkg, governed, err := s.resolveGovernedSkillPackage(SanitizeSkillName(skillName)); err != nil {
		return SkillPackageDetail{}, err
	} else if governed {
		if strings.TrimSpace(pkg.Dir) == "" {
			return SkillPackageDetail{}, &SkillPackageNotFoundError{PackageID: SanitizeSkillName(skillName)}
		}
		detail, loaded, loadErr := s.loadGovernedSkillPackageDetail(pkg)
		if loadErr != nil {
			return SkillPackageDetail{}, loadErr
		}
		if !loaded {
			return SkillPackageDetail{}, &SkillPackageNotFoundError{PackageID: SanitizeSkillName(skillName)}
		}
		return detail, nil
	}
	location, err := s.findVisiblePackageLocation(skillName)
	if err != nil {
		return SkillPackageDetail{}, err
	}
	return s.loadSkillPackageDetailAt(skillName, location)
}

func (s *FileSkillService) GetSkillPackageByScope(skillName, scope, configName string) (SkillPackageDetail, error) {
	if err := s.ensureSkillsDir(); err != nil {
		return SkillPackageDetail{}, err
	}
	location, err := s.findPackageLocation(skillName, scope, configName)
	if err != nil {
		return SkillPackageDetail{}, err
	}
	return s.loadSkillPackageDetailAt(skillName, location)
}

func (s *FileSkillService) GetSkillPackageReferences(skillName string) []config.SkillReference {
	if s == nil || s.ReferenceProvider == nil {
		return nil
	}
	packageID := strings.TrimSpace(skillName)
	if packageID == "" {
		return nil
	}
	return collectUniqueSkillReferences(s.ReferenceProvider, packageID)
}

func (s *FileSkillService) SetSkillPackageEnabled(skillName string, enabled bool) error {
	return s.SetSkillPackageEnabledByScope(skillName, "", "", enabled)
}

func (s *FileSkillService) SetSkillPackageEnabledByScope(skillName, scope, configName string, enabled bool) error {
	if err := s.ensureSkillsDir(); err != nil {
		return err
	}
	location, detail, err := s.resolveMutationTargetLocation(skillName, scope, configName)
	if err != nil {
		return err
	}
	configMap, configPath, err := s.readSkillPackageConfigMap(filepath.Join(location.Dir, detail.PackageID))
	if err != nil {
		return err
	}
	if configMap == nil {
		configMap = make(map[string]any)
	}
	configMap["enabled"] = enabled
	if err := s.writeSkillPackageConfigMap(configPath, configMap); err != nil {
		return err
	}
	logger.Info("SkillPackage enabled 状态已更新: %s enabled=%v", detail.PackageID, enabled)
	return nil
}

func (s *FileSkillService) listLocations(scope, configName string) []skillPackageLocation {
	exclusiveDirs := s.currentExclusiveSkillDirs()
	trimmedConfig := strings.TrimSpace(configName)
	normalizedScope := strings.ToLower(strings.TrimSpace(scope))
	if normalizedScope == "" {
		normalizedScope = SkillPackageScopeAll
	}

	runtimeLocations := s.runtimeLocations()
	appendExclusiveLocation := func(items []skillPackageLocation, owner string) []skillPackageLocation {
		dir := strings.TrimSpace(exclusiveDirs[owner])
		if dir == "" {
			return items
		}
		return append(items, skillPackageLocation{Scope: SkillPackageScopeExclusive, OwnerConfig: owner, Dir: dir})
	}

	switch normalizedScope {
	case SkillPackageScopeBuiltin:
		locations := make([]skillPackageLocation, 0, len(runtimeLocations))
		for _, location := range runtimeLocations {
			if strings.EqualFold(location.Scope, SkillPackageScopeBuiltin) {
				locations = append(locations, location)
			}
		}
		return locations
	case SkillPackageScopeUser:
		locations := make([]skillPackageLocation, 0, len(runtimeLocations))
		for _, location := range runtimeLocations {
			if strings.EqualFold(location.Scope, SkillPackageScopeUser) {
				locations = append(locations, location)
			}
		}
		return locations
	case SkillPackageScopeGlobal:
		locations := make([]skillPackageLocation, 0, len(runtimeLocations))
		for _, location := range runtimeLocations {
			if strings.EqualFold(location.Scope, SkillPackageScopeBuiltin) || strings.EqualFold(location.Scope, SkillPackageScopeUser) || strings.EqualFold(location.Scope, SkillPackageScopeGlobal) {
				locations = append(locations, location)
			}
		}
		return locations
	case SkillPackageScopeExclusive:
		if trimmedConfig != "" {
			if dir := strings.TrimSpace(exclusiveDirs[trimmedConfig]); dir != "" {
				return []skillPackageLocation{{Scope: SkillPackageScopeExclusive, OwnerConfig: trimmedConfig, Dir: dir}}
			}
			return nil
		}
		locations := make([]skillPackageLocation, 0, len(exclusiveDirs))
		for _, owner := range s.sortedExclusiveConfigs(exclusiveDirs) {
			locations = appendExclusiveLocation(locations, owner)
		}
		return locations
	default:
		locations := append([]skillPackageLocation(nil), s.listLocations(SkillPackageScopeGlobal, "")...)
		if trimmedConfig != "" {
			return appendExclusiveLocation(locations, trimmedConfig)
		}
		for _, owner := range s.sortedExclusiveConfigs(exclusiveDirs) {
			locations = appendExclusiveLocation(locations, owner)
		}
		return locations
	}
}

func (s *FileSkillService) currentExclusiveSkillDirs() map[string]string {
	dirs := make(map[string]string, len(s.ExclusiveSkillsDirs))
	for configName, dir := range s.ExclusiveSkillsDirs {
		trimmedName := strings.TrimSpace(configName)
		trimmedDir := strings.TrimSpace(dir)
		if trimmedName == "" || trimmedDir == "" {
			continue
		}
		dirs[trimmedName] = trimmedDir
	}
	if s.ReferenceProvider == nil {
		return dirs
	}
	value := reflect.ValueOf(s.ReferenceProvider)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return dirs
	}
	lister, ok := s.ReferenceProvider.(exclusiveSkillDirProvider)
	if !ok {
		return dirs
	}
	for configName, dir := range lister.ListExclusiveSkillDirs() {
		trimmedName := strings.TrimSpace(configName)
		trimmedDir := strings.TrimSpace(dir)
		if trimmedName == "" || trimmedDir == "" {
			continue
		}
		dirs[trimmedName] = trimmedDir
	}
	return dirs
}

func (s *FileSkillService) sortedExclusiveConfigs(exclusiveDirs map[string]string) []string {
	if len(exclusiveDirs) == 0 {
		return nil
	}
	owners := make([]string, 0, len(exclusiveDirs))
	for configName, dir := range exclusiveDirs {
		if strings.TrimSpace(configName) == "" || strings.TrimSpace(dir) == "" {
			continue
		}
		owners = append(owners, configName)
	}
	sort.Strings(owners)
	return owners
}

func (s *FileSkillService) findPackageLocation(skillName, scope, configName string) (skillPackageLocation, error) {
	packageID := SanitizeSkillName(skillName)
	if packageID == "" {
		return skillPackageLocation{}, &SkillPackageNotFoundError{PackageID: skillName}
	}
	for _, location := range s.listLocations(scope, configName) {
		skillPath := filepath.Join(location.Dir, packageID, "SKILL.md")
		if _, err := os.Stat(skillPath); err == nil {
			return location, nil
		} else if err != nil && !os.IsNotExist(err) {
			return skillPackageLocation{}, fmt.Errorf("读取技能包路径失败: %w", err)
		}
	}
	return skillPackageLocation{}, &SkillPackageNotFoundError{PackageID: packageID}
}

func (s *FileSkillService) ListSkills() ([]map[string]any, error) {
	packages, err := s.ListSkillPackages()
	if err != nil {
		return nil, err
	}
	skills := make([]map[string]any, 0, len(packages))
	for _, pkg := range packages {
		skills = append(skills, legacySkillSummaryFromPackage(pkg))
	}
	return skills, nil
}

func (s *FileSkillService) GetSkillContent(skillName string) (map[string]any, error) {
	detail, err := s.GetSkillPackage(skillName)
	if err != nil {
		return nil, err
	}
	return legacySkillDetailFromPackage(detail), nil
}

func (s *FileSkillService) SaveSkillWithScope(skillName, scope, configName, description, content string) error {
	if err := s.ensureSkillsDir(); err != nil {
		return err
	}
	packageID := SanitizeSkillName(skillName)
	if packageID == "" {
		return fmt.Errorf("skill 名称不能为空")
	}
	location, detail, err := s.resolveMutationTargetLocation(packageID, scope, configName)
	exists := err == nil
	if err != nil {
		if _, ok := err.(*SkillPackageNotFoundError); !ok {
			return err
		}
		if strings.TrimSpace(scope) != "" || strings.TrimSpace(configName) != "" {
			location, err = s.defaultImportLocation(scope, configName)
			if err != nil {
				return err
			}
		}
	}
	packageDir := filepath.Join(location.Dir, packageID)

	frontMatter := map[string]any{}
	if exists {
		raw, readErr := os.ReadFile(detail.SkillPath)
		if readErr != nil {
			return fmt.Errorf("读取 skill 文件失败: %w", readErr)
		}
		parsed, _, parseErr := parseSkillPackageDocument(string(raw))
		if parseErr != nil {
			return fmt.Errorf("解析 skill 文件失败: %w", parseErr)
		}
		frontMatter = parsed
		if strings.TrimSpace(fmt.Sprint(frontMatter["name"])) == "" && strings.TrimSpace(detail.Name) != "" {
			frontMatter["name"] = detail.Name
		}
		if strings.TrimSpace(fmt.Sprint(frontMatter["kind"])) == "" && strings.TrimSpace(detail.Kind) != "" {
			frontMatter["kind"] = detail.Kind
		}
	} else {
		frontMatter["name"] = packageID
		frontMatter["kind"] = skill.SkillPackageKindWorkflow
	}
	frontMatter["description"] = strings.TrimSpace(strings.ReplaceAll(description, "\n", " "))
	rawContent, buildErr := buildSkillPackageDocument(frontMatter, content)
	if buildErr != nil {
		return buildErr
	}
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		return fmt.Errorf("创建 skill 目录失败: %w", err)
	}
	skillPath := filepath.Join(packageDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(rawContent), 0o644); err != nil {
		return fmt.Errorf("写入 skill 文件失败: %w", err)
	}
	logger.Info("Skill 已保存: %s", skillPath)
	return nil
}

func (s *FileSkillService) SaveSkill(skillName, description, content string) error {
	return s.SaveSkillWithScope(skillName, "", "", description, content)
}

func (s *FileSkillService) DeleteSkillWithScope(skillName, scope, configName string) error {
	if err := s.ensureSkillsDir(); err != nil {
		return err
	}
	target := SanitizeSkillName(skillName)
	if target == "" {
		return fmt.Errorf("skill 名称不能为空")
	}
	if refs := s.GetSkillPackageReferences(target); len(refs) > 0 {
		return &SkillReferencedError{SkillName: target, References: refs}
	}
	var (
		location skillPackageLocation
		err      error
	)
	if strings.TrimSpace(scope) != "" || strings.TrimSpace(configName) != "" {
		location, err = s.findPackageLocation(target, scope, configName)
	} else {
		location, err = s.findVisiblePackageLocation(target)
	}
	if err != nil {
		return err
	}
	if !s.isWritableLocation(location) {
		return &SkillMutationUnsupportedError{Operation: "delete", Target: target, Reason: "当前可见技能包来自只读 overlay 来源，无法直接删除，请先删除可写 override"}
	}
	if err := os.RemoveAll(filepath.Join(location.Dir, target)); err != nil {
		return fmt.Errorf("删除 skill 失败: %w", err)
	}
	logger.Info("Skill 已删除: %s", target)
	return nil
}

func (s *FileSkillService) DeleteSkill(skillName string) error {
	return s.DeleteSkillWithScope(skillName, "", "")
}

func (s *FileSkillService) ImportSkillWithScope(sourcePath string, overwrite bool, scope, configName string) (map[string]any, error) {
	if err := s.ensureSkillsDir(); err != nil {
		return nil, err
	}
	targetLocation, err := s.defaultImportLocation(scope, configName)
	if err != nil {
		return nil, err
	}
	return s.importSkillToLocation(sourcePath, overwrite, targetLocation)
}

func (s *FileSkillService) ImportSkill(sourcePath string, overwrite bool) (map[string]any, error) {
	return s.ImportSkillWithScope(sourcePath, overwrite, "", "")
}

func (s *FileSkillService) importSkillToLocation(sourcePath string, overwrite bool, targetLocation skillPackageLocation) (map[string]any, error) {
	skillImportMu.Lock()
	defer skillImportMu.Unlock()
	if err := s.ensureSkillsDir(); err != nil {
		return nil, err
	}
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return nil, fmt.Errorf("source_path 不能为空")
	}
	absPath, err := filepath.Abs(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("解析 source_path 失败: %w", err)
	}
	stat, err := os.Lstat(absPath)
	if err != nil {
		return nil, fmt.Errorf("读取 source_path 失败: %w", err)
	}

	result := map[string]any{
		"action":   "import",
		"mode":     "",
		"imported": []map[string]any{},
		"skipped":  []map[string]any{},
	}

	switch {
	case stat.Mode().IsRegular():
		if !strings.EqualFold(filepath.Ext(absPath), ".md") {
			return nil, fmt.Errorf("请选择 Markdown 技能文件、包含 SKILL.md 的文件夹或有效的扩展清单")
		}
		result["mode"] = "file"
		imported, skipped, importErr := s.importSkillFromFile(absPath, DefaultSkillNameFromPath(absPath), overwrite, targetLocation)
		if importErr != nil {
			return nil, importErr
		}
		if imported != nil {
			result["imported"] = append(result["imported"].([]map[string]any), imported)
		}
		if skipped != nil {
			result["skipped"] = append(result["skipped"].([]map[string]any), skipped)
		}

	case stat.IsDir():
		singleSkillFile := filepath.Join(absPath, "SKILL.md")
		if _, singleErr := os.Stat(singleSkillFile); singleErr == nil {
			if unsupportedErr := validateLegacySkillImportDirectory(absPath); unsupportedErr != nil {
				return nil, unsupportedErr
			}
			result["mode"] = "directory_single"
			imported, skipped, importErr := s.importSkillFromFile(singleSkillFile, SanitizeSkillName(filepath.Base(absPath)), overwrite, targetLocation)
			if importErr != nil {
				return nil, importErr
			}
			if imported != nil {
				result["imported"] = append(result["imported"].([]map[string]any), imported)
			}
			if skipped != nil {
				result["skipped"] = append(result["skipped"].([]map[string]any), skipped)
			}
			break
		}

		result["mode"] = "directory_batch"
		entries, readErr := os.ReadDir(absPath)
		if readErr != nil {
			return nil, fmt.Errorf("读取目录失败: %w", readErr)
		}
		importedList := result["imported"].([]map[string]any)
		skippedList := result["skipped"].([]map[string]any)
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			packageDir := filepath.Join(absPath, entry.Name())
			skillFilePath := filepath.Join(packageDir, "SKILL.md")
			if _, statErr := os.Stat(skillFilePath); statErr != nil {
				continue
			}
			if unsupportedErr := validateLegacySkillImportDirectory(packageDir); unsupportedErr != nil {
				skippedList = append(skippedList, map[string]any{
					"name":   SanitizeSkillName(entry.Name()),
					"source": skillFilePath,
					"reason": unsupportedErr.Error(),
				})
				continue
			}
			imported, skipped, importErr := s.importSkillFromFile(skillFilePath, SanitizeSkillName(entry.Name()), overwrite, targetLocation)
			if importErr != nil {
				skippedList = append(skippedList, map[string]any{
					"name":   SanitizeSkillName(entry.Name()),
					"source": skillFilePath,
					"reason": importErr.Error(),
				})
				continue
			}
			if imported != nil {
				importedList = append(importedList, imported)
			}
			if skipped != nil {
				skippedList = append(skippedList, skipped)
			}
		}
		result["imported"] = importedList
		result["skipped"] = skippedList
		if len(importedList) == 0 && len(skippedList) == 0 {
			return nil, fmt.Errorf("目录中未发现可导入的 SKILL.md")
		}

	default:
		return nil, fmt.Errorf("source_path 必须是文件或目录")
	}

	importedCount := len(result["imported"].([]map[string]any))
	skippedCount := len(result["skipped"].([]map[string]any))
	logger.Info("Skill 导入完成: action=import source_path=%s mode=%v imported=%d skipped=%d", absPath, result["mode"], importedCount, skippedCount)
	return result, nil
}

func (s *FileSkillService) defaultImportLocation(scope, configName string) (skillPackageLocation, error) {
	normalizedScope := strings.ToLower(strings.TrimSpace(scope))
	if normalizedScope == "" && strings.TrimSpace(configName) == "" {
		return s.defaultWritableLocation(), nil
	}
	switch normalizedScope {
	case "", SkillPackageScopeAll, SkillPackageScopeGlobal:
		return s.defaultWritableLocation(), nil
	case SkillPackageScopeUser:
		if strings.TrimSpace(s.UserSkillsDir) == "" {
			return s.defaultWritableLocation(), nil
		}
		return skillPackageLocation{Scope: SkillPackageScopeUser, Dir: strings.TrimSpace(s.UserSkillsDir)}, nil
	case SkillPackageScopeBuiltin:
		return skillPackageLocation{}, &SkillMutationUnsupportedError{Operation: "save", Target: strings.TrimSpace(configName), Reason: "builtin 技能包目录为只读，不能直接写入"}
	}
	locations := s.listLocations(scope, configName)
	if len(locations) == 0 {
		if strings.EqualFold(strings.TrimSpace(scope), SkillPackageScopeExclusive) && strings.TrimSpace(configName) != "" && s.ResolveExclusiveSkillDir != nil {
			if dir := strings.TrimSpace(s.ResolveExclusiveSkillDir(configName)); dir != "" {
				return skillPackageLocation{Scope: SkillPackageScopeExclusive, OwnerConfig: strings.TrimSpace(configName), Dir: dir}, nil
			}
		}
		return skillPackageLocation{}, &SkillPackageNotFoundError{PackageID: strings.TrimSpace(configName)}
	}
	return locations[0], nil
}

func (s *FileSkillService) importSkillFromFile(sourceSkillPath, fallbackName string, overwrite bool, targetLocation skillPackageLocation) (map[string]any, map[string]any, error) {
	var sourcePackageDir string
	if strings.EqualFold(filepath.Base(sourceSkillPath), "SKILL.md") {
		sourcePackageDir = filepath.Dir(sourceSkillPath)
		if err := validateLegacySkillImportDirectory(sourcePackageDir); err != nil {
			return nil, nil, err
		}
	}
	info, err := os.Lstat(sourceSkillPath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("技能文件必须是普通 Markdown 文件: %s", sourceSkillPath)
	}
	data, err := os.ReadFile(sourceSkillPath)
	if err != nil {
		return nil, nil, fmt.Errorf("读取 skill 文件失败: %w", err)
	}
	frontMatter, body, err := parseSkillPackageDocument(string(data))
	if err != nil {
		return nil, nil, fmt.Errorf("解析 skill 文件失败: %w", err)
	}
	packageKind := strings.ToLower(strings.TrimSpace(stringValue(frontMatter["kind"])))
	metadataRaw, err := yaml.Marshal(frontMatter)
	if err != nil {
		return nil, nil, err
	}
	var metadata skill.SkillFrontMatter
	if err := yaml.Unmarshal(metadataRaw, &metadata); err != nil {
		return nil, nil, err
	}
	if err := skill.ValidateToolPackageImport(skill.SkillPackage{Kind: packageKind, FrontMatter: metadata}); err != nil {
		return nil, nil, &SkillMutationUnsupportedError{Operation: "import", Target: sourceSkillPath, Reason: err.Error()}
	}
	packageName := SanitizeSkillName(stringValue(frontMatter["name"]))
	if packageName == "" {
		packageName = SanitizeSkillName(fallbackName)
	}
	if packageName == "" {
		return nil, nil, fmt.Errorf("无法识别 skill 名称: %s", sourceSkillPath)
	}
	targetDir := filepath.Join(targetLocation.Dir, packageName)
	targetPath := filepath.Join(targetDir, "SKILL.md")
	if err := validateSkillImportPaths(sourceSkillPath, sourcePackageDir, targetDir); err != nil {
		return nil, nil, err
	}
	if !overwrite {
		if _, lookupErr := s.findVisiblePackageLocation(packageName); lookupErr == nil {
			return nil, map[string]any{"name": packageName, "source": sourceSkillPath, "reason": "同名 skill 已存在，已跳过"}, nil
		} else {
			var missing *SkillPackageNotFoundError
			if !errors.As(lookupErr, &missing) {
				return nil, nil, lookupErr
			}
		}
	}
	// Also check the chosen writable layer: a visible overlay must not hide an
	// existing or malformed target directory from the overwrite check.
	if _, statErr := os.Lstat(targetDir); statErr == nil {
		if !overwrite {
			return nil, map[string]any{
				"name":   packageName,
				"source": sourceSkillPath,
				"reason": "同名 skill 已存在，已跳过",
			}, nil
		}
		if err := validateLegacySkillImportDirectory(targetDir); err != nil {
			return nil, nil, err
		}
	} else if !os.IsNotExist(statErr) {
		return nil, nil, statErr
	}
	frontMatter["name"] = packageName
	normalized, buildErr := buildSkillPackageDocument(frontMatter, body)
	if buildErr != nil {
		return nil, nil, buildErr
	}
	if err := installSkillImport(sourcePackageDir, targetDir, normalized); err != nil {
		return nil, nil, err
	}
	return map[string]any{
		"name":         packageName,
		"source":       sourceSkillPath,
		"target":       targetPath,
		"scope":        targetLocation.Scope,
		"owner_config": targetLocation.OwnerConfig,
	}, nil, nil
}

var skillImportMu sync.Mutex

func (s *FileSkillService) loadSkillPackageDetailAt(skillName string, location skillPackageLocation) (SkillPackageDetail, error) {
	toolRegistry, err := s.loadRuntimeToolRegistry()
	if err != nil {
		return SkillPackageDetail{}, err
	}
	return s.loadSkillPackageDetailAtWithRegistry(skillName, location, toolRegistry)
}

func (s *FileSkillService) loadSkillPackageDetailAtWithRegistry(skillName string, location skillPackageLocation, toolRegistry *toolpkg.Registry) (SkillPackageDetail, error) {
	packageID := SanitizeSkillName(skillName)
	if packageID == "" {
		return SkillPackageDetail{}, &SkillPackageNotFoundError{PackageID: skillName}
	}
	packageDir := filepath.Join(location.Dir, packageID)
	skillPath := filepath.Join(packageDir, "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		if os.IsNotExist(err) {
			return SkillPackageDetail{}, &SkillPackageNotFoundError{PackageID: packageID}
		}
		return SkillPackageDetail{}, fmt.Errorf("读取 SKILL.md 文件信息失败: %w", err)
	}
	registry := skill.NewRegistry(toolRegistry)
	if err := registry.LoadDirectories(packageDir); err != nil {
		return SkillPackageDetail{}, err
	}
	pkg, ok := registry.GetPackage(packageID)
	if !ok {
		packages := registry.Packages()
		if len(packages) != 1 {
			return SkillPackageDetail{}, &SkillPackageNotFoundError{PackageID: packageID}
		}
		pkg = packages[0]
	}
	meta, err := collectSkillPackageFilesystemMeta(packageDir)
	if err != nil {
		return SkillPackageDetail{}, err
	}
	configMap, _, err := s.readSkillPackageConfigMap(packageDir)
	if err != nil {
		return SkillPackageDetail{}, err
	}
	name := strings.TrimSpace(pkg.Name)
	if name == "" {
		name = packageID
	}
	toolCount := len(pkg.ToolRefs)
	detail := SkillPackageDetail{
		SkillPackageSummary: SkillPackageSummary{
			PackageID:     packageID,
			Name:          name,
			DisplayName:   pkg.DisplayName,
			Domain:        pkg.Domain,
			System:        pkg.System,
			Hidden:        pkg.Hidden,
			AutoMount:     pkg.AutoMount,
			Deprecated:    pkg.FrontMatter.Deprecated,
			Description:   strings.TrimSpace(pkg.FrontMatter.Description),
			Kind:          strings.TrimSpace(pkg.Kind),
			Backend:       strings.TrimSpace(pkg.FrontMatter.Backend),
			Source:        normalizeSkillPackageSourceLabel(pkg.Source, s.SkillsDir),
			Enabled:       pkg.Enabled,
			Tags:          append([]string(nil), pkg.FrontMatter.Tags...),
			RequiresTools: append([]string(nil), pkg.RequiresTools...),
			ToolCount:     toolCount,
			ToolRefCount:  len(pkg.ToolRefs),
			HasTools:      toolCount > 0,
			HasConfig:     meta.HasConfig,
			Path:          packageDir,
			SkillPath:     skillPath,
			UpdatedAt:     meta.UpdatedAt.Format(time.RFC3339),
			SizeBytes:     meta.SizeBytes,
			Scope:         location.Scope,
			OwnerConfig:   location.OwnerConfig,
		},
		FrontMatter:  pkg.FrontMatter,
		MarkdownBody: pkg.MarkdownBody,
		ToolRefs:     append([]string(nil), pkg.ToolRefs...),
		Tools:        cloneSkillDefinitions(pkg.Tools),
		Config:       cloneMapAny(configMap),
	}
	detail.References = s.GetSkillPackageReferences(packageID)
	return detail, nil
}

func normalizeSkillPackageSourceLabel(source, skillsDir string) string {
	trimmedSource := strings.TrimSpace(source)
	if trimmedSource == "" {
		return ""
	}
	if strings.TrimSpace(skillsDir) != "" {
		if filepath.Clean(trimmedSource) == filepath.Clean(strings.TrimSpace(skillsDir)) {
			return "workspace"
		}
	}
	return filepath.Base(trimmedSource)
}

func collectSkillPackageFilesystemMeta(packageDir string) (skillPackageFilesystemMeta, error) {
	meta := skillPackageFilesystemMeta{}
	err := filepath.WalkDir(packageDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(packageDir, path)
		if err != nil {
			return err
		}
		if !isTrackedSkillPackageFile(rel) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		meta.SizeBytes += info.Size()
		if info.ModTime().After(meta.UpdatedAt) {
			meta.UpdatedAt = info.ModTime()
		}
		if strings.EqualFold(rel, "config.yaml") || strings.EqualFold(rel, "config.yml") {
			meta.HasConfig = true
		}
		return nil
	})
	if err != nil {
		return skillPackageFilesystemMeta{}, fmt.Errorf("读取技能包目录失败: %w", err)
	}
	return meta, nil
}

func isTrackedSkillPackageFile(rel string) bool {
	trimmed := filepath.ToSlash(strings.TrimSpace(rel))
	if strings.EqualFold(trimmed, "SKILL.md") || strings.EqualFold(trimmed, "config.yaml") || strings.EqualFold(trimmed, "config.yml") || strings.EqualFold(trimmed, "workflow.yaml") || strings.EqualFold(trimmed, "workflow.yml") || strings.EqualFold(trimmed, "requirements.txt") {
		return true
	}
	if strings.HasPrefix(trimmed, "scripts/") || strings.HasPrefix(trimmed, "handlers/") {
		return true
	}
	return false
}

func (s *FileSkillService) readSkillPackageConfigMap(packageDir string) (map[string]any, string, error) {
	for _, name := range []string{"config.yaml", "config.yml"} {
		configPath := filepath.Join(packageDir, name)
		data, err := os.ReadFile(configPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, "", fmt.Errorf("读取技能包配置失败: %w", err)
		}
		configMap := make(map[string]any)
		if err := yaml.Unmarshal(data, &configMap); err != nil {
			return nil, "", fmt.Errorf("解析技能包配置失败: %w", err)
		}
		return configMap, configPath, nil
	}
	return nil, filepath.Join(packageDir, "config.yaml"), nil
}

func (s *FileSkillService) currentRuntimeToolDirs() []string {
	if s == nil {
		return nil
	}
	if s.ResolveRuntimeToolDirs != nil {
		if resolved := normalizePaths(s.ResolveRuntimeToolDirs()); len(resolved) > 0 {
			return resolved
		}
	}
	if len(s.RuntimeToolDirs) > 0 {
		return append([]string(nil), s.RuntimeToolDirs...)
	}
	if dirs := normalizePaths([]string{s.BuiltinToolsDir, s.UserToolsDir}); len(dirs) > 0 {
		return dirs
	}
	builtinToolsDir := config.ResolveBuiltinToolsDir()
	userToolsDir := config.ResolveUserToolsDir()
	if builtinToolsDir == "" && userToolsDir == "" {
		return nil
	}
	return normalizePaths([]string{builtinToolsDir, userToolsDir})
}

func (s *FileSkillService) loadRuntimeToolRegistry() (*toolpkg.Registry, error) {
	if governance := s.currentRuntimeRegistryGovernance(); governance != nil {
		return governance.LoadToolRegistry()
	}
	return nil, nil
}

func (s *FileSkillService) loadRuntimeSkillRegistry() (*skill.Registry, bool, error) {
	if governance := s.currentRuntimeRegistryGovernance(); governance != nil {
		registry, err := governance.LoadSkillRegistry()
		return registry, true, err
	}
	return nil, false, nil
}

func (s *FileSkillService) resolveGovernedSkillPackage(packageID string) (skill.SkillPackage, bool, error) {
	governance := s.currentRuntimeRegistryGovernance()
	if governance == nil {
		return skill.SkillPackage{}, false, nil
	}
	pkg, ok, err := governance.ResolveSkillPackage(strings.TrimSpace(packageID))
	if err != nil {
		return skill.SkillPackage{}, true, err
	}
	if !ok {
		return skill.SkillPackage{}, true, nil
	}
	return pkg, true, nil
}

func (s *FileSkillService) writeSkillPackageConfigMap(configPath string, configMap map[string]any) error {
	raw, err := yaml.Marshal(configMap)
	if err != nil {
		return fmt.Errorf("序列化技能包配置失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("创建技能包配置目录失败: %w", err)
	}
	if err := os.WriteFile(configPath, raw, 0o644); err != nil {
		return fmt.Errorf("写入技能包配置失败: %w", err)
	}
	return nil
}

func parseSkillPackageDocument(raw string) (map[string]any, string, error) {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return map[string]any{}, strings.TrimSpace(normalized), nil
	}
	lines := strings.Split(normalized, "\n")
	for idx := 1; idx < len(lines); idx++ {
		if strings.TrimSpace(lines[idx]) != "---" {
			continue
		}
		frontMatter := make(map[string]any)
		if err := yaml.Unmarshal([]byte(strings.Join(lines[1:idx], "\n")), &frontMatter); err != nil {
			return nil, "", err
		}
		body := strings.TrimSpace(strings.Join(lines[idx+1:], "\n"))
		return frontMatter, body, nil
	}
	return nil, "", fmt.Errorf("missing closing frontmatter delimiter")
}

func buildSkillPackageDocument(frontMatter map[string]any, body string) (string, error) {
	if frontMatter == nil {
		frontMatter = map[string]any{}
	}
	rawFrontMatter, err := yaml.Marshal(frontMatter)
	if err != nil {
		return "", fmt.Errorf("序列化技能包 frontmatter 失败: %w", err)
	}
	trimmedBody := strings.TrimSpace(body)
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.Write(rawFrontMatter)
	builder.WriteString("---\n")
	if trimmedBody != "" {
		builder.WriteString("\n")
		builder.WriteString(trimmedBody)
		builder.WriteString("\n")
	}
	return builder.String(), nil
}

func validateLegacySkillImportDirectory(packageDir string) error {
	if _, err := os.Stat(filepath.Join(packageDir, "workflow.yaml")); err == nil {
		return &SkillMutationUnsupportedError{Operation: "import", Target: packageDir, Reason: "串行 workflow.yaml 已下线，请使用 FLOW.md 流程包"}
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, name := range []string{"config.yaml", "config.yml"} {
		if _, err := os.Stat(filepath.Join(packageDir, name)); err == nil {
			return &SkillMutationUnsupportedError{Operation: "import", Target: packageDir, Reason: "含 config.yaml 的工具包请使用扩展清单导入"}
		}
	}
	toolsDir := filepath.Join(packageDir, "tools")
	info, err := os.Stat(toolsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取工具目录失败: %w", err)
	}
	if !info.IsDir() {
		return nil
	}
	unsupported := false
	walkErr := filepath.WalkDir(toolsDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yaml" || ext == ".yml" {
			unsupported = true
		}
		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("读取工具目录失败: %w", walkErr)
	}
	if unsupported {
		return &SkillMutationUnsupportedError{Operation: "import", Target: packageDir, Reason: "含 tools/*.yaml 工具定义的目录请使用扩展清单导入"}
	}
	return nil
}

func collectUniqueSkillReferences(provider SkillReferenceProvider, names ...string) []config.SkillReference {
	if provider == nil {
		return nil
	}
	value := reflect.ValueOf(provider)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return nil
	}
	refs := make([]config.SkillReference, 0)
	seen := make(map[string]struct{})
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		for _, ref := range provider.FindSkillReferences(trimmed) {
			key := fmt.Sprintf("%s|%s|%s|%s", ref.ConfigName, ref.AgentName, ref.Layer, ref.Source)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			refs = append(refs, ref)
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ConfigName != refs[j].ConfigName {
			return refs[i].ConfigName < refs[j].ConfigName
		}
		if refs[i].AgentName != refs[j].AgentName {
			return refs[i].AgentName < refs[j].AgentName
		}
		if refs[i].Layer != refs[j].Layer {
			return refs[i].Layer < refs[j].Layer
		}
		return refs[i].Source < refs[j].Source
	})
	return refs
}

func cloneSkillDefinitions(items []skill.SkillDefinition) []skill.SkillDefinition {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]skill.SkillDefinition, len(items))
	for idx, item := range items {
		cloned[idx] = item.Clone()
	}
	return cloned
}

func cloneMapAny(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		switch typed := value.(type) {
		case map[string]any:
			out[key] = cloneMapAny(typed)
		case []any:
			cloned := make([]any, len(typed))
			for idx, item := range typed {
				cloned[idx] = item
			}
			out[key] = cloned
		default:
			out[key] = value
		}
	}
	return out
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if typed, ok := value.(string); ok {
		return typed
	}
	return fmt.Sprint(value)
}

func legacySkillSummaryFromPackage(pkg SkillPackageSummary) map[string]any {
	return map[string]any{
		"skill_id":       pkg.PackageID,
		"name":           pkg.Name,
		"description":    pkg.Description,
		"path":           pkg.SkillPath,
		"package_path":   pkg.Path,
		"updated_at":     pkg.UpdatedAt,
		"size_bytes":     pkg.SizeBytes,
		"kind":           pkg.Kind,
		"backend":        pkg.Backend,
		"enabled":        pkg.Enabled,
		"tool_count":     pkg.ToolCount,
		"tool_ref_count": pkg.ToolRefCount,
		"has_tools":      pkg.HasTools,
		"has_config":     pkg.HasConfig,
		"tags":           append([]string(nil), pkg.Tags...),
		"requires_tools": append([]string(nil), pkg.RequiresTools...),
	}
}

func legacySkillDetailFromPackage(pkg SkillPackageDetail) map[string]any {
	result := legacySkillSummaryFromPackage(pkg.SkillPackageSummary)
	result["skill_name"] = pkg.Name
	result["content"] = pkg.MarkdownBody
	result["markdown_body"] = pkg.MarkdownBody
	result["front_matter"] = pkg.FrontMatter
	result["tool_refs"] = append([]string(nil), pkg.ToolRefs...)
	result["references"] = append([]config.SkillReference(nil), pkg.References...)
	result["config"] = cloneMapAny(pkg.Config)
	return result
}
func SanitizeSkillName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return ""
	}
	name = strings.ReplaceAll(name, "\\", "-")
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, " ", "-")
	for strings.Contains(name, "..") {
		name = strings.ReplaceAll(name, "..", "-")
	}
	name = strings.Trim(name, ".-")
	return strings.TrimSpace(name)
}

func DefaultSkillNameFromPath(path string) string {
	base := filepath.Base(path)
	if strings.EqualFold(base, "SKILL.md") {
		return SanitizeSkillName(filepath.Base(filepath.Dir(path)))
	}
	ext := filepath.Ext(base)
	return SanitizeSkillName(strings.TrimSuffix(base, ext))
}

func BuildStandardSkillMarkdown(skillName, description, content string) string {
	frontMatter := map[string]any{
		"name":        SanitizeSkillName(skillName),
		"description": strings.TrimSpace(strings.ReplaceAll(description, "\n", " ")),
		"kind":        skill.SkillPackageKindWorkflow,
	}
	raw, err := buildSkillPackageDocument(frontMatter, content)
	if err != nil {
		return strings.TrimSpace(content)
	}
	return raw
}

func normalizePaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		cleaned := filepath.Clean(trimmed)
		if _, exists := seen[cleaned]; exists {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

func samePath(left, right string) bool {
	trimmedLeft := strings.TrimSpace(left)
	trimmedRight := strings.TrimSpace(right)
	if trimmedLeft == "" || trimmedRight == "" {
		return false
	}
	return filepath.Clean(trimmedLeft) == filepath.Clean(trimmedRight)
}

func (s *FileSkillService) currentRuntimeSkillDirs() []string {
	if s == nil {
		return nil
	}
	if s.ResolveRuntimeSkillDirs != nil {
		return normalizePaths(s.ResolveRuntimeSkillDirs())
	}
	if len(s.RuntimeSkillDirs) > 0 {
		return normalizePaths(s.RuntimeSkillDirs)
	}
	return normalizePaths([]string{s.BuiltinSkillsDir, s.UserSkillsDir, s.GlobalSkillsDir})
}

func (s *FileSkillService) currentActiveConfigName() string {
	if s == nil {
		return ""
	}
	if s.ResolveActiveConfigName != nil {
		return strings.TrimSpace(s.ResolveActiveConfigName())
	}
	return strings.TrimSpace(s.ActiveConfigName)
}

func (s *FileSkillService) currentRuntimeRegistryGovernance() RuntimeRegistryGovernance {
	if s == nil || s.ResolveRuntimeRegistryGovernance == nil {
		return nil
	}
	return s.ResolveRuntimeRegistryGovernance()
}

func (s *FileSkillService) runtimeLocations() []skillPackageLocation {
	dirs := s.currentRuntimeSkillDirs()
	if len(dirs) == 0 {
		return nil
	}
	exclusiveDirs := s.currentExclusiveSkillDirs()
	locations := make([]skillPackageLocation, 0, len(dirs))
	for _, dir := range dirs {
		location := skillPackageLocation{Scope: SkillPackageScopeBuiltin, Dir: dir}
		for owner, exclusiveDir := range exclusiveDirs {
			if samePath(dir, exclusiveDir) {
				location.Scope = SkillPackageScopeExclusive
				location.OwnerConfig = owner
				break
			}
		}
		if samePath(dir, s.UserSkillsDir) {
			location.Scope = SkillPackageScopeUser
			location.OwnerConfig = ""
		} else if samePath(dir, s.GlobalSkillsDir) {
			location.Scope = SkillPackageScopeGlobal
			location.OwnerConfig = ""
		} else if samePath(dir, s.BuiltinSkillsDir) {
			location.Scope = SkillPackageScopeBuiltin
			location.OwnerConfig = ""
		}
		locations = append(locations, location)
	}
	return locations
}

func (s *FileSkillService) defaultWritableLocation() skillPackageLocation {
	if strings.TrimSpace(s.GlobalSkillsDir) != "" {
		return skillPackageLocation{Scope: SkillPackageScopeGlobal, Dir: strings.TrimSpace(s.GlobalSkillsDir)}
	}
	return skillPackageLocation{Scope: SkillPackageScopeUser, Dir: strings.TrimSpace(s.UserSkillsDir)}
}

func (s *FileSkillService) isWritableLocation(location skillPackageLocation) bool {
	if s == nil {
		return false
	}
	if samePath(location.Dir, s.GlobalSkillsDir) {
		return true
	}
	if samePath(location.Dir, s.UserSkillsDir) {
		return true
	}
	for _, dir := range s.currentExclusiveSkillDirs() {
		if samePath(location.Dir, dir) {
			return true
		}
	}
	if strings.EqualFold(strings.TrimSpace(location.Scope), SkillPackageScopeExclusive) && strings.TrimSpace(location.OwnerConfig) != "" && s.ResolveExclusiveSkillDir != nil {
		if dir := strings.TrimSpace(s.ResolveExclusiveSkillDir(location.OwnerConfig)); samePath(location.Dir, dir) {
			return true
		}
	}
	return false
}

func (s *FileSkillService) findVisiblePackageLocation(skillName string) (skillPackageLocation, error) {
	packageID := SanitizeSkillName(skillName)
	if packageID == "" {
		return skillPackageLocation{}, &SkillPackageNotFoundError{PackageID: skillName}
	}
	if pkg, governed, err := s.resolveGovernedSkillPackage(packageID); err != nil {
		return skillPackageLocation{}, err
	} else if governed {
		if strings.TrimSpace(pkg.Dir) == "" {
			return skillPackageLocation{}, &SkillPackageNotFoundError{PackageID: packageID}
		}
		location, found := s.locationForGovernedPackageDir(pkg.Dir)
		if !found {
			return skillPackageLocation{}, &SkillPackageNotFoundError{PackageID: packageID}
		}
		return location, nil
	}
	locations := s.runtimeLocations()
	for idx := len(locations) - 1; idx >= 0; idx-- {
		location := locations[idx]
		skillPath := filepath.Join(location.Dir, packageID, "SKILL.md")
		if _, err := os.Stat(skillPath); err == nil {
			return location, nil
		} else if err != nil && !os.IsNotExist(err) {
			return skillPackageLocation{}, fmt.Errorf("读取技能包路径失败: %w", err)
		}
	}
	return skillPackageLocation{}, &SkillPackageNotFoundError{PackageID: packageID}
}

func (s *FileSkillService) loadGovernedSkillPackageDetail(pkg skill.SkillPackage) (SkillPackageDetail, bool, error) {
	toolRegistry, err := s.loadRuntimeToolRegistry()
	if err != nil {
		return SkillPackageDetail{}, false, err
	}
	return s.loadGovernedSkillPackageDetailWithRegistry(pkg, toolRegistry)
}

func (s *FileSkillService) loadGovernedSkillPackageDetailWithRegistry(pkg skill.SkillPackage, toolRegistry *toolpkg.Registry) (SkillPackageDetail, bool, error) {
	location, ok := s.locationForGovernedPackageDir(pkg.Dir)
	if !ok {
		return SkillPackageDetail{}, false, nil
	}
	packageID := strings.TrimSpace(filepath.Base(strings.TrimSpace(pkg.Dir)))
	if packageID == "" || packageID == "." || packageID == string(filepath.Separator) {
		return SkillPackageDetail{}, false, nil
	}
	detail, err := s.loadSkillPackageDetailAtWithRegistry(packageID, location, toolRegistry)
	if err != nil {
		var notFoundErr *SkillPackageNotFoundError
		if errors.As(err, &notFoundErr) {
			return SkillPackageDetail{}, false, nil
		}
		return SkillPackageDetail{}, false, err
	}
	return detail, true, nil
}

func (s *FileSkillService) locationForGovernedPackageDir(packageDir string) (skillPackageLocation, bool) {
	trimmedPackageDir := strings.TrimSpace(packageDir)
	if trimmedPackageDir == "" {
		return skillPackageLocation{}, false
	}
	packageRoot := filepath.Dir(filepath.Clean(trimmedPackageDir))
	for _, location := range s.runtimeLocations() {
		if samePath(location.Dir, packageRoot) {
			return location, true
		}
	}
	for owner, dir := range s.currentExclusiveSkillDirs() {
		if samePath(packageRoot, dir) {
			return skillPackageLocation{Scope: SkillPackageScopeExclusive, OwnerConfig: owner, Dir: filepath.Clean(dir)}, true
		}
	}
	if samePath(packageRoot, s.UserSkillsDir) {
		return skillPackageLocation{Scope: SkillPackageScopeUser, Dir: filepath.Clean(packageRoot)}, true
	}
	if samePath(packageRoot, s.GlobalSkillsDir) {
		return skillPackageLocation{Scope: SkillPackageScopeGlobal, Dir: filepath.Clean(packageRoot)}, true
	}
	if samePath(packageRoot, s.BuiltinSkillsDir) {
		return skillPackageLocation{Scope: SkillPackageScopeBuiltin, Dir: filepath.Clean(packageRoot)}, true
	}
	return skillPackageLocation{Scope: SkillPackageScopeBuiltin, Dir: filepath.Clean(packageRoot)}, true
}

func findGovernedRegistryPackage(registry *skill.Registry, packageID string) (skill.SkillPackage, bool) {
	trimmedID := strings.TrimSpace(packageID)
	if registry == nil || trimmedID == "" {
		return skill.SkillPackage{}, false
	}
	if pkg, ok := registry.GetPackage(trimmedID); ok {
		return pkg, true
	}
	for _, pkg := range registry.Packages() {
		dirPackageID := SanitizeSkillName(filepath.Base(strings.TrimSpace(pkg.Dir)))
		if dirPackageID == trimmedID {
			return pkg, true
		}
	}
	return skill.SkillPackage{}, false
}

func (s *FileSkillService) resolveMutationTargetLocation(skillName, scope, configName string) (skillPackageLocation, SkillPackageDetail, error) {
	packageID := SanitizeSkillName(skillName)
	if packageID == "" {
		return skillPackageLocation{}, SkillPackageDetail{}, &SkillPackageNotFoundError{PackageID: skillName}
	}
	if strings.TrimSpace(scope) != "" || strings.TrimSpace(configName) != "" {
		targetLocation, targetErr := s.defaultImportLocation(scope, configName)
		location, err := s.findPackageLocation(packageID, scope, configName)
		if err != nil {
			if _, ok := err.(*SkillPackageNotFoundError); !ok {
				return skillPackageLocation{}, SkillPackageDetail{}, err
			}
			visibleLocation, visibleErr := s.findVisiblePackageLocation(packageID)
			if visibleErr != nil {
				if targetErr == nil {
					return targetLocation, SkillPackageDetail{}, err
				}
				return skillPackageLocation{}, SkillPackageDetail{}, err
			}
			detail, detailErr := s.loadSkillPackageDetailAt(packageID, visibleLocation)
			if detailErr != nil {
				return skillPackageLocation{}, SkillPackageDetail{}, detailErr
			}
			if targetErr != nil {
				return skillPackageLocation{}, SkillPackageDetail{}, targetErr
			}
			if samePath(visibleLocation.Dir, targetLocation.Dir) {
				return visibleLocation, detail, nil
			}
			if !s.isWritableLocation(targetLocation) {
				return skillPackageLocation{}, SkillPackageDetail{}, &SkillMutationUnsupportedError{Operation: "save", Target: packageID, Reason: "目标技能包层不可写，无法创建 override"}
			}
			if err := s.materializeWritableOverride(detail, targetLocation); err != nil {
				return skillPackageLocation{}, SkillPackageDetail{}, err
			}
			materializedDetail, detailErr := s.loadSkillPackageDetailAt(packageID, targetLocation)
			if detailErr != nil {
				return skillPackageLocation{}, SkillPackageDetail{}, detailErr
			}
			return targetLocation, materializedDetail, nil
		}
		detail, err := s.loadSkillPackageDetailAt(packageID, location)
		if err != nil {
			return skillPackageLocation{}, SkillPackageDetail{}, err
		}
		return location, detail, nil
	}
	location, err := s.findVisiblePackageLocation(packageID)
	if err != nil {
		if _, ok := err.(*SkillPackageNotFoundError); ok {
			return s.defaultWritableLocation(), SkillPackageDetail{}, err
		}
		return skillPackageLocation{}, SkillPackageDetail{}, err
	}
	detail, err := s.loadSkillPackageDetailAt(packageID, location)
	if err != nil {
		return skillPackageLocation{}, SkillPackageDetail{}, err
	}
	if s.isWritableLocation(location) {
		return location, detail, nil
	}
	target := s.defaultWritableLocation()
	if err := s.materializeWritableOverride(detail, target); err != nil {
		return skillPackageLocation{}, SkillPackageDetail{}, err
	}
	materializedDetail, err := s.loadSkillPackageDetailAt(packageID, target)
	if err != nil {
		return skillPackageLocation{}, SkillPackageDetail{}, err
	}
	return target, materializedDetail, nil
}

func (s *FileSkillService) materializeWritableOverride(detail SkillPackageDetail, location skillPackageLocation) error {
	packageDir := filepath.Join(location.Dir, detail.PackageID)
	if samePath(packageDir, detail.Path) {
		return nil
	}
	if err := os.RemoveAll(packageDir); err != nil {
		return fmt.Errorf("清理技能包 override 目录失败: %w", err)
	}
	if err := copyTrackedSkillPackageFiles(detail.Path, packageDir); err != nil {
		return err
	}
	return nil
}

func copyTrackedSkillPackageFiles(sourceDir, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("创建技能包 override 目录失败: %w", err)
	}
	return filepath.WalkDir(sourceDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if !isTrackedSkillPackageFile(rel) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取技能包源文件失败: %w", err)
		}
		targetPath := filepath.Join(targetDir, rel)
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("创建技能包目标目录失败: %w", err)
		}
		if err := os.WriteFile(targetPath, data, 0o644); err != nil {
			return fmt.Errorf("写入技能包 override 文件失败: %w", err)
		}
		return nil
	})
}

func ParseSkillFile(raw string) (string, string, string) {
	frontMatter, content, err := parseSkillPackageDocument(raw)
	if err != nil {
		return "", "", raw
	}
	return strings.TrimSpace(stringValue(frontMatter["name"])), strings.TrimSpace(stringValue(frontMatter["description"])), content
}
