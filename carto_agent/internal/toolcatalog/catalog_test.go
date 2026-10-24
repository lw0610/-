package toolcatalog

import (
	"reflect"
	"testing"

	"github.com/autocarto/carto_agent/internal/compute"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/autocarto/carto_agent/internal/toolrouter"
)

func TestChineseDiscoveryMetadataSurvivesRuntimeProjection(t *testing.T) {
	snapshot := toolrouter.Snapshot{Interactive: []toolrouter.InteractiveToolDefinition{{
		Name: "sample", DisplayName: "创建要素缓冲区", Description: "按距离生成缓冲区", Keywords: []string{"邻近分析", "缓冲"}, Available: true,
	}}}
	items := BuildToolInfos([]string{"sample"}, snapshot)
	if len(items) != 1 || items[0].DisplayName != "创建要素缓冲区" || !reflect.DeepEqual(items[0].Keywords, []string{"邻近分析", "缓冲"}) {
		t.Fatalf("runtime projection lost discovery metadata: %#v", items)
	}
	items[0].Keywords[0] = "changed"
	if snapshot.Interactive[0].Keywords[0] != "邻近分析" {
		t.Fatal("projection must isolate keyword slices")
	}
}

func TestDiscoveryMetadataChangesUpdateSchemaRevision(t *testing.T) {
	baseline := []middlewareclient.ToolCatalogItem{{Name: "sample", DisplayName: "创建要素缓冲区", Keywords: []string{"邻近分析", "缓冲"}, Available: true}}
	for _, variant := range []middlewareclient.ToolCatalogItem{
		{Name: "sample", DisplayName: "创建几何缓冲区", Keywords: []string{"邻近分析", "缓冲"}, Available: true},
		{Name: "sample", DisplayName: "创建要素缓冲区", Keywords: []string{"距离分析"}, Available: true},
	} {
		diff := DiffTools(baseline, []middlewareclient.ToolCatalogItem{variant})
		if !diff.SchemaChanged || diff.AvailabilityChanged {
			t.Fatalf("discovery metadata changes must invalidate schema only: %#v", diff)
		}
	}
	reordered := []middlewareclient.ToolCatalogItem{{Name: "sample", DisplayName: "创建要素缓冲区", Keywords: []string{"缓冲", " 邻近分析 ", "缓冲"}, Available: true}}
	if DiffTools(baseline, reordered).Changed {
		t.Fatal("keyword ordering, whitespace and duplicates must not change the revision")
	}
}

func assertRuntimeCatalogParity(t *testing.T, baseline, candidate []middlewareclient.ToolCatalogItem, toleratedDiffs ...string) {
	t.Helper()
	allowed := make(map[string]struct{}, len(toleratedDiffs))
	for _, diff := range toleratedDiffs {
		allowed[diff] = struct{}{}
	}
	allow := func(key string) bool {
		_, ok := allowed[key]
		return ok
	}

	if got, want := ToolSchemaRevision(candidate), ToolSchemaRevision(baseline); got != want && !allow("schema_revision") {
		t.Fatalf("unexpected schema revision drift: baseline=%q candidate=%q", want, got)
	}
	if got, want := ToolAvailabilityRevision(candidate), ToolAvailabilityRevision(baseline); got != want && !allow("availability_revision") {
		t.Fatalf("unexpected availability revision drift: baseline=%q candidate=%q", want, got)
	}

	diff := DiffTools(baseline, candidate)
	if diff.SchemaChanged && !allow("schema_diff") {
		t.Fatalf("unexpected schema diff: %#v", diff)
	}
	if diff.AvailabilityChanged && !allow("availability_diff") {
		t.Fatalf("unexpected availability diff: %#v", diff)
	}
}

