package capability

import (
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/toolcatalog"
)

type ToolCatalogProjectionMode string

const (
	ToolCatalogConfigured ToolCatalogProjectionMode = "configured"
	ToolCatalogEffective  ToolCatalogProjectionMode = "effective"
)

func (s *CapabilitySnapshot) AsRuntimeCatalogItems() []middlewareclient.ToolCatalogItem {
	if s == nil || len(s.Tools) == 0 {
		return []middlewareclient.ToolCatalogItem{}
	}
	items := make([]middlewareclient.ToolCatalogItem, 0, len(s.Tools))
	for _, name := range orderedToolNames(s) {
		tool := s.Tools[name]
		items = append(items, middlewareclient.ToolCatalogItem{
			Name:              tool.Name,
			ProviderID:        tool.ProviderID,
			RawName:           tool.RawName,
			Description:       tool.Description,
			Category:          tool.Category,
			Domain:            tool.Category,
			Source:            tool.Source,
			InputSchema:       cloneSchema(tool.InputSchema),
			Backend:           tool.Backend,
			Available:         tool.Available,
			UnavailableReason: tool.UnavailableReason,
			InstanceIDs:       append([]string(nil), tool.InstanceIDs...),
		})
	}
	return items
}

func (s *CapabilitySnapshot) ProjectToolCatalog(configuredTools []string, mode ToolCatalogProjectionMode) []toolcatalog.Item {
	if s == nil || len(s.Tools) == 0 {
		return []toolcatalog.Item{}
	}
	configuredSet := make(map[string]struct{}, len(configuredTools))
	for _, name := range configuredTools {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			configuredSet[trimmed] = struct{}{}
		}
	}
	items := make([]toolcatalog.Item, 0, len(s.Tools))
	for _, name := range orderedToolNames(s) {
		tool := s.Tools[name]
		if len(configuredSet) > 0 {
			if _, ok := configuredSet[tool.Name]; !ok {
				continue
			}
		}
		if mode == ToolCatalogEffective && !tool.Available {
			continue
		}
		enabled := tool.PackageEnabled
		items = append(items, toolcatalog.Item{
			Name:          tool.Name,
			Description:   tool.Description,
			Category:      tool.Category,
			Domain:        tool.Category,
			Source:        tool.Source,
			Backend:       tool.Backend,
			PackageID:     tool.Package.PackageID,
			Kind:          tool.PackageKind,
			Enabled:       &enabled,
			Available:     tool.Available,
			MissingReason: tool.UnavailableReason,
			InstanceIDs:   append([]string(nil), tool.InstanceIDs...),
		})
	}
	return items
}

func (s *CapabilitySnapshot) BuildCapabilityPayload(filterAgent, filterKeyword string, baseToolDescs map[string]string) map[string]any {
	configName := ""
	if s != nil {
		configName = s.ConfigName
	}
	agentFilter := strings.TrimSpace(filterAgent)
	keywordFilter := strings.ToLower(strings.TrimSpace(filterKeyword))
	agents := make([]map[string]any, 0)
	totalTools := map[string]struct{}{}
	if s != nil {
		for _, agent := range s.Agents {
			if agentFilter != "" && agent.AgentName != agentFilter {
				continue
			}
			toolPayloads := make([]map[string]any, 0, len(agent.Tools))
			for _, tool := range agent.Tools {
				description := strings.TrimSpace(tool.Description)
				if description == "" {
					description = strings.TrimSpace(baseToolDescs[tool.Name])
				}
				if keywordFilter != "" && !strings.Contains(strings.ToLower(tool.Name), keywordFilter) && !strings.Contains(strings.ToLower(description), keywordFilter) {
					continue
				}
				toolPayloads = append(toolPayloads, map[string]any{
					"name":        tool.Name,
					"description": description,
					"package_id":  tool.Package.PackageID,
				})
				totalTools[tool.Name] = struct{}{}
			}
			if keywordFilter != "" && len(toolPayloads) == 0 {
				continue
			}
			agents = append(agents, map[string]any{
				"agent_name":     agent.AgentName,
				"display_name":   agent.DisplayName,
				"description":    agent.Description,
				"is_main":        agent.IsMain,
				"enabled":        agent.Enabled,
				"skill_packages": append([]string(nil), agent.SkillPackages...),
				"tools":          toolPayloads,
			})
		}
	}
	return map[string]any{
		"config_name": configName,
		"agents":      agents,
		"total_tools": len(totalTools),
	}
}

func (s *CapabilitySnapshot) DiffRuntimeCatalog(other *CapabilitySnapshot) toolcatalog.ToolDiffResult {
	if s == nil && other == nil {
		return toolcatalog.ToolDiffResult{}
	}
	if s == nil {
		return toolcatalog.DiffTools(nil, other.AsRuntimeCatalogItems())
	}
	if other == nil {
		return toolcatalog.DiffTools(s.AsRuntimeCatalogItems(), s.AsRuntimeCatalogItems())
	}
	return toolcatalog.DiffTools(s.AsRuntimeCatalogItems(), other.AsRuntimeCatalogItems())
}

func orderedToolNames(snapshot *CapabilitySnapshot) []string {
	if snapshot == nil || len(snapshot.Tools) == 0 {
		return []string{}
	}
	if len(snapshot.ToolOrder) > 0 {
		return append([]string(nil), snapshot.ToolOrder...)
	}
	names := make([]string, 0, len(snapshot.Tools))
	for name := range snapshot.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
