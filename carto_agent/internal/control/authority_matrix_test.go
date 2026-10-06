package control

import (
	"testing"
)

func TestAuthorityMatrix(t *testing.T) {
	matrix := DefaultAuthorityMatrix()

	domains := []Domain{
		DomainConfig,
		DomainDiscovery,
		DomainCatalog,
		DomainSessionTask,
		DomainRegistry,
		DomainAudit,
		DomainDiagnostics,
	}

	for _, domain := range domains {
		t.Run(string(domain), func(t *testing.T) {
			rule, err := matrix.GetRule(domain)
			if err != nil {
				t.Fatalf("expected domain %s to exist, got error: %v", domain, err)
			}

			if rule.SoleOwner == "" {
				t.Errorf("domain %s missing sole owner", domain)
			}

			if len(rule.AllowedWritePaths) == 0 {
				t.Errorf("domain %s missing allowed write paths", domain)
			}

			if len(rule.ReadOnlyConsumers) == 0 {
				t.Errorf("domain %s missing read-only consumers", domain)
			}

			if len(rule.ForbiddenBypasses) == 0 {
				t.Errorf("domain %s missing forbidden bypasses", domain)
			}
		})
	}
}

func TestRejectBypassMutation(t *testing.T) {
	matrix := DefaultAuthorityMatrix()

	testCases := []struct {
		domain   Domain
		path     string
		expected bool // true if allowed, false if rejected
	}{
		// Valid paths
		{DomainConfig, "control.SettingsMutationService.Mutate", true},
		{DomainDiscovery, "control.RuntimeCoordinator.Publish", true},
		{DomainCatalog, "control.AssetCatalogService.Sync", true},
		{DomainSessionTask, "control.SessionManager.Create", true},

		// Explicitly forbidden bypasses
		{DomainConfig, "plugin_local_write", false},
		{DomainConfig, "frontend_local_storage_bypass", false},
		{DomainDiscovery, "plugin_port_override", false},
		{DomainCatalog, "plugin_local_registry_mutation", false},
		{DomainSessionTask, "frontend_synthetic_completion", false},
		{DomainRegistry, "plugin_local_skill_dir_override", false},
		{DomainDiagnostics, "frontend_synthetic_degradation", false},

		// Implicitly forbidden (unknown path)
		{DomainConfig, "plugin.SomeRandomFunction", false},
	}

	for _, tc := range testCases {
		t.Run(string(tc.domain)+"-"+tc.path, func(t *testing.T) {
			err := matrix.ValidateWriteAttempt(tc.domain, tc.path)
			if tc.expected && err != nil {
				t.Errorf("expected path %s to be allowed for domain %s, got error: %v", tc.path, tc.domain, err)
			} else if !tc.expected && err == nil {
				t.Errorf("expected path %s to be rejected for domain %s, but it was allowed", tc.path, tc.domain)
			}
		})
	}
}

func TestReadonlyConsumers(t *testing.T) {
	matrix := DefaultAuthorityMatrix()

	consumers := []string{"plugin", "frontend", "tauri"}

	for _, domain := range []Domain{DomainConfig, DomainDiscovery, DomainCatalog, DomainSessionTask, DomainRegistry, DomainDiagnostics} {
		t.Run(string(domain), func(t *testing.T) {
			rule, _ := matrix.GetRule(domain)

			for _, expectedConsumer := range consumers {
				found := false
				for _, actualConsumer := range rule.ReadOnlyConsumers {
					if expectedConsumer == actualConsumer {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("domain %s missing expected read-only consumer: %s", domain, expectedConsumer)
				}
			}
		})
	}
}
