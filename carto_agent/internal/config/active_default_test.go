package config

import (
	"github.com/autocarto/carto_agent/defaults"
	"os"
	"path/filepath"
	"testing"
)

func TestFreshConfigSelectionPrefersDefaultAndPreservesExplicitActive(t *testing.T) {
	for _, active := range []string{"", "missing", "agent_creator"} {
		t.Run("marker_"+active, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"agent_creator", "default"} {
				raw, err := defaults.FS.ReadFile("agent_configs/" + name + "/agent.yaml")
				if err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(dir, name, "agent.yaml")
				if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(target, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if active != "" {
				if err := os.WriteFile(filepath.Join(dir, activeConfigStateFileName), []byte(active), 0600); err != nil {
					t.Fatal(err)
				}
			}
			want := "default"
			if active == "agent_creator" {
				want = active
			}
			manager := NewAgentConfigManager(dir)
			if got := manager.GetActiveConfigName(); got != want {
				t.Fatalf("active=%q want %q", got, want)
			}
			persisted, err := os.ReadFile(filepath.Join(dir, activeConfigStateFileName))
			if err != nil || string(persisted) != want {
				t.Fatalf("active marker=%q error=%v", persisted, err)
			}
			list, err := manager.ListConfigs("")
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range list {
				if item.IsActive != (item.ConfigName == want) {
					t.Fatalf("public active mismatch: %s", item.ConfigName)
				}
			}
		})
	}
}
