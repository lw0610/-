package toolrouter

import (
	_ "embed"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type ClassificationRegistry struct {
	interactivePatterns     []string
	computeOverridePatterns []string
}

type classificationFile struct {
	InteractivePatterns     []string `yaml:"interactive_patterns"`
	ComputeOverridePatterns []string `yaml:"compute_override_patterns"`
}

//go:embed tool_classification.yaml
var classificationConfigYAML []byte

func NewDefaultClassificationRegistry() *ClassificationRegistry {
	interactivePatterns, computeOverridePatterns := loadClassificationConfig()

	return &ClassificationRegistry{
		interactivePatterns:     interactivePatterns,
		computeOverridePatterns: computeOverridePatterns,
	}
}

func loadClassificationConfig() ([]string, []string) {
	if len(classificationConfigYAML) == 0 {
		return nil, nil
	}

	cfg := classificationFile{}
	if err := yaml.Unmarshal(classificationConfigYAML, &cfg); err != nil {
		return nil, nil
	}

	return normalizePatterns(cfg.InteractivePatterns), normalizePatterns(cfg.ComputeOverridePatterns)
}

func normalizePatterns(patterns []string) []string {
	out := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		trimmed := strings.TrimSpace(pattern)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func (r *ClassificationRegistry) IsComputeOverride(toolName string) bool {
	if r == nil {
		return false
	}
	return matchesPatternList(r.computeOverridePatterns, toolName)
}

func (r *ClassificationRegistry) IsInteractive(toolName string) bool {
	if r == nil {
		return false
	}
	return matchesPatternList(r.interactivePatterns, toolName)
}

func matchesPatternList(patterns []string, toolName string) bool {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return false
	}
	for _, pattern := range patterns {
		ok, err := filepath.Match(pattern, name)
		if err == nil && ok {
			return true
		}
	}
	return false
}
