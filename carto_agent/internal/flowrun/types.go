package flowrun

import (
	"context"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/resource"
	"time"

	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/cloudwego/eino/schema"
)

const APIVersion = "flow-run/v1"

type StepRunner interface {
	Run(context.Context, StepRequest) (StepResult, error)
}
type ToolInvoker interface {
	Invoke(context.Context, string, map[string]any) (any, error)
}
type ModelCaller interface {
	CallJSON(context.Context, string) (string, error)
}
type EventSink interface {
	Emit(context.Context, Event) int64
}
type RunStore interface {
	Save(context.Context, *RunState) error
	Load(context.Context, string, string) (*RunState, error)
}

type Options struct {
	DataGet            func(context.Context, *RunState, plan.Data) (DataResult, error)
	ValidatePlanResume func(context.Context, *RunState) error
	FinalChecks        func(context.Context, *RunState) Verification
	Lifecycle          context.Context
	Runner             StepRunner
	Tools              ToolInvoker
	Model              ModelCaller
	Events             EventSink
	Store              RunStore
	Executors          *ExecutorRegistry
	Now                func() time.Time
}

type StartRequest struct {
	CartoPlan      *plan.Plan
	Guide          *flow.GuideDoc
	Selections     map[string]json.RawMessage
	ProjectPath    string
	FlowRunID      string // Optional trusted reservation for plan execution; validated before use.
	SessionID      string
	SessionDir     string
	ProjectRoot    string
	FlowID         string
	Scheme         string
	Inputs         map[string]any
	OriginalSource string
	Origin         string
}

type ResumeRequest struct {
	ResourceChoice      string
	ResourceFingerprint string
	SessionID           string
	SessionDir          string
	FlowRunID           string
	OriginalSource      string
}

type StepRequest struct {
	Acquisition    *DataPreparation
	Overlay        *RoleOverlaySpec
	FlowID         string
	FlowDir        string
	Lock           *resource.Lock
	Resources      []*resource.ResolvedResource
	SessionID      string
	SessionDir     string
	FlowRunID      string
	RunID          string
	Step           flow.StepPlan
	Role           flow.RoleSpec
	Messages       []*schema.Message
	OriginalSource string
	// Mode is step or confirm. Confirm steps use the existing HITL agent.
	Mode         string
	Confirmation *ConfirmationRequest
}

type ToolSummary struct {
	Name   string `json:"name"`
	Result string `json:"result"`
}
type StepResult struct {
	Err             error
	LastContent     string
	Interrupted     bool
	SuccessfulTools []ToolSummary
}

