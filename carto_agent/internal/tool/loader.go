package tool

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"gopkg.in/yaml.v3"
)

type manifestProbe struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

func LoadDefinitionsFromDirectories(dirs ...string) ([]ToolDefinition, error) {
	definitions := make([]ToolDefinition, 0)
	for _, dir := range dirs {
		loaded, err := loadDefinitionsFromDirectory(dir)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, loaded...)
	}
	return definitions, nil
}

// LoadAuthorityDefinitions reads only the configured tool definitions. An empty
// asset tree stays empty; stale embedded copies must not become authority.
func LoadAuthorityDefinitions(dirs ...string) ([]ToolDefinition, error) {
	return LoadDefinitionsFromDirectories(dirs...)
}

func LoadCanonicalDefinitions() ([]ToolDefinition, error) {
	return LoadDefinitionsFromDirectories(config.ResolveBuiltinToolsDir())
}

func loadDefinitionsFromDirectory(dir string) ([]ToolDefinition, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, nil
	}

	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat tool directory %s: %w", dir, err)
	}

	if !info.IsDir() {
		definition, ok, err := loadMaybeDefinitionFile(dir)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		return []ToolDefinition{definition}, nil
	}

	paths := make([]string, 0)
	if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yaml" || ext == ".yml" {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk tool directory %s: %w", dir, err)
	}
	sort.Strings(paths)

	definitions := make([]ToolDefinition, 0, len(paths))
	for _, path := range paths {
		definition, ok, err := loadMaybeDefinitionFile(path)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

func loadMaybeDefinitionFile(path string) (ToolDefinition, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ToolDefinition{}, false, fmt.Errorf("read tool definition %s: %w", path, err)
	}
	if !looksLikeToolManifest(data) {
		return ToolDefinition{}, false, nil
	}
	var definition ToolDefinition
	if err := yaml.Unmarshal(data, &definition); err != nil {
		return ToolDefinition{}, false, fmt.Errorf("parse tool definition %s: %w", path, err)
	}
	definition.SourcePath = path
	definition.normalize()
	if err := definition.Validate(); err != nil {
		return ToolDefinition{}, false, err
	}
	return definition, true, nil
}

func loadDefinitionFile(path string) (ToolDefinition, error) {
	definition, ok, err := loadMaybeDefinitionFile(path)
	if err != nil {
		return ToolDefinition{}, err
	}
	if !ok {
		return ToolDefinition{}, fmt.Errorf("tool definition %s is not a supported tool manifest", path)
	}
	return definition, nil
}

func looksLikeToolManifest(data []byte) bool {
	var probe manifestProbe
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return false
	}
	apiVersion := strings.TrimSpace(probe.APIVersion)
	kind := strings.TrimSpace(probe.Kind)
	switch {
	case apiVersion == ToolAPIVersionV1 || apiVersion == ToolPackageAPIVersionV1:
		return true
	case kind == string(ToolKindDefinition) || kind == string(ToolKindPackage):
		return true
	default:
		return false
	}
}
