package session

import (
	"encoding/json"
	"time"
)

// PlanContextStatus 规划上下文状态。
type PlanContextStatus string

const (
	PlanContextStatusDraft     PlanContextStatus = "draft"
	PlanContextStatusApproved  PlanContextStatus = "approved"
	PlanContextStatusExecuting PlanContextStatus = "executing"
	PlanContextStatusCompleted PlanContextStatus = "completed"
)

// PlanContext 持久化规划上下文。
type PlanContext struct {
	RawOutput      string            `json:"raw_output,omitempty"`
	StructuredPlan *StructuredPlan   `json:"structured_plan,omitempty"`
	Summary        string            `json:"summary,omitempty"`
	CreatedAt      time.Time         `json:"created_at,omitempty"`
	Status         PlanContextStatus `json:"status,omitempty"`
	EvalResult     *PlanEvalResult   `json:"eval_result,omitempty"`
}

// StructuredPlan 结构化计划。
type StructuredPlan struct {
	Goal        string     `json:"goal,omitempty"`
	Constraints []string   `json:"constraints,omitempty"`
	Steps       []PlanStep `json:"steps,omitempty"`
	Risks       []string   `json:"risks,omitempty"`
}

// PlanStep 结构化计划步骤。
type PlanStep struct {
	Index       int      `json:"index,omitempty"`
	Description string   `json:"description,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	DependsOn   []int    `json:"depends_on,omitempty"`
	Status      string   `json:"status,omitempty"`
}

// PlanEvalResult 规划评估结果。
type PlanEvalResult struct {
	Score       int      `json:"score,omitempty"`
	Approved    bool     `json:"approved"`
	Issues      []string `json:"issues,omitempty"`
	Suggestions []string `json:"suggestions,omitempty"`
	RawOutput   string   `json:"raw_output,omitempty"`
}

func clonePlanContext(plan *PlanContext) *PlanContext {
	if plan == nil {
		return nil
	}

	raw, err := json.Marshal(plan)
	if err != nil {
		clone := *plan
		return &clone
	}

	var clone PlanContext
	if err := json.Unmarshal(raw, &clone); err != nil {
		fallback := *plan
		return &fallback
	}

	return &clone
}
