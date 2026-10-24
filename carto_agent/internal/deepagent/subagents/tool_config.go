package subagents

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

type AgentPrompts struct {
	StaticInstructions     string `yaml:"static_instructions"`
	UserPromptTemplate     string `yaml:"user_prompt_template"`
	RuntimeModeInstruction string `yaml:"runtime_mode_instruction,omitempty"`
}

type RuntimeSkillGlobalConfig struct {
	SkillPackages   []string                               `yaml:"skill_packages,omitempty"`
	PackageTools    map[string][]string                    `yaml:"-"`
	PackageMetadata map[string]RuntimeSkillPackageMetadata `yaml:"-"`
}

type RuntimeSkillPackageMetadata struct {
	DisplayName    string `yaml:"display_name"`
	Domain         string `yaml:"domain"`
	System         bool   `yaml:"system"`
	Hidden         bool   `yaml:"hidden"`
	AutoMount      string `yaml:"auto_mount"`
	BodyCharacters int    `yaml:"-"`
}

type PackageCatalog struct {
	Tools    map[string][]string
	Metadata map[string]RuntimeSkillPackageMetadata
}

type RuntimeSkillAgentConfig struct {
	ToolPackages  []string     `yaml:"tool_packages,omitempty"`
	Kind          string       `yaml:"kind"`
	FlowOnly      bool         `yaml:"flow_only,omitempty"`
	DisplayName   string       `yaml:"display_name"`
	Description   string       `yaml:"description"`
	IsMain        bool         `yaml:"is_main,omitempty"`
	Executor      string       `yaml:"executor,omitempty"`
	Enabled       bool         `yaml:"enabled"`
	SkillPackages []string     `yaml:"skill_packages,omitempty"`
	Prompts       AgentPrompts `yaml:"prompts"`
}

type RuntimeSkillConfig struct {
	Mode    string                             `yaml:"-"`
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
	packages := appendUniqueSkills(nil, map[string]bool{}, c.Global.SkillPackages)
	packages = appendUniqueSkills(packages, skillNameSet(packages), AutoMountedPackageNames(c.Global.PackageMetadata, "", false, ""))
	return expandCategoryToolsFromPackages(packages, c.Global.PackageTools)
}

func (c *RuntimeSkillConfig) GetAgentTools(agentName string) []string {
	agentConfig, ok := c.GetAgentConfig(agentName)
	if !ok {
		return nil
	}
	packages := appendUniqueSkills(nil, map[string]bool{}, agentConfig.EffectiveToolPackages())
	packages = appendUniqueSkills(packages, skillNameSet(packages), AutoMountedPackageNames(c.Global.PackageMetadata, c.Mode, agentConfig.IsMain, agentConfig.Executor))
	return expandCategoryToolsFromPackages(packages, c.Global.PackageTools)
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
	result = appendUniqueSkills(result, skillSet, agentConfig.EffectiveToolPackages())
	result = appendUniqueSkills(result, skillSet, AutoMountedPackageNames(c.Global.PackageMetadata, c.Mode, agentConfig.IsMain, agentConfig.Executor))
	return result
}

func skillNameSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// AutoMountedPackageNames applies package policy without hardcoding package or role IDs.
func AutoMountedPackageNames(metadata map[string]RuntimeSkillPackageMetadata, mode string, isMain bool, executor string) []string {
	result := make([]string, 0)
	mode = strings.ToLower(strings.TrimSpace(mode))
	executor = strings.ToLower(strings.TrimSpace(executor))
	for name, item := range metadata {
		mount := item.System
		if item.Hidden {
			switch item.AutoMount {
			case "main":
				mount = mount || (isMain && mode != "plan")
			case "plan_main":
				mount = mount || (isMain && mode == "plan")
			default:
				mount = mount || (executor != "" && item.AutoMount == "executor:"+executor)
			}
		}
		if mount {
			result = append(result, name)
		}
	}
	sort.Strings(result)
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
			if c.Global.PackageMetadata[trimmed].BodyCharacters < 200 {
				continue
			}
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
	return LoadPackageToolsMultiDir(skillsDir)
}

