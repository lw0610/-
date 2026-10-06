package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toolpkg "github.com/autocarto/carto_agent/internal/tool"
	"gopkg.in/yaml.v3"
)

type PackageCreateRequest struct {
	PackageID   string
	Name        string
	Description string
	Kind        string
	Runtime     string
	Backend     string
	Body        string
}

func CreateSkillPackage(baseDir string, req PackageCreateRequest) (string, error) {
	packageID := sanitizePackageID(req.PackageID)
	if packageID == "" {
		packageID = sanitizePackageID(req.Name)
	}
	if packageID == "" {
		return "", fmt.Errorf("skill package 名称不能为空")
	}
	if strings.TrimSpace(baseDir) == "" {
		return "", fmt.Errorf("skills 目录不能为空")
	}
	packageDir := filepath.Join(strings.TrimSpace(baseDir), packageID)
	if _, err := os.Stat(packageDir); err == nil {
		return "", fmt.Errorf("skill package 已存在: %s", packageID)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("检查技能包目录失败: %w", err)
	}
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		return "", fmt.Errorf("创建技能包目录失败: %w", err)
	}
	frontMatter := map[string]any{
		"name":        packageID,
		"description": strings.TrimSpace(req.Description),
		"kind":        normalizeSkillPackageKind(strings.TrimSpace(req.Kind), "", packageID, false),
	}
	runtimeID := NormalizeRuntime(req.Runtime)
	if runtimeID == "" {
		runtimeID = NormalizeRuntime(req.Backend)
	}
	if runtimeID != "" {
		frontMatter["runtime"] = runtimeID
	}
	body, err := buildSkillPackageMarkdown(frontMatter, strings.TrimSpace(req.Body))
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("写入 SKILL.md 失败: %w", err)
	}
	return packageDir, nil
}

