package creatortools

import (
	"context"
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/skill"
)

const (
	ToolListSkillPackages       = "list_skill_packages"
	ToolCreateSkillPackage      = "create_skill_package"
	ToolValidateSkillDefinition = "validate_skill_definition"
	ToolSaveSkillToPackage      = "save_skill_to_package"
	ToolSaveSkillScript         = "save_skill_script"
	ToolListAgentConfigs        = "list_agent_configs"
	ToolCreateAgentConfig       = "create_agent_config"
	ToolUpdateAgentConfig       = "update_agent_config"
	ToolAssignSkillPackages     = "assign_skill_packages"
)

type Services struct {
	SkillCatalog       *settings.FileSkillService
	AgentConfigManager *config.AgentConfigManager
}

type Handler func(args map[string]any, services Services) (map[string]any, error)

var handlerRegistry = map[string]Handler{
	ToolListSkillPackages: func(_ map[string]any, services Services) (map[string]any, error) {
		return listSkillPackages(services)
	},
	ToolCreateSkillPackage: createSkillPackage,
	ToolValidateSkillDefinition: func(args map[string]any, _ Services) (map[string]any, error) {
		return validateSkillDefinition(args)
	},
	ToolSaveSkillToPackage: saveSkillToPackage,
	ToolSaveSkillScript:    saveSkillScript,
	ToolListAgentConfigs: func(_ map[string]any, services Services) (map[string]any, error) {
		return listAgentConfigs(services)
	},
	ToolCreateAgentConfig:   createAgentConfig,
	ToolUpdateAgentConfig:   updateAgentConfig,
	ToolAssignSkillPackages: assignSkillPackages,
}

func Invoke(ctx context.Context, toolName string, args map[string]any, services Services) (any, error) {
	_ = ctx
	handler, ok := handlerRegistry[strings.TrimSpace(toolName)]
	if !ok {
		return nil, fmt.Errorf("不支持的 creator tool: %s", toolName)
	}
	return handler(args, services)
}

func Handles(toolName string) bool {
	_, ok := handlerRegistry[strings.TrimSpace(toolName)]
	return ok
}

func listSkillPackages(services Services) (map[string]any, error) {
	if services.SkillCatalog == nil {
		return nil, fmt.Errorf("skill catalog 未初始化")
	}
	packages, err := services.SkillCatalog.ListSkillPackages()
	if err != nil {
		return nil, err
	}
	return map[string]any{"packages": packages}, nil
}

