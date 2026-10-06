package subagents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type AgentPrompts struct {
	StaticInstructions     string `yaml:"static_instructions"`
	UserPromptTemplate     string `yaml:"user_prompt_template"`
	RuntimeModeInstruction string `yaml:"runtime_mode_instruction,omitempty"`
}

type RuntimeSkillGlobalConfig struct {
	SkillPackages []string            `yaml:"skill_packages,omitempty"`
	PackageTools  map[string][]string `yaml:"-"`
}

type RuntimeSkillAgentConfig struct {
	DisplayName     string       `yaml:"display_name"`
	Description     string       `yaml:"description"`
	IsMain          bool         `yaml:"is_main,omitempty"`
	Enabled         bool         `yaml:"enabled"`
	WorkflowPackage string       `yaml:"workflow_package,omitempty"`
	SkillPackages   []string     `yaml:"skill_packages,omitempty"`
	Prompts         AgentPrompts `yaml:"prompts"`
}

type RuntimeSkillConfig struct {
	Version string                             `yaml:"version"`
	Global  RuntimeSkillGlobalConfig           `yaml:"global"`
	Agents  map[string]RuntimeSkillAgentConfig `yaml:"agents"`
}

func (c *RuntimeSkillConfig) GetAgentConfig(agentName string) (*RuntimeSkillAgentConfig, bool) {
	if c == nil {
		return nil, false
	}
	config, ok := c.Agents[agentName]
	return &config, ok
}

func (c *RuntimeSkillConfig) GetGlobalCommonTools() []string {
	if c == nil {
		return nil
	}
	return expandCategoryToolsFromPackages(c.Global.SkillPackages, c.Global.PackageTools)
}

func (c *RuntimeSkillConfig) GetAgentTools(agentName string) []string {
	agentConfig, ok := c.GetAgentConfig(agentName)
	if !ok {
		return nil
	}
	return expandCategoryToolsFromPackages(agentConfig.SkillPackages, c.Global.PackageTools)
}

func (c *RuntimeSkillConfig) GetAgentAllowedTools(agentName string) []string {
	if c == nil {
		return nil
	}
	toolSet := make(map[string]bool)
	result := make([]string, 0)
	result = appendUniqueToolNames(result, toolSet, c.GetGlobalCommonTools())
	result = appendUniqueToolNames(result, toolSet, c.GetAgentTools(agentName))
	return result
}

func (c *RuntimeSkillConfig) GetAgentSkills(agentName string) []string {
	agentConfig, ok := c.GetAgentConfig(agentName)
	if !ok {
		return nil
	}

	skillSet := make(map[string]bool)
	result := make([]string, 0)
	result = appendUniqueSkills(result, skillSet, c.Global.SkillPackages)
	result = appendUniqueSkills(result, skillSet, agentConfig.SkillPackages)
	return result
}

