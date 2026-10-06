package toolcatalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/autocarto/carto_agent/internal/toolrouter"
)

const (
	SourceCompute     = skill.RuntimeIDCLI
	SourceInteractive = skill.RuntimeIDQGIS

	MissingReasonComputeUnavailable      = "compute_unavailable"
	MissingReasonComputeCatalogPending   = "compute_catalog_pending"
	MissingReasonInteractiveDisconnected = "interactive_instance_disconnected"
	MissingReasonRuntimeCatalogMissing   = "runtime_catalog_missing"
	MissingReasonProviderMissing         = "provider_missing"
	MissingReasonUnsupportedRuntime      = "unsupported_runtime"
	MissingReasonAbnormalEmptyCatalog    = "abnormal_empty"

	ContractVersion = "1"

	CapabilityTypeToolCatalog = "tool_catalog"
	CapabilityTypeTool        = "tool"

	AvailabilityAvailable   = "available"
	AvailabilityValidEmpty  = "valid_empty"
	AvailabilityDegraded    = "degraded"
	AvailabilityUnavailable = "unavailable"
	AvailabilityUnsupported = "unsupported"

	DegradationStatusNone               = "none"
	DegradationStatusMissingDependency  = "missing_dependency"
	DegradationStatusProviderMissing    = "provider_missing"
	DegradationStatusUnsupportedRuntime = "unsupported_runtime"
	DegradationStatusAbnormalEmpty      = "abnormal_empty"

	contractSourceControlPlane = "go_control_plane"
	contractRuntimeTargetMixed = "mixed"
)

