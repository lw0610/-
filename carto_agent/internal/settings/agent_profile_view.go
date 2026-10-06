package settings

import (
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
)

type AgentProfileGlobalView struct {
	SkillPackages []string `json:"skill_packages"`
}

type AgentProfileRuntimeAgentData struct {
	DisplayName   string                `json:"display_name"`
	Description   string                `json:"description"`
	IsMain        bool                  `json:"is_main,omitempty"`
	Deletable     bool                  `json:"deletable,omitempty"`
	Enabled       bool                  `json:"enabled"`
	LLM           config.AgentLLMConfig `json:"llm,omitempty"`
	SkillPackages []string              `json:"skill_packages,omitempty"`
	Prompts       map[string]string     `json:"prompts"`
}

type AgentProfilePlanAgentData struct {
	DisplayName   string                `json:"display_name"`
	IsMain        bool                  `json:"is_main,omitempty"`
	Mode          string                `json:"mode,omitempty"`
	LLM           config.AgentLLMConfig `json:"llm,omitempty"`
	SkillPackages []string              `json:"skill_packages,omitempty"`
	Prompts       map[string]string     `json:"prompts"`
}

type AgentProfileRuntimeAgentView struct {
	Name  string                       `json:"name"`
	Agent AgentProfileRuntimeAgentData `json:"agent"`
}

type AgentProfilePlanAgentView struct {
	Name  string                    `json:"name"`
	Agent AgentProfilePlanAgentData `json:"agent"`
}

type AgentProfilePlanView struct {
	Enabled       bool                        `json:"enabled"`
	MaxIterations int                         `json:"max_iterations"`
	Shared        AgentProfileGlobalView      `json:"shared"`
	Agents        []AgentProfilePlanAgentView `json:"agents"`
}

type AgentProfileDiagnosticsView struct {
	SelectedConfig           string             `json:"selected_config,omitempty"`
	RuntimeActiveConfig      string             `json:"runtime_active_config,omitempty"`
	RuntimeRevision          string             `json:"runtime_revision,omitempty"`
	PlanReady                bool               `json:"plan_ready"`
	PlanIssue                *SettingsModeIssue `json:"plan_issue,omitempty"`
	ToolSchemaRevision       string             `json:"tool_schema_revision,omitempty"`
	ToolAvailabilityRevision string             `json:"tool_availability_revision,omitempty"`
	ActiveInstanceID         string             `json:"active_instance_id,omitempty"`
}

type AgentProfileView struct {
	Meta          config.AgentConfigMeta         `json:"meta"`
	RuntimeShared AgentProfileGlobalView         `json:"runtime_shared"`
	RuntimeAgents []AgentProfileRuntimeAgentView `json:"runtime_agents"`
	HasPlan       bool                           `json:"has_plan"`
	Plan          AgentProfilePlanView           `json:"plan"`
	Diagnostics   AgentProfileDiagnosticsView    `json:"diagnostics,omitempty"`
}

func BuildAgentProfileView(configName string, data *config.AgentConfigData, diagnostics RuntimeDiagnosticsService) AgentProfileView {
	profile := AgentProfileView{}
	if data != nil {
		profile.Meta = cloneAgentConfigMeta(data.Meta)
		if data.Runtime != nil {
			profile.RuntimeShared = cloneGlobalAgentProfileView(data.Runtime.Global)
			profile.RuntimeAgents = buildRuntimeAgentViews(data.Runtime.Agents)
		}
		if data.Plan != nil {
			profile.HasPlan = true
			profile.Plan.MaxIterations = data.Plan.MaxIterations
			profile.Plan.Enabled = data.Meta.PlanEnabled
			profile.Plan.Shared = cloneGlobalAgentProfileView(data.Plan.Global)
			profile.Plan.Agents = buildPlanAgentViews(data.Plan.Agents)
		}
	}
	if profile.HasPlan && profile.Plan.MaxIterations <= 0 {
		profile.Plan.MaxIterations = 100
	}
	profile.Diagnostics = buildAgentProfileDiagnostics(configName, profile.HasPlan && profile.Plan.Enabled, diagnostics)
	return profile
}