func LoadPackageToolsMultiDir(skillsDirs ...string) (map[string][]string, error) {
	catalog, err := LoadPackageCatalogMultiDir(skillsDirs...)
	return catalog.Tools, err
}

func LoadPackageCatalogMultiDir(skillsDirs ...string) (PackageCatalog, error) {
	result := PackageCatalog{Tools: map[string][]string{}, Metadata: map[string]RuntimeSkillPackageMetadata{}}
	for _, skillsDir := range skillsDirs {
		skillsDir = strings.TrimSpace(skillsDir)
		if skillsDir == "" {
			continue
		}
		entries, err := os.ReadDir(skillsDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return result, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name, kind, enabled, tools, metadata, err := readPackageInfo(filepath.Join(skillsDir, entry.Name()))
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return result, err
			}
			// A higher-priority disabled or non-category package shadows the base package too.
			delete(result.Tools, name)
			delete(result.Metadata, name)
			if kind != "category" || !enabled {
				continue
			}
			result.Tools[name] = tools
			result.Metadata[name] = metadata
		}
	}
	return result, nil
}

func readPackageInfo(packageDir string) (string, string, bool, []string, RuntimeSkillPackageMetadata, error) {
	metadata := RuntimeSkillPackageMetadata{}
	raw, err := os.ReadFile(filepath.Join(packageDir, "SKILL.md"))
	if err != nil {
		return "", "", false, nil, metadata, err
	}
	frontMatter, err := parsePackageFrontMatter(raw)
	if err != nil {
		return "", "", false, nil, metadata, err
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
	rawMetadata, err := yaml.Marshal(frontMatter)
	if err != nil {
		return "", "", false, nil, metadata, err
	}
	if err := yaml.Unmarshal(rawMetadata, &metadata); err != nil {
		return "", "", false, nil, metadata, err
	}
	metadata.DisplayName = strings.TrimSpace(metadata.DisplayName)
	metadata.Domain = strings.TrimSpace(metadata.Domain)
	metadata.AutoMount = strings.ToLower(strings.TrimSpace(metadata.AutoMount))
	metadata.BodyCharacters = packageBodyCharacters(raw)
	if kind == "category" && metadata.AutoMount != "" {
		valid := metadata.AutoMount == "main" || metadata.AutoMount == "plan_main" || (strings.HasPrefix(metadata.AutoMount, "executor:") && strings.TrimSpace(strings.TrimPrefix(metadata.AutoMount, "executor:")) != "")
		if !metadata.Hidden || !valid {
			return "", "", false, nil, metadata, fmt.Errorf("工具包 %s 的 auto_mount 无效", packageName)
		}
	}
	enabled := true
	if value, ok := frontMatter["enabled"].(bool); ok {
		enabled = value
	}
	for _, name := range []string{"config.yaml", "config.yml"} {
		configPath := filepath.Join(packageDir, name)
		configRaw, err := os.ReadFile(configPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", "", false, nil, metadata, err
		}
		var config map[string]any
		if err := yaml.Unmarshal(configRaw, &config); err != nil {
			return "", "", false, nil, metadata, err
		}
		if rawEnabled, ok := config["enabled"]; ok {
			if boolEnabled, ok := rawEnabled.(bool); ok {
				enabled = boolEnabled
			}
		}
		break
	}
	return packageName, kind, enabled, tools, metadata, nil
}

// packageBodyCharacters excludes front matter, Markdown headings and whitespace.
// Use runes so the eligibility threshold means characters for Chinese too.
func packageBodyCharacters(raw []byte) int {
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	start := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				start = i + 1
				break
			}
		}
	}
	count := 0
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "#") {
			continue
		}
		if i+1 < len(lines) {
			underline := strings.TrimSpace(lines[i+1])
			if len(underline) >= 3 && (strings.Trim(underline, "=") == "" || strings.Trim(underline, "-") == "") {
				i++
				continue
			}
		}
		for _, r := range line {
			if !unicode.IsSpace(r) {
				count++
			}
		}
	}
	return count
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

// EffectiveToolPackages accepts the internal v1 struct during the migration window.
func (c RuntimeSkillAgentConfig) EffectiveToolPackages() []string {
	if c.ToolPackages != nil {
		return c.ToolPackages
	}
	return c.SkillPackages
}