// Item 工具目录项，包含可用性状态。
type Item struct {
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Category      string   `json:"category,omitempty"`
	Domain        string   `json:"domain,omitempty"`
	Source        string   `json:"source,omitempty"`
	Backend       string   `json:"backend,omitempty"`
	PackageID     string   `json:"package_id,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	Enabled       *bool    `json:"enabled,omitempty"`
	Available     bool     `json:"available"`
	MissingReason string   `json:"missing_reason,omitempty"`
	InstanceIDs   []string `json:"instance_ids,omitempty"`
}

type Policy struct {
	Scope      string `json:"scope,omitempty"`
	ConfigName string `json:"config_name,omitempty"`
	Enabled    *bool  `json:"enabled,omitempty"`
}

type Capability struct {
	ID                      string   `json:"id"`
	Source                  string   `json:"source,omitempty"`
	CapabilityType          string   `json:"capability_type"`
	RuntimeTarget           string   `json:"runtime_target,omitempty"`
	Availability            string   `json:"availability"`
	MissingDependencyReason string   `json:"missing_dependency_reason,omitempty"`
	Policy                  *Policy  `json:"policy,omitempty"`
	DegradationStatus       string   `json:"degradation_status,omitempty"`
	InstanceIDs             []string `json:"instance_ids,omitempty"`
}

type Contract struct {
	ID                      string       `json:"id"`
	Version                 string       `json:"version"`
	Source                  string       `json:"source"`
	CapabilityType          string       `json:"capability_type"`
	RuntimeTarget           string       `json:"runtime_target"`
	Availability            string       `json:"availability"`
	MissingDependencyReason string       `json:"missing_dependency_reason,omitempty"`
	Policy                  *Policy      `json:"policy,omitempty"`
	DegradationStatus       string       `json:"degradation_status"`
	SchemaRevision          string       `json:"schema_revision,omitempty"`
	AvailabilityRevision    string       `json:"availability_revision,omitempty"`
	PolicyRevision          string       `json:"policy_revision,omitempty"`
	DegradedRevision        string       `json:"degraded_revision,omitempty"`
	Items                   []Capability `json:"items"`
}

// ToolDiffResult 表示工具目录结构化差异。
type ToolDiffResult struct {
	Changed             bool     `json:"changed"`
	SchemaChanged       bool     `json:"schema_changed"`
	AvailabilityChanged bool     `json:"availability_changed"`
	Added               []string `json:"added"`
	Removed             []string `json:"removed"`
	Modified            []string `json:"modified"`
	AvailabilityOnly    []string `json:"availability_only"`
}

type metadata struct {
	description       string
	category          string
	domain            string
	source            string
	available         bool
	unavailableReason string
	instanceIDs       []string
}

type AvailabilityHints struct {
	ComputeReady      bool
	KnownComputeTools []string
}

func BuildContract(scope, configName string, items []Item, configuredTools []string, services map[string]middlewareclient.ServiceStatus) Contract {
	normalizedScope := strings.TrimSpace(scope)
	if normalizedScope == "" {
		normalizedScope = "global"
	}
	contract := Contract{
		ID:                "tool_catalog/" + normalizedScope,
		Version:           ContractVersion,
		Source:            contractSourceControlPlane,
		CapabilityType:    CapabilityTypeToolCatalog,
		RuntimeTarget:     contractRuntimeTargetMixed,
		Availability:      AvailabilityAvailable,
		DegradationStatus: DegradationStatusNone,
		Policy: &Policy{
			Scope:      normalizedScope,
			ConfigName: strings.TrimSpace(configName),
		},
		Items: make([]Capability, 0, len(items)),
	}

	hasUnavailable := false
	hasProviderMissing := false
	hasUnsupportedRuntime := false
	firstMissingReason := ""
	runtimeTargets := make(map[string]struct{})
	for _, item := range items {
		capability := buildCapability(item, normalizedScope, configName, services)
		contract.Items = append(contract.Items, capability)
		if capability.RuntimeTarget != "" {
			runtimeTargets[capability.RuntimeTarget] = struct{}{}
		}
		switch capability.DegradationStatus {
		case DegradationStatusProviderMissing:
			hasUnavailable = true
			hasProviderMissing = true
		case DegradationStatusUnsupportedRuntime:
			hasUnavailable = true
			hasUnsupportedRuntime = true
		case DegradationStatusMissingDependency:
			hasUnavailable = true
		}
		if firstMissingReason == "" {
			firstMissingReason = strings.TrimSpace(capability.MissingDependencyReason)
		}
	}
	contract.RuntimeTarget = summarizeRuntimeTargets(runtimeTargets)

	switch {
	case len(contract.Items) == 0 && len(configuredTools) == 0:
		contract.Availability = AvailabilityValidEmpty
	case len(contract.Items) == 0 && len(configuredTools) > 0:
		contract.Availability = AvailabilityUnavailable
		contract.MissingDependencyReason = MissingReasonAbnormalEmptyCatalog
		contract.DegradationStatus = DegradationStatusAbnormalEmpty
	case hasUnsupportedRuntime:
		contract.Availability = AvailabilityUnsupported
		contract.MissingDependencyReason = MissingReasonUnsupportedRuntime
		contract.DegradationStatus = DegradationStatusUnsupportedRuntime
	case hasProviderMissing:
		contract.Availability = AvailabilityDegraded
		contract.MissingDependencyReason = MissingReasonProviderMissing
		contract.DegradationStatus = DegradationStatusProviderMissing
	case hasUnavailable:
		contract.Availability = AvailabilityDegraded
		contract.MissingDependencyReason = firstNonEmpty(firstMissingReason, MissingReasonRuntimeCatalogMissing)
		contract.DegradationStatus = DegradationStatusMissingDependency
	}
	contract.PolicyRevision = contractPolicyRevision(contract)
	contract.DegradedRevision = contractDegradedRevision(contract)

	return contract
}

func WithPublicRevisions(contract Contract, schemaRevision, availabilityRevision string) Contract {
	contract.SchemaRevision = strings.TrimSpace(schemaRevision)
	contract.AvailabilityRevision = strings.TrimSpace(availabilityRevision)
	if strings.TrimSpace(contract.PolicyRevision) == "" {
		contract.PolicyRevision = contractPolicyRevision(contract)
	}
	if strings.TrimSpace(contract.DegradedRevision) == "" {
		contract.DegradedRevision = contractDegradedRevision(contract)
	}
	return contract
}

// BuildToolInfos 构建工具目录与运行时可用性信息。
func BuildToolInfos(tools []string, snapshot toolrouter.Snapshot) []Item {
	return BuildToolInfosWithHints(tools, snapshot, AvailabilityHints{})
}

// BuildToolInfosWithHints 构建带运行时提示的工具目录。
func BuildToolInfosWithHints(tools []string, snapshot toolrouter.Snapshot, hints AvailabilityHints) []Item {
	metaMap := buildMetadataMap(snapshot)
	classifier := toolrouter.NewDefaultClassificationRegistry()

	items := make([]Item, 0, len(tools))
	for _, rawName := range tools {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}

		if meta, ok := metaMap[name]; ok {
			missingReason := ""
			if !meta.available {
				missingReason = strings.TrimSpace(meta.unavailableReason)
				if missingReason == "" {
					missingReason = inferMissingReason(meta.source, hints)
				}
			}
			items = append(items, Item{
				Name:          name,
				Description:   meta.description,
				Category:      meta.category,
				Domain:        firstNonEmpty(meta.domain, meta.category),
				Source:        meta.source,
				Available:     meta.available,
				MissingReason: missingReason,
				InstanceIDs:   append([]string(nil), meta.instanceIDs...),
			})
			continue
		}

		source := inferUnavailableSource(name, classifier, hints)
		items = append(items, Item{
			Name:          name,
			Source:        source,
			Available:     false,
			MissingReason: MissingReasonRuntimeCatalogMissing,
		})
	}

	return items
}

func buildCapability(item Item, scope, configName string, services map[string]middlewareclient.ServiceStatus) Capability {
	runtimeTarget := capabilityRuntimeTarget(item)
	capability := Capability{
		ID:             strings.TrimSpace(item.Name),
		Source:         firstNonEmpty(strings.TrimSpace(item.PackageID), strings.TrimSpace(item.Source), strings.TrimSpace(item.Backend), contractSourceControlPlane),
		CapabilityType: CapabilityTypeTool,
		RuntimeTarget:  runtimeTarget,
		Availability:   AvailabilityAvailable,
		Policy: &Policy{
			Scope:      strings.TrimSpace(scope),
			ConfigName: strings.TrimSpace(configName),
			Enabled:    item.Enabled,
		},
		DegradationStatus: DegradationStatusNone,
		InstanceIDs:       append([]string(nil), item.InstanceIDs...),
	}

	switch {
	case unsupportedRuntimeTarget(runtimeTarget):
		capability.Availability = AvailabilityUnsupported
		capability.MissingDependencyReason = MissingReasonUnsupportedRuntime
		capability.DegradationStatus = DegradationStatusUnsupportedRuntime
	case item.Available:
		capability.Availability = AvailabilityAvailable
	case providerMissingForTarget(runtimeTarget, services):
		capability.Availability = AvailabilityUnavailable
		capability.MissingDependencyReason = MissingReasonProviderMissing
		capability.DegradationStatus = DegradationStatusProviderMissing
	default:
		capability.Availability = AvailabilityUnavailable
		capability.MissingDependencyReason = firstNonEmpty(strings.TrimSpace(item.MissingReason), MissingReasonRuntimeCatalogMissing)
		capability.DegradationStatus = DegradationStatusMissingDependency
	}

	return capability
}

func capabilityRuntimeTarget(item Item) string {
	return firstNonEmpty(strings.TrimSpace(item.Backend), normalizeCatalogRuntime(strings.TrimSpace(item.Source)))
}

func normalizeCatalogRuntime(raw string) string {
	normalized := skill.NormalizeRuntime(raw)
	if normalized != "" {
		return normalized
	}
	return strings.ToLower(strings.TrimSpace(raw))
}

func unsupportedRuntimeTarget(runtimeTarget string) bool {
	switch normalizeCatalogRuntime(runtimeTarget) {
	case "", skill.RuntimeIDCLI, skill.RuntimeIDQGIS, skill.RuntimeIDInternal:
		return false
	default:
		return true
	}
}

func providerMissingForTarget(runtimeTarget string, services map[string]middlewareclient.ServiceStatus) bool {
	if len(services) == 0 {
		return false
	}
	serviceKeyCandidates := []string{normalizeCatalogRuntime(runtimeTarget)}
	switch normalizeCatalogRuntime(runtimeTarget) {
	case skill.RuntimeIDCLI:
		serviceKeyCandidates = append(serviceKeyCandidates, "compute")
	case skill.RuntimeIDQGIS:
		serviceKeyCandidates = append(serviceKeyCandidates, "bridge")
	}
	for _, key := range serviceKeyCandidates {
		service, ok := services[key]
		if !ok {
			continue
		}
		if service.Enabled && service.ProviderCount == 0 {
			return true
		}
	}
	return false
}

func summarizeRuntimeTargets(targets map[string]struct{}) string {
	if len(targets) == 1 {
		for target := range targets {
			return target
		}
	}
	return contractRuntimeTargetMixed
}

// DiffTools 比较两份工具目录定义，识别 schema 与 availability 差异。
func DiffTools(oldTools, newTools []middlewareclient.ToolCatalogItem) ToolDiffResult {
	oldSchema := make(map[string]string, len(oldTools))
	newSchema := make(map[string]string, len(newTools))
	oldAvailability := make(map[string]string, len(oldTools))
	newAvailability := make(map[string]string, len(newTools))

	for _, item := range oldTools {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		oldSchema[name] = toolSchemaSignature(item)
		oldAvailability[name] = toolAvailabilitySignature(item)
	}
	for _, item := range newTools {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		newSchema[name] = toolSchemaSignature(item)
		newAvailability[name] = toolAvailabilitySignature(item)
	}

	result := ToolDiffResult{}
	for name, newSchemaSig := range newSchema {
		oldSchemaSig, exists := oldSchema[name]
		if !exists {
			result.Added = append(result.Added, name)
			result.SchemaChanged = true
			continue
		}
		if oldSchemaSig != newSchemaSig {
			result.Modified = append(result.Modified, name)
			result.SchemaChanged = true
			continue
		}
		if oldAvailability[name] != newAvailability[name] {
			result.AvailabilityOnly = append(result.AvailabilityOnly, name)
			result.AvailabilityChanged = true
		}
	}
	for name := range oldSchema {
		if _, exists := newSchema[name]; !exists {
			result.Removed = append(result.Removed, name)
			result.SchemaChanged = true
		}
	}

	sort.Strings(result.Added)
	sort.Strings(result.Removed)
	sort.Strings(result.Modified)
	sort.Strings(result.AvailabilityOnly)
	result.Changed = result.SchemaChanged || result.AvailabilityChanged
	return result
}

// ToolSchemaRevision 计算工具 schema 级修订号。
func ToolSchemaRevision(tools []middlewareclient.ToolCatalogItem) string {
	return toolRevision(tools, toolSchemaSignature)
}

// ToolAvailabilityRevision 计算工具 availability 级修订号。
func ToolAvailabilityRevision(tools []middlewareclient.ToolCatalogItem) string {
	return toolRevision(tools, toolAvailabilitySignature)
}

// PublicCatalogItems 返回可公开暴露的工具目录项。
func PublicCatalogItems(tools []middlewareclient.ToolCatalogItem) []middlewareclient.ToolCatalogItem {
	if len(tools) == 0 {
		return []middlewareclient.ToolCatalogItem{}
	}
	filtered := make([]middlewareclient.ToolCatalogItem, 0, len(tools))
	for _, item := range tools {
		switch strings.ToLower(strings.TrimSpace(item.Visibility)) {
		case "public":
			cloned := item
			cloned.AuthorizationScopes = normalizedAuthorizationScopes(item.AuthorizationScopes)
			filtered = append(filtered, cloned)
		}
	}
	return filtered
}

func buildMetadataMap(snapshot toolrouter.Snapshot) map[string]metadata {
	metaMap := make(map[string]metadata, len(snapshot.Compute)+len(snapshot.Interactive))

	for _, item := range snapshot.Compute {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		if _, exists := metaMap[name]; exists {
			continue
		}
		metaMap[name] = metadata{
			description:       strings.TrimSpace(item.Description),
			category:          strings.TrimSpace(item.Category),
			domain:            strings.TrimSpace(item.Category),
			source:            SourceCompute,
			available:         runtimeToolAvailable(item.Available, item.UnavailableReason),
			unavailableReason: strings.TrimSpace(item.UnavailableReason),
		}
	}

	// 交互工具优先级更高，允许覆盖同名 compute。
	for _, item := range snapshot.Interactive {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		metaMap[name] = metadata{
			description:       strings.TrimSpace(item.Description),
			category:          strings.TrimSpace(item.Category),
			domain:            strings.TrimSpace(item.Category),
			source:            SourceInteractive,
			available:         runtimeToolAvailable(item.Available, item.UnavailableReason),
			unavailableReason: strings.TrimSpace(item.UnavailableReason),
			instanceIDs:       append([]string(nil), item.InstanceIDs...),
		}
	}

	return metaMap
}

func runtimeToolAvailable(available bool, unavailableReason string) bool {
	if available {
		return true
	}
	return strings.TrimSpace(unavailableReason) == ""
}

func inferUnavailableSource(name string, classifier *toolrouter.ClassificationRegistry, hints AvailabilityHints) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return ""
	}

	if source, ok := classifyUnavailableSource(normalized, classifier, hints); ok {
		return source
	}

	return ""
}

func classifyUnavailableSource(name string, classifier *toolrouter.ClassificationRegistry, hints AvailabilityHints) (string, bool) {
	if name == "" {
		return "", false
	}
	if knownComputeTool(name, hints.KnownComputeTools) {
		return SourceCompute, true
	}
	if classifier != nil && classifier.IsComputeOverride(name) {
		return SourceCompute, true
	}
	if classifier != nil && classifier.IsInteractive(name) {
		return SourceInteractive, true
	}
	return "", false
}

func knownComputeTool(name string, tools []string) bool {
	if name == "" || len(tools) == 0 {
		return false
	}
	for _, raw := range tools {
		if strings.EqualFold(strings.TrimSpace(raw), name) {
			return true
		}
	}
	return false
}

func inferMissingReason(source string, hints AvailabilityHints) string {
	if strings.EqualFold(strings.TrimSpace(source), SourceInteractive) {
		return MissingReasonInteractiveDisconnected
	}
	if hints.ComputeReady {
		return MissingReasonComputeCatalogPending
	}
	return MissingReasonComputeUnavailable
}

func toolRevision(
	tools []middlewareclient.ToolCatalogItem,
	signature func(middlewareclient.ToolCatalogItem) string,
) string {
	if len(tools) == 0 {
		return ""
	}
	entries := make([]string, 0, len(tools))
	for _, item := range tools {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		entries = append(entries, name+"="+signature(item))
	}
	if len(entries) == 0 {
		return ""
	}
	sort.Strings(entries)
	sum := sha256.Sum256([]byte(strings.Join(entries, "\n")))
	return hex.EncodeToString(sum[:8])
}

func toolSchemaSignature(item middlewareclient.ToolCatalogItem) string {
	rawSchema, _ := json.Marshal(item.InputSchema)
	return strings.Join([]string{
		strings.TrimSpace(item.Name),
		canonicalToolRuntimeIdentity(item),
		strings.TrimSpace(item.RawName),
		strings.TrimSpace(item.Description),
		firstNonEmpty(strings.TrimSpace(item.Domain), strings.TrimSpace(item.Category)),
		strings.TrimSpace(item.Category),
		strings.ToLower(strings.TrimSpace(item.Visibility)),
		strings.Join(normalizedAuthorizationScopes(item.AuthorizationScopes), ","),
		string(rawSchema),
	}, "|")
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

func canonicalToolRuntimeIdentity(item middlewareclient.ToolCatalogItem) string {
	if runtimeID := skill.CatalogItemRuntime(item); runtimeID != "" {
		return runtimeID
	}
	parts := []string{
		strings.TrimSpace(item.ProviderID),
		strings.TrimSpace(item.Source),
		strings.TrimSpace(item.Backend),
	}
	return strings.Join(parts, "|")
}

func toolAvailabilitySignature(item middlewareclient.ToolCatalogItem) string {
	instanceIDs := append([]string(nil), item.InstanceIDs...)
	sort.Strings(instanceIDs)
	rawInstanceIDs, _ := json.Marshal(instanceIDs)
	available := "false"
	if item.Available {
		available = "true"
	}
	return strings.Join([]string{
		strings.TrimSpace(item.Name),
		available,
		strings.TrimSpace(item.UnavailableReason),
		string(rawInstanceIDs),
	}, "|")
}

func normalizedAuthorizationScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		trimmed := strings.TrimSpace(scope)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	sort.Strings(normalized)
	return normalized
}

func contractPolicyRevision(contract Contract) string {
	entries := []string{contractPolicySignature(contract.Policy)}
	for _, item := range contract.Items {
		entries = append(entries, strings.Join([]string{
			strings.TrimSpace(item.ID),
			contractPolicySignature(item.Policy),
		}, "|"))
	}
	return revisionFromEntries(entries)
}

func contractDegradedRevision(contract Contract) string {
	entries := []string{strings.Join([]string{
		strings.TrimSpace(contract.Availability),
		strings.TrimSpace(contract.MissingDependencyReason),
		strings.TrimSpace(contract.DegradationStatus),
	}, "|")}
	for _, item := range contract.Items {
		instanceIDs := append([]string(nil), item.InstanceIDs...)
		sort.Strings(instanceIDs)
		rawInstanceIDs, _ := json.Marshal(instanceIDs)
		entries = append(entries, strings.Join([]string{
			strings.TrimSpace(item.ID),
			strings.TrimSpace(item.RuntimeTarget),
			strings.TrimSpace(item.Availability),
			strings.TrimSpace(item.MissingDependencyReason),
			strings.TrimSpace(item.DegradationStatus),
			string(rawInstanceIDs),
		}, "|"))
	}
	return revisionFromEntries(entries)
}

func contractPolicySignature(policy *Policy) string {
	if policy == nil {
		return ""
	}
	enabled := ""
	if policy.Enabled != nil {
		if *policy.Enabled {
			enabled = "true"
		} else {
			enabled = "false"
		}
	}
	return strings.Join([]string{
		strings.TrimSpace(policy.Scope),
		strings.TrimSpace(policy.ConfigName),
		enabled,
	}, "|")
}

func revisionFromEntries(entries []string) string {
	filtered := make([]string, 0, len(entries))
	for _, entry := range entries {
		filtered = append(filtered, strings.TrimSpace(entry))
	}
	if len(filtered) == 0 {
		return ""
	}
	sort.Strings(filtered)
	sum := sha256.Sum256([]byte(strings.Join(filtered, "\n")))
	return hex.EncodeToString(sum[:8])
}
