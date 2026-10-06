package config

import "strings"

const (
	CompressStrategySummary       = "summary"
	CompressStrategyTruncate      = "truncate"
	CompressStrategySlidingWindow = "sliding_window"
)

func NormalizeCompressStrategy(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", CompressStrategySummary:
		return CompressStrategySummary
	case CompressStrategyTruncate:
		return CompressStrategyTruncate
	case CompressStrategySlidingWindow:
		return CompressStrategySlidingWindow
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func ContextCompressionTargetTokens(providerCfg *LLMProviderConfig) int {
	if providerCfg == nil || providerCfg.MaxContextTokens <= 0 {
		return 0
	}

	thresholdPercent := clampCompressionPercent(providerCfg.AutoCompressThreshold)
	if thresholdPercent <= 0 {
		return 0
	}

	maxUsagePercent := clampCompressionPercent(providerCfg.MaxContextUsage)
	if maxUsagePercent > 0 && maxUsagePercent < thresholdPercent {
		thresholdPercent = maxUsagePercent
	}

	triggerTokens := providerCfg.MaxContextTokens * thresholdPercent / 100
	if triggerTokens <= 0 {
		return 1
	}
	return triggerTokens
}

func clampCompressionPercent(value int) int {
	switch {
	case value <= 0:
		return 0
	case value >= 100:
		return 100
	default:
		return value
	}
}
