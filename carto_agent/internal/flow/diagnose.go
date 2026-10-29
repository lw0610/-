package flow

import (
	"strings"
	"unicode/utf8"
)

func stepHasToolDetail(instructions string, capabilities []CapabilitySpec) bool {
	if utf8.RuneCountInString(instructions) > 600 {
		return true
	}
	identifier := func(b byte) bool {
		return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
	}
	for _, capability := range capabilities {
		for _, raw := range capability.Parameters {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			for from := 0; from < len(instructions); {
				offset := strings.Index(instructions[from:], name)
				if offset < 0 {
					break
				}
				start := from + offset
				end := start + len(name)
				if (start == 0 || !identifier(instructions[start-1])) && (end == len(instructions) || !identifier(instructions[end])) {
					return true
				}
				from = end
			}
		}
	}
	return false
}
