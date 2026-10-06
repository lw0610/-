package websocket

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/session"
)

const sessionPlanContextKey = "plan_context"

func normalizePlanContextStatus(status session.PlanContextStatus) session.PlanContextStatus {
	switch strings.ToLower(strings.TrimSpace(string(status))) {
	case string(session.PlanContextStatusApproved):
		return session.PlanContextStatusApproved
	case string(session.PlanContextStatusExecuting):
		return session.PlanContextStatusExecuting
	case string(session.PlanContextStatusCompleted):
		return session.PlanContextStatusCompleted
	default:
		return session.PlanContextStatusDraft
	}
}

func clonePlanContext(plan *session.PlanContext) *session.PlanContext {
	if plan == nil {
		return nil
	}

	raw, err := json.Marshal(plan)
	if err != nil {
		fallback := *plan
		return &fallback
	}

	var out session.PlanContext
	if err := json.Unmarshal(raw, &out); err != nil {
		fallback := *plan
		return &fallback
	}

	out.Status = normalizePlanContextStatus(out.Status)
	if out.CreatedAt.IsZero() {
		out.CreatedAt = time.Now()
	}
	return &out
}

func decodePlanContext(raw any) *session.PlanContext {
	if raw == nil {
		return nil
	}

	bytes, err := json.Marshal(raw)
	if err != nil {
		return nil
	}

	var plan session.PlanContext
	if err := json.Unmarshal(bytes, &plan); err != nil {
		return nil
	}

	plan.Status = normalizePlanContextStatus(plan.Status)
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = time.Now()
	}
	return &plan
}

func encodePlanContext(plan *session.PlanContext) (map[string]any, error) {
	if plan == nil {
		return nil, nil
	}

	normalized := clonePlanContext(plan)
	raw, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}

	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
