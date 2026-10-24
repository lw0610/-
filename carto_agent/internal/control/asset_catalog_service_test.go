package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/capability"
	"github.com/autocarto/carto_agent/internal/compute"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/autocarto/carto_agent/internal/toolcatalog"
	"github.com/autocarto/carto_agent/internal/toolrouter"
)

type stubRuntimeCoordinator struct {
	snapshot middlewareclient.RuntimeSnapshot
}

type stubToolAggregatorSource struct {
	snapshot toolrouter.Snapshot
}

type stubAssetCatalogSkillStore struct {
	packages []settings.SkillPackageSummary
	details  map[string]settings.SkillPackageDetail
}

func TestToolPackageCatalogIndexPrefersFormalCategoryOwnership(t *testing.T) {
	store := stubAssetCatalogSkillStore{packages: []settings.SkillPackageSummary{
		{PackageID: "formal", Kind: "category", Scope: settings.SkillPackageScopeGlobal},
		{PackageID: "legacy", Kind: "category", Hidden: true, Scope: settings.SkillPackageScopeGlobal},
		{PackageID: "a-method", Kind: "workflow", Scope: settings.SkillPackageScopeGlobal},
	}, details: map[string]settings.SkillPackageDetail{}}
	for _, pkg := range store.packages {
		store.details[pkg.PackageID] = settings.SkillPackageDetail{SkillPackageSummary: pkg, ToolRefs: []string{"sample"}}
	}
	service := &assetCatalogService{skillCatalog: store}
	if got := service.buildToolPackageCatalogIndex()["sample"].PackageID; got != "formal" {
		t.Fatalf("expected visible category ownership, got %q", got)
	}
}

func (s stubAssetCatalogSkillStore) ListSkillPackages() ([]settings.SkillPackageSummary, error) {
	return append([]settings.SkillPackageSummary(nil), s.packages...), nil
}

func (s stubAssetCatalogSkillStore) GetSkillPackage(name string) (settings.SkillPackageDetail, error) {
	if detail, ok := s.details[name]; ok {
		return detail, nil
	}
	return settings.SkillPackageDetail{}, os.ErrNotExist
}

func (s stubAssetCatalogSkillStore) GetSkillPackageReferences(string) []config.SkillReference {
	return nil
}

func (s stubAssetCatalogSkillStore) SetSkillPackageEnabled(string, bool) error {
	return nil
}

func (s stubAssetCatalogSkillStore) ListSkills() ([]map[string]any, error) {
	return nil, nil
}

func (s stubAssetCatalogSkillStore) GetSkillContent(string) (map[string]any, error) {
	return nil, nil
}

func (s stubAssetCatalogSkillStore) SaveSkill(string, string, string) error {
	return nil
}

func (s stubAssetCatalogSkillStore) DeleteSkill(string) error {
	return nil
}

func (s stubAssetCatalogSkillStore) ImportSkill(string, bool) (map[string]any, error) {
	return nil, nil
}

func (s stubRuntimeCoordinator) Snapshot(context.Context) middlewareclient.RuntimeSnapshot {
	return s.snapshot
}

func (s stubRuntimeCoordinator) Services(context.Context) RuntimeServicesView {
	return RuntimeServicesView{}
}

func (s stubToolAggregatorSource) GetToolAggregatorSnapshot() toolrouter.Snapshot {
	return s.snapshot
}

func decodeToolCatalogViewJSON(t *testing.T, view ToolCatalogView) map[string]any {
	t.Helper()
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal tool catalog view: %v", err)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal tool catalog view: %v", err)
	}
	return decoded
}

func requireDecodedCatalog(t *testing.T, decoded map[string]any) map[string]any {
	t.Helper()
	catalog, ok := decoded["catalog"].(map[string]any)
	if !ok {
		t.Fatalf("expected catalog map in %#v", decoded)
	}
	return catalog
}

func requireDecodedCatalogItemIDs(t *testing.T, catalog map[string]any) []string {
	t.Helper()
	rawItems, ok := catalog["items"].([]any)
	if !ok {
		t.Fatalf("expected catalog items slice, got %#v", catalog["items"])
	}
	ids := make([]string, 0, len(rawItems))
	for _, rawItem := range rawItems {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		id, _ := item["id"].(string)
		if strings.TrimSpace(id) != "" {
			ids = append(ids, strings.TrimSpace(id))
		}
	}
	return ids
}