func createSkillPackage(args map[string]any, services Services) (map[string]any, error) {
	if services.SkillCatalog == nil {
		return nil, fmt.Errorf("skill catalog 未初始化")
	}
	path, err := skill.CreateSkillPackage(services.SkillCatalog.ManagedSkillsDir(), skill.PackageCreateRequest{
		PackageID:   stringArg(args, "package_id", "name"),
		Name:        stringArg(args, "name", "package_id"),
		Description: stringArg(args, "description"),
		Kind:        stringArg(args, "kind"),
		Runtime:     stringArg(args, "runtime"),
		Body:        stringArg(args, "body"),
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": path, "package_id": strings.TrimSpace(stringArg(args, "package_id", "name"))}, nil
}

func validateSkillDefinition(args map[string]any) (map[string]any, error) {
	definitionRaw := args["definition"]
	if definitionRaw == nil {
		definitionRaw = args
	}
	def, err := skill.DecodeToolDefinition(definitionRaw)
	if err != nil {
		return map[string]any{"valid": false}, err
	}
	return map[string]any{"valid": true, "definition": def}, nil
}

func saveSkillToPackage(args map[string]any, services Services) (map[string]any, error) {
	if services.SkillCatalog == nil {
		return nil, fmt.Errorf("skill catalog 未初始化")
	}
	packageID := stringArg(args, "package_id", "package")
	definitionRaw := args["definition"]
	if definitionRaw == nil {
		return nil, fmt.Errorf("definition 不能为空")
	}
	def, err := skill.DecodeToolDefinition(definitionRaw)
	if err != nil {
		return nil, err
	}
	path, err := skill.SaveToolToPackage(services.SkillCatalog.ManagedSkillsDir(), packageID, def)
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": path, "tool_name": def.Metadata.Name, "package_id": packageID}, nil
}

func saveSkillScript(args map[string]any, services Services) (map[string]any, error) {
	if services.SkillCatalog == nil {
		return nil, fmt.Errorf("skill catalog 未初始化")
	}
	packageID := stringArg(args, "package_id", "package")
	scriptPath := stringArg(args, "script_path", "path")
	content := stringArg(args, "content")
	path, err := skill.SaveSkillScript(services.SkillCatalog.ManagedSkillsDir(), packageID, scriptPath, content)
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": path, "package_id": packageID, "script_path": scriptPath}, nil
}

func listAgentConfigs(services Services) (map[string]any, error) {
	if services.AgentConfigManager == nil {
		return nil, fmt.Errorf("agent config manager 未初始化")
	}
	configs, err := services.AgentConfigManager.ListConfigs("")
	if err != nil {
		return nil, err
	}
	return map[string]any{"configs": configs}, nil
}

func createAgentConfig(args map[string]any, services Services) (map[string]any, error) {
	if services.AgentConfigManager == nil {
		return nil, fmt.Errorf("agent config manager 未初始化")
	}
	configName := stringArg(args, "config_name", "name")
	cloneFrom := stringArg(args, "clone_from")
	var data *config.AgentConfigData
	if raw := mapArg(args, "config_data"); raw != nil {
		decoded, err := settings.DecodeAgentConfigData(raw)
		if err != nil {
			return nil, err
		}
		data = decoded
	}
	if err := config.CreateManagedConfig(services.AgentConfigManager, configName, cloneFrom, data); err != nil {
		return nil, err
	}
	return map[string]any{"config_name": configName, "clone_from": cloneFrom}, nil
}

func updateAgentConfig(args map[string]any, services Services) (map[string]any, error) {
	if services.AgentConfigManager == nil {
		return nil, fmt.Errorf("agent config manager 未初始化")
	}
	configName := stringArg(args, "config_name", "name")
	configData := mapArg(args, "config_data")
	if configData == nil {
		return nil, fmt.Errorf("config_data 不能为空")
	}
	decoded, err := settings.DecodeAgentConfigData(configData)
	if err != nil {
		return nil, err
	}
	if err := config.UpdateManagedConfig(services.AgentConfigManager, configName, decoded); err != nil {
		return nil, err
	}
	return map[string]any{"config_name": configName}, nil
}

func assignSkillPackages(args map[string]any, services Services) (map[string]any, error) {
	if services.AgentConfigManager == nil {
		return nil, fmt.Errorf("agent config manager 未初始化")
	}
	configName := stringArg(args, "config_name")
	mode := stringArg(args, "mode")
	target := stringArg(args, "target")
	agentName := stringArg(args, "agent_name")
	skills := stringSliceArg(args, "skill_packages")
	data, err := config.AssignManagedSkillPackages(services.AgentConfigManager, configName, mode, target, agentName, skills)
	if err != nil {
		return nil, err
	}
	return map[string]any{"config_name": configName, "mode": mode, "target": target, "agent_name": agentName, "skill_packages": skills, "config_data": data}, nil
}

func stringArg(values map[string]any, names ...string) string {
	for _, name := range names {
		if name == "" {
			continue
		}
		if raw, ok := values[name].(string); ok {
			trimmed := strings.TrimSpace(raw)
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func mapArg(values map[string]any, name string) map[string]any {
	raw, _ := values[name].(map[string]any)
	return raw
}

func stringSliceArg(values map[string]any, name string) []string {
	raw, ok := values[name].([]any)
	if !ok {
		if direct, ok := values[name].([]string); ok {
			return append([]string(nil), direct...)
		}
		return nil
	}
	items := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}
