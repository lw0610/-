package settings

import "testing"

func TestClassifyConfigPathUsesCurrentSettingsDomains(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "log.level", want: MutationDomainSystem},
		{path: "data_dir", want: MutationDomainSystem},
		{path: "performance.max_concurrent_tasks", want: MutationDomainSystem},
		{path: "agent.loop_max_iterations", want: MutationDomainSystem},
		{path: "qgis.executable_path", want: MutationDomainSystem},
		{path: "session.default_name_pattern", want: MutationDomainSystem},
		{path: "ports.agent_system.default", want: MutationDomainServices},
		{path: "compute.port", want: MutationDomainServices},
		{path: "middleware.port", want: MutationDomainServices},
		{path: "services.lifecycle.auto_start_on_app_launch", want: MutationDomainServices},
		{path: "llm.providers", want: MutationDomainModels},
		{path: "plan_mode.enabled", want: MutationDomainAgents},
		{path: "tools.catalog", want: MutationDomainTools},
		{path: "skills.default", want: MutationDomainSkills},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := classifyConfigPath(tt.path); got != tt.want {
				t.Fatalf("classifyConfigPath(%q)=%q want=%q", tt.path, got, tt.want)
			}
		})
	}
}

func TestFinalizeMutationResultInfersUpdatedDomainsAndActions(t *testing.T) {
	tests := []struct {
		name   string
		input  SettingsMutationResult
		domain string
		action string
	}{
		{
			name: "system writes keep instant application when restart not required",
			input: SettingsMutationResult{
				Scope:         "config",
				AffectedPaths: []string{"log.level"},
			},
			domain: MutationDomainSystem,
			action: MutationRequiredActionNone,
		},
		{
			name: "service writes request desktop restart",
			input: SettingsMutationResult{
				Scope:         "config",
				AffectedPaths: []string{"compute.port"},
			},
			domain: MutationDomainServices,
			action: MutationRequiredActionDesktopRestart,
		},
		{
			name: "tool catalog writes rebuild runtime",
			input: SettingsMutationResult{
				Scope:         "config",
				AffectedPaths: []string{"tools.catalog"},
			},
			domain: MutationDomainTools,
			action: MutationRequiredActionRuntimeRebuild,
		},
		{
			name: "skill import stays deferred without rebuild requirement",
			input: SettingsMutationResult{
				Scope: "skill_import",
			},
			domain: MutationDomainSkills,
			action: MutationRequiredActionNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FinalizeMutationResult(tt.input)
			if got.AffectedDomain != tt.domain {
				t.Fatalf("AffectedDomain=%q want=%q", got.AffectedDomain, tt.domain)
			}
			if got.RequiredAction != tt.action {
				t.Fatalf("RequiredAction=%q want=%q", got.RequiredAction, tt.action)
			}
		})
	}
}