func ValidateSkillDefinition(def SkillDefinition) error {
	def.normalize()
	if strings.TrimSpace(def.APIVersion) == "" {
		def.APIVersion = SkillAPIVersionV1
	}
	if def.APIVersion != SkillAPIVersionV1 && def.APIVersion != SkillAPIVersionV2 {
		return fmt.Errorf("不支持的 apiVersion: %s", def.APIVersion)
	}
	if def.Kind != SkillKindAtomic && def.Kind != SkillKindComposite {
		return fmt.Errorf("不支持的 skill kind: %s", def.Kind)
	}
	if strings.TrimSpace(def.Metadata.Name) == "" {
		return fmt.Errorf("skill metadata.name 不能为空")
	}
	runtimeID := NormalizeRuntime(def.Metadata.Runtime)
	if runtimeID == "" {
		runtimeID = NormalizeRuntime(def.Metadata.Backend)
	}
	if runtimeID != RuntimeIDCLI && runtimeID != RuntimeIDInternal && runtimeID != RuntimeIDQGIS {
		return fmt.Errorf("不支持的 runtime: %s", firstNonEmpty(def.Metadata.Runtime, def.Metadata.Backend))
	}
	if def.Kind == SkillKindAtomic && strings.TrimSpace(def.Execution.BackendTool) == "" && (def.Execution.CLI == nil || strings.TrimSpace(def.Execution.CLI.Command) == "") {
		return fmt.Errorf("atomic skill 缺少 execution.backend_tool")
	}
	seen := make(map[string]struct{}, len(def.Spec.Parameters))
	for _, param := range def.Spec.Parameters {
		name := strings.TrimSpace(param.Name)
		if name == "" {
			return fmt.Errorf("skill parameter 名称不能为空")
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("存在重复参数名: %s", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func DecodeSkillDefinition(raw any) (SkillDefinition, error) {
	payload, err := yaml.Marshal(raw)
	if err != nil {
		return SkillDefinition{}, fmt.Errorf("序列化 skill 定义失败: %w", err)
	}
	var def SkillDefinition
	if err := yaml.Unmarshal(payload, &def); err != nil {
		return SkillDefinition{}, fmt.Errorf("解析 skill 定义失败: %w", err)
	}
	if strings.TrimSpace(def.APIVersion) == "" {
		def.APIVersion = SkillAPIVersionV1
	}
	return def, ValidateSkillDefinition(def)
}

func SaveSkillToPackage(baseDir, packageID string, def SkillDefinition) (string, error) {
	if err := ValidateSkillDefinition(def); err != nil {
		return "", err
	}
	baseDir = strings.TrimSpace(baseDir)
	packageID = sanitizePackageID(packageID)
	if baseDir == "" || packageID == "" {
		return "", fmt.Errorf("skill package 路径无效")
	}
	packageDir := filepath.Join(baseDir, packageID)
	if _, err := os.Stat(filepath.Join(packageDir, "SKILL.md")); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("skill package 不存在: %s", packageID)
		}
		return "", fmt.Errorf("读取技能包失败: %w", err)
	}
	toolsDir := managedToolsDirFromSkillsBase(baseDir, packageID)
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		return "", fmt.Errorf("创建 tools 目录失败: %w", err)
	}
	fileName := sanitizePackageID(def.Metadata.Name)
	if fileName == "" {
		return "", fmt.Errorf("skill 文件名无效")
	}
	path := filepath.Join(toolsDir, fileName+".yaml")
	raw, err := yaml.Marshal(def)
	if err != nil {
		return "", fmt.Errorf("序列化 skill 定义失败: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", fmt.Errorf("写入 skill 定义失败: %w", err)
	}
	if err := ensureToolRefInPackageMarkdown(filepath.Join(packageDir, "SKILL.md"), def.Metadata.Name); err != nil {
		return "", err
	}
	return path, nil
}

func DecodeToolDefinition(raw any) (toolpkg.ToolDefinition, error) {
	payload, err := yaml.Marshal(raw)
	if err != nil {
		return toolpkg.ToolDefinition{}, fmt.Errorf("序列化 tool 定义失败: %w", err)
	}
	var def toolpkg.ToolDefinition
	if err := yaml.Unmarshal(payload, &def); err != nil {
		return toolpkg.ToolDefinition{}, fmt.Errorf("解析 tool 定义失败: %w", err)
	}
	if strings.TrimSpace(def.APIVersion) == "" {
		def.APIVersion = toolpkg.ToolAPIVersionV1
	}
	if strings.TrimSpace(string(def.Kind)) == "" {
		def.Kind = toolpkg.ToolKindDefinition
	}
	if err := toolpkg.ValidateDefinition(def); err != nil {
		return toolpkg.ToolDefinition{}, err
	}
	return def, nil
}

func SaveToolToPackage(baseDir, packageID string, def toolpkg.ToolDefinition) (string, error) {
	if err := toolpkg.ValidateDefinition(def); err != nil {
		return "", err
	}
	baseDir = strings.TrimSpace(baseDir)
	packageID = sanitizePackageID(packageID)
	if baseDir == "" || packageID == "" {
		return "", fmt.Errorf("skill package 路径无效")
	}
	packageDir := filepath.Join(baseDir, packageID)
	if _, err := os.Stat(filepath.Join(packageDir, "SKILL.md")); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("skill package 不存在: %s", packageID)
		}
		return "", fmt.Errorf("读取技能包失败: %w", err)
	}
	toolsDir := managedToolsDirFromSkillsBase(baseDir, packageID)
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		return "", fmt.Errorf("创建 tools 目录失败: %w", err)
	}
	fileName := sanitizePackageID(def.Metadata.Name)
	if fileName == "" {
		return "", fmt.Errorf("tool 文件名无效")
	}
	path := filepath.Join(toolsDir, fileName+".yaml")
	raw, err := yaml.Marshal(def)
	if err != nil {
		return "", fmt.Errorf("序列化 tool 定义失败: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", fmt.Errorf("写入 tool 定义失败: %w", err)
	}
	if err := ensureToolRefInPackageMarkdown(filepath.Join(packageDir, "SKILL.md"), def.Metadata.Name); err != nil {
		return "", err
	}
	return path, nil
}