func TestBuildToolInfosIncludesRuntimeAvailability(t *testing.T) {
	tools := []string{"test_compute_tool", "test_interactive_tool"}
	snapshot := toolrouter.Snapshot{
		Compute: []compute.ToolDefinition{{
			Name:        "test_compute_tool",
			Description: "compute desc",
			Category:    "compute",
		}},
		Interactive: []toolrouter.InteractiveToolDefinition{{
			Name:        "test_interactive_tool",
			Description: "interactive desc",
			Category:    "interactive",
		}},
	}

	infos := BuildToolInfos(tools, snapshot)
	if len(infos) != 2 {
		t.Fatalf("expected 2 infos, got %d", len(infos))
	}

	byName := map[string]Item{}
	for _, item := range infos {
		byName[item.Name] = item
	}

	computeInfo := byName["test_compute_tool"]
	if !computeInfo.Available {
		t.Fatal("expected test_compute_tool available=true")
	}
	if computeInfo.Source != "cli" {
		t.Fatalf("unexpected compute source: %s", computeInfo.Source)
	}
	if computeInfo.MissingReason != "" {
		t.Fatalf("expected empty missing reason for available compute tool, got=%s", computeInfo.MissingReason)
	}

	interactiveInfo := byName["test_interactive_tool"]
	if !interactiveInfo.Available {
		t.Fatal("expected test_interactive_tool available=true")
	}
	if interactiveInfo.Source != "qgis" {
		t.Fatalf("unexpected interactive source: %s", interactiveInfo.Source)
	}
	if interactiveInfo.MissingReason != "" {
		t.Fatalf("expected empty missing reason for available interactive tool, got=%s", interactiveInfo.MissingReason)
	}
}

func TestBuildToolInfosClassifiesUnavailableReasons(t *testing.T) {
	tools := []string{"pyqgis_get_context", "compute_missing_tool"}
	infos := BuildToolInfos(tools, toolrouter.Snapshot{})
	if len(infos) != 2 {
		t.Fatalf("expected 2 infos, got %d", len(infos))
	}

	byName := map[string]Item{}
	for _, item := range infos {
		byName[item.Name] = item
	}

	pyqgis := byName["pyqgis_get_context"]
	if pyqgis.Available {
		t.Fatal("expected pyqgis_get_context available=false")
	}
	if pyqgis.Source != "" {
		t.Fatalf("unexpected pyqgis source: %s", pyqgis.Source)
	}
	if pyqgis.MissingReason != MissingReasonRuntimeCatalogMissing {
		t.Fatalf("unexpected pyqgis missing reason: %s", pyqgis.MissingReason)
	}

	computeMissing := byName["compute_missing_tool"]
	if computeMissing.Available {
		t.Fatal("expected compute_missing_tool available=false")
	}
	if computeMissing.Source != "" {
		t.Fatalf("unexpected compute source: %s", computeMissing.Source)
	}
	if computeMissing.MissingReason != MissingReasonRuntimeCatalogMissing {
		t.Fatalf("unexpected compute missing reason: %s", computeMissing.MissingReason)
	}
}

func TestBuildToolInfosSkipsBlankToolNamesWithoutGuessingSource(t *testing.T) {
	infos := BuildToolInfosWithHints([]string{"", "   "}, toolrouter.Snapshot{}, AvailabilityHints{ComputeReady: true})
	if len(infos) != 0 {
		t.Fatalf("expected blank tool names to be skipped, got %#v", infos)
	}

	if got := inferUnavailableSource("", toolrouter.NewDefaultClassificationRegistry(), AvailabilityHints{ComputeReady: true}); got != "" {
		t.Fatalf("expected empty name source to remain blank, got %q", got)
	}
}

func TestBuildToolInfosPreservesRuntimeUnavailableState(t *testing.T) {
	tools := []string{"runtime_compute_tool", "runtime_interactive_tool"}
	snapshot := toolrouter.Snapshot{
		Compute: []compute.ToolDefinition{{
			Name:              "runtime_compute_tool",
			Description:       "compute desc",
			Category:          "compute",
			Available:         false,
			UnavailableReason: "compute_catalog_pending",
		}},
		Interactive: []toolrouter.InteractiveToolDefinition{{
			Name:              "runtime_interactive_tool",
			Description:       "interactive desc",
			Category:          "interactive",
			Available:         false,
			UnavailableReason: "interactive_instance_disconnected",
		}},
	}

	infos := BuildToolInfosWithHints(tools, snapshot, AvailabilityHints{ComputeReady: true})
	if len(infos) != 2 {
		t.Fatalf("expected 2 infos, got %d", len(infos))
	}

	byName := map[string]Item{}
	for _, item := range infos {
		byName[item.Name] = item
	}

	computeInfo := byName["runtime_compute_tool"]
	if computeInfo.Available {
		t.Fatal("expected runtime_compute_tool available=false")
	}
	if computeInfo.MissingReason != MissingReasonComputeCatalogPending {
		t.Fatalf("unexpected compute missing reason: %s", computeInfo.MissingReason)
	}

	interactiveInfo := byName["runtime_interactive_tool"]
	if interactiveInfo.Available {
		t.Fatal("expected runtime_interactive_tool available=false")
	}
	if interactiveInfo.MissingReason != MissingReasonInteractiveDisconnected {
		t.Fatalf("unexpected interactive missing reason: %s", interactiveInfo.MissingReason)
	}
}

