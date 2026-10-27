package api

import "github.com/autocarto/carto_agent/internal/flow"

func summarizeFlowData(plan *flow.FlowPlan) map[string]int {
	counts := map[string]int{"requirements": 0, "items": 0, "acquirable": 0, "available": 0}
	if plan == nil {
		return counts
	}
	counts["requirements"] = len(plan.Requirements)
	if plan.Acquisition == nil {
		return counts
	}
	counts["items"] = len(plan.Acquisition.Items)
	for _, item := range plan.Acquisition.Items {
		if item.Acquirable {
			counts["acquirable"]++
		}
		if item.Acquirable && item.Available {
			counts["available"]++
		}
	}
	return counts
}
