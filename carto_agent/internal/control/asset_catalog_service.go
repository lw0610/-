package control

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/capability"
	"github.com/autocarto/carto_agent/internal/compute"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/logger"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
	"github.com/autocarto/carto_agent/internal/toolcatalog"
	"github.com/autocarto/carto_agent/internal/toolrouter"
)

// AssetCatalogServiceDeps 表示 AssetCatalogService 的依赖。
type AssetCatalogServiceDeps struct {
	AgentConfigManager   *config.AgentConfigManager
	SkillCatalog         SkillCatalogStore
	RuntimeCoordinator   RuntimeCoordinator
	RuntimeDiagnostics   settings.RuntimeDiagnosticsService
	ToolAggregatorSource ToolAggregatorSource
	SnapshotStore        *capability.SnapshotStore
	SnapshotMode         string
}

type assetCatalogService struct {
	agentConfigManager   *config.AgentConfigManager
	skillCatalog         SkillCatalogStore
	runtimeCoordinator   RuntimeCoordinator
	runtimeDiagnostics   settings.RuntimeDiagnosticsService
	toolAggregatorSource ToolAggregatorSource
	snapshotStore        *capability.SnapshotStore
	snapshotMode         string
}

type toolPackageCatalogMeta struct {
	PackageID string
	Kind      string
	Backend   string
	Source    string
	Domain    string
	Enabled   bool
	Hidden    bool
	System    bool
	Priority  int
}

type scopedSkillCatalog interface {
	ListSkillPackagesByScope(scope, configName string) ([]settings.SkillPackageSummary, error)
	GetSkillPackageByScope(name, scope, configName string) (settings.SkillPackageDetail, error)
}

type toolCatalogPolicy struct {
	Visibility          string
	AuthorizationScopes []string
}

var loadToolCatalogPolicies = defaultLoadToolCatalogPolicies
var loadToolCatalogPoliciesFromDirs = defaultLoadToolCatalogPoliciesFromDirs

// NewAssetCatalogService 创建资产目录服务。
func NewAssetCatalogService(deps AssetCatalogServiceDeps) AssetCatalogService {
	return &assetCatalogService{
		agentConfigManager:   deps.AgentConfigManager,
		skillCatalog:         deps.SkillCatalog,
		runtimeCoordinator:   deps.RuntimeCoordinator,
		runtimeDiagnostics:   deps.RuntimeDiagnostics,
		toolAggregatorSource: deps.ToolAggregatorSource,
		snapshotStore:        deps.SnapshotStore,
		snapshotMode:         strings.TrimSpace(deps.SnapshotMode),
	}
}

func (s *assetCatalogService) SettingsWorkspace() map[string]any {
	return buildSettingsWorkspaceData()
}

func (s *assetCatalogService) ListAgentConfigs(userID string) ([]config.StoredAgentConfig, error) {
	if s.agentConfigManager == nil {
		return nil, newAgentConfigUnavailableError("asset_catalog")
	}
	return s.agentConfigManager.ListConfigs(userID)
}

func (s *assetCatalogService) GetAgentConfig(userID, name string) (*config.StoredAgentConfig, error) {
	configs, err := s.ListAgentConfigs(userID)
	if err != nil {
		return nil, err
	}
	for _, cfg := range configs {
		if strings.TrimSpace(cfg.ConfigName) == strings.TrimSpace(name) {
			copied := cfg
			return &copied, nil
		}
	}
	return nil, errConfigNotFound(name)
}

func (s *assetCatalogService) GetAgentProfile(name string) (settings.AgentProfileView, error) {
	if s.agentConfigManager == nil {
		return settings.AgentProfileView{}, newAgentConfigUnavailableError("asset_catalog")
	}
	data, err := s.agentConfigManager.GetConfigData(name)
	if err != nil {
		return settings.AgentProfileView{}, err
	}
	return settings.BuildAgentProfileView(name, data, s.runtimeDiagnostics), nil
}

// PreviewAgentConfigMigration is optional for asset catalog implementations.
// Persistence still goes through SettingsMutationService's active-run guards.
func (s *assetCatalogService) PreviewAgentConfigMigration(name string) (*config.AgentConfigMigrationResult, error) {
	if s.agentConfigManager == nil {
		return nil, fmt.Errorf("智能体配置管理器未初始化")
	}
	return s.agentConfigManager.PreviewMigration(name)
}