func TestBuildToolInfosMarksComputePendingWhenServiceIsReady(t *testing.T) {
	tools := []string{"compute_missing_tool"}
	infos := BuildToolInfosWithHints(tools, toolrouter.Snapshot{}, AvailabilityHints{ComputeReady: true})
	if len(infos) != 1 {
		t.Fatalf("expected 1 info, got %d", len(infos))
	}
	if infos[0].MissingReason != MissingReasonRuntimeCatalogMissing {
		t.Fatalf("unexpected compute pending reason: %s", infos[0].MissingReason)
	}
}

func TestBuildToolInfosPrefersComputeOverridesForUnavailableExports(t *testing.T) {
	tools := []string{"export_layout_pdf", "output_init_dir"}
	infos := BuildToolInfosWithHints(tools, toolrouter.Snapshot{}, AvailabilityHints{ComputeReady: true})
	if len(infos) != 2 {
		t.Fatalf("expected 2 infos, got %d", len(infos))
	}

	byName := map[string]Item{}
	for _, item := range infos {
		byName[item.Name] = item
	}
	if byName["export_layout_pdf"].Source != SourceInteractive {
		t.Fatalf("expected export_layout_pdf source=interactive, got=%s", byName["export_layout_pdf"].Source)
	}
	if byName["export_layout_pdf"].MissingReason != MissingReasonRuntimeCatalogMissing {
		t.Fatalf("expected export_layout_pdf interactive disconnected, got=%s", byName["export_layout_pdf"].MissingReason)
	}
	if byName["output_init_dir"].Source != SourceCompute {
		t.Fatalf("expected output_init_dir source=compute, got=%s", byName["output_init_dir"].Source)
	}
	if byName["output_init_dir"].MissingReason != MissingReasonRuntimeCatalogMissing {
		t.Fatalf("expected output_init_dir pending compute catalog, got=%s", byName["output_init_dir"].MissingReason)
	}
}

func TestBuildToolInfosUsesLegacyPrefixesOnlyAsFallback(t *testing.T) {
	infos := BuildToolInfosWithHints(
		[]string{"qgis_bridge_unknown_tool", "legacy_compute_unknown_tool"},
		toolrouter.Snapshot{},
		AvailabilityHints{ComputeReady: true},
	)
	if len(infos) != 2 {
		t.Fatalf("expected 2 infos, got %d", len(infos))
	}

	byName := map[string]Item{}
	for _, item := range infos {
		byName[item.Name] = item
	}
	if got := byName["qgis_bridge_unknown_tool"].Source; got != "" {
		t.Fatalf("unexpected qgis_bridge source: %s", got)
	}
	if got := byName["qgis_bridge_unknown_tool"].MissingReason; got != MissingReasonRuntimeCatalogMissing {
		t.Fatalf("unexpected qgis_bridge missing reason: %s", got)
	}
	if got := byName["legacy_compute_unknown_tool"].Source; got != "" {
		t.Fatalf("unexpected legacy_compute source: %s", got)
	}
	if got := byName["legacy_compute_unknown_tool"].MissingReason; got != MissingReasonRuntimeCatalogMissing {
		t.Fatalf("unexpected legacy_compute missing reason: %s", got)
	}
}