func writeDefaultAssetCatalogConfig(t *testing.T, configDir string) {
	t.Helper()
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte("version: '1.0'\nruntime:\n  global:\n    skill_packages: []\n  agents:\n    orchestrator:\n      enabled: true\n      prompts: {}\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func newAssetCatalogGovernanceTestService(t *testing.T, runtimeSnapshot middlewareclient.RuntimeSnapshot) *assetCatalogService {
	t.Helper()
	rootDir := t.TempDir()
	configDir := filepath.Join(rootDir, "agent_configs")
	writeDefaultAssetCatalogConfig(t, configDir)
	return NewAssetCatalogService(AssetCatalogServiceDeps{
		AgentConfigManager: config.NewAgentConfigManager(configDir),
		RuntimeCoordinator: stubRuntimeCoordinator{snapshot: runtimeSnapshot},
	}).(*assetCatalogService)
}

func writeAssetCatalogSkillPackage(t *testing.T, rootDir, packageName, backend string, toolNames ...string) {
	t.Helper()
	packageDir := filepath.Join(rootDir, "skills", packageName)
	toolsDir := filepath.Join(packageDir, "tools")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatalf("mkdir skill package dir: %v", err)
	}
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("mkdir skill tools dir: %v", err)
	}
	toolRefs := strings.Join(toolNames, "\n  - ")
	skillMarkdown := strings.TrimSpace(`---
name: `+packageName+`
description: `+packageName+` package
kind: category
backend: `+backend+`
tools:
  - `+toolRefs+`
---

`+packageName+` package`) + "\n"
	if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte(skillMarkdown), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	runtime := skill.NormalizeRuntime(backend)
	if runtime == "" {
		runtime = backend
	}
	for _, toolName := range toolNames {
		toolYAML := strings.TrimSpace(`apiVersion: tool/v1
kind: Tool
metadata:
  name: `+toolName+`
  runtime: `+runtime+`
spec:
  description: `+toolName+` desc
execution:
  backend_tool: `+toolName) + "\n"
		if err := os.WriteFile(filepath.Join(toolsDir, toolName+".yaml"), []byte(toolYAML), 0o644); err != nil {
			t.Fatalf("write tool yaml: %v", err)
		}
	}
}

func chdirAssetCatalogTestRoot(t *testing.T, rootDir string) {
	t.Helper()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
}

