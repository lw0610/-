package capability

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/compute"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
	"github.com/autocarto/carto_agent/internal/toolcatalog"
	"github.com/autocarto/carto_agent/internal/toolrouter"
)

func TestBuiltinToolBindingsFollowCategoryOwnership(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository fixtures")
	}
	assets := filepath.Join(filepath.Dir(file), "..", "..", "..", "autocarto")
	tools := toolpkg.NewRegistry()
	if err := tools.LoadDirectories(filepath.Join(assets, "tools", "system")); err != nil {
		t.Fatal(err)
	}
	registry := skill.NewRegistry(tools)
	if err := registry.LoadDirectories(filepath.Join(assets, "skills", "system")); err != nil {
		t.Fatal(err)
	}
	details := map[string]settings.SkillPackageDetail{}
	for _, pkg := range registry.Packages() {
		details[buildPackageKey(settings.SkillPackageScopeBuiltin, "", pkg.Name)] = settings.SkillPackageDetail{
			SkillPackageSummary: settings.SkillPackageSummary{PackageID: pkg.Name, Name: pkg.Name, Kind: pkg.Kind, System: pkg.System, Hidden: pkg.Hidden, Scope: settings.SkillPackageScopeBuiltin, Enabled: pkg.Enabled},
			ToolRefs:            pkg.ToolRefs,
		}
	}
	snapshot, err := BuildSnapshot(BuildInput{Registry: registry, PackageDetails: details})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"bar_chart": "charts", "rasterize_mesh_dataset": "mesh-analysis", "merge_vector_layers": "vector-geometry", "route_draw": "route-cartography", "route_points_build": "route-cartography", "import_layout_template": "print-layout", "export_print_layout": "print-layout"}
	if _, exists := snapshot.Tools["cartography_build_and_draw_from_tables"]; exists {
		t.Fatal("retired combined tool remains in capability catalog")
	}
	for tool, packageID := range want {
		if got := snapshot.Tools[tool].Package.PackageID; got != packageID {
			t.Fatalf("%s package=%q want=%q", tool, got, packageID)
		}
	}
}

func TestToolDiscoveryMetadataReachesCapabilityCatalog(t *testing.T) {
	root := t.TempDir()
	toolDir := filepath.Join(root, "tools")
	skillDir := filepath.Join(root, "skills")
	writeTestSkillFile(t, filepath.Join(toolDir, "sample.yaml"), `apiVersion: tool/v1
kind: Tool
metadata:
  name: sample
  display_name: 创建要素缓冲区
  keywords: [缓冲, 邻近分析]
  domain: vector
  runtime: qgis
spec:
  description: 按距离生成缓冲区
execution:
  backend_tool: native:buffer
`)
	writeTestSkillFile(t, filepath.Join(skillDir, "analysis", "SKILL.md"), "---\nname: analysis\ndescription: sample\nkind: category\ntools: [sample]\n---\n")
	tools := toolpkg.NewRegistry()
	if err := tools.LoadDirectories(toolDir); err != nil {
		t.Fatal(err)
	}
	registry := skill.NewRegistry(tools)
	if err := registry.LoadDirectories(skillDir); err != nil {
		t.Fatal(err)
	}
	snapshot, err := BuildSnapshot(BuildInput{Registry: registry})
	if err != nil {
		t.Fatal(err)
	}
	runtimeItems := snapshot.AsRuntimeCatalogItems()
	publicItems := snapshot.ProjectToolCatalog(nil, ToolCatalogConfigured)
	want := []string{"缓冲", "邻近分析"}
	if len(runtimeItems) != 1 || len(publicItems) != 1 || runtimeItems[0].DisplayName != "创建要素缓冲区" || publicItems[0].DisplayName != "创建要素缓冲区" || !reflect.DeepEqual(runtimeItems[0].Keywords, want) || !reflect.DeepEqual(publicItems[0].Keywords, want) {
		t.Fatalf("YAML-to-catalog discovery metadata lost: runtime=%#v public=%#v", runtimeItems, publicItems)
	}
	publicItems[0].Keywords[0] = "changed"
	if snapshot.Tools["sample"].Keywords[0] != "缓冲" {
		t.Fatal("public projection changed the capability snapshot")
	}
}

