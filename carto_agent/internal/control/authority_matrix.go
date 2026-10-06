package control

import (
	"fmt"
)

// Domain defines the functional boundary in the authority matrix
type Domain string

const (
	DomainConfig      Domain = "config"
	DomainDiscovery   Domain = "discovery"
	DomainCatalog     Domain = "catalog"
	DomainSessionTask Domain = "session/task"
	DomainRegistry    Domain = "registry"
	DomainAudit       Domain = "audit"
	DomainDiagnostics Domain = "diagnostics"
)

// AuthoritativeOwner indicates which component has the exclusive right to mutate a domain
type AuthoritativeOwner string

const (
	OwnerSettingsMutationService AuthoritativeOwner = "settings_mutation_service"
	OwnerRuntimeCoordinator      AuthoritativeOwner = "runtime_coordinator"
	OwnerAssetCatalogService     AuthoritativeOwner = "asset_catalog_service"
	OwnerSessionManager          AuthoritativeOwner = "session_manager"
	OwnerRegistryGovernance      AuthoritativeOwner = "registry_governance"
	OwnerAuditLog                AuthoritativeOwner = "audit_log"
	OwnerDiagnosticsService      AuthoritativeOwner = "diagnostics_service"
)

// AuthorityMatrix defines the strict ownership and write paths for the system
type AuthorityMatrix struct {
	rules map[Domain]DomainRule
}

// DomainRule specifies the access control for a single domain
type DomainRule struct {
	SoleOwner         AuthoritativeOwner
	AllowedWritePaths []string
	ReadOnlyConsumers []string
	ForbiddenBypasses []string
}

// DefaultAuthorityMatrix returns the singleton instance of the system's authority rules
func DefaultAuthorityMatrix() *AuthorityMatrix {
	return &AuthorityMatrix{
		rules: map[Domain]DomainRule{
			DomainConfig: {
				SoleOwner:         OwnerSettingsMutationService,
				AllowedWritePaths: []string{"control.SettingsMutationService.Mutate", "control.SettingsMutationService.Reset"},
				ReadOnlyConsumers: []string{"plugin", "frontend", "tauri", "agent_internal"},
				ForbiddenBypasses: []string{"plugin_local_write", "frontend_local_storage_bypass"},
			},
			DomainDiscovery: {
				SoleOwner:         OwnerRuntimeCoordinator,
				AllowedWritePaths: []string{"control.RuntimeCoordinator.Publish", "control.RuntimeCoordinator.UpdatePort"},
				ReadOnlyConsumers: []string{"plugin", "frontend", "tauri"},
				ForbiddenBypasses: []string{"plugin_port_override", "frontend_env_override"},
			},
			DomainCatalog: {
				SoleOwner:         OwnerAssetCatalogService,
				AllowedWritePaths: []string{"control.AssetCatalogService.Publish", "control.AssetCatalogService.Sync"},
				ReadOnlyConsumers: []string{"plugin", "frontend", "tauri"},
				ForbiddenBypasses: []string{"plugin_local_registry_mutation", "frontend_local_tool_definition"},
			},
			DomainSessionTask: {
				SoleOwner:         OwnerSessionManager,
				AllowedWritePaths: []string{"control.SessionManager.Create", "control.SessionManager.UpdateState", "control.SessionManager.Cancel"},
				ReadOnlyConsumers: []string{"plugin", "frontend", "tauri"},
				ForbiddenBypasses: []string{"frontend_synthetic_completion", "plugin_synthetic_cancellation"},
			},
			DomainRegistry: {
				SoleOwner:         OwnerRegistryGovernance,
				AllowedWritePaths: []string{"control.RegistryGovernance.Register", "control.RegistryGovernance.Unregister"},
				ReadOnlyConsumers: []string{"plugin", "frontend", "tauri"},
				ForbiddenBypasses: []string{"plugin_local_skill_dir_override"},
			},
			DomainAudit: {
				SoleOwner:         OwnerAuditLog,
				AllowedWritePaths: []string{"control.AuditLog.Append"},
				ReadOnlyConsumers: []string{"plugin", "frontend", "tauri", "agent_internal"},
				ForbiddenBypasses: []string{"direct_db_write", "file_log_bypass"},
			},
			DomainDiagnostics: {
				SoleOwner:         OwnerDiagnosticsService,
				AllowedWritePaths: []string{"control.DiagnosticsService.Report", "control.DiagnosticsService.UpdateHealth"},
				ReadOnlyConsumers: []string{"plugin", "frontend", "tauri"},
				ForbiddenBypasses: []string{"frontend_synthetic_degradation", "plugin_synthetic_error"},
			},
		},
	}
}

// ValidateWriteAttempt checks if a requested write path is authorized for the given domain
func (m *AuthorityMatrix) ValidateWriteAttempt(domain Domain, callerPath string) error {
	rule, exists := m.rules[domain]
	if !exists {
		return fmt.Errorf("unknown domain: %s", domain)
	}

	for _, allowed := range rule.AllowedWritePaths {
		if allowed == callerPath {
			return nil
		}
	}

	for _, bypass := range rule.ForbiddenBypasses {
		if bypass == callerPath {
			return fmt.Errorf("bypass forbidden for domain %s: %s", domain, callerPath)
		}
	}

	return fmt.Errorf("unauthorized write path for domain %s: %s (sole owner: %s)", domain, callerPath, rule.SoleOwner)
}

// GetRule returns the rule for a domain, useful for testing
func (m *AuthorityMatrix) GetRule(domain Domain) (DomainRule, error) {
	rule, exists := m.rules[domain]
	if !exists {
		return DomainRule{}, fmt.Errorf("unknown domain: %s", domain)
	}
	return rule, nil
}
