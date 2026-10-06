package plantools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/capability"
	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/logger"
	"github.com/autocarto/carto_agent/internal/toolresult"
)

const ToolQueryBuildCapabilities = "query_build_capabilities"

// Services 提供 plantools 所需的运行时依赖。
type Services struct {
	AgentConfigManager *config.AgentConfigManager
	ActiveConfigName   string
	ActiveConfig       *config.AgentConfigFile
	SnapshotStore      *capability.SnapshotStore
	SnapshotMode       string
	SkillsDir          string
	BuiltinSkillsDir   string
	UserSkillsDir      string
	PluginSkillsDir    string
	ConfigBaseDir      string
	BaseTools          []tool.BaseTool
}

// Handles 判断给定工具名是否由 plantools 处理。
func Handles(toolName string) bool {
	return strings.TrimSpace(toolName) == ToolQueryBuildCapabilities
}

// Invoke 路由到对应的 plantools 处理函数。
func Invoke(ctx context.Context, toolName string, args map[string]any, services Services) (any, error) {
	switch strings.TrimSpace(toolName) {
	case ToolQueryBuildCapabilities:
		return queryBuildCapabilities(ctx, args, services)
	default:
		return nil, fmt.Errorf("不支持的 plan tool: %s", toolName)
	}
}

func NewQueryBuildCapabilitiesTool(services Services) tool.BaseTool {
	state := &queryBuildCapabilitiesToolState{services: services}
	info, _ := state.Info(context.Background())
	return toolutils.NewEnhancedTool(info, state.run)
}

type queryBuildCapabilitiesToolState struct {
	services Services
}

func (t *queryBuildCapabilitiesToolState) Info(context.Context) (*schema.ToolInfo, error) {
	rawSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent_name": map[string]any{
				"type":        "string",
				"description": "可选，按 BUILD agent 名称过滤",
			},
			"keyword": map[string]any{
				"type":        "string",
				"description": "可选，按工具名称或描述关键词过滤",
			},
		},
		"additionalProperties": false,
	}
	return &schema.ToolInfo{
		Name:        ToolQueryBuildCapabilities,
		Desc:        "查询当前智能体方案在 BUILD 模式下各个 agent 的可用工具和技能列表",
		ParamsOneOf: schema.NewParamsOneOfByJSONSchema(localJSONSchema(rawSchema)),
	}, nil
}

func (t *queryBuildCapabilitiesToolState) run(ctx context.Context, args map[string]any) (*schema.ToolResult, error) {
	result, err := queryBuildCapabilities(ctx, args, t.services)
	if err != nil {
		return nil, err
	}
	return toolresult.JSON(result)
}

type capabilityToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	PackageID   string `json:"package_id,omitempty"`
}

type capabilityAgentInfo struct {
	AgentName     string               `json:"agent_name"`
	DisplayName   string               `json:"display_name,omitempty"`
	Description   string               `json:"description,omitempty"`
	IsMain        bool                 `json:"is_main,omitempty"`
	Enabled       bool                 `json:"enabled"`
	SkillPackages []string             `json:"skill_packages,omitempty"`
	Tools         []capabilityToolInfo `json:"tools"`
}

func queryBuildCapabilities(_ context.Context, args map[string]any, services Services) (any, error) {
	filterAgent, _ := args["agent_name"].(string)
	filterAgent = strings.TrimSpace(filterAgent)
	filterKeyword, _ := args["keyword"].(string)
	filterKeyword = strings.ToLower(strings.TrimSpace(filterKeyword))
	baseToolInfos := collectBaseToolDescriptions(services.BaseTools)
	mode := strings.ToLower(strings.TrimSpace(services.SnapshotMode))
	if mode == "shadow" && services.SnapshotStore != nil {
		if snapshot := services.SnapshotStore.Get(); snapshot != nil && len(snapshot.Agents) > 0 {
			legacyPayload, err := buildLegacyCapabilityPayload(args, services, baseToolInfos)
			if err == nil {
				shadowPayload := snapshot.BuildCapabilityPayload(filterAgent, filterKeyword, baseToolInfos)
				raw, _ := json.Marshal(map[string]any{"changed": !jsonEqualPayloads(legacyPayload, shadowPayload)})
				if !jsonEqualPayloads(legacyPayload, shadowPayload) {
					logger.Warn("snapshot_shadow_diff planner_capability=%s", string(raw))
				} else {
					logger.Info("snapshot_shadow_diff planner_capability=%s", string(raw))
				}
			}
		}
	}
	if services.SnapshotStore != nil && (mode == "cutover" || mode == "primary") {
		if snapshot := services.SnapshotStore.Get(); snapshot != nil && len(snapshot.Agents) > 0 {
			return snapshot.BuildCapabilityPayload(filterAgent, filterKeyword, baseToolInfos), nil
		}
		if mode == "primary" {
			return map[string]any{
				"config_name": strings.TrimSpace(services.ActiveConfigName),
				"agents":      []capabilityAgentInfo{},
				"total_tools": 0,
			}, nil
		}
	}

	return buildLegacyCapabilityPayload(args, services, baseToolInfos)
}

