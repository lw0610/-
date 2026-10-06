package skill

import (
	"testing"

	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
)

func TestApplyBridgeRuntimeAvailabilityMarksInteractiveUnavailableWithoutRunningInstance(t *testing.T) {
	t.Parallel()
	items := []middlewareclient.ToolCatalogItem{
		{Name: "buffer", ProviderID: "cli", Source: "compute", Available: true},
		{Name: "open_project", ProviderID: "qgis_bridge", Source: "interactive", Backend: RuntimeIDQGIS, Available: true},
	}
	updated := ApplyBridgeRuntimeAvailability(items, middlewareclient.RuntimeSnapshot{})
	if updated[0].Available != true {
		t.Fatalf("expected compute tool availability to stay true, got %#v", updated[0])
	}
	if updated[1].Available {
		t.Fatalf("expected bridge tool to become unavailable, got %#v", updated[1])
	}
	if updated[1].UnavailableReason != MissingReasonInteractiveInstanceDisconnected {
		t.Fatalf("unexpected unavailable reason: %#v", updated[1].UnavailableReason)
	}
	if len(updated[1].InstanceIDs) != 0 {
		t.Fatalf("expected no instance ids, got %#v", updated[1].InstanceIDs)
	}
}

func TestApplyBridgeRuntimeAvailabilityInjectsRunningInstanceIDs(t *testing.T) {
	t.Parallel()
	items := []middlewareclient.ToolCatalogItem{{Name: "get_project_info", ProviderID: "qgis_bridge", Source: "interactive", Backend: RuntimeIDQGIS}}
	snapshot := middlewareclient.RuntimeSnapshot{
		Instances: middlewareclient.InstancesSnapshot{
			Items: []*discovery.QGISInstance{
				{ID: "inst-b", Status: "running"},
				{ID: "inst-a", Status: "running"},
				{ID: "inst-z", Status: "stopped"},
			},
		},
	}
	updated := ApplyBridgeRuntimeAvailability(items, snapshot)
	if !updated[0].Available {
		t.Fatalf("expected bridge tool to be available, got %#v", updated[0])
	}
	if updated[0].UnavailableReason != "" {
		t.Fatalf("expected empty unavailable reason, got %#v", updated[0].UnavailableReason)
	}
	if len(updated[0].InstanceIDs) != 2 || updated[0].InstanceIDs[0] != "inst-a" || updated[0].InstanceIDs[1] != "inst-b" {
		t.Fatalf("unexpected instance ids: %#v", updated[0].InstanceIDs)
	}
}

func TestApplyBridgeRuntimeAvailabilityPreservesExplicitToolInstanceIDs(t *testing.T) {
	t.Parallel()

	updated := ApplyBridgeRuntimeAvailability([]middlewareclient.ToolCatalogItem{{
		Name:        "flash_feature",
		Backend:     RuntimeIDQGIS,
		Source:      RuntimeIDQGIS,
		InstanceIDs: []string{"inst-b", "inst-c"},
	}}, middlewareclient.RuntimeSnapshot{Instances: middlewareclient.InstancesSnapshot{Items: []*discovery.QGISInstance{{ID: "inst-a", Status: "running"}, {ID: "inst-b", Status: "running"}}}})

	if len(updated) != 1 {
		t.Fatalf("unexpected updated item count: %d", len(updated))
	}
	if len(updated[0].InstanceIDs) != 1 || updated[0].InstanceIDs[0] != "inst-b" {
		t.Fatalf("expected explicit running instance ids to be preserved, got %#v", updated[0].InstanceIDs)
	}
	if !updated[0].Available || updated[0].UnavailableReason != "" {
		t.Fatalf("expected tool to stay available, got %#v", updated[0])
	}
}

func TestApplyBridgeRuntimeAvailabilityIgnoresLegacyProviderHintsWhenRuntimeIsCLI(t *testing.T) {
	t.Parallel()
	items := []middlewareclient.ToolCatalogItem{{
		Name:       "buffer",
		ProviderID: "qgis_bridge",
		Source:     "interactive",
		Backend:    RuntimeIDCLI,
		Available:  true,
	}}

	updated := ApplyBridgeRuntimeAvailability(items, middlewareclient.RuntimeSnapshot{})
	if !updated[0].Available {
		t.Fatalf("expected cli runtime item to remain available, got %#v", updated[0])
	}
	if updated[0].UnavailableReason != "" {
		t.Fatalf("expected cli runtime item to keep empty unavailable reason, got %#v", updated[0].UnavailableReason)
	}
	if len(updated[0].InstanceIDs) != 0 {
		t.Fatalf("expected cli runtime item to keep empty instance ids, got %#v", updated[0].InstanceIDs)
	}
}

func TestApplyBridgeRuntimeAvailabilityPrefersCanonicalSourceOverLegacyProvider(t *testing.T) {
	t.Parallel()
	items := []middlewareclient.ToolCatalogItem{{
		Name:       "open_project",
		ProviderID: "legacy_cli_provider",
		Source:     RuntimeIDQGIS,
		Available:  true,
	}}

	updated := ApplyBridgeRuntimeAvailability(items, middlewareclient.RuntimeSnapshot{})
	if updated[0].Available {
		t.Fatalf("expected canonical qgis source item to become unavailable without running instance, got %#v", updated[0])
	}
	if updated[0].UnavailableReason != MissingReasonInteractiveInstanceDisconnected {
		t.Fatalf("expected qgis source item to use interactive disconnected reason, got %#v", updated[0].UnavailableReason)
	}
}

func TestCatalogItemRuntimePrefersBackendThenSourceThenProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		item middlewareclient.ToolCatalogItem
		want string
	}{
		{
			name: "backend wins over legacy fields",
			item: middlewareclient.ToolCatalogItem{Backend: RuntimeIDCLI, Source: "interactive", ProviderID: "qgis_bridge"},
			want: RuntimeIDCLI,
		},
		{
			name: "canonical source wins over provider",
			item: middlewareclient.ToolCatalogItem{Source: RuntimeIDQGIS, ProviderID: "legacy_cli_provider"},
			want: RuntimeIDQGIS,
		},
		{
			name: "legacy interactive source maps to qgis",
			item: middlewareclient.ToolCatalogItem{Source: "interactive", ProviderID: "legacy_cli_provider"},
			want: RuntimeIDQGIS,
		},
		{
			name: "provider fallback no longer infers cli",
			item: middlewareclient.ToolCatalogItem{ProviderID: "legacy_cli_provider"},
			want: "",
		},
		{
			name: "legacy source legacy_compute no longer infers cli",
			item: middlewareclient.ToolCatalogItem{Source: "legacy_compute"},
			want: "",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CatalogItemRuntime(tc.item); got != tc.want {
				t.Fatalf("CatalogItemRuntime(%#v) = %q, want %q", tc.item, got, tc.want)
			}
		})
	}
}