func TestAssetCatalogServiceUsesSnapshotProjectionInCutoverMode(t *testing.T) {
	rootDir := t.TempDir()
	configDir := filepath.Join(rootDir, "agent_configs")
	skillsDir := filepath.Join(rootDir, "skills")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(skillsDir, "analysis-skill", "tools"), 0o755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "analysis-skill", "SKILL.md"), []byte("---\nname: analysis-skill\ndescription: analysis\nkind: category\nbackend: compute\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "analysis-skill", "tools", "buffer.yaml"), []byte("apiVersion: skill/v1\nkind: AtomicSkill\nmetadata:\n  name: buffer\n  backend: compute\nspec:\n  description: buffer\nexecution:\n  backend_tool: buffer\n  param_mapping:\n    INPUT: '{{ INPUT }}'\n"), 0o644); err != nil {
		t.Fatalf("write tool yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte("version: '1.0'\nruntime:\n  global:\n    skill_packages: [analysis-skill]\n  agents:\n    orchestrator:\n      enabled: true\n      prompts: {}\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	manager := config.NewAgentConfigManager(configDir)
	store := capability.NewSnapshotStore()
	store.Set(&capability.CapabilitySnapshot{
		Tools: map[string]capability.ToolCapability{
			"open_project": {
				Name:           "open_project",
				Description:    "打开工程",
				Category:       "project",
				Source:         skill.RuntimeIDQGIS,
				Backend:        skill.RuntimeIDQGIS,
				Available:      true,
				Package:        capability.PackageRef{PackageID: "analysis-skill"},
				PackageKind:    "category",
				PackageEnabled: true,
			},
		},
		ToolOrder: []string{"open_project"},
	})

	service := NewAssetCatalogService(AssetCatalogServiceDeps{
		AgentConfigManager: manager,
		RuntimeCoordinator: stubRuntimeCoordinator{},
		SnapshotStore:      store,
		SnapshotMode:       "cutover",
	}).(*assetCatalogService)

	view, err := service.GetToolCatalog(context.Background(), "active")
	if err != nil {
		t.Fatalf("GetToolCatalog returned error: %v", err)
	}
	if len(view.ToolInfos) != 1 || view.ToolInfos[0].Name != "open_project" {
		t.Fatalf("unexpected tool infos: %#v", view.ToolInfos)
	}
	if view.ToolInfos[0].PackageID != "analysis-skill" || view.ToolInfos[0].Kind != "category" || !view.ToolInfos[0].Available {
		t.Fatalf("unexpected snapshot-projected tool info: %#v", view.ToolInfos[0])
	}
}

func TestAggregatorIsInternalOnly(t *testing.T) {
	decoded := decodeToolCatalogViewJSON(t, ToolCatalogView{
		Scope:           "shadow-scope",
		ConfigName:      "shadow-config",
		RuntimeRevision: "runtime-1",
		Catalog: toolcatalog.BuildContract(
			"global",
			"default",
			[]toolcatalog.Item{{Name: "buffer", Source: toolcatalog.SourceCompute, Available: true}},
			[]string{"buffer"},
			nil,
		),
		AvailableCount:    1,
		ConfiguredCount:   1,
		ToolCatalogStatus: map[string]any{"qgis_tool_count": 1},
		ToolInfos:         []toolcatalog.Item{{Name: "buffer", Source: toolcatalog.SourceCompute, Available: true}},
		Tools:             []string{"buffer"},
	})

	for _, key := range []string{"available_count", "configured_count", "tool_catalog_status", "tool_infos", "tools"} {
		if _, ok := decoded[key]; ok {
			t.Fatalf("expected %q to remain internal-only, got %#v", key, decoded[key])
		}
	}
}

func TestCatalogProjectionUsesGovernance(t *testing.T) {
	decoded := decodeToolCatalogViewJSON(t, ToolCatalogView{
		Scope:           "shadow-scope",
		ConfigName:      "shadow-config",
		RuntimeRevision: "runtime-1",
		Catalog: toolcatalog.BuildContract(
			"governed-scope",
			"governed-config",
			[]toolcatalog.Item{{Name: "buffer", Source: toolcatalog.SourceCompute, Available: true}},
			[]string{"buffer"},
			nil,
		),
	})

	if got, _ := decoded["scope"].(string); got != "governed-scope" {
		t.Fatalf("expected public scope from governance contract, got %q", got)
	}
	if got, _ := decoded["config_name"].(string); got != "governed-config" {
		t.Fatalf("expected public config_name from governance contract, got %q", got)
	}
}

func TestRevisionPropagation(t *testing.T) {
	service := newAssetCatalogGovernanceTestService(t, middlewareclient.RuntimeSnapshot{
		ToolSchemaRevision:       "schema-1",
		ToolAvailabilityRevision: "availability-1",
	})
	service.toolAggregatorSource = stubToolAggregatorSource{snapshot: toolrouter.Snapshot{
		Interactive: []toolrouter.InteractiveToolDefinition{{
			Name:        "open_project",
			Description: "open",
			Category:    "project",
			InputSchema: map[string]any{"type": "object", "title": "open-project-v1"},
		}},
	}}

	view, err := service.GetToolCatalog(context.Background(), "active")
	if err != nil {
		t.Fatalf("GetToolCatalog returned error: %v", err)
	}
	catalog := requireDecodedCatalog(t, decodeToolCatalogViewJSON(t, view))

	if got, _ := view.ToolCatalogStatus["tool_schema_revision"].(string); got != "schema-1" {
		t.Fatalf("expected runtime schema revision to stay in internal status, got %#v", view.ToolCatalogStatus["tool_schema_revision"])
	}
	if got, _ := view.ToolCatalogStatus["tool_availability_revision"].(string); got != "availability-1" {
		t.Fatalf("expected runtime availability revision to stay in internal status, got %#v", view.ToolCatalogStatus["tool_availability_revision"])
	}
	if got, _ := catalog["schema_revision"].(string); strings.TrimSpace(got) == "" || got == "schema-1" {
		t.Fatalf("expected public schema_revision to be recomputed from public catalog, got %q", got)
	}
	if got, _ := catalog["availability_revision"].(string); strings.TrimSpace(got) == "" || got == "availability-1" {
		t.Fatalf("expected public availability_revision to be recomputed from public catalog, got %q", got)
	}
	if got, _ := catalog["policy_revision"].(string); got == "" {
		t.Fatalf("expected non-empty policy_revision, got %#v", catalog["policy_revision"])
	}
	if got, _ := catalog["degraded_revision"].(string); got == "" {
		t.Fatalf("expected non-empty degraded_revision, got %#v", catalog["degraded_revision"])
	}
}

func TestSchemaRevisionBump(t *testing.T) {
	originalLoadPolicies := loadToolCatalogPolicies
	loadToolCatalogPolicies = func() (map[string]toolCatalogPolicy, error) {
		return map[string]toolCatalogPolicy{
			"open_project": {Visibility: "public", AuthorizationScopes: []string{"tool.invoke"}},
		}, nil
	}
	t.Cleanup(func() { loadToolCatalogPolicies = originalLoadPolicies })
	serviceA := newAssetCatalogGovernanceTestService(t, middlewareclient.RuntimeSnapshot{})
	serviceA.toolAggregatorSource = stubToolAggregatorSource{snapshot: toolrouter.Snapshot{
		Interactive: []toolrouter.InteractiveToolDefinition{{
			Name:        "open_project",
			Description: "open",
			Category:    "project",
			InputSchema: map[string]any{"type": "object", "title": "open-project-v1"},
		}},
	}}
	viewA, err := serviceA.GetToolCatalog(context.Background(), "global")
	if err != nil {
		t.Fatalf("GetToolCatalog schema-a returned error: %v", err)
	}
	loadToolCatalogPolicies = func() (map[string]toolCatalogPolicy, error) {
		return map[string]toolCatalogPolicy{
			"open_project": {Visibility: "hidden", AuthorizationScopes: []string{"tool.invoke.internal"}},
		}, nil
	}
	serviceB := newAssetCatalogGovernanceTestService(t, middlewareclient.RuntimeSnapshot{})
	serviceB.toolAggregatorSource = stubToolAggregatorSource{snapshot: toolrouter.Snapshot{
		Interactive: []toolrouter.InteractiveToolDefinition{{
			Name: "open_project", Description: "open", Category: "project",
			InputSchema: map[string]any{"type": "object", "title": "open-project-v2"},
		}},
	}}
	viewB, err := serviceB.GetToolCatalog(context.Background(), "global")
	if err != nil {
		t.Fatalf("GetToolCatalog schema-b returned error: %v", err)
	}
	catalogA := requireDecodedCatalog(t, decodeToolCatalogViewJSON(t, viewA))
	catalogB := requireDecodedCatalog(t, decodeToolCatalogViewJSON(t, viewB))

	if gotA, _ := catalogA["schema_revision"].(string); gotA == "" {
		t.Fatalf("expected non-empty schema_revision, got %#v", catalogA["schema_revision"])
	}
	if gotA, _ := catalogA["schema_revision"].(string); gotA == catalogB["schema_revision"] {
		t.Fatalf("expected schema_revision bump, got A=%#v B=%#v", catalogA["schema_revision"], catalogB["schema_revision"])
	}
}

func TestAvailabilityRevisionBump(t *testing.T) {
	originalLoadPolicies := loadToolCatalogPolicies
	loadToolCatalogPolicies = func() (map[string]toolCatalogPolicy, error) {
		return map[string]toolCatalogPolicy{
			"open_project": {Visibility: "public", AuthorizationScopes: []string{"tool.invoke"}},
		}, nil
	}
	t.Cleanup(func() { loadToolCatalogPolicies = originalLoadPolicies })
	serviceA := newAssetCatalogGovernanceTestService(t, middlewareclient.RuntimeSnapshot{})
	serviceA.toolAggregatorSource = stubToolAggregatorSource{snapshot: toolrouter.Snapshot{
		Interactive: []toolrouter.InteractiveToolDefinition{{
			Name:        "open_project",
			Description: "open",
			Category:    "project",
			InputSchema: map[string]any{"type": "object", "title": "open-project-v1"},
			Available:   true,
		}},
	}}
	viewA, err := serviceA.GetToolCatalog(context.Background(), "global")
	if err != nil {
		t.Fatalf("GetToolCatalog availability-a returned error: %v", err)
	}
	serviceB := newAssetCatalogGovernanceTestService(t, middlewareclient.RuntimeSnapshot{})
	serviceB.toolAggregatorSource = stubToolAggregatorSource{snapshot: toolrouter.Snapshot{
		Interactive: []toolrouter.InteractiveToolDefinition{{
			Name:              "open_project",
			Description:       "open",
			Category:          "project",
			InputSchema:       map[string]any{"type": "object", "title": "open-project-v1"},
			Available:         false,
			UnavailableReason: toolcatalog.MissingReasonInteractiveDisconnected,
		}},
	}}
	viewB, err := serviceB.GetToolCatalog(context.Background(), "global")
	if err != nil {
		t.Fatalf("GetToolCatalog availability-b returned error: %v", err)
	}
	catalogA := requireDecodedCatalog(t, decodeToolCatalogViewJSON(t, viewA))
	catalogB := requireDecodedCatalog(t, decodeToolCatalogViewJSON(t, viewB))

	if gotA, _ := catalogA["availability_revision"].(string); gotA == "" {
		t.Fatalf("expected non-empty availability_revision, got %#v", catalogA["availability_revision"])
	}
	if gotA, _ := catalogA["availability_revision"].(string); gotA == catalogB["availability_revision"] {
		t.Fatalf("expected availability_revision bump, got A=%#v B=%#v", catalogA["availability_revision"], catalogB["availability_revision"])
	}
}

func TestAssetCatalogServicePublicCatalogOmitsNonPublicTools(t *testing.T) {
	originalLoadPolicies := loadToolCatalogPolicies
	loadToolCatalogPolicies = func() (map[string]toolCatalogPolicy, error) {
		return map[string]toolCatalogPolicy{
			"open_project":       {Visibility: "public", AuthorizationScopes: []string{"catalog.read", "tool.invoke"}},
			"open_project_debug": {Visibility: "hidden", AuthorizationScopes: []string{"tool.invoke.internal"}},
			"system_probe":       {Visibility: "internal", AuthorizationScopes: []string{"tool.invoke.internal"}},
		}, nil
	}
	t.Cleanup(func() {
		loadToolCatalogPolicies = originalLoadPolicies
	})

	service := newAssetCatalogGovernanceTestService(t, middlewareclient.RuntimeSnapshot{})
	service.toolAggregatorSource = stubToolAggregatorSource{snapshot: toolrouter.Snapshot{
		Interactive: []toolrouter.InteractiveToolDefinition{
			{Name: "open_project", Description: "open", Category: "project"},
			{Name: "open_project_debug", Description: "debug", Category: "project"},
			{Name: "rogue_public_candidate", Description: "rogue", Category: "project"},
		},
		Compute: []compute.ToolDefinition{{Name: "system_probe", Description: "probe", Category: "system"}},
	}}

	view, err := service.GetToolCatalog(context.Background(), "global")
	if err != nil {
		t.Fatalf("GetToolCatalog returned error: %v", err)
	}
	catalog := requireDecodedCatalog(t, decodeToolCatalogViewJSON(t, view))
	got := requireDecodedCatalogItemIDs(t, catalog)
	if !containsCatalogID(got, "open_project") {
		t.Fatalf("expected public tool to remain in catalog, got %#v", got)
	}
	if containsCatalogID(got, "open_project_debug") {
		t.Fatalf("expected hidden tool to be filtered from public catalog, got %#v", got)
	}
	if containsCatalogID(got, "system_probe") {
		t.Fatalf("expected internal tool to be filtered from public catalog, got %#v", got)
	}
	if containsCatalogID(got, "rogue_public_candidate") {
		t.Fatalf("expected unknown-policy tool to be filtered from public catalog, got %#v", got)
	}
	if got, _ := catalog["schema_revision"].(string); strings.TrimSpace(got) == "" {
		t.Fatalf("expected public schema revision, got %#v", catalog["schema_revision"])
	}
	if got, _ := catalog["availability_revision"].(string); strings.TrimSpace(got) == "" {
		t.Fatalf("expected public availability revision, got %#v", catalog["availability_revision"])
	}
}

func containsCatalogID(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestAssetCatalogServiceLoadsPublicPoliciesFromImportedGlobalToolDirs(t *testing.T) {
	service := newAssetCatalogGovernanceTestService(t, middlewareclient.RuntimeSnapshot{})
	configBaseDir := service.agentConfigManager.ConfigBaseDir()
	toolDir := filepath.Join(config.ResolveGlobalToolsDir(configBaseDir), "hydrology-tools", "analysis", "stream_order")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatalf("mkdir tool dir failed: %v", err)
	}
	toolYAML := `apiVersion: tool-package/v1
kind: ToolPackage
metadata:
  name: stream_order
  domain: analysis
  runtime: qgis
spec:
  description: stream order
  parameters:
    - name: input
      type: string
policy:
  visibility: public
  authorization_scopes: [tool.invoke]
execution:
  provider: qgis
  binding: handler
  handler: handlers.analysis.stream_order
`
	if err := os.WriteFile(filepath.Join(toolDir, "tool.yaml"), []byte(toolYAML), 0o644); err != nil {
		t.Fatalf("write tool manifest failed: %v", err)
	}
	service.toolAggregatorSource = stubToolAggregatorSource{snapshot: toolrouter.Snapshot{
		Interactive: []toolrouter.InteractiveToolDefinition{{
			Name:              "stream_order",
			Description:       "stream order",
			Category:          "analysis",
			Available:         false,
			UnavailableReason: toolcatalog.MissingReasonInteractiveDisconnected,
		}},
	}}

	view, err := service.GetToolCatalog(context.Background(), "global")
	if err != nil {
		t.Fatalf("GetToolCatalog returned error: %v", err)
	}
	if !containsCatalogID(view.Tools, "stream_order") {
		t.Fatalf("expected imported global tool visible, got %#v", view.Tools)
	}
	catalog := requireDecodedCatalog(t, decodeToolCatalogViewJSON(t, view))
	items, _ := catalog["items"].([]any)
	var target map[string]any
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if got, _ := item["id"].(string); got == "stream_order" {
			target = item
			break
		}
	}
	if target == nil {
		t.Fatalf("expected stream_order in catalog items, got %#v", items)
	}
	if got, _ := target["missing_dependency_reason"].(string); got != toolcatalog.MissingReasonInteractiveDisconnected {
		t.Fatalf("unexpected missing reason: %#v", target)
	}
}

