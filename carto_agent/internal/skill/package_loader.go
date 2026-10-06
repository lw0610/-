package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/workflow"
	"gopkg.in/yaml.v3"
)

func loadPackagesFromDirectory(dir string) ([]SkillPackage, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, nil
	}

	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat skill package directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("skill package path %s is not a directory", dir)
	}

	if hasSkillPackageMarker(dir) {
		pkg, ok, err := loadPackageDirectory(dir, dir)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		return []SkillPackage{pkg}, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read skill package directory %s: %w", dir, err)
	}
	packages := make([]SkillPackage, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pkg, ok, loadErr := loadPackageDirectory(filepath.Join(dir, entry.Name()), dir)
		if loadErr != nil {
			return nil, loadErr
		}
		if ok {
			packages = append(packages, pkg)
		}
	}
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].Name < packages[j].Name
	})
	return packages, nil
}

func loadPackageDirectory(dir string, source string) (SkillPackage, bool, error) {
	skillPath := filepath.Join(dir, "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		if os.IsNotExist(err) {
			return SkillPackage{}, false, nil
		}
		return SkillPackage{}, false, fmt.Errorf("stat skill package %s: %w", skillPath, err)
	}

	frontMatter, body, err := parseSkillPackageMarkdownFile(skillPath)
	if err != nil {
		return SkillPackage{}, false, err
	}
	config, err := loadSkillPackageConfig(dir)
	if err != nil {
		return SkillPackage{}, false, err
	}
	workflowDef, err := loadSkillPackageWorkflow(dir)
	if err != nil {
		return SkillPackage{}, false, err
	}

	pkg := SkillPackage{
		Name:         frontMatter.Name,
		Dir:          dir,
		Kind:         frontMatter.Kind,
		FrontMatter:  frontMatter,
		MarkdownBody: body,
		Config:       config,
		Workflow:     workflowDef,
		Source:       source,
	}
	pkg.normalize()
	if pkg.Name == "" {
		return SkillPackage{}, false, fmt.Errorf("skill package %s missing name", skillPath)
	}
	if err := validateSkillPackage(pkg); err != nil {
		return SkillPackage{}, false, err
	}
	return pkg, true, nil
}

func loadSkillPackageWorkflow(dir string) (*workflow.Definition, error) {
	path := filepath.Join(dir, "workflow.yaml")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat workflow definition %s: %w", path, err)
	}
	definition, err := workflow.LoadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load workflow definition %s: %w", path, err)
	}
	return definition, nil
}

func validateSkillPackage(pkg SkillPackage) error {
	switch pkg.Kind {
	case SkillPackageKindCategory, SkillPackageKindWorkflow, SkillPackageKindKnowledge:
	default:
		return fmt.Errorf("invalid skill package %s kind %q", pkg.Name, pkg.Kind)
	}
	return nil
}

func hasSkillPackageMarker(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil
}

func parseSkillPackageMarkdownFile(path string) (SkillFrontMatter, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SkillFrontMatter{}, "", fmt.Errorf("read skill package markdown %s: %w", path, err)
	}
	frontMatter, body, err := parseSkillPackageMarkdown(string(data))
	if err != nil {
		return SkillFrontMatter{}, "", fmt.Errorf("parse skill package markdown %s: %w", path, err)
	}
	frontMatter.normalize()
	return frontMatter, body, nil
}

func parseSkillPackageMarkdown(raw string) (SkillFrontMatter, string, error) {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return SkillFrontMatter{}, strings.TrimSpace(normalized), nil
	}
	lines := strings.Split(normalized, "\n")
	for idx := 1; idx < len(lines); idx++ {
		if strings.TrimSpace(lines[idx]) != "---" {
			continue
		}
		var frontMatter SkillFrontMatter
		frontMatterRaw := strings.Join(lines[1:idx], "\n")
		if err := yaml.Unmarshal([]byte(frontMatterRaw), &frontMatter); err != nil {
			return SkillFrontMatter{}, "", err
		}
		body := strings.Join(lines[idx+1:], "\n")
		return frontMatter, strings.TrimSpace(body), nil
	}
	return SkillFrontMatter{}, "", fmt.Errorf("missing closing frontmatter delimiter")
}

func loadSkillPackageConfig(dir string) (*SkillPackageConfig, error) {
	for _, name := range []string{"config.yaml", "config.yml"} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read skill package config %s: %w", path, err)
		}
		var cfg SkillPackageConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse skill package config %s: %w", path, err)
		}
		cfg.normalize()
		return &cfg, nil
	}
	return nil, nil
}

func loadDefinitionFile(path string) (SkillDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SkillDefinition{}, fmt.Errorf("read skill definition %s: %w", path, err)
	}

	var definition SkillDefinition
	if err := yaml.Unmarshal(data, &definition); err != nil {
		return SkillDefinition{}, fmt.Errorf("parse skill definition %s: %w", path, err)
	}
	definition.SourcePath = path
	definition.normalize()
	if err := definition.Validate(); err != nil {
		return SkillDefinition{}, err
	}
	return definition, nil
}
