package control

import (
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/defaults"
	"gopkg.in/yaml.v3"
)

const (
	ArchitectureMixedVersionPolicyPath = "architecture/mixed_version_policy.yaml"
	ArchitectureRemovalLedgerPath      = "architecture/removal_ledger.yaml"
	ArchitectureWaveGatesPath          = "architecture/wave_gates.yaml"
)

type ArchitecturePolicies struct {
	MixedVersion  MixedVersionPolicy  `json:"mixed_version_policy"`
	RemovalLedger RemovalLedgerPolicy `json:"removal_ledger"`
	WaveGates     WaveGatePolicy      `json:"wave_gates"`
}

type MixedVersionPolicy struct {
	Contract           string               `yaml:"contract" json:"contract"`
	Description        string               `yaml:"description,omitempty" json:"description,omitempty"`
	GovernsConfigPaths []string             `yaml:"governs_config_paths,omitempty" json:"governs_config_paths,omitempty"`
	Windows            []MixedVersionWindow `yaml:"windows" json:"windows"`
}

type MixedVersionWindow struct {
	Wave                string   `yaml:"wave" json:"wave"`
	AllowedCombinations []string `yaml:"allowed_combinations" json:"allowed_combinations"`
	HardBlock           string   `yaml:"hard_block" json:"hard_block"`
	SunsetRule          string   `yaml:"sunset_rule" json:"sunset_rule"`
}

type RemovalLedgerPolicy struct {
	Contract          string                   `yaml:"contract" json:"contract"`
	Description       string                   `yaml:"description,omitempty" json:"description,omitempty"`
	CompatLayers      []CompatLayerPolicy      `yaml:"compat_layers" json:"compat_layers"`
	CompletedRemovals []CompletedRemovalPolicy `yaml:"completed_removals,omitempty" json:"completed_removals,omitempty"`
}

type CompatLayerPolicy struct {
	ID               string   `yaml:"id" json:"id"`
	Path             string   `yaml:"path" json:"path"`
	WhyTemporary     string   `yaml:"why_temporary" json:"why_temporary"`
	RemoveInWave     string   `yaml:"remove_in_wave" json:"remove_in_wave"`
	EvidenceRequired []string `yaml:"evidence_required" json:"evidence_required"`
	BlockingTests    []string `yaml:"blocking_tests" json:"blocking_tests"`
	Governs          []string `yaml:"governs,omitempty" json:"governs,omitempty"`
	Status           string   `yaml:"status,omitempty" json:"status,omitempty"`
}

type CompletedRemovalPolicy struct {
	ID            string   `yaml:"id" json:"id"`
	Path          string   `yaml:"path" json:"path"`
	RemovedInWave string   `yaml:"removed_in_wave" json:"removed_in_wave"`
	Evidence      []string `yaml:"evidence" json:"evidence"`
}

type WaveGatePolicy struct {
	Contract    string               `yaml:"contract" json:"contract"`
	Description string               `yaml:"description,omitempty" json:"description,omitempty"`
	Gates       []WaveGateDefinition `yaml:"gates" json:"gates"`
}

type WaveGateDefinition struct {
	Wave                 string   `yaml:"wave" json:"wave"`
	Scope                string   `yaml:"scope" json:"scope"`
	ExitGate             string   `yaml:"exit_gate" json:"exit_gate"`
	RequiredTasks        []string `yaml:"required_tasks" json:"required_tasks"`
	EvidencePack         []string `yaml:"evidence_pack" json:"evidence_pack"`
	VerificationCommands []string `yaml:"verification_commands" json:"verification_commands"`
}

func LoadArchitecturePolicies() (ArchitecturePolicies, error) {
	mixed, err := readEmbeddedArchitecturePolicy[MixedVersionPolicy](ArchitectureMixedVersionPolicyPath)
	if err != nil {
		return ArchitecturePolicies{}, err
	}

	ledger, err := readEmbeddedArchitecturePolicy[RemovalLedgerPolicy](ArchitectureRemovalLedgerPath)
	if err != nil {
		return ArchitecturePolicies{}, err
	}

	gates, err := readEmbeddedArchitecturePolicy[WaveGatePolicy](ArchitectureWaveGatesPath)
	if err != nil {
		return ArchitecturePolicies{}, err
	}

	policies := ArchitecturePolicies{
		MixedVersion:  mixed,
		RemovalLedger: ledger,
		WaveGates:     gates,
	}

	if err := policies.Validate(); err != nil {
		return ArchitecturePolicies{}, err
	}

	return policies, nil
}