func TestPolicyRevisionBump(t *testing.T) {
	catalogA := requireDecodedCatalog(t, decodeToolCatalogViewJSON(t, ToolCatalogView{
		Catalog: toolcatalog.BuildContract(
			"global",
			"default",
			[]toolcatalog.Item{{Name: "buffer", Source: toolcatalog.SourceCompute, Available: true, Enabled: boolPtr(true)}},
			[]string{"buffer"},
			nil,
		),
	}))
	catalogB := requireDecodedCatalog(t, decodeToolCatalogViewJSON(t, ToolCatalogView{
		Catalog: toolcatalog.BuildContract(
			"global",
			"alternate",
			[]toolcatalog.Item{{Name: "buffer", Source: toolcatalog.SourceCompute, Available: true, Enabled: boolPtr(true)}},
			[]string{"buffer"},
			nil,
		),
	}))

	if gotA, _ := catalogA["policy_revision"].(string); gotA == "" {
		t.Fatalf("expected non-empty policy_revision, got %#v", catalogA["policy_revision"])
	}
	if gotA, _ := catalogA["policy_revision"].(string); gotA == catalogB["policy_revision"] {
		t.Fatalf("expected policy_revision bump, got A=%#v B=%#v", catalogA["policy_revision"], catalogB["policy_revision"])
	}
}

