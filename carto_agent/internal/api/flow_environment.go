package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/integrations"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/autocarto/carto_agent/internal/tool"
)

func revisionOf(value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func (a *API) loadFlowEnvironmentUncached(ctx context.Context, scheme string) (flow.Environment, error) {
	if err := ctx.Err(); err != nil {
		return flow.Environment{}, err
	}
	if a.assetCatalogService == nil {
		return flow.Environment{}, fmt.Errorf("方案资产服务尚未配置")
	}
	scheme = strings.TrimSpace(scheme)
	if scheme == "" {
		configs, err := a.assetCatalogService.ListAgentConfigs("")
		if err != nil {
			return flow.Environment{}, err
		}
		for _, item := range configs {
			if item.IsActive {
				scheme = item.ConfigName
				break
			}
		}
		if scheme == "" {
			return flow.Environment{}, fmt.Errorf("当前没有启用的智能体方案")
		}
	}
	profile, err := a.assetCatalogService.GetAgentProfile(scheme)
	if err != nil {
		return flow.Environment{}, err
	}
	env := flow.Environment{Scheme: scheme, SchemeRevision: revisionOf(settings.ExpandAgentProfileView(profile)), Roles: []flow.RoleSpec{}, Packages: []flow.PackageSpec{}, Capabilities: []flow.CapabilitySpec{}}
	packages, err := a.assetCatalogService.ListSkillPackages()
	if err != nil {
		return env, err
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].Name < packages[j].Name })
	metadata := map[string]subagents.RuntimeSkillPackageMetadata{}
	packageByID := map[string]flow.PackageSpec{}
	toolPackages := map[string][]string{}
	capabilities := map[string]flow.CapabilitySpec{}
	configDir, err := config.ResolveAutoCartoConfigDir()
	if err != nil {
		return env, err
	}
	definitions, err := tool.LoadAuthorityDefinitions(config.ResolveBuiltinToolsDir(), config.ResolvePluginUserToolsDir(), config.ResolveGlobalToolsDir(configDir))
	if err != nil {
		return env, err
	}
	for _, definition := range definitions {
		capabilities[definition.Metadata.Name] = flow.CapabilitySpec{Name: definition.Metadata.Name, DisplayName: definition.Metadata.DisplayName, Description: definition.Spec.Description, Keywords: append([]string{}, definition.Metadata.Keywords...), Parameters: flowParameterNames(definition.Spec.Parameters), PackageIDs: []string{}}
	}
	for _, summary := range packages {
		if summary.Kind != "category" {
			continue
		}
		detail, err := a.assetCatalogService.GetSkillPackage(summary.Name)
		if err != nil {
			return env, fmt.Errorf("读取工具包“%s”失败：%w", summary.DisplayName, err)
		}
		id := summary.PackageID
		if id == "" {
			id = summary.Name
		}
		if id == "flow-control" && summary.Enabled {
			env.PlanningInstructions = detail.MarkdownBody
		}
		entry := flow.PackageSpec{ID: id, DisplayName: summary.DisplayName, Tools: uniqueFlowStrings(detail.ToolRefs), Enabled: summary.Enabled, Kind: summary.Kind, Hidden: summary.Hidden}
		for _, definition := range detail.Tools {
			name := definition.Metadata.Name
			entry.Tools = uniqueFlowStrings(append(entry.Tools, name))
			if _, ok := capabilities[name]; !ok {
				capabilities[name] = flow.CapabilitySpec{Name: name, DisplayName: definition.Metadata.DisplayName, Description: definition.Spec.Description, Keywords: definition.Metadata.Keywords, Parameters: flowSkillParameterNames(definition.Spec.Parameters)}
			}
		}
		sort.Strings(entry.Tools)
		env.Packages = append(env.Packages, entry)
		packageByID[id] = entry
		if summary.Name != id {
			packageByID[summary.Name] = entry
		}
		if entry.Enabled {
			metadata[id] = subagents.RuntimeSkillPackageMetadata{DisplayName: summary.DisplayName, Domain: summary.Domain, System: summary.System, Hidden: summary.Hidden, AutoMount: summary.AutoMount}
		}
		for _, name := range entry.Tools {
			toolPackages[name] = append(toolPackages[name], id)
		}
	}
	for name, item := range capabilities {
		// 正式目录中没有归属的孤立定义不作为“其他工具包”候选。
		if len(toolPackages[name]) == 0 {
			continue
		}
		item.PackageIDs = uniqueFlowStrings(toolPackages[name])
		sort.Strings(item.PackageIDs)
		env.Capabilities = append(env.Capabilities, item)
	}
	sort.Slice(env.Capabilities, func(i, j int) bool { return env.Capabilities[i].Name < env.Capabilities[j].Name })
	providerName := ""
	for _, item := range profile.RuntimeAgents {
		agent := item.Agent
		ids := uniqueFlowStrings(append(append(append([]string{}, profile.RuntimeShared.ToolPackages...), agent.ToolPackages...), subagents.AutoMountedPackageNames(metadata, "deep", agent.IsMain, agent.Executor)...))
		actualIDs, tools := []string{}, []string{}
		for _, id := range ids {
			if entry, ok := packageByID[id]; ok && entry.Enabled {
				actualIDs = append(actualIDs, entry.ID)
				tools = append(tools, entry.Tools...)
			}
		}
		actualIDs, tools = uniqueFlowStrings(actualIDs), uniqueFlowStrings(tools)
		sort.Strings(actualIDs)
		sort.Strings(tools)
		kind := agent.Kind
		if kind == "" {
			kind = "agent"
		}
		env.Roles = append(env.Roles, flow.RoleSpec{ID: item.Name, Provider: agent.LLM.Provider, DisplayName: agent.DisplayName, Description: agent.Description, Kind: kind, Executor: agent.Executor, FlowOnly: agent.FlowOnly, Enabled: agent.Enabled, IsMain: agent.IsMain, ToolPackages: actualIDs, Tools: tools})
		if agent.IsMain && agent.Enabled {
			providerName = agent.LLM.Provider
		}
	}
	env.MainProvider = providerName
	env.CatalogRevision = revisionOf(struct {
		Packages     []flow.PackageSpec
		Capabilities []flow.CapabilitySpec
	}{env.Packages, env.Capabilities})
	if a.configService != nil {
		env.SchemeRevision = revisionOf([]string{env.SchemeRevision, a.configService.GetConfigRevision()})
		settings, err := a.configService.GetFullConfig()
		if err == nil {
			provider := settings.GetActiveProvider()
			if env.MainProvider == "" && provider != nil {
				env.MainProvider = provider.Name
			}
			for _, configured := range settings.LLM.Providers {
				if configured.Vision {
					env.VisionProviders = append(env.VisionProviders, configured.Name)
				}
			}

		}
	}
	assets, assetsErr := config.ResolveAssetsRoot()
	home, homeErr := config.ResolveAutoCartoHome()
	if assetsErr == nil && homeErr == nil {
		env.DataSources, err = datasource.Load(filepath.Join(assets, "tools", "system", "data-acquisition", "assets"), filepath.Join(home, "sources"))
		if err != nil {
			return env, fmt.Errorf("加载数据来源目录：%w", err)
		}
	}
	env.Integrations = map[string]bool{}
	env.Secrets = map[string]bool{}
	if a.configService != nil {
		if services, e := a.configService.GetIntegrationsConfig(); e == nil {
			raw, _ := json.Marshal(services)
			var enabled map[string]struct {
				Enabled bool `json:"enabled"`
			}
			_ = json.Unmarshal(raw, &enabled)
			for id, service := range enabled {
				env.Integrations[id] = service.Enabled
			}
		}
		for _, name := range a.configService.ConfiguredSecretNames() {
			env.Secrets[name] = true
		}
		acquisition := a.configService.GetAcquisitionConfig()
		env.MaxDownloadMB = acquisition.MaxDownloadMB
		env.WebSearchEnabled = acquisition.WebSearchEnabled
	}
	env.PackageReady = func(id string) bool {
		p, ok := packageByID[id]
		return ok && integrations.PackageReady(p.Tools, env.Integrations)
	}
	sourceRevision := ""
	if env.DataSources != nil {
		sourceRevision = env.DataSources.Revision
	}
	env.CatalogRevision = revisionOf([]any{env.CatalogRevision, sourceRevision, env.Integrations, env.Secrets})
	return env, nil
}

func uniqueFlowStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

// Keep the diagnostic vocabulary tied to the same authority definitions used by tools.
func flowParameterNames(parameters []tool.ToolParameter) []string {
	names := []string{}
	for _, parameter := range parameters {
		if name := strings.TrimSpace(parameter.Name); name != "" {
			names = append(names, name)
		}
	}
	names = uniqueFlowStrings(names)
	sort.Strings(names)
	return names
}

func flowSkillParameterNames(parameters []skill.SkillParameter) []string {
	names := []string{}
	for _, parameter := range parameters {
		if name := strings.TrimSpace(parameter.Name); name != "" {
			names = append(names, name)
		}
	}
	names = uniqueFlowStrings(names)
	sort.Strings(names)
	return names
}

// PlanEnvironment builds only local role, capability and data catalogs.
func (a *API) PlanEnvironment(ctx context.Context, scheme string) (flow.Environment, error) {
	return a.loadFlowEnvironment(ctx, scheme)
}