func (p ArchitecturePolicies) Validate() error {
	if strings.TrimSpace(p.MixedVersion.Contract) != "mixed-version-policy" {
		return newArchitecturePolicyInvalidError("mixed-version policy contract 无效", map[string]any{"path": ArchitectureMixedVersionPolicyPath, "contract": p.MixedVersion.Contract})
	}
	if len(p.MixedVersion.Windows) == 0 {
		return newArchitecturePolicyInvalidError("mixed-version policy 缺少 windows", map[string]any{"path": ArchitectureMixedVersionPolicyPath})
	}
	for _, window := range p.MixedVersion.Windows {
		if strings.TrimSpace(window.Wave) == "" || len(window.AllowedCombinations) == 0 || strings.TrimSpace(window.HardBlock) == "" || strings.TrimSpace(window.SunsetRule) == "" {
			return newArchitecturePolicyInvalidError("mixed-version policy window 缺少必填字段", map[string]any{"path": ArchitectureMixedVersionPolicyPath, "wave": window.Wave})
		}
	}
	if !containsTrimmed(p.MixedVersion.GovernsConfigPaths, "defaults.config.ports.agent_system.default") {
		return newArchitecturePolicyInvalidError("mixed-version policy 未登记 legacy default port 治理路径", map[string]any{"path": ArchitectureMixedVersionPolicyPath})
	}
	if !containsTrimmed(p.MixedVersion.GovernsConfigPaths, "defaults.config.ports.agent_system.auto_fallback") {
		return newArchitecturePolicyInvalidError("mixed-version policy 未登记 auto_fallback 治理路径", map[string]any{"path": ArchitectureMixedVersionPolicyPath})
	}

	if strings.TrimSpace(p.RemovalLedger.Contract) != "removal-ledger" {
		return newArchitecturePolicyInvalidError("removal ledger contract 无效", map[string]any{"path": ArchitectureRemovalLedgerPath, "contract": p.RemovalLedger.Contract})
	}
	if len(p.RemovalLedger.CompatLayers) == 0 {
		return newArchitecturePolicyInvalidError("removal ledger 缺少 compat layers", map[string]any{"path": ArchitectureRemovalLedgerPath})
	}
	for _, layer := range p.RemovalLedger.CompatLayers {
		if strings.TrimSpace(layer.ID) == "" || strings.TrimSpace(layer.Path) == "" || strings.TrimSpace(layer.WhyTemporary) == "" || strings.TrimSpace(layer.RemoveInWave) == "" {
			return newArchitecturePolicyInvalidError("compat layer 缺少必填字段", map[string]any{"path": ArchitectureRemovalLedgerPath, "compat_layer": layer.ID})
		}
		if len(layer.EvidenceRequired) == 0 {
			return newArchitecturePolicyInvalidError("compat layer 缺少 evidence_required", map[string]any{"path": ArchitectureRemovalLedgerPath, "compat_layer": layer.ID})
		}
		if len(layer.BlockingTests) == 0 {
			return newArchitecturePolicyInvalidError("compat layer 缺少 blocking_tests", map[string]any{"path": ArchitectureRemovalLedgerPath, "compat_layer": layer.ID})
		}
		if strings.TrimSpace(layer.Status) == "" {
			return newArchitecturePolicyInvalidError("compat layer 缺少 status", map[string]any{"path": ArchitectureRemovalLedgerPath, "compat_layer": layer.ID})
		}
	}
	for _, removal := range p.RemovalLedger.CompletedRemovals {
		if strings.TrimSpace(removal.ID) == "" || strings.TrimSpace(removal.Path) == "" || strings.TrimSpace(removal.RemovedInWave) == "" {
			return newArchitecturePolicyInvalidError("completed removal 缺少必填字段", map[string]any{"path": ArchitectureRemovalLedgerPath, "completed_removal": removal.ID})
		}
		if len(removal.Evidence) == 0 {
			return newArchitecturePolicyInvalidError("completed removal 缺少 evidence", map[string]any{"path": ArchitectureRemovalLedgerPath, "completed_removal": removal.ID})
		}
	}

	if strings.TrimSpace(p.WaveGates.Contract) != "wave-gate-policy" {
		return newArchitecturePolicyInvalidError("wave gate policy contract 无效", map[string]any{"path": ArchitectureWaveGatesPath, "contract": p.WaveGates.Contract})
	}
	if len(p.WaveGates.Gates) == 0 {
		return newArchitecturePolicyInvalidError("wave gate policy 缺少 gates", map[string]any{"path": ArchitectureWaveGatesPath})
	}
	for _, gate := range p.WaveGates.Gates {
		if strings.TrimSpace(gate.Wave) == "" || strings.TrimSpace(gate.Scope) == "" || strings.TrimSpace(gate.ExitGate) == "" {
			return newArchitecturePolicyInvalidError("wave gate 缺少基础字段", map[string]any{"path": ArchitectureWaveGatesPath, "wave": gate.Wave})
		}
		if len(gate.RequiredTasks) == 0 || len(gate.EvidencePack) == 0 || len(gate.VerificationCommands) == 0 {
			return newArchitecturePolicyInvalidError("wave gate 缺少任务/证据/校验命令", map[string]any{"path": ArchitectureWaveGatesPath, "wave": gate.Wave})
		}
	}

	return nil
}

func readEmbeddedArchitecturePolicy[T any](path string) (T, error) {
	var decoded T

	raw, err := defaults.FS.ReadFile(path)
	if err != nil {
		return decoded, newArchitecturePolicyReadError(path, err)
	}
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		return decoded, newArchitecturePolicyInvalidError("architecture policy YAML 解析失败", map[string]any{"path": path, "error": err.Error()})
	}

	return decoded, nil
}

func newArchitecturePolicyReadError(path string, err error) error {
	envelope := NewErrorEnvelope("ARCHITECTURE_POLICY_READ_FAILED", "architecture policy 读取失败", "control", "architecture_policy")
	envelope.Retryable = false
	envelope.UserAction = "请确认 defaults 内嵌的 architecture policy 资产已随构建一起发布。"
	return envelope.WithDebugContext(map[string]any{"path": path, "cause": fmt.Sprintf("%v", err)})
}

func newArchitecturePolicyInvalidError(message string, debugContext map[string]any) error {
	envelope := NewErrorEnvelope("ARCHITECTURE_POLICY_INVALID", strings.TrimSpace(message), "control", "architecture_policy")
	envelope.Retryable = false
	envelope.UserAction = "请修复 mixed-version policy、removal ledger 或 wave gate 资产中的缺失字段。"
	return envelope.WithDebugContext(debugContext)
}

func containsTrimmed(items []string, target string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}
