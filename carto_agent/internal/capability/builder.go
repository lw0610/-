package capability

import (
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/compute"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/autocarto/carto_agent/internal/toolcatalog"
	"github.com/autocarto/carto_agent/internal/toolrouter"
)

func BuildSnapshot(input BuildInput) (*CapabilitySnapshot, error) {
	snapshot := &CapabilitySnapshot{
		ConfigName: strings.TrimSpace(input.ActiveConfigName),
		Tools:      map[string]ToolCapability{},
		ToolOrder:  []string{},
		Packages:   map[string]PackageCapability{},
		Agents:     []AgentCapabilityView{},
		BuiltAt:    time.Now(),
	}

	for _, summary := range input.PackageSummaries {
		pkg := buildPackageCapability(summary)
		snapshot.Packages[pkg.Key] = pkg
	}
	for key, detail := range input.PackageDetails {
		pkg, ok := snapshot.Packages[strings.TrimSpace(key)]
		if !ok {
			pkg = buildPackageCapability(detail.SkillPackageSummary)
		}
		pkg.ToolNames = extractPackageToolNames(detail)
		pkg.References = cloneReferences(detail.References)
		snapshot.Packages[pkg.Key] = pkg
	}

	if input.Registry == nil {
		return snapshot, nil
	}

	items, err := skill.BuildCatalogItemsFromRegistry(input.Registry)
	if err != nil {
		return nil, err
	}
	definitions := input.Registry.Definitions()
	defsByName := make(map[string]skill.SkillDefinition, len(definitions))
	for _, def := range definitions {
		name := strings.TrimSpace(def.Metadata.Name)
		if name == "" {
			continue
		}
		defsByName[name] = def.Clone()
	}

	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		def := defsByName[name]
		binding := resolveToolPackageBinding(name, snapshot.Packages, snapshot.ConfigName)
		available, unavailableReason, instanceIDs := resolveToolAvailability(item, def, input.ToolAggregatorSnapshot, input.RuntimeSnapshot, input.ComputeReady)
		snapshot.Tools[name] = ToolCapability{
			Name:              name,
			RawName:           name,
			DisplayName:       strings.TrimSpace(def.Metadata.DisplayName),
			Description:       strings.TrimSpace(item.Description),
			Category:          strings.TrimSpace(item.Category),
			Backend:           strings.TrimSpace(item.Backend),
			ProviderID:        strings.TrimSpace(item.ProviderID),
			Source:            strings.TrimSpace(item.Source),
			InputSchema:       cloneSchema(item.InputSchema),
			Available:         available,
			UnavailableReason: unavailableReason,
			InstanceIDs:       instanceIDs,
			Package:           binding.Package,
			PackageKind:       binding.PackageKind,
			PackageEnabled:    binding.PackageEnabled,
			PackagePath:       binding.PackagePath,
		}
		snapshot.ToolOrder = append(snapshot.ToolOrder, name)
	}
	sort.Strings(snapshot.ToolOrder)
	snapshot.Agents = buildAgentCapabilityViews(input, snapshot)
	applySnapshotRevisions(snapshot)
	return snapshot, nil
}

func applySnapshotRevisions(snapshot *CapabilitySnapshot) {
	if snapshot == nil {
		return
	}
	runtimeItems := snapshot.AsRuntimeCatalogItems()
	snapshot.SchemaRevision = strings.TrimSpace(toolcatalog.ToolSchemaRevision(runtimeItems))
	snapshot.AvailabilityRevision = strings.TrimSpace(toolcatalog.ToolAvailabilityRevision(runtimeItems))
	snapshot.ToolCatalogRevision = joinSnapshotRevision(snapshot.SchemaRevision, snapshot.AvailabilityRevision)
	snapshot.RuntimeRevision = snapshot.ToolCatalogRevision
}

func joinSnapshotRevision(schemaRevision, availabilityRevision string) string {
	schemaRevision = strings.TrimSpace(schemaRevision)
	availabilityRevision = strings.TrimSpace(availabilityRevision)
	if schemaRevision == "" {
		return availabilityRevision
	}
	if availabilityRevision == "" {
		return schemaRevision
	}
	return schemaRevision + "/" + availabilityRevision
}

type toolPackageBinding struct {
	Package        PackageRef
	PackageKind    string
	PackageEnabled bool
	PackagePath    string
}