func ExpandAgentProfileView(profile AgentProfileView) *config.AgentConfigData {
	meta := cloneAgentConfigMeta(profile.Meta)
	hasPlan := profile.HasPlan || hasPlanProfileData(profile.Plan)
	meta.PlanEnabled = hasPlan && profile.Plan.Enabled

	result := &config.AgentConfigData{
		Meta: meta,
		Runtime: &config.RuntimeConfig{
			Global: expandGlobalAgentProfileView(profile.RuntimeShared),
			Agents: expandRuntimeAgentViews(profile.RuntimeAgents),
		},
	}

	if hasPlan {
		result.Plan = &config.PlanConfig{
			MaxIterations: normalizePlanMaxIterations(profile.Plan.MaxIterations),
			Global:        expandGlobalAgentProfileView(profile.Plan.Shared),
			Agents:        expandPlanAgentViews(profile.Plan.Agents),
		}
	}

	return result
}

func buildRuntimeAgentViews(agents map[string]config.AgentDefData) []AgentProfileRuntimeAgentView {
	if len(agents) == 0 {
		return []AgentProfileRuntimeAgentView{}
	}
	result := make([]AgentProfileRuntimeAgentView, 0, len(agents))
	for name, agent := range agents {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		result = append(result, AgentProfileRuntimeAgentView{
			Name:  trimmed,
			Agent: cloneRuntimeAgentView(agent),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Agent.IsMain != result[j].Agent.IsMain {
			return result[i].Agent.IsMain
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func buildPlanAgentViews(agents map[string]config.PlanAgentDefData) []AgentProfilePlanAgentView {
	if len(agents) == 0 {
		return []AgentProfilePlanAgentView{}
	}
	result := make([]AgentProfilePlanAgentView, 0, len(agents))
	for name, agent := range agents {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		result = append(result, AgentProfilePlanAgentView{
			Name:  trimmed,
			Agent: clonePlanAgentView(agent),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Agent.IsMain != result[j].Agent.IsMain {
			return result[i].Agent.IsMain
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func expandRuntimeAgentViews(items []AgentProfileRuntimeAgentView) map[string]config.AgentDefData {
	result := make(map[string]config.AgentDefData, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		result[name] = expandRuntimeAgentView(item.Agent)
	}
	return result
}

func expandPlanAgentViews(items []AgentProfilePlanAgentView) map[string]config.PlanAgentDefData {
	result := make(map[string]config.PlanAgentDefData, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		result[name] = expandPlanAgentView(item.Agent)
	}
	return result
}

func buildAgentProfileDiagnostics(configName string, planEnabled bool, diagnostics RuntimeDiagnosticsService) AgentProfileDiagnosticsView {
	view := AgentProfileDiagnosticsView{
		SelectedConfig: strings.TrimSpace(configName),
		PlanReady:      false,
	}
	if diagnostics == nil {
		return view
	}

	view.RuntimeActiveConfig = strings.TrimSpace(diagnostics.GetRuntimeActiveConfigName())
	view.RuntimeRevision = strings.TrimSpace(diagnostics.GetRuntimeRevision())
	view.ToolSchemaRevision = strings.TrimSpace(diagnostics.GetToolSchemaRevision())
	view.ToolAvailabilityRevision = strings.TrimSpace(diagnostics.GetToolAvailabilityRevision())
	view.ActiveInstanceID = strings.TrimSpace(diagnostics.GetActiveInstanceID())

	issues := diagnostics.GetRuntimeModeIssues()
	if issue, ok := issues["plan"]; ok {
		issueCopy := issue
		view.PlanIssue = &issueCopy
	}
	view.PlanReady = planEnabled && view.PlanIssue == nil && view.RuntimeActiveConfig == strings.TrimSpace(configName)
	return view
}

func hasPlanProfileData(plan AgentProfilePlanView) bool {
	if plan.Enabled {
		return true
	}
	if len(plan.Agents) > 0 {
		return true
	}
	if plan.MaxIterations > 0 {
		return true
	}
	return hasGlobalAgentProfileContent(plan.Shared)
}

func hasGlobalAgentProfileContent(cfg AgentProfileGlobalView) bool {
	if len(cfg.SkillPackages) > 0 {
		return true
	}
	return false
}

func normalizePlanMaxIterations(value int) int {
	if value > 0 {
		return value
	}
	return 100
}

func cloneAgentConfigMeta(meta config.AgentConfigMeta) config.AgentConfigMeta {
	return config.AgentConfigMeta{
		DefaultExecutionMode: strings.TrimSpace(meta.DefaultExecutionMode),
		Description:          strings.TrimSpace(meta.Description),
		PlanEnabled:          meta.PlanEnabled,
	}
}

func cloneGlobalAgentProfileView(cfg config.GlobalAgentConfig) AgentProfileGlobalView {
	return AgentProfileGlobalView{
		SkillPackages: append([]string(nil), cfg.SkillPackages...),
	}
}

func expandGlobalAgentProfileView(cfg AgentProfileGlobalView) config.GlobalAgentConfig {
	return config.GlobalAgentConfig{
		SkillPackages: append([]string(nil), cfg.SkillPackages...),
	}
}

func cloneRuntimeAgentView(agent config.AgentDefData) AgentProfileRuntimeAgentData {
	return AgentProfileRuntimeAgentData{
		DisplayName: strings.TrimSpace(agent.DisplayName),
		Description: strings.TrimSpace(agent.Description),
		IsMain:      agent.IsMain,
		Deletable:   agent.Deletable,
		Enabled:     agent.Enabled,
		LLM: config.AgentLLMConfig{
			Provider: strings.TrimSpace(agent.LLM.Provider),
		},
		SkillPackages: append([]string(nil), agent.SkillPackages...),
		Prompts:       clonePromptMap(agent.Prompts),
	}
}

func expandRuntimeAgentView(agent AgentProfileRuntimeAgentData) config.AgentDefData {
	return config.AgentDefData{
		DisplayName: strings.TrimSpace(agent.DisplayName),
		Description: strings.TrimSpace(agent.Description),
		IsMain:      agent.IsMain,
		Deletable:   agent.Deletable,
		Enabled:     agent.Enabled,
		LLM: config.AgentLLMConfig{
			Provider: strings.TrimSpace(agent.LLM.Provider),
		},
		SkillPackages: append([]string(nil), agent.SkillPackages...),
		Prompts:       clonePromptMap(agent.Prompts),
	}
}

func clonePlanAgentView(agent config.PlanAgentDefData) AgentProfilePlanAgentData {
	return AgentProfilePlanAgentData{
		DisplayName: strings.TrimSpace(agent.DisplayName),
		IsMain:      agent.IsMain,
		Mode:        strings.TrimSpace(agent.Mode),
		LLM: config.AgentLLMConfig{
			Provider: strings.TrimSpace(agent.LLM.Provider),
		},
		SkillPackages: append([]string(nil), agent.SkillPackages...),
		Prompts:       clonePromptMap(agent.Prompts),
	}
}

func expandPlanAgentView(agent AgentProfilePlanAgentData) config.PlanAgentDefData {
	mode := strings.TrimSpace(agent.Mode)
	if mode == "" {
		mode = config.PlanToolStrategyReadonlyPlus
	}
	return config.PlanAgentDefData{
		DisplayName: strings.TrimSpace(agent.DisplayName),
		IsMain:      agent.IsMain,
		Mode:        mode,
		LLM: config.AgentLLMConfig{
			Provider: strings.TrimSpace(agent.LLM.Provider),
		},
		SkillPackages: append([]string(nil), agent.SkillPackages...),
		Prompts:       clonePromptMap(agent.Prompts),
	}
}

func clonePromptMap(prompts map[string]string) map[string]string {
	if len(prompts) == 0 {
		return map[string]string{}
	}
	result := make(map[string]string, len(prompts))
	for key, value := range prompts {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		result[trimmedKey] = value
	}
	return result
}
