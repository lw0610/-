package agentruntime

import "strings"

func normalizeToolName(name string) string { return strings.ToLower(strings.TrimSpace(name)) }