func buildPackageCapability(summary settings.SkillPackageSummary) PackageCapability {
	return PackageCapability{
		Key:         buildPackageKey(summary.Scope, summary.OwnerConfig, summary.PackageID),
		PackageID:   strings.TrimSpace(summary.PackageID),
		Name:        strings.TrimSpace(summary.Name),
		Kind:        strings.TrimSpace(summary.Kind),
		Backend:     strings.TrimSpace(summary.Backend),
		Source:      strings.TrimSpace(summary.Source),
		Enabled:     summary.Enabled,
		Scope:       strings.TrimSpace(summary.Scope),
		OwnerConfig: strings.TrimSpace(summary.OwnerConfig),
		Path:        strings.TrimSpace(summary.Path),
		HasTools:    summary.HasTools,
		HasConfig:   summary.HasConfig,
		ToolNames:   []string{},
	}
}

func buildPackageKey(scope, ownerConfig, packageID string) string {
	trimmedScope := strings.TrimSpace(scope)
	trimmedOwnerConfig := strings.TrimSpace(ownerConfig)
	trimmedPackageID := strings.TrimSpace(packageID)
	if trimmedScope == settings.SkillPackageScopeExclusive {
		return trimmedScope + ":" + trimmedOwnerConfig + ":" + trimmedPackageID
	}
	return trimmedScope + "::" + trimmedPackageID
}

func extractPackageToolNames(detail settings.SkillPackageDetail) []string {
	toolRefs := normalizeToolRefs(detail.ToolRefs)
	if len(toolRefs) > 0 {
		return toolRefs
	}
	toolRefs = normalizeToolRefs(detail.FrontMatter.Tools)
	if len(toolRefs) > 0 {
		return toolRefs
	}
	return nil
}

func normalizeToolRefs(toolRefs []string) []string {
	if len(toolRefs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(toolRefs))
	normalized := make([]string, 0, len(toolRefs))
	for _, toolRef := range toolRefs {
		name := strings.TrimSpace(toolRef)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}
	sort.Strings(normalized)
	return normalized
}

func resolveToolPackageBinding(toolName string, packages map[string]PackageCapability, activeConfigName string) toolPackageBinding {
	trimmedToolName := strings.TrimSpace(toolName)
	trimmedConfigName := strings.TrimSpace(activeConfigName)
	selected := PackageCapability{}
	selectedPriority := -1
	for _, pkg := range packages {
		if !containsToolName(pkg.ToolNames, trimmedToolName) {
			continue
		}
		priority := packageBindingPriority(pkg, trimmedConfigName)
		if priority > selectedPriority || (priority == selectedPriority && selectedPriority >= 0 && strings.TrimSpace(pkg.Key) < strings.TrimSpace(selected.Key)) {
			selected = pkg
			selectedPriority = priority
		}
	}
	if selectedPriority < 0 {
		return toolPackageBinding{}
	}
	return toolPackageBinding{
		Package: PackageRef{
			Key:         selected.Key,
			PackageID:   selected.PackageID,
			Scope:       selected.Scope,
			OwnerConfig: selected.OwnerConfig,
		},
		PackageKind:    selected.Kind,
		PackageEnabled: selected.Enabled,
		PackagePath:    selected.Path,
	}
}

func packageBindingPriority(pkg PackageCapability, activeConfigName string) int {
	if pkg.Scope == settings.SkillPackageScopeExclusive && pkg.OwnerConfig == activeConfigName {
		return 2
	}
	if pkg.Scope == settings.SkillPackageScopeGlobal {
		return 1
	}
	return 0
}

func containsToolName(toolNames []string, toolName string) bool {
	for _, item := range toolNames {
		if strings.TrimSpace(item) == toolName {
			return true
		}
	}
	return false
}

func cloneSchema(schema map[string]any) map[string]any {
	if len(schema) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(schema))
	maps.Copy(cloned, schema)
	return cloned
}

func cloneReferences(references []config.SkillReference) []config.SkillReference {
	if len(references) == 0 {
		return nil
	}
	cloned := make([]config.SkillReference, len(references))
	copy(cloned, references)
	return cloned
}

