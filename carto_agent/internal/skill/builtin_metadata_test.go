package skill

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBuiltinSkillFilesUseUnifiedRuntimeMetadata(t *testing.T) {
	t.Parallel()

	root := repoSamplePath(t, "autocarto/skills/system")
	problems := make([]string, 0)

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		switch {
		case strings.EqualFold(entry.Name(), "SKILL.md"):
			issues, err := validateBuiltinPackageFrontMatter(path, rel)
			if err != nil {
				return err
			}
			problems = append(problems, issues...)
		case strings.HasPrefix(rel, "."):
			return nil
		}

		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir() error = %v", err)
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("builtin skill metadata is not fully migrated:\n%s", strings.Join(problems, "\n"))
	}
}

func repoSamplePath(t *testing.T, relative string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve caller path")
	}
	base := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	return filepath.Join(base, filepath.FromSlash(relative))
}

func TestEmbeddedExclusiveSkillFilesUseUnifiedRuntimeMetadata(t *testing.T) {
	t.Parallel()

	roots := []string{
		repoSamplePath(t, "carto_agent/defaults/agent_configs/skill_creator/skills"),
		repoSamplePath(t, "carto_agent/defaults/agent_configs/agent_creator/skills"),
	}
	problems := make([]string, 0)

	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}

			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)

			switch {
			case strings.EqualFold(entry.Name(), "SKILL.md"):
				issues, err := validateBuiltinPackageFrontMatter(path, rel)
				if err != nil {
					return err
				}
				problems = append(problems, issues...)
			case strings.Contains(rel, "/tools/"):
				ext := strings.ToLower(filepath.Ext(path))
				if ext != ".yaml" && ext != ".yml" {
					return nil
				}
				issues, err := validateBuiltinToolDefinition(path, rel)
				if err != nil {
					return err
				}
				problems = append(problems, issues...)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WalkDir() error = %v", err)
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("embedded exclusive skill metadata is not fully migrated:\n%s", strings.Join(problems, "\n"))
	}
}

func TestDefaultBuiltinSkillsStayWorkflowOrKnowledgeOnly(t *testing.T) {
	t.Parallel()

	root := repoSamplePath(t, "carto_agent/defaults/skills")
	problems := make([]string, 0)

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		switch {
		case strings.EqualFold(entry.Name(), "SKILL.md"):
			frontMatterRaw, ok := extractFrontMatterYAML(mustReadFile(t, path))
			if !ok {
				return nil
			}
			rootNode, err := parseYAMLMappingNode([]byte(frontMatterRaw))
			if err != nil {
				return fmt.Errorf("parse frontmatter %s: %w", rel, err)
			}
			kindNode, ok := yamlMappingValue(rootNode, "kind")
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: kind is missing", rel))
				return nil
			}
			kind := strings.ToLower(strings.TrimSpace(kindNode.Value))
			if kind != string(SkillPackageKindWorkflow) && kind != string(SkillPackageKindKnowledge) {
				problems = append(problems, fmt.Sprintf("%s: kind=%q should stay workflow/knowledge only", rel, kind))
			}
		case strings.Contains(rel, "/tools/"):
			problems = append(problems, fmt.Sprintf("%s: defaults/skills should not contain tools/*.yaml", rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir() error = %v", err)
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("defaults skill directory breaches role boundary:\n%s", strings.Join(problems, "\n"))
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s failed: %v", path, err)
	}
	return raw
}

func validateBuiltinToolDefinition(path string, relative string) ([]string, error) {
	definition, err := loadDefinitionFile(path)
	if err != nil {
		return nil, fmt.Errorf("load tool definition %s: %w", relative, err)
	}

	problems := make([]string, 0)
	if definition.APIVersion != SkillAPIVersionV2 {
		problems = append(problems, fmt.Sprintf("%s: apiVersion=%q, want %q", relative, definition.APIVersion, SkillAPIVersionV2))
	}
	if !isUnifiedRuntime(definition.Metadata.Runtime) {
		problems = append(problems, fmt.Sprintf("%s: metadata.runtime=%q is invalid", relative, definition.Metadata.Runtime))
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tool definition %s: %w", relative, err)
	}
	root, err := parseYAMLMappingNode(raw)
	if err != nil {
		return nil, fmt.Errorf("parse raw tool yaml %s: %w", relative, err)
	}
	metadataNode, ok := yamlMappingValue(root, "metadata")
	if !ok {
		problems = append(problems, fmt.Sprintf("%s: metadata mapping is missing", relative))
		return problems, nil
	}
	if yamlMappingHasKey(metadataNode, "backend") {
		problems = append(problems, fmt.Sprintf("%s: metadata.backend should be removed", relative))
	}
	if runtimeNode, ok := yamlMappingValue(metadataNode, "runtime"); !ok {
		problems = append(problems, fmt.Sprintf("%s: metadata.runtime is missing", relative))
	} else if !isUnifiedRuntime(strings.TrimSpace(runtimeNode.Value)) {
		problems = append(problems, fmt.Sprintf("%s: raw metadata.runtime=%q is invalid", relative, runtimeNode.Value))
	}

	return problems, nil
}

func validateBuiltinPackageFrontMatter(path string, relative string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read package markdown %s: %w", relative, err)
	}
	frontMatterRaw, ok := extractFrontMatterYAML(raw)
	if !ok {
		return nil, nil
	}
	root, err := parseYAMLMappingNode([]byte(frontMatterRaw))
	if err != nil {
		return nil, fmt.Errorf("parse frontmatter %s: %w", relative, err)
	}

	problems := make([]string, 0)
	if yamlMappingHasKey(root, "backend") {
		problems = append(problems, fmt.Sprintf("%s: frontmatter backend should be removed", relative))
	}
	if runtimeNode, ok := yamlMappingValue(root, "runtime"); ok && !isUnifiedRuntime(strings.TrimSpace(runtimeNode.Value)) {
		problems = append(problems, fmt.Sprintf("%s: frontmatter runtime=%q is invalid", relative, runtimeNode.Value))
	}

	return problems, nil
}

func extractFrontMatterYAML(raw []byte) (string, bool) {
	normalized := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return "", false
	}
	lines := strings.Split(normalized, "\n")
	for idx := 1; idx < len(lines); idx++ {
		if strings.TrimSpace(lines[idx]) == "---" {
			return strings.Join(lines[1:idx], "\n"), true
		}
	}
	return "", false
}

func parseYAMLMappingNode(raw []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("empty yaml document")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected mapping node, got kind %d", root.Kind)
	}
	return root, nil
}

func yamlMappingHasKey(node *yaml.Node, key string) bool {
	_, ok := yamlMappingValue(node, key)
	return ok
}

func yamlMappingValue(node *yaml.Node, key string) (*yaml.Node, bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, false
	}
	for idx := 0; idx+1 < len(node.Content); idx += 2 {
		if strings.TrimSpace(node.Content[idx].Value) == key {
			return node.Content[idx+1], true
		}
	}
	return nil, false
}

func isUnifiedRuntime(value string) bool {
	switch strings.TrimSpace(value) {
	case RuntimeIDQGIS, RuntimeIDCLI, RuntimeIDInternal:
		return true
	default:
		return false
	}
}
