package control

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/governance"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
)

var _ governance.GovernedRegistry = (*RegistryGovernance)(nil)

type DynamicToolRegistry interface {
	ApplyToRegistry(registry *toolpkg.Registry)
}

type RegistryGovernanceDeps struct {
	ResolveToolDirs     func() []string
	ResolveSkillDirs    func() []string
	DynamicToolRegistry DynamicToolRegistry
}

type RegistryAvailability struct {
	Status          string   `json:"status,omitempty"`
	Code            string   `json:"code,omitempty"`
	Message         string   `json:"message,omitempty"`
	MissingRuntimes []string `json:"missing_runtimes,omitempty"`
}

type RegistryGovernance struct {
	resolveToolDirs     func() []string
	resolveSkillDirs    func() []string
	dynamicToolRegistry DynamicToolRegistry
}

func NewRegistryGovernance(deps RegistryGovernanceDeps) *RegistryGovernance {
	return &RegistryGovernance{
		resolveToolDirs:     deps.ResolveToolDirs,
		resolveSkillDirs:    deps.ResolveSkillDirs,
		dynamicToolRegistry: deps.DynamicToolRegistry,
	}
}

func (g *RegistryGovernance) LoadToolRegistry() (*toolpkg.Registry, error) {
	if g == nil {
		return nil, nil
	}
	dirs := normalizeRegistryPaths(nil)
	if g.resolveToolDirs != nil {
		dirs = normalizeRegistryPaths(g.resolveToolDirs())
	}
	if len(dirs) == 0 && g.dynamicToolRegistry == nil {
		return nil, nil
	}
	loadCanonicalDefaults := false
	filteredDirs := make([]string, 0, len(dirs))
	builtinDir := strings.TrimSpace(config.ResolveBuiltinToolsDir())
	for _, dir := range dirs {
		if sameRegistryPath(dir, builtinDir) {
			loadCanonicalDefaults = true
			continue
		}
		filteredDirs = append(filteredDirs, dir)
	}
	loadedDefinitions := make([]toolpkg.ToolDefinition, 0)
	if loadCanonicalDefaults {
		definitions, err := toolpkg.LoadCanonicalDefinitions()
		if err != nil {
			return nil, err
		}
		loadedDefinitions = append(loadedDefinitions, definitions...)
	}
	if len(filteredDirs) > 0 {
		definitions, err := toolpkg.LoadDefinitionsFromDirectories(filteredDirs...)
		if err != nil {
			return nil, err
		}
		loadedDefinitions = append(loadedDefinitions, definitions...)
	}
	registry := toolpkg.NewRegistry()
	if len(loadedDefinitions) > 0 {
		if err := registry.LoadDefinitions(loadedDefinitions); err != nil {
			return nil, err
		}
	}
	if g.dynamicToolRegistry != nil {
		g.dynamicToolRegistry.ApplyToRegistry(registry)
	}
	if len(registry.All()) == 0 {
		return nil, nil
	}
	return registry, nil
}

func sameRegistryPath(left string, right string) bool {
	trimmedLeft := strings.TrimSpace(left)
	trimmedRight := strings.TrimSpace(right)
	if trimmedLeft == "" || trimmedRight == "" {
		return false
	}
	return filepath.Clean(trimmedLeft) == filepath.Clean(trimmedRight)
}

func (g *RegistryGovernance) LoadSkillRegistry() (*skill.Registry, error) {
	if g == nil || g.resolveSkillDirs == nil {
		return nil, nil
	}
	dirs := normalizeRegistryPaths(g.resolveSkillDirs())
	if len(dirs) == 0 {
		return nil, nil
	}
	toolRegistry, err := g.LoadToolRegistry()
	if err != nil {
		return nil, err
	}
	registry := skill.NewRegistry(toolRegistry)
	if err := registry.LoadDirectories(dirs...); err != nil {
		return nil, err
	}
	return registry, nil
}