func (s *assetCatalogService) GetAgent(configName, agentName string) (config.AgentDefData, error) {
	if s.agentConfigManager == nil {
		return config.AgentDefData{}, newAgentConfigUnavailableError("asset_catalog")
	}
	agent, err := s.agentConfigManager.GetAgent(configName, agentName)
	if err != nil {
		return config.AgentDefData{}, err
	}
	if agent == nil {
		return config.AgentDefData{}, newAgentNotFoundError()
	}
	return *agent, nil
}

func (s *assetCatalogService) GetToolCatalog(ctx context.Context, scope string) (ToolCatalogView, error) {
	if s.agentConfigManager == nil {
		return ToolCatalogView{}, newAgentConfigUnavailableError("asset_catalog")
	}
	parsedScope := parseToolCatalogScope(scope)
	var (
		tools []string
		err   error
	)
	switch parsedScope {
	case "active", "effective":
		tools, err = s.agentConfigManager.GetActiveToolCatalog()
	default:
		tools, err = s.agentConfigManager.GetGlobalToolCatalog()
	}
	if err != nil {
		return ToolCatalogView{}, err
	}

	snapshot := toolrouter.Snapshot{}
	if s.toolAggregatorSource != nil {
		snapshot = s.toolAggregatorSource.GetToolAggregatorSnapshot()
	}
	if parsedScope == "global" {
		tools = mergeGlobalCatalogTools(tools, snapshot)
	}
	computeReady := s.computeServiceReady(ctx)
	toolInfos := s.projectToolCatalogFromCurrentSource(tools, parsedScope, snapshot, computeReady)
	policies, err := s.loadToolCatalogPolicies(parsedScope)
	if err != nil {
		return ToolCatalogView{}, err
	}
	publicTools, publicToolInfos, publicRuntimeItems := applyPublicToolCatalogPolicy(tools, toolInfos, snapshot, policies)
	s.logToolCatalogShadowDiff(tools, parsedScope, toolInfos)
	availableCount := 0
	for _, item := range publicToolInfos {
		if item.Available {
			availableCount++
		}
	}

	activeConfigName := strings.TrimSpace(s.agentConfigManager.GetActiveConfigName())
	runtimeSnapshot := middlewareclient.RuntimeSnapshot{}
	if s.runtimeCoordinator != nil {
		runtimeSnapshot = s.runtimeCoordinator.Snapshot(ctx)
	}
	toolCatalogStatus := map[string]any{
		"active_config":                 activeConfigName,
		"scope":                         parsedScope,
		"cli_ready":                     computeReady,
		"qgis_tool_count":               len(snapshot.Interactive),
		"cli_tool_count":                len(snapshot.Compute),
		"active_instance_id":            strings.TrimSpace(runtimeSnapshot.Instances.ActiveID),
		"running_instance_count":        runtimeSnapshot.Instances.RunningCount,
		"runtime_tool_catalog_revision": strings.TrimSpace(runtimeSnapshot.ToolCatalogRevision),
		"tool_schema_revision":          strings.TrimSpace(runtimeSnapshot.ToolSchemaRevision),
		"tool_availability_revision":    strings.TrimSpace(runtimeSnapshot.ToolAvailabilityRevision),
		"last_rebuild_reason":           strings.TrimSpace(runtimeSnapshot.LastRebuildReason),
	}
	if !runtimeSnapshot.LastRebuildAt.IsZero() {
		toolCatalogStatus["last_rebuild_at"] = runtimeSnapshot.LastRebuildAt
	}
	if planIssue := strings.TrimSpace(runtimeSnapshot.ModeIssues["plan"].Message); planIssue != "" {
		toolCatalogStatus["planner_ready_reason"] = planIssue
	}
	catalog := toolcatalog.BuildContract(parsedScope, activeConfigName, publicToolInfos, publicTools, runtimeSnapshot.Services)
	catalog = toolcatalog.WithPublicRevisions(
		catalog,
		toolcatalog.ToolSchemaRevision(publicRuntimeItems),
		toolcatalog.ToolAvailabilityRevision(publicRuntimeItems),
	)
	projectedScope := parsedScope
	projectedConfigName := activeConfigName
	if catalog.Policy != nil {
		if scope := strings.TrimSpace(catalog.Policy.Scope); scope != "" {
			projectedScope = scope
		}
		if configName := strings.TrimSpace(catalog.Policy.ConfigName); configName != "" {
			projectedConfigName = configName
		}
	}

	return ToolCatalogView{
		Scope:             projectedScope,
		ConfigName:        projectedConfigName,
		RuntimeRevision:   strings.TrimSpace(runtimeSnapshot.RuntimeRevision),
		Catalog:           catalog,
		AvailableCount:    availableCount,
		ConfiguredCount:   len(publicTools),
		ToolCatalogStatus: toolCatalogStatus,
		ToolInfos:         publicToolInfos,
		Tools:             publicTools,
	}, nil
}

