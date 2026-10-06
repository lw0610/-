package control

import (
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/defaults"
	"gopkg.in/yaml.v3"
)

const (
	testMixedVersionPolicyPath = "architecture/mixed_version_policy.yaml"
	testRemovalLedgerPath      = "architecture/removal_ledger.yaml"
	testWaveGatesPath          = "architecture/wave_gates.yaml"
)

type testEmbeddedMixedVersionPolicy struct {
	Contract           string   `yaml:"contract"`
	GovernsConfigPaths []string `yaml:"governs_config_paths"`
	Windows            []struct {
		Wave                string   `yaml:"wave"`
		AllowedCombinations []string `yaml:"allowed_combinations"`
		HardBlock           string   `yaml:"hard_block"`
		SunsetRule          string   `yaml:"sunset_rule"`
	} `yaml:"windows"`
}

type testEmbeddedRemovalLedger struct {
	Contract     string `yaml:"contract"`
	CompatLayers []struct {
		ID               string   `yaml:"id"`
		Path             string   `yaml:"path"`
		WhyTemporary     string   `yaml:"why_temporary"`
		RemoveInWave     string   `yaml:"remove_in_wave"`
		EvidenceRequired []string `yaml:"evidence_required"`
		BlockingTests    []string `yaml:"blocking_tests"`
		Governs          []string `yaml:"governs"`
		Status           string   `yaml:"status"`
	} `yaml:"compat_layers"`
	CompletedRemovals []struct {
		ID            string   `yaml:"id"`
		Path          string   `yaml:"path"`
		RemovedInWave string   `yaml:"removed_in_wave"`
		Evidence      []string `yaml:"evidence"`
	} `yaml:"completed_removals"`
}

type testEmbeddedWaveGates struct {
	Contract string `yaml:"contract"`
	Gates    []struct {
		Wave                 string   `yaml:"wave"`
		Scope                string   `yaml:"scope"`
		ExitGate             string   `yaml:"exit_gate"`
		RequiredTasks        []string `yaml:"required_tasks"`
		EvidencePack         []string `yaml:"evidence_pack"`
		VerificationCommands []string `yaml:"verification_commands"`
	} `yaml:"gates"`
}

func TestArchitecturePolicyLoader(t *testing.T) {
	policies, err := LoadArchitecturePolicies()
	if err != nil {
		t.Fatalf("LoadArchitecturePolicies returned error: %v", err)
	}
	if policies.MixedVersion.Contract != "mixed-version-policy" {
		t.Fatalf("unexpected loaded mixed-version contract: %q", policies.MixedVersion.Contract)
	}

	mixed := readEmbeddedArchitecturePolicyForTest[testEmbeddedMixedVersionPolicy](t, testMixedVersionPolicyPath)
	ledger := readEmbeddedArchitecturePolicyForTest[testEmbeddedRemovalLedger](t, testRemovalLedgerPath)
	gates := readEmbeddedArchitecturePolicyForTest[testEmbeddedWaveGates](t, testWaveGatesPath)

	if mixed.Contract != "mixed-version-policy" {
		t.Fatalf("unexpected mixed-version policy contract: %q", mixed.Contract)
	}
	if len(mixed.Windows) == 0 {
		t.Fatal("expected mixed-version policy windows to be defined")
	}
	if ledger.Contract != "removal-ledger" {
		t.Fatalf("unexpected removal ledger contract: %q", ledger.Contract)
	}
	if len(ledger.CompatLayers) == 0 {
		t.Fatal("expected removal ledger compat layers to be defined")
	}
	if len(ledger.CompletedRemovals) == 0 {
		t.Fatal("expected removal ledger completed removals to be defined")
	}
	if gates.Contract != "wave-gate-policy" {
		t.Fatalf("unexpected wave gate contract: %q", gates.Contract)
	}
	if len(gates.Gates) == 0 {
		t.Fatal("expected wave gates to be defined")
	}
	if !containsString(mixed.GovernsConfigPaths, "defaults.config.ports.agent_system.default") {
		t.Fatal("expected mixed-version policy to govern defaults.config.ports.agent_system.default")
	}
	if !containsString(mixed.GovernsConfigPaths, "defaults.config.ports.agent_system.auto_fallback") {
		t.Fatal("expected mixed-version policy to govern defaults.config.ports.agent_system.auto_fallback")
	}
}