func TestToolPackageBindingUsesFormalCategoryOwnership(t *testing.T) {
	packages := map[string]PackageCapability{
		"a-method": {Key: "a-method", PackageID: "a-method", Kind: "workflow", ToolNames: []string{"sample"}, Scope: settings.SkillPackageScopeGlobal},
		"legacy":   {Key: "legacy", PackageID: "legacy", Kind: "category", Hidden: true, ToolNames: []string{"sample"}, Scope: settings.SkillPackageScopeGlobal},
		"formal":   {Key: "formal", PackageID: "formal", Kind: "category", ToolNames: []string{"sample"}, Scope: settings.SkillPackageScopeGlobal},
	}
	if got := resolveToolPackageBinding("sample", packages, "default").Package.PackageID; got != "formal" {
		t.Fatalf("expected visible category ownership, got %q", got)
	}
}

func TestBuildSnapshotReturnsEmptySnapshotForEmptyInput(t *testing.T) {
	snapshot, err := BuildSnapshot(BuildInput{})
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot to be non-nil")
	}
	if snapshot.ConfigName != "" {
		t.Fatalf("expected empty config name, got %q", snapshot.ConfigName)
	}
	if snapshot.Tools == nil {
		t.Fatal("expected Tools map to be initialized")
	}
	if len(snapshot.Tools) != 0 {
		t.Fatalf("expected empty Tools map, got %d entries", len(snapshot.Tools))
	}
	if snapshot.Packages == nil {
		t.Fatal("expected Packages map to be initialized")
	}
	if len(snapshot.Packages) != 0 {
		t.Fatalf("expected empty Packages map, got %d entries", len(snapshot.Packages))
	}
	if snapshot.ToolOrder == nil {
		t.Fatal("expected ToolOrder slice to be initialized")
	}
	if len(snapshot.ToolOrder) != 0 {
		t.Fatalf("expected empty ToolOrder, got %d entries", len(snapshot.ToolOrder))
	}
	if snapshot.Agents == nil {
		t.Fatal("expected Agents slice to be initialized")
	}
	if len(snapshot.Agents) != 0 {
		t.Fatalf("expected empty Agents slice, got %d entries", len(snapshot.Agents))
	}
	if snapshot.BuiltAt.IsZero() {
		t.Fatal("expected BuiltAt to be set")
	}
	if snapshot.RuntimeRevision != "" || snapshot.ToolCatalogRevision != "" || snapshot.SchemaRevision != "" || snapshot.AvailabilityRevision != "" {
		t.Fatalf("expected empty revisions for empty snapshot, got %#v", snapshot)
	}
}