func TestDegradedRevisionBump(t *testing.T) {
	catalogA := requireDecodedCatalog(t, decodeToolCatalogViewJSON(t, ToolCatalogView{
		Catalog: toolcatalog.BuildContract(
			"global",
			"default",
			[]toolcatalog.Item{{Name: "buffer", Source: toolcatalog.SourceCompute, Available: true}},
			[]string{"buffer"},
			nil,
		),
	}))
	catalogB := requireDecodedCatalog(t, decodeToolCatalogViewJSON(t, ToolCatalogView{
		Catalog: toolcatalog.BuildContract(
			"global",
			"default",
			[]toolcatalog.Item{{Name: "buffer", Source: toolcatalog.SourceCompute, Available: false, MissingReason: toolcatalog.MissingReasonRuntimeCatalogMissing}},
			[]string{"buffer"},
			nil,
		),
	}))

	if gotA, _ := catalogA["degraded_revision"].(string); gotA == "" {
		t.Fatalf("expected non-empty degraded_revision, got %#v", catalogA["degraded_revision"])
	}
	if gotA, _ := catalogA["degraded_revision"].(string); gotA == catalogB["degraded_revision"] {
		t.Fatalf("expected degraded_revision bump, got A=%#v B=%#v", catalogA["degraded_revision"], catalogB["degraded_revision"])
	}
}