func (s *assetCatalogService) ResolveToolAccessPolicy(_ context.Context, toolName string) (string, []string, bool, error) {
	policies, err := s.loadToolCatalogPolicies("active")
	if err != nil {
		return "", nil, false, err
	}
	trimmedToolName := strings.TrimSpace(toolName)
	policy, ok := policies[trimmedToolName]
	if !ok {
		fallback, resolved := s.resolveToolAccessPolicyFallback(trimmedToolName)
		if !resolved {
			return "", nil, false, nil
		}
		return fallback.Visibility, append([]string(nil), fallback.AuthorizationScopes...), true, nil
	}
	return policy.Visibility, append([]string(nil), policy.AuthorizationScopes...), true, nil
}

func (s *assetCatalogService) logToolCatalogShadowDiff(tools []string, scope string, current []toolcatalog.Item) {
	if s == nil || s.snapshotStore == nil {
		return
	}
	mode := strings.ToLower(strings.TrimSpace(s.snapshotMode))
	if mode != "shadow" {
		return
	}
	snapshotView := s.snapshotStore.Get()
	if snapshotView == nil {
		return
	}
	projectionMode := capability.ToolCatalogConfigured
	if scope == "effective" {
		projectionMode = capability.ToolCatalogEffective
	}
	projected := snapshotView.ProjectToolCatalog(tools, projectionMode)
	diff := summarizeToolCatalogShadowDiff(current, projected)
	raw, _ := json.Marshal(diff)
	if diff.Changed {
		logger.Warn("snapshot_shadow_diff asset_catalog=%s", string(raw))
		return
	}
	logger.Info("snapshot_shadow_diff asset_catalog=%s", string(raw))
}

type toolCatalogShadowDiff struct {
	Changed bool `json:"changed"`
}

func summarizeToolCatalogShadowDiff(current, projected []toolcatalog.Item) toolCatalogShadowDiff {
	if len(current) != len(projected) {
		return toolCatalogShadowDiff{Changed: true}
	}
	currentRaw, currentErr := json.Marshal(current)
	projectedRaw, projectedErr := json.Marshal(projected)
	if currentErr != nil || projectedErr != nil {
		return toolCatalogShadowDiff{Changed: true}
	}
	return toolCatalogShadowDiff{Changed: string(currentRaw) != string(projectedRaw)}
}

func (s *assetCatalogService) projectToolCatalogFromCurrentSource(tools []string, scope string, snapshot toolrouter.Snapshot, computeReady bool) []toolcatalog.Item {
	if projected, ok := s.projectToolCatalogFromSnapshot(tools, scope); ok {
		return projected
	}
	if strings.EqualFold(strings.TrimSpace(s.snapshotMode), "primary") {
		return []toolcatalog.Item{}
	}
	toolInfos := toolcatalog.BuildToolInfosWithHints(tools, snapshot, toolcatalog.AvailabilityHints{
		ComputeReady:      computeReady,
		KnownComputeTools: computeToolNames(snapshot.Compute),
	})
	s.enrichToolCatalogWithPackageMetadata(toolInfos)
	if scope == "effective" {
		availableOnly := make([]toolcatalog.Item, 0, len(toolInfos))
		for _, item := range toolInfos {
			if item.Available {
				availableOnly = append(availableOnly, item)
			}
		}
		return availableOnly
	}
	return toolInfos
}

