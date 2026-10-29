package sessionfiles

import "fmt"

// RecoverToolArtifacts repairs older indexes using this session's persisted tool
// results. It deliberately does not inspect prompts, arguments, reasoning, loaded
// input layers, or enumerate shared directories. Existing entries keep their
// origin and registration time, including entries updated by file replacement.
func (s *Store) RecoverToolArtifacts(session map[string]any) []error {
	type candidate struct {
		path   string
		origin Origin
	}
	candidates := map[string]candidate{}
	for _, block := range objectList(session["workflow_blocks"]) {
		agent, _ := block["agent"].(string)
		for _, entry := range objectList(block["entries"]) {
			if entry["type"] != "tool" {
				continue
			}
			data, _ := entry["data"].(map[string]any)
			card, _ := entry["toolCard"].(map[string]any)
			name, _ := data["tool"].(string)
			if name == "" {
				name, _ = card["tool"].(string)
			}
			if name == "" || ReadsInputPaths(name) {
				continue
			}
			result := data["result"]
			if result == nil {
				result = card["result"]
			}
			callID, _ := data["call_id"].(string)
			origin := Origin{Type: "tool", Tool: name, Label: name, Agent: agent, CallID: callID}
			parts := objectList(data["result_parts"])
			names := ResultNames(result, parts)
			for _, path := range ResultPaths(result, parts) {
				origin.Name = names[path]
				candidates[path] = candidate{path, origin}
			}
		}
	}
	var failures []error
	for _, item := range candidates {
		if _, err := s.registerMissing(item.path, item.origin); err != nil {
			failures = append(failures, fmt.Errorf("tool=%s path=%s: %w", item.origin.Tool, item.path, err))
		}
	}
	return failures
}

func objectList(value any) []map[string]any {
	if list, ok := value.([]map[string]any); ok {
		return list
	}
	var result []map[string]any
	if list, ok := value.([]any); ok {
		for _, item := range list {
			if object, ok := item.(map[string]any); ok {
				result = append(result, object)
			}
		}
	}
	return result
}