func TestCatalogContract(t *testing.T) {
	rootDir := t.TempDir()
	configDir := filepath.Join(rootDir, "agent_configs")
	chdirAssetCatalogTestRoot(t, rootDir)
	writeAssetCatalogSkillPackage(t, rootDir, "canvas-navigation", "bridge", "test_interactive_tool")
	writeAssetCatalogSkillPackage(t, rootDir, "raster-analysis", "compute", "test_compute_tool")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte("version: '1.0'\nruntime:\n  global:\n    skill_packages: [canvas-navigation, raster-analysis]\n  agents:\n    orchestrator:\n      enabled: true\n      prompts: {}\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	service := NewAssetCatalogService(AssetCatalogServiceDeps{
		AgentConfigManager: config.NewAgentConfigManager(configDir),
		ToolAggregatorSource: stubToolAggregatorSource{snapshot: toolrouter.Snapshot{
			Interactive: []toolrouter.InteractiveToolDefinition{{
				Name:        "test_interactive_tool",
				Description: "交互工具描述",
				Category:    "interactive",
			}},
			Compute: []compute.ToolDefinition{{
				Name:        "test_compute_tool",
				Description: "计算工具描述",
				Category:    "compute",
			}},
		}},
		RuntimeCoordinator: stubRuntimeCoordinator{},
	}).(*assetCatalogService)

	view, err := service.GetToolCatalog(context.Background(), "global")
	if err != nil {
		t.Fatalf("GetToolCatalog returned error: %v", err)
	}
	decoded := decodeToolCatalogViewJSON(t, view)
	catalog, ok := decoded["catalog"].(map[string]any)
	if !ok {
		t.Fatalf("expected authoritative catalog contract in view JSON, got %#v", decoded)
	}
	if got, _ := catalog["capability_type"].(string); got != "tool_catalog" {
		t.Fatalf("unexpected catalog capability_type: %q", got)
	}
}

func TestEmptyCatalogSemantics(t *testing.T) {
	t.Run("valid empty catalog", func(t *testing.T) {
		catalog := decodeToolCatalogViewJSON(t, ToolCatalogView{
			Scope:   "global",
			Catalog: toolcatalog.BuildContract("global", "default", nil, nil, nil),
		})["catalog"].(map[string]any)
		if got, _ := catalog["availability"].(string); got != "valid_empty" {
			t.Fatalf("unexpected valid empty availability: %q", got)
		}
	})

	t.Run("abnormal empty catalog", func(t *testing.T) {
		catalog := decodeToolCatalogViewJSON(t, ToolCatalogView{
			Scope: "global",
			Catalog: toolcatalog.BuildContract(
				"global",
				"default",
				nil,
				[]string{"output_init_dir"},
				nil,
			),
		})["catalog"].(map[string]any)
		if got, _ := catalog["degradation_status"].(string); got != "abnormal_empty" {
			t.Fatalf("unexpected abnormal empty degradation_status: %q", got)
		}
	})
}

func TestProviderMissingSemantics(t *testing.T) {
	catalog := decodeToolCatalogViewJSON(t, ToolCatalogView{
		Scope: "global",
		Catalog: toolcatalog.BuildContract(
			"global",
			"default",
			[]toolcatalog.Item{{
				Name:          "output_init_dir",
				Source:        toolcatalog.SourceCompute,
				Available:     false,
				MissingReason: toolcatalog.MissingReasonRuntimeCatalogMissing,
			}},
			[]string{"output_init_dir"},
			map[string]middlewareclient.ServiceStatus{
				skill.RuntimeIDCLI: {
					ID:            skill.RuntimeIDCLI,
					Status:        "stopped",
					Enabled:       true,
					ProviderCount: 0,
				},
			},
		),
	})["catalog"].(map[string]any)
	if got, _ := catalog["degradation_status"].(string); got != "provider_missing" {
		t.Fatalf("unexpected provider missing degradation_status: %q", got)
	}
}

func TestUnsupportedRuntimeSemantics(t *testing.T) {
	decoded := decodeToolCatalogViewJSON(t, ToolCatalogView{
		Scope: "global",
		Catalog: toolcatalog.BuildContract(
			"global",
			"default",
			[]toolcatalog.Item{{
				Name:      "wasm_render_map",
				Backend:   "wasm",
				Source:    "wasm-skill",
				Available: false,
			}},
			[]string{"wasm_render_map"},
			nil,
		),
	})
	catalog, ok := decoded["catalog"].(map[string]any)
	if !ok {
		t.Fatalf("expected catalog contract in view JSON, got %#v", decoded)
	}
	if got, _ := catalog["degradation_status"].(string); got != "unsupported_runtime" {
		t.Fatalf("unexpected unsupported runtime degradation_status: %q", got)
	}
}