func buildLegacyCapabilityPayload(args map[string]any, services Services, baseToolInfos map[string]string) (map[string]any, error) {
	filterAgent, _ := args["agent_name"].(string)
	filterAgent = strings.TrimSpace(filterAgent)
	filterKeyword, _ := args["keyword"].(string)
	filterKeyword = strings.ToLower(strings.TrimSpace(filterKeyword))
	activeConfigName := strings.TrimSpace(services.ActiveConfigName)
	activeConfig := services.ActiveConfig
	if services.AgentConfigManager != nil {
		activeConfigName = services.AgentConfigManager.GetActiveConfigName()
		activeConfig = services.AgentConfigManager.GetActiveConfig()
	}
	if activeConfig == nil || activeConfig.Runtime == nil {
		return nil, fmt.Errorf("active runtime config is unavailable")
	}

	userSkillsDir := strings.TrimSpace(services.UserSkillsDir)
	if userSkillsDir == "" {
		userSkillsDir = strings.TrimSpace(services.SkillsDir)
	}
	runtimeSkillConfig := config.ConvertToRuntimeSkillConfigWithPlugin(activeConfig, services.PluginSkillsDir, services.BuiltinSkillsDir, userSkillsDir, services.ConfigBaseDir, activeConfigName)
	if runtimeSkillConfig == nil {
		return nil, fmt.Errorf("runtime skill config is unavailable")
	}
	packageTools, err := subagents.LoadPackageToolsMultiDir(config.ResolveRuntimeSkillDirsWithPlugin(services.PluginSkillsDir, services.BuiltinSkillsDir, userSkillsDir, services.ConfigBaseDir, activeConfigName)...)
	if err != nil {
		return nil, fmt.Errorf("load package tools: %w", err)
	}
	agentNames := make([]string, 0, len(activeConfig.Runtime.Agents))
	for agentName := range activeConfig.Runtime.Agents {
		agentNames = append(agentNames, agentName)
	}
	sort.Strings(agentNames)

	agents := make([]capabilityAgentInfo, 0, len(agentNames))
	allToolSet := make(map[string]struct{})
	for _, agentName := range agentNames {
		rawAgent := activeConfig.Runtime.Agents[agentName]
		if filterAgent != "" && filterAgent != agentName {
			continue
		}
		agentToolToPackage := buildOrderedToolToPackage(runtimeSkillConfig.GetAgentSkills(agentName), packageTools)
		allowedTools := runtimeSkillConfig.GetAgentAllowedTools(agentName)
		toolInfos := make([]capabilityToolInfo, 0, len(allowedTools))
		for _, tn := range allowedTools {
			normalized := normalizeToolName(tn)
			description := lookupToolDescription(baseToolInfos, normalized)
			if filterKeyword != "" && !strings.Contains(strings.ToLower(normalized), filterKeyword) && !strings.Contains(strings.ToLower(description), filterKeyword) {
				continue
			}
			toolInfos = append(toolInfos, capabilityToolInfo{
				Name:        strings.TrimSpace(tn),
				Description: description,
				PackageID:   agentToolToPackage[normalized],
			})
			allToolSet[normalized] = struct{}{}
		}
		if filterKeyword != "" && len(toolInfos) == 0 {
			continue
		}
		agents = append(agents, capabilityAgentInfo{
			AgentName:     agentName,
			DisplayName:   rawAgent.DisplayName,
			Description:   rawAgent.Description,
			IsMain:        rawAgent.IsMain,
			Enabled:       rawAgent.Enabled,
			SkillPackages: runtimeSkillConfig.GetAgentSkills(agentName),
			Tools:         toolInfos,
		})
	}

	return map[string]any{
		"config_name": activeConfigName,
		"agents":      agents,
		"total_tools": len(allToolSet),
	}, nil
}

func jsonEqualPayloads(left, right map[string]any) bool {
	leftRaw, leftErr := json.Marshal(left)
	rightRaw, rightErr := json.Marshal(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return string(leftRaw) == string(rightRaw)
}

func normalizeToolName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func buildOrderedToolToPackage(packageOrder []string, packageTools map[string][]string) map[string]string {
	if len(packageOrder) == 0 || len(packageTools) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string)
	for _, packageID := range packageOrder {
		trimmedPackageID := strings.TrimSpace(packageID)
		if trimmedPackageID == "" {
			continue
		}
		for _, toolName := range packageTools[trimmedPackageID] {
			normalized := normalizeToolName(toolName)
			if normalized == "" {
				continue
			}
			if _, exists := out[normalized]; exists {
				continue
			}
			out[normalized] = trimmedPackageID
		}
	}
	return out
}

func lookupToolDescription(baseToolInfos map[string]string, normalizedName string) string {
	if len(baseToolInfos) == 0 {
		return ""
	}
	return strings.TrimSpace(baseToolInfos[normalizeToolName(normalizedName)])
}

func collectBaseToolDescriptions(baseTools []tool.BaseTool) map[string]string {
	if len(baseTools) == 0 {
		return nil
	}
	result := make(map[string]string, len(baseTools))
	for _, item := range baseTools {
		info, err := item.Info(context.Background())
		if err != nil || info == nil {
			continue
		}
		name := normalizeToolName(info.Name)
		if name == "" {
			continue
		}
		result[name] = strings.TrimSpace(info.Desc)
	}
	return result
}

func localJSONSchema(rawSchema map[string]any) *jsonschema.Schema {
	raw, err := json.Marshal(rawSchema)
	if err != nil {
		return &jsonschema.Schema{Type: "object"}
	}
	schemaDef := &jsonschema.Schema{}
	if err := json.Unmarshal(raw, schemaDef); err != nil {
		return &jsonschema.Schema{Type: "object"}
	}
	if strings.TrimSpace(schemaDef.Type) == "" {
		schemaDef.Type = "object"
	}
	return schemaDef
}
