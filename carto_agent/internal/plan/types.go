// Package plan defines the model-authored, deterministic cartographic plan contract.
package plan

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/autocarto/carto_agent/internal/flow"
)

const APIVersion = "carto-plan/v2"
const LegacyAPIVersion = "carto-plan/v1"
const MaxRevisions = 3

var Identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type GuideRef struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint,omitempty"`
}
type Fact struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
	From  string `json:"from"`
}
type Assumption struct {
	Text   string `json:"text"`
	Impact string `json:"impact"`
}
type Data struct {
	Source     string  `json:"source,omitempty"`
	Ref        string  `json:"ref,omitempty"`
	Need       *Need   `json:"need,omitempty"`
	Extent     *Extent `json:"extent,omitempty"`
	Method     string  `json:"method,omitempty"`
	LabelField string  `json:"label_field,omitempty"`
	Kind       string  `json:"kind,omitempty"`
	LayerName  string  `json:"layer_name,omitempty"`
	Style      string  `json:"style,omitempty"`
	Geometry   string  `json:"geometry,omitempty"`
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	From       string  `json:"from,omitempty"`
	Path       string  `json:"path,omitempty"`
	Layer      string  `json:"layer,omitempty"`
	FileID     string  `json:"file_id,omitempty"`
	Step       string  `json:"step,omitempty"`
	Output     string  `json:"output,omitempty"`
	FindID     string  `json:"find_id,omitempty"`
	Choice     any     `json:"choice,omitempty"`
}

// OutputLabel identifies the producer's output, independently of the data's
// display name. Old saved plans used Name as the reference and remain readable.
func (d Data) OutputLabel() string {
	if d.Output != "" {
		return d.Output
	}
	return d.Name
}

type Step struct {
	Inputs        []string                 `json:"inputs,omitempty"`
	Produces      []Product                `json:"produces,omitempty"`
	ID            string                   `json:"id"`
	Title         string                   `json:"title"`
	Kind          string                   `json:"kind,omitempty"`
	Capabilities  []string                 `json:"capabilities,omitempty"`
	RoleHint      string                   `json:"role_hint,omitempty"`
	Uses          []string                 `json:"uses,omitempty"`
	Data          []string                 `json:"data,omitempty"`
	DependsOn     []string                 `json:"depends_on,omitempty"`
	Layers        []string                 `json:"layers,omitempty"`
	GuideSections []string                 `json:"guide_sections,omitempty"`
	Instructions  string                   `json:"instructions,omitempty"`
	Parameters    map[string]string        `json:"parameters,omitempty"`
	Outputs       []flow.OutputDeclaration `json:"outputs,omitempty"`
	Optional      bool                     `json:"optional,omitempty"`
}
type Plan struct {
	APIVersion   string                   `json:"api_version"`
	ID           string                   `json:"plan_id"`
	Version      int                      `json:"version"`
	Goal         string                   `json:"goal"`
	Guide        *GuideRef                `json:"guide,omitempty"`
	Facts        []Fact                   `json:"facts"`
	Assumptions  []Assumption             `json:"assumptions"`
	Data         []Data                   `json:"data"`
	Steps        []Step                   `json:"steps"`
	Deliverables []flow.OutputDeclaration `json:"deliverables"`
	Checks       []string                 `json:"checks"`
}

// Snapshot is persisted with each version and is also the plan-card wire contract.
type Snapshot struct {
	Precheck            []Precheck                 `json:"precheck,omitempty"`
	Notes               []string                   `json:"notes,omitempty"`
	Diagnostics         []flow.Diagnostic          `json:"diagnostics,omitempty"`
	Selections          map[string]json.RawMessage `json:"data_selections,omitempty"`
	Plan                *Plan                      `json:"plan"`
	SessionID           string                     `json:"session_id"`
	PlanID              string                     `json:"plan_id"`
	PlanVersion         int                        `json:"plan_version"`
	Status              string                     `json:"status"`
	NeedsConfirmation   bool                       `json:"needs_confirmation"`
	Replanning          bool                       `json:"replanning,omitempty"`
	ConfirmationReasons []string                   `json:"confirmation_reasons"`
	ExecutionMode       string                     `json:"execution_mode"`
	FindResults         map[string]any             `json:"find_results,omitempty"`
	RunID               string                     `json:"flow_run_id,omitempty"`
	Run                 any                        `json:"run,omitempty"`
	CreatedAt           time.Time                  `json:"created_at"`
	UpdatedAt           time.Time                  `json:"updated_at"`
	OriginalSource      string                     `json:"original_source,omitempty"`
	GuideSnapshot       json.RawMessage            `json:"guide_snapshot,omitempty"`
}

func NewID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "cp-" + time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b), nil
}
func Clone(p *Plan) (*Plan, error) {
	if p == nil {
		return nil, fmt.Errorf("计划为空")
	}
	type storedPlan Plan
	b, e := json.Marshal((*storedPlan)(p))
	if e != nil {
		return nil, e
	}
	var out storedPlan
	e = json.Unmarshal(b, &out)
	return (*Plan)(&out), e
}
func (p *Plan) DataByID(id string) (Data, bool) {
	for _, d := range p.Data {
		if d.ID == id {
			return d, true
		}
	}
	return Data{}, false
}
func (p *Plan) StepByID(id string) (Step, bool) {
	for _, s := range p.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return Step{}, false
}

type Attachment struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}
type LayerReference struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type EditRequest struct {
	Source  string `json:"source,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Method  string `json:"method,omitempty"`
	Version int    `json:"version"`
	DataID  string `json:"data_id"`
	From    string `json:"from"`
	Choice  any    `json:"choice,omitempty"`
	Path    string `json:"path,omitempty"`
	Layer   string `json:"layer,omitempty"`
	FileID  string `json:"file_id,omitempty"`
}
type ActionRequest struct {
	Version             int    `json:"version"`
	Action              string `json:"action,omitempty"`
	DataID              string `json:"data_id,omitempty"`
	Path                string `json:"path,omitempty"`
	ResumeRunID         string `json:"resume_run_id,omitempty"`
	ResourceChoice      string `json:"resume_resource_choice,omitempty"`
	ResourceFingerprint string `json:"resource_fingerprint,omitempty"`
}