func (s *assetCatalogService) projectToolCatalogFromSnapshot(tools []string, scope string) ([]toolcatalog.Item, bool) {
	if s == nil || s.snapshotStore == nil {
		return nil, false
	}
	mode := strings.ToLower(strings.TrimSpace(s.snapshotMode))
	if mode != "cutover" && mode != "primary" {
		return nil, false
	}
	snapshot := s.snapshotStore.Get()
	if snapshot == nil {
		return nil, false
	}
	projectionMode := capability.ToolCatalogConfigured
	if scope == "effective" {
		projectionMode = capability.ToolCatalogEffective
	}
	return snapshot.ProjectToolCatalog(tools, projectionMode), true
}

func (s *assetCatalogService) enrichToolCatalogWithPackageMetadata(items []toolcatalog.Item) {
	packageIndex := s.buildToolPackageCatalogIndex()
	if len(packageIndex) == 0 {
		return
	}
	for index := range items {
		meta, ok := packageIndex[strings.TrimSpace(items[index].Name)]
		if !ok {
			continue
		}
		items[index].PackageID = meta.PackageID
		items[index].Kind = meta.Kind
		items[index].Enabled = boolPtr(meta.Enabled)
		if strings.TrimSpace(meta.Domain) != "" {
			items[index].Domain = strings.TrimSpace(meta.Domain)
		}
		if strings.TrimSpace(items[index].Backend) == "" {
			items[index].Backend = meta.Backend
		}
		if strings.TrimSpace(items[index].Source) == "" {
			items[index].Source = strings.TrimSpace(meta.Source)
			if items[index].Source == "" {
				items[index].Source = skill.NormalizeRuntime(meta.Backend)
			}
		}
	}
}

func (s *assetCatalogService) buildToolPackageCatalogIndex() map[string]toolPackageCatalogMeta {
	if s == nil || s.skillCatalog == nil {
		return nil
	}
	packages, err := s.skillCatalog.ListSkillPackages()
	if err != nil || len(packages) == 0 {
		return nil
	}
	index := make(map[string]toolPackageCatalogMeta)
	activeConfigName := ""
	if s.agentConfigManager != nil {
		activeConfigName = s.agentConfigManager.GetActiveConfigName()
	}
	for _, summary := range packages {
		if summary.Kind != skill.SkillPackageKindCategory {
			continue
		}
		packageID := strings.TrimSpace(summary.PackageID)
		if packageID == "" {
			continue
		}
		detail, detailErr := s.skillCatalog.GetSkillPackage(packageID)
		if detailErr != nil {
			continue
		}
		toolRefs := normalizePackageToolRefs(detail.ToolRefs)
		if len(toolRefs) == 0 {
			toolRefs = normalizePackageToolRefs(detail.FrontMatter.Tools)
		}
		if len(toolRefs) == 0 {
			continue
		}
		meta := toolPackageCatalogMeta{
			PackageID: packageID,
			Kind:      strings.TrimSpace(summary.Kind),
			Backend:   strings.TrimSpace(summary.Backend),
			Source:    strings.TrimSpace(summary.Source),
			Enabled:   summary.Enabled,
			Hidden:    summary.Hidden,
			System:    summary.System,
		}
		if summary.Scope == settings.SkillPackageScopeExclusive && summary.OwnerConfig == activeConfigName {
			meta.Priority = 20
		} else if summary.Scope == settings.SkillPackageScopeGlobal {
			meta.Priority = 10
		}
		if meta.System || !meta.Hidden {
			meta.Priority++
		}
		for _, toolName := range toolRefs {
			previous, exists := index[toolName]
			if !exists || meta.Priority > previous.Priority || (meta.Priority == previous.Priority && meta.PackageID < previous.PackageID) {
				index[toolName] = meta
			}
		}
	}
	return index
}

func normalizePackageToolRefs(toolRefs []string) []string {
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
	return normalized
}

func mergeGlobalCatalogTools(configured []string, snapshot toolrouter.Snapshot) []string {
	toolSet := make(map[string]struct{}, len(configured)+len(snapshot.Compute)+len(snapshot.Interactive))
	merged := make([]string, 0, len(toolSet))

	add := func(name string) {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return
		}
		if _, exists := toolSet[trimmed]; exists {
			return
		}
		toolSet[trimmed] = struct{}{}
		merged = append(merged, trimmed)
	}

	for _, name := range configured {
		add(name)
	}
	for _, tool := range snapshot.Compute {
		add(tool.Name)
	}
	for _, tool := range snapshot.Interactive {
		add(tool.Name)
	}

	sort.Strings(merged)
	return merged
}