func (g *RegistryGovernance) ResolveSkillPackage(packageName string) (skill.SkillPackage, bool, error) {
	registry, err := g.LoadSkillRegistry()
	if err != nil {
		return skill.SkillPackage{}, false, err
	}
	if registry == nil {
		return skill.SkillPackage{}, false, nil
	}
	trimmedName := strings.TrimSpace(packageName)
	pkg, ok := registry.GetPackage(trimmedName)
	if !ok {
		for _, candidate := range registry.Packages() {
			if sanitizeGovernedPackageID(candidate.Dir) == trimmedName {
				return candidate, true, nil
			}
		}
	}
	return pkg, ok, nil
}

func sanitizeGovernedPackageID(packageDir string) string {
	trimmed := strings.TrimSpace(filepath.Base(strings.TrimSpace(packageDir)))
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ReplaceAll(trimmed, "\\", "-")
	trimmed = strings.ReplaceAll(trimmed, "/", "-")
	trimmed = strings.ReplaceAll(trimmed, " ", "-")
	for strings.Contains(trimmed, "..") {
		trimmed = strings.ReplaceAll(trimmed, "..", "-")
	}
	trimmed = strings.Trim(trimmed, ".-")
	return strings.TrimSpace(trimmed)
}

func (g *RegistryGovernance) EvaluateAvailability(services map[string]middlewareclient.ServiceStatus) (RegistryAvailability, error) {
	registry, err := g.LoadSkillRegistry()
	if err != nil {
		return RegistryAvailability{}, err
	}
	requiredRuntimes := collectRegistryRuntimes(registry)
	if len(requiredRuntimes) == 0 {
		return RegistryAvailability{Status: "ready"}, nil
	}
	providerMissing := make([]string, 0)
	unsupported := make([]string, 0)
	for _, runtimeID := range requiredRuntimes {
		service, ok := services[runtimeID]
		if !ok {
			unsupported = append(unsupported, runtimeID)
			continue
		}
		if runtimeID == skill.RuntimeIDCLI && service.ProviderCount <= 0 {
			providerMissing = append(providerMissing, runtimeID)
			continue
		}
		if !service.Enabled {
			unsupported = append(unsupported, runtimeID)
			continue
		}
		status := strings.ToLower(strings.TrimSpace(service.Status))
		if !service.Ready && status != "running" {
			unsupported = append(unsupported, runtimeID)
		}
	}
	if len(providerMissing) > 0 {
		return RegistryAvailability{
			Status:          "degraded",
			Code:            "provider_missing",
			Message:         fmt.Sprintf("registry provider missing for runtimes: %s", strings.Join(providerMissing, ", ")),
			MissingRuntimes: providerMissing,
		}, nil
	}
	if len(unsupported) > 0 {
		return RegistryAvailability{
			Status:          "degraded",
			Code:            "unsupported_runtime",
			Message:         fmt.Sprintf("registry runtime unavailable: %s", strings.Join(unsupported, ", ")),
			MissingRuntimes: unsupported,
		}, nil
	}
	return RegistryAvailability{Status: "ready"}, nil
}

func collectRegistryRuntimes(registry *skill.Registry) []string {
	if registry == nil {
		return nil
	}
	seen := make(map[string]struct{})
	runtimes := make([]string, 0)
	for _, definition := range registry.AllDefinitions() {
		runtimeID := skill.NormalizeRuntime(strings.TrimSpace(definition.Metadata.Runtime))
		if runtimeID == "" {
			runtimeID = skill.NormalizeRuntime(strings.TrimSpace(definition.Metadata.Backend))
		}
		if runtimeID == "" || runtimeID == skill.RuntimeIDInternal || (runtimeID == skill.RuntimeIDCLI && definition.Execution.CLI != nil && strings.TrimSpace(definition.Execution.CLI.Command) != "") {
			continue
		}
		if _, exists := seen[runtimeID]; exists {
			continue
		}
		seen[runtimeID] = struct{}{}
		runtimes = append(runtimes, runtimeID)
	}
	sort.Strings(runtimes)
	return runtimes
}

func normalizeRegistryPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	normalized := make([]string, 0, len(paths))
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
		normalized = append(normalized, cleaned)
	}
	return normalized
}