type Receipt struct {
	Status  string         `json:"status"`
	Summary string         `json:"summary"`
	Outputs map[string]any `json:"outputs"`
	Issues  []string       `json:"issues"`
}
type Verification struct {
	OK     bool     `json:"ok"`
	Errors []string `json:"errors"`
}
type ConditionResult struct {
	Evaluated bool   `json:"evaluated"`
	Result    bool   `json:"result"`
	By        string `json:"by"`
	Reason    string `json:"reason"`
}
type Choice struct {
	Choice   string `json:"choice"`
	Guidance string `json:"guidance,omitempty"`
}
type ReceiptRepair struct {
	Input  string `json:"input"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}
type Attempt struct {
	DataID          string         `json:"data_id,omitempty"`
	UserInitiated   bool           `json:"user_initiated,omitempty"`
	RunID           string         `json:"run_id"`
	Mode            string         `json:"mode"`
	StartedAt       time.Time      `json:"started_at"`
	FinishedAt      *time.Time     `json:"finished_at,omitempty"`
	Receipt         *Receipt       `json:"receipt,omitempty"`
	ReceiptRepaired bool           `json:"receipt_repaired"`
	Repair          *ReceiptRepair `json:"repair,omitempty"`
	Verification    Verification   `json:"verification"`
	UserChoice      *Choice        `json:"user_choice"`
}
type StepState struct {
	PendingManual     bool            `json:"pending_manual,omitempty"`
	ID                string          `json:"id"`
	Index             int             `json:"index"`
	Title             string          `json:"title"`
	Role              string          `json:"role"`
	Status            string          `json:"status"`
	Condition         ConditionResult `json:"condition"`
	Attempts          []Attempt       `json:"attempts"`
	Outputs           map[string]any  `json:"outputs"`
	LastFailure       string          `json:"last_failure,omitempty"`
	UserGuidance      string          `json:"user_guidance,omitempty"`
	AutomaticFailures int             `json:"automatic_failures"`
	UserRetries       int             `json:"user_retries"`
	AwaitingChoice    bool            `json:"awaiting_choice,omitempty"`
	UpstreamTruncated bool            `json:"upstream_truncated,omitempty"`
}
type RunResult struct {
	Deliverables map[string]any `json:"deliverables"`
	Issues       []string       `json:"issues"`
	Summary      string         `json:"summary"`
}
type AcquisitionState struct {
	Plan  flow.AcquisitionPlan `json:"plan"`
	Items []acquire.ItemResult `json:"items"`
}
type RunState struct {
	CartoPlan           *plan.Plan                   `json:"carto_plan,omitempty"`
	PlanHistory         []*plan.Plan                 `json:"plan_history,omitempty"`
	Guide               *flow.GuideDoc               `json:"guide,omitempty"`
	Selections          map[string]json.RawMessage   `json:"data_selections,omitempty"`
	Revisions           int                          `json:"revisions"`
	FinalVerification   *Verification                `json:"final_verification,omitempty"`
	LayerOverrides      map[string]map[string]string `json:"layer_overrides,omitempty"`
	ProjectPath         string                       `json:"project_path,omitempty"`
	Acquisition         *AcquisitionState            `json:"acquisition,omitempty"`
	AcquiredInputs      map[string]any               `json:"acquired_inputs,omitempty"`
	EntryOverrides      map[string]map[string]string `json:"entry_overrides,omitempty"`
	SourceNotes         []string                     `json:"source_notes,omitempty"`
	ResourceChange      *ResourceChange              `json:"resource_change,omitempty"`
	ResourceFingerprint string                       `json:"resource_fingerprint,omitempty"`
	EventSeq            int64                        `json:"event_seq"`
	APIVersion          string                       `json:"api_version"`
	ID                  string                       `json:"flow_run_id"`
	SessionID           string                       `json:"session_id"`
	SessionDir          string                       `json:"session_dir"`
	ProjectRoot         string                       `json:"project_root,omitempty"`
	FlowID              string                       `json:"flow_id"`
	FlowVersion         string                       `json:"flow_version"`
	PlanCacheKey        string                       `json:"plan_cache_key"`
	Scheme              string                       `json:"scheme"`
	Origin              string                       `json:"origin"`
	Status              string                       `json:"status"`
	StartedAt           time.Time                    `json:"started_at"`
	FinishedAt          *time.Time                   `json:"finished_at,omitempty"`
	Inputs              map[string]any               `json:"inputs"`
	Cursor              int                          `json:"cursor"`
	Steps               []StepState                  `json:"steps"`
	Result              RunResult                    `json:"result"`
	RunJSONPath         string                       `json:"run_json_path"`
	Plan                *flow.FlowPlan               `json:"plan"`
}
type Event struct {
	PlanID      string         `json:"plan_id,omitempty"`
	PlanVersion int            `json:"plan_version,omitempty"`
	Event       string         `json:"event"`
	SessionID   string         `json:"session_id"`
	FlowRunID   string         `json:"flow_run_id,omitempty"`
	Payload     map[string]any `json:"payload"`
}

func IsTerminal(status string) bool {
	switch status {
	case "completed", "completed_with_issues", "failed", "cancelled":
		return true
	}
	return false
}

// DataResult comes from the unified service after normalization and verification.
type DataResult struct {
	Preparation    *DataPreparation
	Value          any
	EntryOverrides map[string]string
	LayerOverrides map[string]string
	Notes          []string
	Outputs        map[string]any
}

// DataPreparation is a trusted acquisition handoff, not model-authored output.
// The controller runs it through the same step runner and interrupt lifecycle
// as ordinary steps, then asks DataGet to verify the registered result.
type DataPreparation struct {
	DataID    string
	Selection json.RawMessage
	Role      flow.RoleSpec
	Messages  []*schema.Message
}
