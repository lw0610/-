package main

import "testing"

func TestFlowEnvironmentInvalidationPrecedesCatalogRefresh(t *testing.T) {
	for _, reason := range []string{"agent_config:test", "skill_import", "skills_watcher", "runtime_tool_catalog:local"} {
		invalidated := false
		requester := flowEnvironmentRefreshRequester{invalidate: func() { invalidated = true }, request: func(got string) {
			if !invalidated || got != reason {
				t.Fatal("catalog queued before invalidation", got)
			}
		}}
		requester.RequestRefresh(reason)
	}
}