func TestAssetCatalogServiceEffectiveScopeFiltersUnavailableSnapshotTools(t *testing.T) {
	rootDir := t.TempDir()
	configDir := filepath.Join(rootDir, "agent_configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte("version: '1.0'\nruntime:\n  global:\n    skill_packages: []\n  agents:\n    orchestrator:\n      enabled: true\n      prompts: {}\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	manager := config.NewAgentConfigManager(configDir)
	store := capability.NewSnapshotStore()
	store.Set(&capability.CapabilitySnapshot{
		Tools: map[string]capability.ToolCapability{
			"open_project": {
				Name:        "open_project",
				Description: "打开工程",
				Available:   true,
				Package:     capability.PackageRef{PackageID: "analysis-skill"},
			},
			"get_project_info": {
				Name:              "get_project_info",
				Description:       "工程信息",
				Available:         false,
				UnavailableReason: toolcatalog.MissingReasonInteractiveDisconnected,
				Package:           capability.PackageRef{PackageID: "analysis-skill"},
			},
		},
		ToolOrder: []string{"open_project", "get_project_info"},
	})

	service := NewAssetCatalogService(AssetCatalogServiceDeps{
		AgentConfigManager: manager,
		RuntimeCoordinator: stubRuntimeCoordinator{},
		SnapshotStore:      store,
		SnapshotMode:       "cutover",
	}).(*assetCatalogService)

	view, err := service.GetToolCatalog(context.Background(), "effective")
	if err != nil {
		t.Fatalf("GetToolCatalog returned error: %v", err)
	}
	if got := len(view.ToolInfos); got != 1 {
		t.Fatalf("expected effective scope to filter unavailable tools, got %d: %#v", got, view.ToolInfos)
	}
	if got := view.ToolInfos[0].Name; got != "open_project" {
		t.Fatalf("unexpected effective tool: %q", got)
	}
}

func TestAssetCatalogServiceComputeServiceReadyAcceptsCLIRuntimeKey(t *testing.T) {
	service := &assetCatalogService{
		runtimeCoordinator: stubRuntimeCoordinator{snapshot: middlewareclient.RuntimeSnapshot{
			Services: map[string]middlewareclient.ServiceStatus{
				skill.RuntimeIDCLI: {
					ID:      skill.RuntimeIDCLI,
					Status:  "running",
					Enabled: true,
					Ready:   true,
				},
			},
		}},
	}

	if !service.computeServiceReady(context.Background()) {
		t.Fatal("expected computeServiceReady to accept cli runtime service key from runtime snapshot")
	}
}

func TestAssetCatalogServiceToolCatalogStatusUsesRuntimeSemanticKeys(t *testing.T) {
	rootDir := t.TempDir()
	configDir := filepath.Join(rootDir, "agent_configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte("version: '1.0'\nruntime:\n  global:\n    skill_packages: []\n  agents:\n    orchestrator:\n      enabled: true\n      prompts: {}\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	manager := config.NewAgentConfigManager(configDir)
	service := NewAssetCatalogService(AssetCatalogServiceDeps{
		AgentConfigManager: manager,
		RuntimeCoordinator: stubRuntimeCoordinator{snapshot: middlewareclient.RuntimeSnapshot{
			Instances:                middlewareclient.InstancesSnapshot{ActiveID: "inst-a", RunningCount: 1},
			ToolCatalogRevision:      "rev-1",
			ToolSchemaRevision:       "schema-1",
			ToolAvailabilityRevision: "availability-1",
			ModeIssues:               map[string]middlewareclient.ModeIssue{"plan": {Message: "ready"}},
		}},
	}).(*assetCatalogService)

	view, err := service.GetToolCatalog(context.Background(), "active")
	if err != nil {
		t.Fatalf("GetToolCatalog returned error: %v", err)
	}
	status := view.ToolCatalogStatus
	if _, ok := status["cli_ready"]; !ok {
		t.Fatalf("expected cli_ready in tool catalog status, got %#v", status)
	}
	if _, ok := status["qgis_tool_count"]; !ok {
		t.Fatalf("expected qgis_tool_count in tool catalog status, got %#v", status)
	}
	if _, ok := status["cli_tool_count"]; !ok {
		t.Fatalf("expected cli_tool_count in tool catalog status, got %#v", status)
	}
	if _, ok := status["compute_ready"]; ok {
		t.Fatalf("did not expect legacy compute_ready in tool catalog status: %#v", status)
	}
	if _, ok := status["interactive_tool_count"]; ok {
		t.Fatalf("did not expect legacy interactive_tool_count in tool catalog status: %#v", status)
	}
	if _, ok := status["compute_tool_count"]; ok {
		t.Fatalf("did not expect legacy compute_tool_count in tool catalog status: %#v", status)
	}
}

func TestAssetCatalogServiceBuildToolPackageCatalogIndexUsesToolRefsWithoutEmbeddedTools(t *testing.T) {
	service := &assetCatalogService{
		skillCatalog: stubAssetCatalogSkillStore{
			packages: []settings.SkillPackageSummary{{
				PackageID: "analysis-skill",
				Kind:      "category",
				Backend:   skill.RuntimeIDCLI,
				Source:    "global",
				Enabled:   true,
				HasTools:  true,
			}},
			details: map[string]settings.SkillPackageDetail{
				"analysis-skill": {
					SkillPackageSummary: settings.SkillPackageSummary{
						PackageID: "analysis-skill",
						Kind:      "category",
						Backend:   skill.RuntimeIDCLI,
						Source:    "global",
						Enabled:   true,
						HasTools:  true,
					},
					FrontMatter: skill.SkillFrontMatter{Tools: []string{"buffer"}},
				},
			},
		},
	}

	index := service.buildToolPackageCatalogIndex()
	meta, ok := index["buffer"]
	if !ok {
		t.Fatalf("expected tool ref to be indexed, got %#v", index)
	}
	if meta.PackageID != "analysis-skill" || meta.Kind != "category" || meta.Backend != skill.RuntimeIDCLI || meta.Source != "global" {
		t.Fatalf("unexpected tool package meta: %#v", meta)
	}
}

func TestAssetCatalogServiceBuildToolPackageCatalogIndexIgnoresEmbeddedToolsWithoutRefs(t *testing.T) {
	service := &assetCatalogService{
		skillCatalog: stubAssetCatalogSkillStore{
			packages: []settings.SkillPackageSummary{{
				PackageID: "analysis-skill",
				Kind:      "category",
				Backend:   skill.RuntimeIDCLI,
				Source:    "global",
				Enabled:   true,
				HasTools:  true,
			}},
			details: map[string]settings.SkillPackageDetail{
				"analysis-skill": {
					SkillPackageSummary: settings.SkillPackageSummary{
						PackageID: "analysis-skill",
						Kind:      "category",
						Backend:   skill.RuntimeIDCLI,
						Source:    "global",
						Enabled:   true,
						HasTools:  true,
					},
					Tools: []skill.SkillDefinition{{Metadata: skill.SkillMetadata{Name: "buffer", Backend: skill.RuntimeIDCLI}}},
				},
			},
		},
	}

	index := service.buildToolPackageCatalogIndex()
	if _, ok := index["buffer"]; ok {
		t.Fatalf("expected embedded tools without refs to stay out of package index, got %#v", index)
	}
}

func TestAssetCatalogServiceGlobalScopeIncludesCanonicalRuntimeTools(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", rootDir)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", filepath.Join(rootDir, "autocarto"))
	configDir := filepath.Join(rootDir, "autocarto", "config", "agent_configs")
	systemSkillsDir := filepath.Join(rootDir, "autocarto", "skills", "system")
	userSkillsDir := filepath.Join(rootDir, "autocarto", "skills", "user")
	systemToolsDir := filepath.Join(rootDir, "autocarto", "tools", "system")
	userToolsDir := filepath.Join(rootDir, "autocarto", "tools", "user")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	for _, dir := range []string{systemSkillsDir, userSkillsDir, systemToolsDir, userToolsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir plugin tree dir %s: %v", dir, err)
		}
	}
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	// Populate the isolated authority from the actual definitions. Empty asset
	// directories no longer activate the retired embedded contract copy.
	for _, name := range []string{"get_layers_info", "open_project"} {
		source := filepath.Join(originalWD, "..", "..", "..", "autocarto", "tools", "system", "project-state", name+".yaml")
		raw, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(systemToolsDir, name+".yaml"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte("version: '1.0'\nruntime:\n  global:\n    skill_packages: []\n  agents:\n    orchestrator:\n      enabled: true\n      prompts: {}\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	manager := config.NewAgentConfigManager(configDir)
	service := NewAssetCatalogService(AssetCatalogServiceDeps{
		AgentConfigManager: manager,
		RuntimeCoordinator: stubRuntimeCoordinator{},
		ToolAggregatorSource: stubToolAggregatorSource{snapshot: toolrouter.Snapshot{
			Compute: []compute.ToolDefinition{{
				Name:        "get_layers_info",
				Description: "图层信息",
				Category:    "project",
				Available:   true,
			}},
			Interactive: []toolrouter.InteractiveToolDefinition{{
				Name:              "open_project",
				Description:       "打开工程",
				Category:          "project",
				Available:         false,
				UnavailableReason: toolcatalog.MissingReasonInteractiveDisconnected,
				InstanceIDs:       []string{"instance-a"},
			}},
		}},
	}).(*assetCatalogService)

	view, err := service.GetToolCatalog(context.Background(), "global")
	if err != nil {
		t.Fatalf("GetToolCatalog returned error: %v", err)
	}
	if got := len(view.ToolInfos); got != 2 {
		t.Fatalf("expected 2 tool infos, got %d: %#v", got, view.ToolInfos)
	}

	items := make(map[string]settingsToolCatalogAssertion, len(view.ToolInfos))
	for _, item := range view.ToolInfos {
		items[item.Name] = settingsToolCatalogAssertion{
			available:     item.Available,
			missingReason: item.MissingReason,
			source:        item.Source,
		}
	}

	computeItem, ok := items["get_layers_info"]
	if !ok {
		t.Fatalf("expected get_layers_info in catalog: %#v", view.ToolInfos)
	}
	if !computeItem.available || computeItem.source != skill.RuntimeIDCLI {
		t.Fatalf("unexpected compute tool info: %#v", computeItem)
	}

	interactiveItem, ok := items["open_project"]
	if !ok {
		t.Fatalf("expected open_project in catalog: %#v", view.ToolInfos)
	}
	if interactiveItem.available || interactiveItem.missingReason != "interactive_instance_disconnected" || interactiveItem.source != skill.RuntimeIDQGIS {
		t.Fatalf("unexpected interactive tool info: %#v", interactiveItem)
	}
	if got := len(view.Tools); got != 2 {
		t.Fatalf("expected tools list to include canonical runtime tools, got %d: %#v", got, view.Tools)
	}
}

type settingsToolCatalogAssertion struct {
	available     bool
	missingReason string
	source        string
}