func buildAgentCapabilityViews(input BuildInput, snapshot *CapabilitySnapshot) []AgentCapabilityView {
	if input.ActiveConfig == nil || input.ActiveConfig.Runtime == nil || input.RuntimeSkillConfig == nil {
		return []AgentCapabilityView{}
	}
	agentNames := make([]string, 0, len(input.ActiveConfig.Runtime.Agents))
	for agentName := range input.ActiveConfig.Runtime.Agents {
		agentNames = append(agentNames, agentName)
	}
	sort.Strings(agentNames)
	views := make([]AgentCapabilityView, 0, len(agentNames))
	for _, agentName := range agentNames {
		agentDef := input.ActiveConfig.Runtime.Agents[agentName]
		skillPackages := input.RuntimeSkillConfig.GetAgentSkills(agentName)
		allowedTools := input.RuntimeSkillConfig.GetAgentAllowedTools(agentName)
		toolRefs := make([]ToolRef, 0, len(allowedTools))
		for _, toolName := range allowedTools {
			toolCapability, ok := snapshot.Tools[strings.TrimSpace(toolName)]
			if !ok {
				continue
			}
			toolRefs = append(toolRefs, ToolRef{
				Name:        toolCapability.Name,
				Description: toolCapability.Description,
				Package:     toolCapability.Package,
			})
		}
		views = append(views, AgentCapabilityView{
			AgentName:     agentName,
			DisplayName:   strings.TrimSpace(agentDef.DisplayName),
			Description:   strings.TrimSpace(agentDef.Description),
			IsMain:        agentDef.IsMain,
			Enabled:       agentDef.Enabled,
			SkillPackages: append([]string(nil), skillPackages...),
			Tools:         toolRefs,
		})
	}
	return views
}

func resolveToolAvailability(item middlewareclient.ToolCatalogItem, definition skill.SkillDefinition, snapshot toolrouter.Snapshot, runtimeSnapshot middlewareclient.RuntimeSnapshot, computeReady bool) (bool, string, []string) {
	name := strings.TrimSpace(item.Name)
	runtimeID := skill.CatalogItemRuntime(item)
	if runtimeID == skill.RuntimeIDInternal {
		return true, "", nil
	}
	if runtimeID == skill.RuntimeIDCLI && definition.Execution.CLI != nil && strings.TrimSpace(definition.Execution.CLI.Command) != "" {
		return true, "", nil
	}
	if isInteractiveCatalogItem(item) {
		runningInstanceIDs := collectRunningInstanceIDs(runtimeSnapshot)
		if interactive, ok := lookupInteractiveTool(snapshot.Interactive, name); ok {
			instanceIDs := append([]string(nil), interactive.InstanceIDs...)
			if len(instanceIDs) == 0 {
				instanceIDs = append([]string(nil), runningInstanceIDs...)
			}
			if len(instanceIDs) == 0 {
				return false, toolcatalog.MissingReasonInteractiveDisconnected, nil
			}
			unavailableReason := strings.TrimSpace(interactive.UnavailableReason)
			if unavailableReason == toolcatalog.MissingReasonInteractiveDisconnected && len(runningInstanceIDs) > 0 {
				return true, "", instanceIDs
			}
			if interactive.Available || unavailableReason == "" {
				return true, "", instanceIDs
			}
			return false, unavailableReason, instanceIDs
		}
		if len(runningInstanceIDs) == 0 {
			return false, toolcatalog.MissingReasonInteractiveDisconnected, nil
		}
		return true, "", runningInstanceIDs
	}

	if computeTool, ok := lookupComputeTool(snapshot.Compute, name); ok {
		return computeTool.Available, strings.TrimSpace(computeTool.UnavailableReason), nil
	}
	if computeReady {
		return false, toolcatalog.MissingReasonComputeCatalogPending, nil
	}
	return false, toolcatalog.MissingReasonComputeUnavailable, nil
}

func lookupComputeTool(tools []compute.ToolDefinition, name string) (compute.ToolDefinition, bool) {
	for _, item := range tools {
		if strings.TrimSpace(item.Name) == name {
			return item, true
		}
	}
	return compute.ToolDefinition{}, false
}

func lookupInteractiveTool(tools []toolrouter.InteractiveToolDefinition, name string) (toolrouter.InteractiveToolDefinition, bool) {
	for _, item := range tools {
		if strings.TrimSpace(item.Name) == name {
			return item, true
		}
	}
	return toolrouter.InteractiveToolDefinition{}, false
}

func isInteractiveCatalogItem(item middlewareclient.ToolCatalogItem) bool {
	return strings.EqualFold(skill.CatalogItemRuntime(item), skill.RuntimeIDQGIS)
}

func collectRunningInstanceIDs(snapshot middlewareclient.RuntimeSnapshot) []string {
	ids := make([]string, 0, len(snapshot.Instances.Items))
	for _, instance := range snapshot.Instances.Items {
		if instance == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(instance.Status), "running") {
			continue
		}
		instanceID := strings.TrimSpace(instance.ID)
		if instanceID == "" {
			continue
		}
		ids = append(ids, instanceID)
	}
	sort.Strings(ids)
	return ids
}