func TestBuildSnapshotCompilesRegistryToolsAndPackageBindings(t *testing.T) {
	registry := loadTestRegistry(t, map[string]map[string]string{
		"analysis-global": {
			"buffer": "缓冲区分析",
		},
	})

	snapshot, err := BuildSnapshot(BuildInput{
		ActiveConfigName: "default",
		Registry:         registry,
		ToolAggregatorSnapshot: toolrouter.Snapshot{
			Compute: []compute.ToolDefinition{{
				Name:        "buffer",
				Description: "缓冲区分析",
				Category:    "vector",
				Backend:     skill.RuntimeIDCLI,
				Available:   true,
			}},
		},
		PackageSummaries: []settings.SkillPackageSummary{{
			PackageID:   "analysis-global",
			Name:        "analysis-global",
			Kind:        "category",
			Backend:     "compute",
			Source:      "global",
			Enabled:     true,
			Scope:       settings.SkillPackageScopeGlobal,
			OwnerConfig: "",
			Path:        "/tmp/skills/analysis-global",
			HasTools:    true,
		}},
		PackageDetails: map[string]settings.SkillPackageDetail{
			buildPackageKey(settings.SkillPackageScopeGlobal, "", "analysis-global"): {
				SkillPackageSummary: settings.SkillPackageSummary{
					PackageID:   "analysis-global",
					Name:        "analysis-global",
					Kind:        "category",
					Backend:     "compute",
					Source:      "global",
					Enabled:     true,
					Scope:       settings.SkillPackageScopeGlobal,
					OwnerConfig: "",
					Path:        "/tmp/skills/analysis-global",
					HasTools:    true,
				},
				ToolRefs: []string{"buffer"},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}

	tool, ok := snapshot.Tools["buffer"]
	if !ok {
		t.Fatalf("expected tool buffer in snapshot: %#v", snapshot.Tools)
	}
	if tool.Description != "缓冲区分析" {
		t.Fatalf("unexpected tool description: %#v", tool)
	}
	if tool.DisplayName != "缓冲区" {
		t.Fatalf("unexpected tool display name: %#v", tool)
	}
	if tool.Category != "vector" || tool.Backend != skill.RuntimeIDCLI {
		t.Fatalf("unexpected tool classification: %#v", tool)
	}
	if tool.ProviderID != skill.RuntimeIDCLI || tool.Source != skill.RuntimeIDCLI {
		t.Fatalf("unexpected provider identity: %#v", tool)
	}
	if !tool.Available {
		t.Fatalf("expected registry-compiled tool to be available by default: %#v", tool)
	}
	if tool.Package.Key != "global::analysis-global" {
		t.Fatalf("unexpected package binding: %#v", tool.Package)
	}
	if len(snapshot.ToolOrder) != 1 || snapshot.ToolOrder[0] != "buffer" {
		t.Fatalf("unexpected tool order: %#v", snapshot.ToolOrder)
	}
	if pkg, ok := snapshot.Packages["global::analysis-global"]; !ok || len(pkg.ToolNames) != 1 || pkg.ToolNames[0] != "buffer" {
		t.Fatalf("unexpected package projection: %#v", snapshot.Packages)
	}
	if snapshot.SchemaRevision == "" || snapshot.AvailabilityRevision == "" || snapshot.ToolCatalogRevision == "" || snapshot.RuntimeRevision == "" {
		t.Fatalf("expected snapshot revisions to be populated: %#v", snapshot)
	}
	if snapshot.ToolCatalogRevision != snapshot.SchemaRevision+"/"+snapshot.AvailabilityRevision {
		t.Fatalf("unexpected tool catalog revision: %#v", snapshot)
	}
}

func TestBuildSnapshotPrefersExclusivePackageBindingForSameToolName(t *testing.T) {
	registry := loadTestRegistry(t, map[string]map[string]string{
		"analysis-global": {
			"buffer": "缓冲区分析",
		},
	})

	snapshot, err := BuildSnapshot(BuildInput{
		ActiveConfigName: "default",
		Registry:         registry,
		PackageDetails: map[string]settings.SkillPackageDetail{
			buildPackageKey(settings.SkillPackageScopeGlobal, "", "analysis-global"): {
				SkillPackageSummary: settings.SkillPackageSummary{PackageID: "analysis-global", Name: "analysis-global", Kind: "category", Scope: settings.SkillPackageScopeGlobal, Enabled: true},
				ToolRefs:            []string{"buffer"},
			},
			buildPackageKey(settings.SkillPackageScopeExclusive, "default", "analysis-exclusive"): {
				SkillPackageSummary: settings.SkillPackageSummary{PackageID: "analysis-exclusive", Name: "analysis-exclusive", Kind: "category", Scope: settings.SkillPackageScopeExclusive, OwnerConfig: "default", Enabled: true},
				ToolRefs:            []string{"buffer"},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}

	tool := snapshot.Tools["buffer"]
	if tool.Package.Key != "exclusive:default:analysis-exclusive" {
		t.Fatalf("expected exclusive package binding, got %#v", tool.Package)
	}
}

func TestBuildSnapshotUsesDeterministicBindingForSamePriorityPackages(t *testing.T) {
	registry := loadTestRegistry(t, map[string]map[string]string{
		"analysis-alpha": {"buffer": "缓冲区分析"},
	})
	snapshot, err := BuildSnapshot(BuildInput{
		Registry: registry,
		ToolAggregatorSnapshot: toolrouter.Snapshot{Compute: []compute.ToolDefinition{{
			Name:      "buffer",
			Backend:   skill.RuntimeIDCLI,
			Available: true,
		}}},
		PackageDetails: map[string]settings.SkillPackageDetail{
			buildPackageKey(settings.SkillPackageScopeGlobal, "", "analysis-zeta"): {
				SkillPackageSummary: settings.SkillPackageSummary{PackageID: "analysis-zeta", Name: "analysis-zeta", Kind: "category", Scope: settings.SkillPackageScopeGlobal, Enabled: true},
				ToolRefs:            []string{"buffer"},
			},
			buildPackageKey(settings.SkillPackageScopeGlobal, "", "analysis-alpha"): {
				SkillPackageSummary: settings.SkillPackageSummary{PackageID: "analysis-alpha", Name: "analysis-alpha", Kind: "category", Scope: settings.SkillPackageScopeGlobal, Enabled: true},
				ToolRefs:            []string{"buffer"},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}
	if got := snapshot.Tools["buffer"].Package.PackageID; got != "analysis-alpha" {
		t.Fatalf("expected deterministic lowest-key package binding, got %q", got)
	}
}

func TestBuildSnapshotUsesToolRefsWhenPackageDetailOmitsEmbeddedTools(t *testing.T) {
	registry := loadTestRegistry(t, map[string]map[string]string{
		"analysis-global": {
			"buffer": "缓冲区分析",
		},
	})

	snapshot, err := BuildSnapshot(BuildInput{
		ActiveConfigName: "default",
		Registry:         registry,
		ToolAggregatorSnapshot: toolrouter.Snapshot{Compute: []compute.ToolDefinition{{
			Name:        "buffer",
			Description: "缓冲区分析",
			Category:    "vector",
			Backend:     skill.RuntimeIDCLI,
			Available:   true,
		}},
		},
		PackageSummaries: []settings.SkillPackageSummary{{
			PackageID:   "analysis-global",
			Name:        "analysis-global",
			Kind:        "category",
			Backend:     "compute",
			Source:      "global",
			Enabled:     true,
			Scope:       settings.SkillPackageScopeGlobal,
			OwnerConfig: "",
			Path:        "/tmp/skills/analysis-global",
			HasTools:    true,
		}},
		PackageDetails: map[string]settings.SkillPackageDetail{
			buildPackageKey(settings.SkillPackageScopeGlobal, "", "analysis-global"): {
				SkillPackageSummary: settings.SkillPackageSummary{
					PackageID:   "analysis-global",
					Name:        "analysis-global",
					Kind:        "category",
					Backend:     "compute",
					Source:      "global",
					Enabled:     true,
					Scope:       settings.SkillPackageScopeGlobal,
					OwnerConfig: "",
					Path:        "/tmp/skills/analysis-global",
					HasTools:    true,
				},
				FrontMatter: skill.SkillFrontMatter{Tools: []string{"buffer"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}
	if pkg, ok := snapshot.Packages["global::analysis-global"]; !ok || len(pkg.ToolNames) != 1 || pkg.ToolNames[0] != "buffer" {
		t.Fatalf("expected package projection from tool refs, got %#v", snapshot.Packages)
	}
	if tool := snapshot.Tools["buffer"]; tool.Package.PackageID != "analysis-global" {
		t.Fatalf("expected tool binding from tool refs, got %#v", tool)
	}
}

func TestBuildSnapshotIgnoresEmbeddedToolsWithoutToolRefs(t *testing.T) {
	registry := loadTestRegistry(t, map[string]map[string]string{
		"analysis-global": {
			"buffer": "缓冲区分析",
		},
	})

	snapshot, err := BuildSnapshot(BuildInput{
		Registry: registry,
		PackageDetails: map[string]settings.SkillPackageDetail{
			buildPackageKey(settings.SkillPackageScopeGlobal, "", "analysis-global"): {
				SkillPackageSummary: settings.SkillPackageSummary{PackageID: "analysis-global", Name: "analysis-global", Kind: "category", Scope: settings.SkillPackageScopeGlobal, Enabled: true},
				Tools:               []skill.SkillDefinition{{Metadata: skill.SkillMetadata{Name: "buffer", Backend: skill.RuntimeIDCLI}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}
	if pkg := snapshot.Packages["global::analysis-global"]; len(pkg.ToolNames) != 0 {
		t.Fatalf("expected package projection to ignore embedded tools without refs, got %#v", pkg)
	}
	if tool := snapshot.Tools["buffer"]; tool.Package.PackageID != "" {
		t.Fatalf("expected embedded tools without refs not to bind package, got %#v", tool)
	}
}

func TestBuildSnapshotAppliesComputeAvailabilityHints(t *testing.T) {
	registry := loadTestRegistry(t, map[string]map[string]string{
		"analysis-global": {
			"buffer": "缓冲区分析",
		},
	})

	pendingSnapshot, err := BuildSnapshot(BuildInput{
		Registry:     registry,
		ComputeReady: true,
	})
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}
	if tool := pendingSnapshot.Tools["buffer"]; tool.Available || tool.UnavailableReason != toolcatalog.MissingReasonComputeCatalogPending {
		t.Fatalf("expected compute_catalog_pending, got %#v", tool)
	}

	unavailableSnapshot, err := BuildSnapshot(BuildInput{
		Registry:     registry,
		ComputeReady: false,
	})
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}
	if tool := unavailableSnapshot.Tools["buffer"]; tool.Available || tool.UnavailableReason != toolcatalog.MissingReasonComputeUnavailable {
		t.Fatalf("expected compute_unavailable, got %#v", tool)
	}

	readySnapshot, err := BuildSnapshot(BuildInput{
		Registry: registry,
		ToolAggregatorSnapshot: toolrouter.Snapshot{
			Compute: []compute.ToolDefinition{{
				Name:        "buffer",
				Description: "缓冲区分析",
				Category:    "vector",
				Backend:     skill.RuntimeIDCLI,
				Available:   true,
			}},
		},
		ComputeReady: false,
	})
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}
	if tool := readySnapshot.Tools["buffer"]; !tool.Available || tool.UnavailableReason != "" {
		t.Fatalf("expected compute tool to be available from runtime snapshot, got %#v", tool)
	}
}

func TestBuildSnapshotTreatsRegisteredLocalToolsAsAvailable(t *testing.T) {
	skillRoot := t.TempDir()
	toolRoot := t.TempDir()
	writeTestSkillFile(t, filepath.Join(skillRoot, "local-tools", "SKILL.md"), `---
name: local-tools
description: local tools
kind: category
tools:
  - create_directory
  - load_review_images
---
`)
	writeTestSkillFile(t, filepath.Join(toolRoot, "system", "local-tools", "create_directory.yaml"), `apiVersion: tool/v1
kind: Tool
metadata:
  name: create_directory
  runtime: internal
spec:
  description: Create a directory.
execution:
  backend_tool: create_directory
`)
	writeTestSkillFile(t, filepath.Join(toolRoot, "system", "local-tools", "load_review_images.yaml"), `apiVersion: tool/v1
kind: Tool
metadata:
  name: load_review_images
  runtime: cli
spec:
  description: Load review images.
execution:
  cli:
    command: python3
    args:
      - review.py
    stdin: json
    stdout: json
`)

	toolRegistry := toolpkg.NewRegistry()
	if err := toolRegistry.LoadDirectories(toolRoot); err != nil {
		t.Fatalf("LoadDirectories(toolRoot) failed: %v", err)
	}
	registry := skill.NewRegistry(toolRegistry)
	if err := registry.LoadDirectories(skillRoot); err != nil {
		t.Fatalf("LoadDirectories(skillRoot) failed: %v", err)
	}

	snapshot, err := BuildSnapshot(BuildInput{
		Registry:     registry,
		ComputeReady: true,
		RuntimeSnapshot: middlewareclient.RuntimeSnapshot{Services: map[string]middlewareclient.ServiceStatus{
			skill.RuntimeIDCLI: {
				ID:            skill.RuntimeIDCLI,
				Enabled:       true,
				ProviderCount: 0,
			},
		}},
	})
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}
	for _, toolName := range []string{"create_directory", "load_review_images"} {
		tool := snapshot.Tools[toolName]
		if !tool.Available || tool.UnavailableReason != "" {
			t.Fatalf("expected local tool %s to be available, got %#v", toolName, tool)
		}
	}
}

func TestBuildSnapshotAppliesInteractiveAvailabilityHints(t *testing.T) {
	registry := loadTestRegistry(t, map[string]map[string]string{
		"interactive-global": {
			"flash_feature": "高亮要素",
		},
	})

	snapshot, err := BuildSnapshot(BuildInput{
		Registry: registry,
		RuntimeSnapshot: middlewareclient.RuntimeSnapshot{
			Instances: middlewareclient.InstancesSnapshot{},
		},
	})
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}
	if tool := snapshot.Tools["flash_feature"]; tool.Available || tool.UnavailableReason != toolcatalog.MissingReasonInteractiveDisconnected {
		t.Fatalf("expected interactive_instance_disconnected, got %#v", tool)
	}

	runningSnapshot, err := BuildSnapshot(BuildInput{
		Registry: registry,
		RuntimeSnapshot: middlewareclient.RuntimeSnapshot{
			Instances: middlewareclient.InstancesSnapshot{
				Items: []*discovery.QGISInstance{{ID: "inst-a", Status: "running"}, {ID: "inst-b", Status: "running"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}
	tool := runningSnapshot.Tools["flash_feature"]
	if !tool.Available || tool.UnavailableReason != "" || len(tool.InstanceIDs) != 2 {
		t.Fatalf("expected qgis runtime availability semantics, got %#v", tool)
	}
}

func TestBuildSnapshotRecoversFromStaleInteractiveDisconnectedAggregatorState(t *testing.T) {
	registry := loadTestRegistry(t, map[string]map[string]string{
		"interactive-global": {
			"flash_feature": "高亮要素",
		},
	})

	snapshot, err := BuildSnapshot(BuildInput{
		Registry: registry,
		ToolAggregatorSnapshot: toolrouter.Snapshot{
			Interactive: []toolrouter.InteractiveToolDefinition{{
				Name:              "flash_feature",
				Available:         false,
				UnavailableReason: toolcatalog.MissingReasonInteractiveDisconnected,
			}},
		},
		RuntimeSnapshot: middlewareclient.RuntimeSnapshot{
			Instances: middlewareclient.InstancesSnapshot{
				Items: []*discovery.QGISInstance{{ID: "inst-a", Status: "running"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}

	tool := snapshot.Tools["flash_feature"]
	if !tool.Available || tool.UnavailableReason != "" {
		t.Fatalf("expected running instance to recover stale interactive disconnected state, got %#v", tool)
	}
	if len(tool.InstanceIDs) != 1 || tool.InstanceIDs[0] != "inst-a" {
		t.Fatalf("expected running instance ids, got %#v", tool.InstanceIDs)
	}
}

func TestBuildSnapshotRuntimeProjectionUsesRuntimeSemantics(t *testing.T) {
	registry := loadTestRegistry(t, map[string]map[string]string{
		"analysis-global": {
			"buffer": "缓冲区分析",
		},
		"interactive-global": {
			"flash_feature": "高亮要素",
		},
	})
	runtimeSnapshot := middlewareclient.RuntimeSnapshot{
		Instances: middlewareclient.InstancesSnapshot{
			Items: []*discovery.QGISInstance{{ID: "inst-b", Status: "running"}, {ID: "inst-a", Status: "running"}},
		},
	}
	aggregatorSnapshot := toolrouter.Snapshot{
		Compute: []compute.ToolDefinition{{
			Name:        "buffer",
			Description: "缓冲区分析",
			Category:    "vector",
			Backend:     skill.RuntimeIDCLI,
			Available:   true,
		}},
	}

	snapshot, err := BuildSnapshot(BuildInput{
		Registry:               registry,
		ToolAggregatorSnapshot: aggregatorSnapshot,
		RuntimeSnapshot:        runtimeSnapshot,
		ComputeReady:           false,
	})
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}
	projected := snapshot.AsRuntimeCatalogItems()
	if len(projected) != 2 {
		t.Fatalf("unexpected projected catalog length: got=%d want=2", len(projected))
	}
	byName := map[string]middlewareclient.ToolCatalogItem{}
	for _, item := range projected {
		byName[item.Name] = item
	}
	if item := byName["buffer"]; item.Backend != skill.RuntimeIDCLI || item.Source != skill.RuntimeIDCLI {
		t.Fatalf("expected buffer to project cli runtime semantics, got %#v", item)
	}
	if item := byName["flash_feature"]; item.Backend != skill.RuntimeIDQGIS || item.Source != skill.RuntimeIDQGIS {
		t.Fatalf("expected flash_feature to project qgis runtime semantics, got %#v", item)
	}
}

func TestBuildSnapshotBuildsAgentCapabilityViewsFromRuntimeConfig(t *testing.T) {
	registry := loadTestRegistry(t, map[string]map[string]string{
		"analysis-global": {
			"buffer": "缓冲区分析",
		},
		"exclusive-tools": {
			"clip": "裁剪分析",
		},
	})
	snapshot, err := BuildSnapshot(BuildInput{
		ActiveConfigName: "default",
		ActiveConfig: &config.AgentConfigFile{
			Runtime: &config.RuntimeConfig{
				Global: config.GlobalAgentConfig{SkillPackages: []string{"analysis-global"}},
				Agents: map[string]config.AgentDefData{
					"orchestrator": {
						DisplayName:   "编排智能体",
						Description:   "负责工具编排",
						IsMain:        true,
						Enabled:       true,
						SkillPackages: []string{"exclusive-tools"},
					},
				},
			},
		},
		RuntimeSkillConfig: &subagents.RuntimeSkillConfig{
			Global: subagents.RuntimeSkillGlobalConfig{
				SkillPackages: []string{"analysis-global"},
				PackageTools: map[string][]string{
					"analysis-global": {"buffer"},
					"exclusive-tools": {"clip"},
				},
			},
			Agents: map[string]subagents.RuntimeSkillAgentConfig{
				"orchestrator": {
					DisplayName:   "编排智能体",
					Description:   "负责工具编排",
					IsMain:        true,
					Enabled:       true,
					SkillPackages: []string{"exclusive-tools"},
				},
			},
		},
		Registry: registry,
		ToolAggregatorSnapshot: toolrouter.Snapshot{Compute: []compute.ToolDefinition{
			{Name: "buffer", Description: "缓冲区分析", Category: "vector", Backend: skill.RuntimeIDCLI, Available: true},
			{Name: "clip", Description: "裁剪分析", Category: "vector", Backend: skill.RuntimeIDCLI, Available: true},
		}},
		PackageDetails: map[string]settings.SkillPackageDetail{
			buildPackageKey(settings.SkillPackageScopeGlobal, "", "analysis-global"): {
				SkillPackageSummary: settings.SkillPackageSummary{PackageID: "analysis-global", Name: "analysis-global", Kind: "category", Scope: settings.SkillPackageScopeGlobal, Enabled: true},
				ToolRefs:            []string{"buffer"},
			},
			buildPackageKey(settings.SkillPackageScopeExclusive, "default", "exclusive-tools"): {
				SkillPackageSummary: settings.SkillPackageSummary{PackageID: "exclusive-tools", Name: "exclusive-tools", Scope: settings.SkillPackageScopeExclusive, OwnerConfig: "default", Enabled: true},
				ToolRefs:            []string{"clip"},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}
	if len(snapshot.Agents) != 1 {
		t.Fatalf("expected one agent capability view, got %#v", snapshot.Agents)
	}
	agent := snapshot.Agents[0]
	if agent.AgentName != "orchestrator" || !agent.IsMain || !agent.Enabled {
		t.Fatalf("unexpected agent projection: %#v", agent)
	}
	if len(agent.SkillPackages) != 2 || agent.SkillPackages[0] != "analysis-global" || agent.SkillPackages[1] != "exclusive-tools" {
		t.Fatalf("unexpected agent skill packages: %#v", agent.SkillPackages)
	}
	if len(agent.Tools) != 2 || agent.Tools[0].Name != "buffer" || agent.Tools[1].Name != "clip" {
		t.Fatalf("unexpected agent tool refs: %#v", agent.Tools)
	}
	payload := snapshot.BuildCapabilityPayload("orchestrator", "", nil)
	agents, ok := payload["agents"].([]map[string]any)
	if !ok || len(agents) != 1 {
		t.Fatalf("unexpected payload agents: %#v", payload)
	}
	tools, ok := agents[0]["tools"].([]map[string]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("unexpected payload tools: %#v", agents)
	}
}

func loadTestRegistry(t *testing.T, packages map[string]map[string]string) *skill.Registry {
	t.Helper()
	skillRoot := t.TempDir()
	toolRoot := t.TempDir()
	for packageName, tools := range packages {
		toolNames := make([]string, 0, len(tools))
		for toolName := range tools {
			toolNames = append(toolNames, toolName)
		}
		sort.Strings(toolNames)
		writeTestSkillFile(t, filepath.Join(skillRoot, packageName, "SKILL.md"), "---\nname: "+packageName+"\ndescription: test package\nkind: category\nbackend: compute\ntools:\n  - "+strings.Join(toolNames, "\n  - ")+"\n---\n")
		for toolName, description := range tools {
			backend := "compute"
			if toolName == "flash_feature" {
				backend = "bridge"
			}
			runtime := skill.RuntimeIDCLI
			domain := "vector"
			if backend == "bridge" {
				runtime = skill.RuntimeIDQGIS
				domain = "interactive"
			}
			writeTestSkillFile(t, filepath.Join(toolRoot, "system", packageName, toolName+".yaml"), "apiVersion: tool/v1\nkind: Tool\nmetadata:\n  name: "+toolName+"\n  display_name: 缓冲区\n  domain: "+domain+"\n  runtime: "+runtime+"\nspec:\n  description: "+description+"\n  parameters:\n    - name: INPUT\n      type: string\n      required: true\nexecution:\n  backend_tool: "+toolName+"\n  param_mapping:\n    INPUT: '{{ INPUT }}'\n")
		}
	}
	toolReg := toolpkg.NewRegistry()
	if err := toolReg.LoadDirectories(toolRoot); err != nil {
		t.Fatalf("LoadDirectories(toolRoot) failed: %v", err)
	}
	registry := skill.NewRegistry(toolReg)
	if err := registry.LoadDirectories(skillRoot); err != nil {
		t.Fatalf("LoadDirectories failed: %v", err)
	}
	return registry
}

func writeTestSkillFile(t *testing.T, path, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}