func defaultLoadToolCatalogPolicies() (map[string]toolCatalogPolicy, error) {
	return defaultLoadToolCatalogPoliciesFromDirs(config.ResolveBuiltinToolsDir(), config.ResolveUserToolsDir())
}

func defaultLoadToolCatalogPoliciesFromDirs(dirs ...string) (map[string]toolCatalogPolicy, error) {
	definitions, err := toolpkg.LoadAuthorityDefinitions(dirs...)
	if err != nil {
		return nil, err
	}
	policyIndex := toolpkg.BuildAccessPolicyIndex(definitions)
	policies := make(map[string]toolCatalogPolicy, len(policyIndex))
	for name, policy := range policyIndex {
		policies[strings.TrimSpace(name)] = toolCatalogPolicy{
			Visibility:          strings.TrimSpace(policy.Visibility),
			AuthorizationScopes: append([]string(nil), policy.AuthorizationScopes...),
		}
	}
	return policies, nil
}

func (s *assetCatalogService) loadToolCatalogPolicies(scope string) (map[string]toolCatalogPolicy, error) {
	dirs := existingToolPolicyDirs(s.resolveToolPolicyDirs(scope))
	if len(dirs) == 0 {
		return loadToolCatalogPolicies()
	}
	return loadToolCatalogPoliciesFromDirs(dirs...)
}

func (s *assetCatalogService) resolveToolPolicyDirs(scope string) []string {
	normalizedScope := parseToolCatalogScope(scope)
	dirs := []string{config.ResolveBuiltinToolsDir(), config.ResolveUserToolsDir()}
	if s == nil || s.agentConfigManager == nil {
		return normalizeToolPolicyDirs(dirs...)
	}
	configBaseDir := strings.TrimSpace(s.agentConfigManager.ConfigBaseDir())
	if configBaseDir == "" {
		return normalizeToolPolicyDirs(dirs...)
	}
	dirs = append(dirs, config.ResolveGlobalToolsDir(configBaseDir))
	if normalizedScope != "global" {
		activeConfigName := strings.TrimSpace(s.agentConfigManager.GetActiveConfigName())
		dirs = append(dirs, config.ResolveExclusiveToolsDir(configBaseDir, activeConfigName))
	}
	return normalizeToolPolicyDirs(dirs...)
}