func (c *RuntimeSkillConfig) GetAgentMiddlewareSkills(agentName string) []string {
	if c == nil {
		return nil
	}
	allSkills := c.GetAgentSkills(agentName)
	if len(allSkills) == 0 {
		return nil
	}
	result := make([]string, 0, len(allSkills))
	for _, skillName := range allSkills {
		trimmed := strings.TrimSpace(skillName)
		if trimmed == "" {
			continue
		}
		if _, isCategoryPackage := c.Global.PackageTools[trimmed]; isCategoryPackage {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

func appendUniqueSkills(result []string, skillSet map[string]bool, values []string) []string {
	for _, skill := range values {
		trimmed := strings.TrimSpace(skill)
		if trimmed == "" || skillSet[trimmed] {
			continue
		}
		skillSet[trimmed] = true
		result = append(result, trimmed)
	}
	return result
}

func appendUniqueToolNames(result []string, toolSet map[string]bool, values []string) []string {
	for _, toolName := range values {
		trimmed := strings.TrimSpace(toolName)
		if trimmed == "" || toolSet[trimmed] {
			continue
		}
		toolSet[trimmed] = true
		result = append(result, trimmed)
	}
	return result
}

func expandCategoryToolsFromPackages(packageNames []string, packageTools map[string][]string) []string {
	if len(packageNames) == 0 || len(packageTools) == 0 {
		return nil
	}
	toolSet := make(map[string]bool)
	result := make([]string, 0)
	for _, packageName := range packageNames {
		for _, toolName := range packageTools[strings.TrimSpace(packageName)] {
			trimmed := strings.TrimSpace(toolName)
			if trimmed == "" || toolSet[trimmed] {
				continue
			}
			toolSet[trimmed] = true
			result = append(result, trimmed)
		}
	}
	return result
}

func LoadPackageTools(skillsDir string) (map[string][]string, error) {
	result := make(map[string][]string)
	trimmedDir := strings.TrimSpace(skillsDir)
	if trimmedDir == "" {
		return result, nil
	}
	entries, err := os.ReadDir(trimmedDir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		packageDir := filepath.Join(trimmedDir, entry.Name())
		packageName, kind, enabled, tools, err := readPackageInfo(packageDir)
		if err != nil || kind != "category" || !enabled {
			if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			continue
		}
		result[packageName] = tools
	}
	return result, nil
}

func LoadPackageToolsMultiDir(skillsDirs ...string) (map[string][]string, error) {
	result := make(map[string][]string)
	for _, skillsDir := range skillsDirs {
		trimmedDir := strings.TrimSpace(skillsDir)
		if trimmedDir == "" {
			continue
		}
		packageTools, err := LoadPackageTools(trimmedDir)
		if err != nil {
			return nil, err
		}
		for packageName, tools := range packageTools {
			result[packageName] = append([]string(nil), tools...)
		}
	}
	return result, nil
}

func readPackageInfo(packageDir string) (string, string, bool, []string, error) {
	raw, err := os.ReadFile(filepath.Join(packageDir, "SKILL.md"))
	if err != nil {
		return "", "", false, nil, err
	}
	frontMatter, err := parsePackageFrontMatter(raw)
	if err != nil {
		return "", "", false, nil, err
	}
	packageName := strings.TrimSpace(stringValue(frontMatter["name"]))
	if packageName == "" {
		packageName = filepath.Base(packageDir)
	}
	kind := strings.ToLower(strings.TrimSpace(stringValue(frontMatter["kind"])))
	tools := stringSliceValue(frontMatter["tool_refs"])
	if len(tools) == 0 {
		tools = stringSliceValue(frontMatter["tools"])
	}
	if len(tools) == 0 {
		tools = stringSliceValue(frontMatter["requires_tools"])
	}
	enabled := true
	for _, name := range []string{"config.yaml", "config.yml"} {
		configPath := filepath.Join(packageDir, name)
		configRaw, err := os.ReadFile(configPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", "", false, nil, err
		}
		var config map[string]any
		if err := yaml.Unmarshal(configRaw, &config); err != nil {
			return "", "", false, nil, err
		}
		if rawEnabled, ok := config["enabled"]; ok {
			if boolEnabled, ok := rawEnabled.(bool); ok {
				enabled = boolEnabled
			}
		}
		break
	}
	return packageName, kind, enabled, tools, nil
}

func parsePackageFrontMatter(raw []byte) (map[string]any, error) {
	normalized := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return map[string]any{}, nil
	}
	lines := strings.Split(normalized, "\n")
	for idx := 1; idx < len(lines); idx++ {
		if strings.TrimSpace(lines[idx]) != "---" {
			continue
		}
		frontMatter := make(map[string]any)
		if err := yaml.Unmarshal([]byte(strings.Join(lines[1:idx], "\n")), &frontMatter); err != nil {
			return nil, err
		}
		return frontMatter, nil
	}
	return nil, fmt.Errorf("missing closing frontmatter delimiter")
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

func stringSliceValue(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(stringValue(item))
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