func TestBuildToolInfosDoesNotInferCanonicalSourceFromLegacyPrefixesWithoutRuntimeMetadata(t *testing.T) {
	infos := BuildToolInfosWithHints(
		[]string{"legacy_compute_export_layout_pdf", "qgis_bridge_output_init_dir"},
		toolrouter.Snapshot{},
		AvailabilityHints{ComputeReady: true},
	)
	if len(infos) != 2 {
		t.Fatalf("expected 2 infos, got %d", len(infos))
	}

	byName := map[string]Item{}
	for _, item := range infos {
		byName[item.Name] = item
	}
	if got := byName["legacy_compute_export_layout_pdf"].Source; got != "" {
		t.Fatalf("expected legacy_compute-prefixed tool to avoid canonical source inference without runtime metadata, got=%s", got)
	}
	if got := byName["legacy_compute_export_layout_pdf"].MissingReason; got != MissingReasonRuntimeCatalogMissing {
		t.Fatalf("unexpected legacy_compute export missing reason: %s", got)
	}
	if got := byName["qgis_bridge_output_init_dir"].Source; got != "" {
		t.Fatalf("expected qgis_bridge-prefixed tool to avoid canonical source inference without runtime metadata, got=%s", got)
	}
	if got := byName["qgis_bridge_output_init_dir"].MissingReason; got != MissingReasonRuntimeCatalogMissing {
		t.Fatalf("unexpected qgis_bridge output missing reason: %s", got)
	}
}

func TestBuildToolInfosCatalogRevisionIsStableAcrossRuntimeOrdering(t *testing.T) {
	t.Parallel()
	baseline := []middlewareclient.ToolCatalogItem{
		{
			Name:        "buffer",
			ProviderID:  skill.RuntimeIDCLI,
			Source:      skill.RuntimeIDCLI,
			Backend:     skill.RuntimeIDCLI,
			Description: "buffer tool",
			Category:    "vector",
			InputSchema: map[string]any{"type": "object", "title": "buffer-v1"},
			Available:   true,
		},
		{
			Name:        "open_project",
			ProviderID:  skill.RuntimeIDQGIS,
			Source:      skill.RuntimeIDQGIS,
			Backend:     skill.RuntimeIDQGIS,
			Description: "open project",
			Category:    "project",
			InputSchema: map[string]any{"type": "object", "title": "open-project-v1"},
			Available:   true,
			InstanceIDs: []string{"inst-a"},
		},
	}
	reordered := []middlewareclient.ToolCatalogItem{baseline[1], baseline[0]}

	assertRuntimeCatalogParity(t, baseline, reordered)

	if got, want := ToolSchemaRevision(reordered), ToolSchemaRevision(baseline); got != want {
		t.Fatalf("expected schema revision stable across ordering, baseline=%q candidate=%q", want, got)
	}
	if got, want := ToolAvailabilityRevision(reordered), ToolAvailabilityRevision(baseline); got != want {
		t.Fatalf("expected availability revision stable across ordering, baseline=%q candidate=%q", want, got)
	}
}

func TestBuildToolInfosParityAllowsAvailabilityOnlyDiff(t *testing.T) {
	t.Parallel()
	baseline := []middlewareclient.ToolCatalogItem{{
		Name:        "open_project",
		ProviderID:  skill.RuntimeIDQGIS,
		Source:      skill.RuntimeIDQGIS,
		Backend:     skill.RuntimeIDQGIS,
		Description: "open project",
		Category:    "project",
		InputSchema: map[string]any{"type": "object", "title": "open-project-v1"},
		Available:   true,
		InstanceIDs: []string{"inst-a"},
	}}
	candidate := []middlewareclient.ToolCatalogItem{{
		Name:              "open_project",
		ProviderID:        skill.RuntimeIDQGIS,
		Source:            skill.RuntimeIDQGIS,
		Backend:           skill.RuntimeIDQGIS,
		Description:       "open project",
		Category:          "project",
		InputSchema:       map[string]any{"type": "object", "title": "open-project-v1"},
		Available:         false,
		UnavailableReason: MissingReasonInteractiveDisconnected,
		InstanceIDs:       []string{"inst-a"},
	}}

	diff := DiffTools(baseline, candidate)
	if diff.SchemaChanged {
		t.Fatalf("expected availability-only drift to avoid schema change: %#v", diff)
	}
	if !diff.AvailabilityChanged {
		t.Fatalf("expected availability-only drift to be reported: %#v", diff)
	}
	assertRuntimeCatalogParity(t, baseline, candidate, "availability_revision", "availability_diff")

	if got, want := ToolSchemaRevision(candidate), ToolSchemaRevision(baseline); got != want {
		t.Fatalf("expected schema revision unchanged for availability-only drift, baseline=%q candidate=%q", want, got)
	}
	if got, want := ToolAvailabilityRevision(candidate), ToolAvailabilityRevision(baseline); got == want {
		t.Fatalf("expected availability revision bump, baseline=%q candidate=%q", want, got)
	}
}