func TestCompatLayerRequiresRemovalCriteria(t *testing.T) {
	ledger := readEmbeddedArchitecturePolicyForTest[testEmbeddedRemovalLedger](t, testRemovalLedgerPath)

	sawDefaultPortGovernance := false
	sawAutoFallbackGovernance := false
	for _, layer := range ledger.CompatLayers {
		if strings.TrimSpace(layer.ID) == "" {
			t.Fatal("expected compat layer id to be defined")
		}
		if strings.TrimSpace(layer.Path) == "" {
			t.Fatalf("compat layer %q missing path", layer.ID)
		}
		if strings.TrimSpace(layer.WhyTemporary) == "" {
			t.Fatalf("compat layer %q missing why_temporary", layer.ID)
		}
		if strings.TrimSpace(layer.RemoveInWave) == "" {
			t.Fatalf("compat layer %q missing remove_in_wave", layer.ID)
		}
		if len(layer.EvidenceRequired) == 0 {
			t.Fatalf("compat layer %q missing evidence_required", layer.ID)
		}
		if len(layer.BlockingTests) == 0 {
			t.Fatalf("compat layer %q missing blocking_tests", layer.ID)
		}
		if strings.TrimSpace(layer.Status) == "" {
			t.Fatalf("compat layer %q missing status", layer.ID)
		}
		if containsString(layer.Governs, "defaults.config.ports.agent_system.default") {
			sawDefaultPortGovernance = true
		}
		if containsString(layer.Governs, "defaults.config.ports.agent_system.auto_fallback") {
			sawAutoFallbackGovernance = true
		}
	}

	if !sawDefaultPortGovernance {
		t.Fatal("expected a compat layer to govern defaults.config.ports.agent_system.default")
	}
	if !sawAutoFallbackGovernance {
		t.Fatal("expected a compat layer to govern defaults.config.ports.agent_system.auto_fallback")
	}
}

func TestCompletedRemovalsRequireEvidence(t *testing.T) {
	ledger := readEmbeddedArchitecturePolicyForTest[testEmbeddedRemovalLedger](t, testRemovalLedgerPath)
	if len(ledger.CompletedRemovals) == 0 {
		t.Fatal("expected completed removals to be defined")
	}

	for _, removal := range ledger.CompletedRemovals {
		if strings.TrimSpace(removal.ID) == "" {
			t.Fatal("expected completed removal id to be defined")
		}
		if strings.TrimSpace(removal.Path) == "" {
			t.Fatalf("completed removal %q missing path", removal.ID)
		}
		if strings.TrimSpace(removal.RemovedInWave) == "" {
			t.Fatalf("completed removal %q missing removed_in_wave", removal.ID)
		}
		if len(removal.Evidence) == 0 {
			t.Fatalf("completed removal %q missing evidence", removal.ID)
		}
	}
}

func TestWaveGateDefinitions(t *testing.T) {
	gates := readEmbeddedArchitecturePolicyForTest[testEmbeddedWaveGates](t, testWaveGatesPath)

	sawWaveOne := false
	sawBatchZero := false
	for _, gate := range gates.Gates {
		if strings.TrimSpace(gate.Wave) == "" {
			t.Fatal("expected wave gate wave to be defined")
		}
		if strings.TrimSpace(gate.Scope) == "" {
			t.Fatalf("wave gate %q missing scope", gate.Wave)
		}
		if strings.TrimSpace(gate.ExitGate) == "" {
			t.Fatalf("wave gate %q missing exit_gate", gate.Wave)
		}
		if len(gate.RequiredTasks) == 0 {
			t.Fatalf("wave gate %q missing required_tasks", gate.Wave)
		}
		if len(gate.EvidencePack) == 0 {
			t.Fatalf("wave gate %q missing evidence_pack", gate.Wave)
		}
		if len(gate.VerificationCommands) == 0 {
			t.Fatalf("wave gate %q missing verification_commands", gate.Wave)
		}
		if gate.Wave == "wave_1" && containsString(gate.RequiredTasks, "T10") {
			sawWaveOne = true
		}
		if gate.Wave == "batch_0" && containsString(gate.RequiredTasks, "T8") && containsString(gate.RequiredTasks, "T9") && containsString(gate.RequiredTasks, "T10") {
			sawBatchZero = true
		}
	}

	if !sawWaveOne {
		t.Fatal("expected wave_1 gate to require T10")
	}
	if !sawBatchZero {
		t.Fatal("expected batch_0 gate to require T8/T9/T10 foundation tasks")
	}
}

func readEmbeddedArchitecturePolicyForTest[T any](t *testing.T, path string) T {
	t.Helper()

	raw, err := defaults.FS.ReadFile(path)
	if err != nil {
		t.Fatalf("expected embedded architecture policy %s to exist: %v", path, err)
	}

	var decoded T
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("expected embedded architecture policy %s to decode: %v", path, err)
	}

	return decoded
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}
