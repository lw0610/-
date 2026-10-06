package config

import "testing"

func TestNormalizeCompressStrategy(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "", want: CompressStrategySummary},
		{raw: "summary", want: CompressStrategySummary},
		{raw: " Summary ", want: CompressStrategySummary},
		{raw: "truncate", want: CompressStrategyTruncate},
		{raw: "sliding_window", want: CompressStrategySlidingWindow},
		{raw: "custom", want: "custom"},
	}

	for _, tt := range tests {
		if got := NormalizeCompressStrategy(tt.raw); got != tt.want {
			t.Fatalf("NormalizeCompressStrategy(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestContextCompressionTargetTokens(t *testing.T) {
	tests := []struct {
		name string
		cfg  *LLMProviderConfig
		want int
	}{
		{
			name: "nil config",
			cfg:  nil,
			want: 0,
		},
		{
			name: "basic threshold",
			cfg: &LLMProviderConfig{
				MaxContextTokens:      1000,
				MaxContextUsage:       90,
				AutoCompressThreshold: 85,
			},
			want: 850,
		},
		{
			name: "clamped by max usage",
			cfg: &LLMProviderConfig{
				MaxContextTokens:      1000,
				MaxContextUsage:       70,
				AutoCompressThreshold: 85,
			},
			want: 700,
		},
		{
			name: "disabled when threshold invalid",
			cfg: &LLMProviderConfig{
				MaxContextTokens:      1000,
				AutoCompressThreshold: 0,
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContextCompressionTargetTokens(tt.cfg); got != tt.want {
				t.Fatalf("ContextCompressionTargetTokens() = %d, want %d", got, tt.want)
			}
		})
	}
}