func normalizeToolPolicyDirs(paths ...string) []string {
	seen := make(map[string]struct{}, len(paths))
	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func existingToolPolicyDirs(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	existing := make([]string, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		existing = append(existing, path)
	}
	return existing
}

func applyPublicToolCatalogPolicy(
	tools []string,
	items []toolcatalog.Item,
	snapshot toolrouter.Snapshot,
	policies map[string]toolCatalogPolicy,
) ([]string, []toolcatalog.Item, []middlewareclient.ToolCatalogItem) {
	runtimeIndex := buildRuntimeToolCatalogIndex(snapshot)
	publicInfos := make([]toolcatalog.Item, 0, len(items))
	publicRuntimeItems := make([]middlewareclient.ToolCatalogItem, 0, len(items))
	publicSet := make(map[string]struct{}, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		policy, known := policies[name]
		if !known {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(policy.Visibility), "public") {
			continue
		}
		publicInfos = append(publicInfos, item)
		publicSet[name] = struct{}{}
		publicRuntimeItems = append(publicRuntimeItems, buildPublicRuntimeCatalogItem(item, runtimeIndex[name], policy, known))
	}
	publicTools := make([]string, 0, len(tools))
	for _, toolName := range tools {
		trimmed := strings.TrimSpace(toolName)
		if _, ok := publicSet[trimmed]; ok {
			publicTools = append(publicTools, trimmed)
		}
	}
	return publicTools, publicInfos, toolcatalog.PublicCatalogItems(publicRuntimeItems)
}

func buildRuntimeToolCatalogIndex(snapshot toolrouter.Snapshot) map[string]middlewareclient.ToolCatalogItem {
	index := make(map[string]middlewareclient.ToolCatalogItem, len(snapshot.Compute)+len(snapshot.Interactive))
	for _, item := range snapshot.Compute {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		index[name] = middlewareclient.ToolCatalogItem{
			Name:        name,
			Description: strings.TrimSpace(item.Description),
			Category:    strings.TrimSpace(item.Category),
			Domain:      strings.TrimSpace(item.Category),
			Source:      skill.RuntimeIDCLI,
			Backend:     firstNonEmpty(strings.TrimSpace(item.Backend), skill.RuntimeIDCLI),
			InputSchema: cloneSchemaMap(item.InputSchema),
		}
	}
	for _, item := range snapshot.Interactive {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		index[name] = middlewareclient.ToolCatalogItem{
			Name:        name,
			Description: strings.TrimSpace(item.Description),
			Category:    strings.TrimSpace(item.Category),
			Domain:      strings.TrimSpace(item.Category),
			Source:      skill.RuntimeIDQGIS,
			Backend:     skill.RuntimeIDQGIS,
			InputSchema: cloneSchemaMap(item.InputSchema),
		}
	}
	return index
}

func inferToolCatalogPolicy(item toolcatalog.Item, runtimeItem middlewareclient.ToolCatalogItem) (toolCatalogPolicy, bool) {
	runtimeID := skill.NormalizeRuntime(firstNonEmpty(
		strings.TrimSpace(item.Backend),
		strings.TrimSpace(item.Source),
		strings.TrimSpace(runtimeItem.Backend),
		strings.TrimSpace(runtimeItem.Source),
	))
	if runtimeID == "" {
		return toolCatalogPolicy{}, false
	}
	if runtimeID == skill.RuntimeIDInternal {
		return toolCatalogPolicy{Visibility: toolpkg.VisibilityInternal}, true
	}
	return toolCatalogPolicy{
		Visibility:          toolpkg.VisibilityPublic,
		AuthorizationScopes: []string{"tool.invoke"},
	}, true
}

func (s *assetCatalogService) resolveToolAccessPolicyFallback(toolName string) (toolCatalogPolicy, bool) {
	if s == nil || s.toolAggregatorSource == nil {
		return toolCatalogPolicy{}, false
	}
	snapshot := s.toolAggregatorSource.GetToolAggregatorSnapshot()
	for _, item := range snapshot.Interactive {
		if strings.TrimSpace(item.Name) != toolName {
			continue
		}
		return inferToolCatalogPolicy(toolcatalog.Item{
			Name:    toolName,
			Backend: skill.RuntimeIDQGIS,
			Source:  skill.RuntimeIDQGIS,
		}, middlewareclient.ToolCatalogItem{Name: toolName, Backend: skill.RuntimeIDQGIS, Source: skill.RuntimeIDQGIS})
	}
	for _, item := range snapshot.Compute {
		if strings.TrimSpace(item.Name) != toolName {
			continue
		}
		return inferToolCatalogPolicy(toolcatalog.Item{
			Name:    toolName,
			Backend: strings.TrimSpace(item.Backend),
			Source:  skill.RuntimeIDCLI,
		}, middlewareclient.ToolCatalogItem{Name: toolName, Backend: strings.TrimSpace(item.Backend), Source: skill.RuntimeIDCLI})
	}
	return toolCatalogPolicy{}, false
}

func buildPublicRuntimeCatalogItem(item toolcatalog.Item, runtime middlewareclient.ToolCatalogItem, policy toolCatalogPolicy, known bool) middlewareclient.ToolCatalogItem {
	runtime.Name = strings.TrimSpace(item.Name)
	runtime.DisplayName = firstNonEmpty(strings.TrimSpace(item.DisplayName), strings.TrimSpace(runtime.DisplayName))
	if len(item.Keywords) > 0 {
		runtime.Keywords = append([]string(nil), item.Keywords...)
	}
	runtime.Description = firstNonEmpty(strings.TrimSpace(item.Description), strings.TrimSpace(runtime.Description))
	runtime.Category = firstNonEmpty(strings.TrimSpace(item.Category), strings.TrimSpace(runtime.Category))
	runtime.Domain = firstNonEmpty(strings.TrimSpace(item.Domain), strings.TrimSpace(runtime.Domain), strings.TrimSpace(runtime.Category))
	runtime.Source = firstNonEmpty(strings.TrimSpace(item.Source), strings.TrimSpace(runtime.Source))
	runtime.Backend = firstNonEmpty(strings.TrimSpace(item.Backend), strings.TrimSpace(runtime.Backend), skill.NormalizeRuntime(runtime.Source))
	runtime.Available = item.Available
	runtime.UnavailableReason = strings.TrimSpace(item.MissingReason)
	runtime.InstanceIDs = append([]string(nil), item.InstanceIDs...)
	if known {
		runtime.Visibility = strings.TrimSpace(policy.Visibility)
		runtime.AuthorizationScopes = append([]string(nil), policy.AuthorizationScopes...)
	}
	return runtime
}

func cloneSchemaMap(schema map[string]interface{}) map[string]any {
	if len(schema) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(schema))
	for key, value := range schema {
		cloned[key] = value
	}
	return cloned
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func boolPtr(value bool) *bool {
	ptr := new(bool)
	*ptr = value
	return ptr
}

func (s *assetCatalogService) ListSkills() ([]map[string]any, error) {
	if s.skillCatalog == nil {
		return nil, newSkillCatalogUnavailableError("asset_catalog")
	}
	return s.skillCatalog.ListSkills()
}

func (s *assetCatalogService) ListSkillPackages() ([]settings.SkillPackageSummary, error) {
	if s.skillCatalog == nil {
		return nil, newSkillCatalogUnavailableError("asset_catalog")
	}
	return s.skillCatalog.ListSkillPackages()
}

func (s *assetCatalogService) ListSkillPackagesByScope(scope, configName string) ([]settings.SkillPackageSummary, error) {
	if s.skillCatalog == nil {
		return nil, newSkillCatalogUnavailableError("asset_catalog")
	}
	if scopedCatalog, ok := s.skillCatalog.(scopedSkillCatalog); ok {
		return scopedCatalog.ListSkillPackagesByScope(scope, configName)
	}
	return s.skillCatalog.ListSkillPackages()
}

func (s *assetCatalogService) GetSkillPackage(name string) (settings.SkillPackageDetail, error) {
	return s.GetSkillPackageByScope(name, "", "")
}

func (s *assetCatalogService) GetSkillPackageByScope(name, scope, configName string) (settings.SkillPackageDetail, error) {
	if s.skillCatalog == nil {
		return settings.SkillPackageDetail{}, newSkillCatalogUnavailableError("asset_catalog")
	}
	if scopedCatalog, ok := s.skillCatalog.(scopedSkillCatalog); ok {
		return scopedCatalog.GetSkillPackageByScope(name, scope, configName)
	}
	return s.skillCatalog.GetSkillPackage(name)
}

func (s *assetCatalogService) GetSkillPackageReferences(name string) []config.SkillReference {
	if s.skillCatalog == nil {
		return []config.SkillReference{}
	}
	return s.skillCatalog.GetSkillPackageReferences(name)
}

func (s *assetCatalogService) GetSkillContent(name string) (map[string]any, error) {
	if s.skillCatalog == nil {
		return nil, newSkillCatalogUnavailableError("asset_catalog")
	}
	return s.skillCatalog.GetSkillContent(name)
}

func (s *assetCatalogService) GetSkillReferences(name string) []config.SkillReference {
	if s.skillCatalog != nil {
		return s.skillCatalog.GetSkillPackageReferences(name)
	}
	if s.agentConfigManager == nil {
		return []config.SkillReference{}
	}
	return s.agentConfigManager.FindSkillReferences(name)
}

func (s *assetCatalogService) computeServiceReady(ctx context.Context) bool {
	if s.runtimeCoordinator == nil {
		return false
	}
	runtimeSnapshot := s.runtimeCoordinator.Snapshot(ctx)
	computeStatus, ok := runtimeSnapshot.Services[skill.RuntimeIDCLI]
	if !ok {
		computeStatus, ok = runtimeSnapshot.Services["compute"]
	}
	if !ok || !computeStatus.Enabled {
		return false
	}
	if computeStatus.Ready {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(computeStatus.Status)) {
	case "running", "ok", "starting", "restarting":
		return true
	default:
		return false
	}
}

func parseToolCatalogScope(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "active":
		return "active"
	case "effective":
		return "effective"
	default:
		return "global"
	}
}

func computeToolNames(items []compute.ToolDefinition) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

func errConfigNotFound(name string) error {
	return newConfigNotFoundError(name)
}