func TestDiffToolsSeparatesAvailabilityFromSchema(t *testing.T) {
	oldTools := []middlewareclient.ToolCatalogItem{{
		Name:        "get_project_info",
		Description: "desc",
		Category:    "interactive",
		Source:      SourceInteractive,
		Backend:     skill.RuntimeIDQGIS,
		Available:   true,
		InstanceIDs: []string{"instance-a"},
	}}
	newTools := []middlewareclient.ToolCatalogItem{{
		Name:              "get_project_info",
		Description:       "desc",
		Category:          "interactive",
		Source:            SourceInteractive,
		Backend:           skill.RuntimeIDQGIS,
		Available:         false,
		UnavailableReason: "interactive_instance_disconnected",
		InstanceIDs:       []string{"instance-b"},
	}}

	diff := DiffTools(oldTools, newTools)
	if diff.SchemaChanged {
		t.Fatalf("expected schema_changed=false, got true: %#v", diff)
	}
	if !diff.AvailabilityChanged {
		t.Fatalf("expected availability_changed=true, got false: %#v", diff)
	}
	if len(diff.AvailabilityOnly) != 1 || diff.AvailabilityOnly[0] != "get_project_info" {
		t.Fatalf("unexpected availability_only: %#v", diff.AvailabilityOnly)
	}
}

func TestDiffToolsMarksSchemaChangeWhenDefinitionChanges(t *testing.T) {
	oldTools := []middlewareclient.ToolCatalogItem{{
		Name:        "buffer",
		Description: "buffer layers",
		Category:    "vector",
		Source:      SourceCompute,
		Backend:     skill.RuntimeIDCLI,
		InputSchema: map[string]any{"type": "object", "title": "buffer-v1"},
	}}
	newTools := []middlewareclient.ToolCatalogItem{{
		Name:        "buffer",
		Description: "buffer layers",
		Category:    "vector",
		Source:      SourceCompute,
		Backend:     skill.RuntimeIDCLI,
		InputSchema: map[string]any{"type": "object", "title": "buffer-v2"},
	}}

	diff := DiffTools(oldTools, newTools)
	if !diff.SchemaChanged {
		t.Fatalf("expected schema_changed=true, got false: %#v", diff)
	}
	if len(diff.Modified) != 1 || diff.Modified[0] != "buffer" {
		t.Fatalf("unexpected modified items: %#v", diff.Modified)
	}
	if diff.AvailabilityChanged {
		t.Fatalf("expected availability_changed=false, got true: %#v", diff)
	}
}

func TestToolSchemaRevisionIgnoresLegacyRuntimeAliasesWhenCanonicalRuntimeMatches(t *testing.T) {
	t.Parallel()
	legacyRevision := ToolSchemaRevision([]middlewareclient.ToolCatalogItem{{
		Name:        "open_project",
		ProviderID:  "qgis_bridge",
		Source:      "interactive",
		Description: "open project",
		Category:    "project",
		InputSchema: map[string]any{"type": "object"},
	}})
	canonicalRevision := ToolSchemaRevision([]middlewareclient.ToolCatalogItem{{
		Name:        "open_project",
		ProviderID:  "legacy_cli_provider",
		Source:      skill.RuntimeIDQGIS,
		Backend:     skill.RuntimeIDQGIS,
		Description: "open project",
		Category:    "project",
		InputSchema: map[string]any{"type": "object"},
	}})
	if legacyRevision != canonicalRevision {
		t.Fatalf("expected canonical runtime identity to stabilize schema revision, got legacy=%q canonical=%q", legacyRevision, canonicalRevision)
	}
}

