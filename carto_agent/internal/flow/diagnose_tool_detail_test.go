package flow

import (
	"strings"
	"testing"
)

func TestFlowStepToolDetailUsesLengthAndActualParameterNames(t *testing.T) {
	catalog := []CapabilitySpec{{Name: "set_style", Parameters: []string{"", "line_width", "dpi"}}}
	for _, tc := range []struct {
		name, text string
		detail     bool
	}{
		{"business intent", "按路线样式区分主体，保留全部路线和地点。", false},
		{"Chinese boundary", strings.Repeat("图", 600), false},
		{"Chinese over limit", strings.Repeat("图", 601), true},
		{"parameter quoted", "设置 `line_width` 为 2", true},
		{"parameter touches Chinese", "设置line_width为2", true},
		{"parameter exact text", "dpi", true},
		{"unrelated identifier suffix", "outline_width", false},
		{"unrelated identifier prefix", "line_width_mm", false},
		{"parameter after prefix match", "line_width_mm，然后用line_width", true},
		{"not in catalog", "set_opacity 为 0.8", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := stepHasToolDetail(tc.text, catalog); got != tc.detail {
				t.Fatalf("detail=%v want=%v", got, tc.detail)
			}

		})
	}
}