func managedToolsDirFromSkillsBase(baseDir, packageID string) string {
	cleanBase := filepath.Clean(strings.TrimSpace(baseDir))
	if cleanBase == "" {
		return filepath.Join(packageID)
	}
	baseName := filepath.Base(cleanBase)
	parentName := filepath.Base(filepath.Dir(cleanBase))
	if (strings.EqualFold(baseName, "user") || strings.EqualFold(baseName, "system")) && strings.EqualFold(parentName, "skills") {
		root := filepath.Dir(filepath.Dir(cleanBase))
		return filepath.Join(root, "tools", baseName, packageID)
	}
	return filepath.Join(cleanBase, "..", "tools", "user", packageID)
}

func ensureToolRefInPackageMarkdown(skillPath, toolName string) error {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return nil
	}
	frontMatter, body, err := parseSkillPackageMarkdownFile(skillPath)
	if err != nil {
		return fmt.Errorf("读取技能包 frontmatter 失败: %w", err)
	}
	frontMatter.ToolRefs = append(frontMatter.ToolRefs, toolName)
	frontMatter.ToolRefs = normalizeStringSlice(frontMatter.ToolRefs)
	frontMatter.Tools = append([]string(nil), frontMatter.ToolRefs...)
	frontMatterMap := map[string]any{
		"name":        frontMatter.Name,
		"description": frontMatter.Description,
		"kind":        frontMatter.Kind,
	}
	if frontMatter.Runtime != "" {
		frontMatterMap["runtime"] = frontMatter.Runtime
	}
	if frontMatter.Backend != "" {
		frontMatterMap["backend"] = frontMatter.Backend
	}
	if frontMatter.Enabled != nil {
		frontMatterMap["enabled"] = *frontMatter.Enabled
	}
	if len(frontMatter.Tags) > 0 {
		frontMatterMap["tags"] = frontMatter.Tags
	}
	if len(frontMatter.ToolRefs) > 0 {
		frontMatterMap["tool_refs"] = frontMatter.ToolRefs
	}
	if len(frontMatter.RequiresTools) > 0 {
		frontMatterMap["requires_tools"] = frontMatter.RequiresTools
	}
	raw, err := buildSkillPackageMarkdown(frontMatterMap, body)
	if err != nil {
		return err
	}
	if err := os.WriteFile(skillPath, []byte(raw), 0o644); err != nil {
		return fmt.Errorf("更新技能包 SKILL.md 失败: %w", err)
	}
	return nil
}

func SaveSkillScript(baseDir, packageID, scriptPath, content string) (string, error) {
	baseDir = strings.TrimSpace(baseDir)
	packageID = sanitizePackageID(packageID)
	trimmedScriptPath := filepath.Clean(strings.TrimSpace(scriptPath))
	if baseDir == "" || packageID == "" {
		return "", fmt.Errorf("skill package 路径无效")
	}
	if trimmedScriptPath == "." || trimmedScriptPath == "" {
		return "", fmt.Errorf("script_path 不能为空")
	}
	if strings.HasPrefix(trimmedScriptPath, "..") || filepath.IsAbs(trimmedScriptPath) {
		return "", fmt.Errorf("script_path 非法: %s", scriptPath)
	}
	packageDir := filepath.Join(baseDir, packageID)
	if _, err := os.Stat(filepath.Join(packageDir, "SKILL.md")); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("skill package 不存在: %s", packageID)
		}
		return "", fmt.Errorf("读取技能包失败: %w", err)
	}
	path := filepath.Join(packageDir, trimmedScriptPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("创建脚本目录失败: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("写入脚本失败: %w", err)
	}
	return path, nil
}

func sanitizePackageID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", "_", "-")
	cleaned := replacer.Replace(strings.ToLower(raw))
	parts := strings.FieldsFunc(cleaned, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-')
	})
	return strings.Trim(strings.Join(parts, "-"), "-")
}

func buildSkillPackageMarkdown(frontMatter map[string]any, body string) (string, error) {
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