func TestToolSchemaRevisionNormalizesInteractiveAliasButNotLegacyHeadlessAlias(t *testing.T) {
	t.Parallel()
	legacyComputeRevision := ToolSchemaRevision([]middlewareclient.ToolCatalogItem{{
		Name:        "buffer",
		Source:      "legacy_compute",
		Description: "buffer tool",
		Category:    "vector",
		InputSchema: map[string]any{"type": "object"},
	}})
	canonicalComputeRevision := ToolSchemaRevision([]middlewareclient.ToolCatalogItem{{
		Name:        "buffer",
		Source:      skill.RuntimeIDCLI,
		Description: "buffer tool",
		Category:    "vector",
		InputSchema: map[string]any{"type": "object"},
	}})
	if legacyComputeRevision == canonicalComputeRevision {
		t.Fatalf("did not expect legacy_compute source alias to normalize to cli, got legacy=%q canonical=%q", legacyComputeRevision, canonicalComputeRevision)
	}

	legacyInteractiveRevision := ToolSchemaRevision([]middlewareclient.ToolCatalogItem{{
		Name:        "open_project",
		Source:      "qgis_bridge",
		Description: "open project",
		Category:    "project",
		InputSchema: map[string]any{"type": "object"},
	}})
	canonicalInteractiveRevision := ToolSchemaRevision([]middlewareclient.ToolCatalogItem{{
		Name:        "open_project",
		Source:      skill.RuntimeIDQGIS,
		Description: "open project",
		Category:    "project",
		InputSchema: map[string]any{"type": "object"},
	}})
	if legacyInteractiveRevision != canonicalInteractiveRevision {
		t.Fatalf("expected qgis_bridge source alias to normalize to qgis, got legacy=%q canonical=%q", legacyInteractiveRevision, canonicalInteractiveRevision)
	}
}

func TestPublicToolsAppearInCatalog(t *testing.T) {
	t.Parallel()
	baseline := []middlewareclient.ToolCatalogItem{
		{
			Name:                "open_project",
			Source:              skill.RuntimeIDQGIS,
			Backend:             skill.RuntimeIDQGIS,
			Description:         "open project",
			Category:            "project",
			InputSchema:         map[string]any{"type": "object", "title": "open-project-v1"},
			Available:           true,
			Visibility:          "public",
			AuthorizationScopes: []string{"catalog.read", "tool.invoke"},
		},
		{
			Name:                "open_project_debug",
			Source:              skill.RuntimeIDQGIS,
			Backend:             skill.RuntimeIDQGIS,
			Description:         "debug open project",
			Category:            "project",
			InputSchema:         map[string]any{"type": "object", "title": "open-project-debug-v1"},
			Available:           true,
			Visibility:          "hidden",
			AuthorizationScopes: []string{"tool.invoke.internal"},
		},
		{
			Name:                "system_probe",
			Source:              skill.RuntimeIDInternal,
			Backend:             skill.RuntimeIDInternal,
			Description:         "system probe",
			Category:            "system",
			InputSchema:         map[string]any{"type": "object", "title": "system-probe-v1"},
			Available:           true,
			Visibility:          "internal",
			AuthorizationScopes: []string{"tool.invoke.internal"},
		},
	}

	publicOnly := PublicCatalogItems(baseline)
	if len(publicOnly) != 1 {
		t.Fatalf("expected only public tools in catalog, got %#v", publicOnly)
	}
	if publicOnly[0].Name != "open_project" {
		t.Fatalf("expected open_project to remain public, got %#v", publicOnly)
	}

	candidate := []middlewareclient.ToolCatalogItem{{
		Name:                "open_project",
		Source:              skill.RuntimeIDQGIS,
		Backend:             skill.RuntimeIDQGIS,
		Description:         "open project",
		Category:            "project",
		InputSchema:         map[string]any{"type": "object", "title": "open-project-v1"},
		Available:           true,
		Visibility:          "public",
		AuthorizationScopes: []string{"catalog.read", "tool.invoke", "tool.invoke.internal"},
	}}

	if got, want := ToolAvailabilityRevision(PublicCatalogItems(candidate)), ToolAvailabilityRevision(publicOnly); got != want {
		t.Fatalf("expected policy-only change to keep availability revision stable, baseline=%q candidate=%q", want, got)
	}
	if got, want := ToolSchemaRevision(PublicCatalogItems(candidate)), ToolSchemaRevision(publicOnly); got == want {
		t.Fatalf("expected public policy change to bump schema revision, baseline=%q candidate=%q", want, got)
	}

	diff := DiffTools(publicOnly, PublicCatalogItems(candidate))
	if !diff.SchemaChanged {
		t.Fatalf("expected policy change to be treated as schema drift, got %#v", diff)
	}
	if diff.AvailabilityChanged {
		t.Fatalf("expected policy-only change to avoid availability drift, got %#v", diff)
	}
}
