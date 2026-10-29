// Package dataservice is the single local discovery and verified acquisition entry.
package dataservice

import (
	"context"
	"fmt"
	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"sync"
	"time"
)

type What struct {
	Category    string `json:"category,omitempty"`
	Description string `json:"description,omitempty"`
}
type Need struct {
	Method          string                 `json:"method,omitempty"`
	ExtentPlan      *datasource.ExtentPlan `json:"extent_plan,omitempty"`
	MapFrameWidthMM float64                `json:"map_frame_width_mm,omitempty"`
	Paper           string                 `json:"paper,omitempty"`
	Geometry        string                 `json:"geometry,omitempty"`
	Name            string                 `json:"name"`
	What            What                   `json:"what"`
	Extent          string                 `json:"extent,omitempty"`
	Scale           string                 `json:"scale,omitempty"`
	Fields          map[string]string      `json:"fields,omitempty"`
	Params          map[string]string      `json:"params,omitempty"`
}
type FindRequest struct {
	Provided        []string `json:"provided,omitempty"`
	Need            Need     `json:"need"`
	Guide           string   `json:"guide,omitempty"`
	IncludeFallback bool     `json:"include_fallback"`
}
type Diagnostic struct {
	Code        string                 `json:"code"`
	Message     string                 `json:"message"`
	Row         string                 `json:"row,omitempty"`
	Suggestions []string               `json:"suggestions,omitempty"`
	Parameters  []ParameterRequirement `json:"parameters,omitempty"`
}
type ParameterRequirement struct {
	Path          string   `json:"path"`
	Required      bool     `json:"required"`
	AllowedValues []string `json:"allowed_values,omitempty"`
	ProvidedValue string   `json:"provided_value,omitempty"`
}
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string           { return e.Code + ": " + e.Message }
func failure(code, message string) error { return &Error{code, message} }

type Binding struct {
	Explicit bool     `json:"explicit,omitempty"`
	Current  bool     `json:"current,omitempty"`
	Sublayer string   `json:"sublayer,omitempty"`
	Name     string   `json:"name"`
	Category string   `json:"category,omitempty"`
	Path     string   `json:"path,omitempty"`
	LayerID  string   `json:"layer_id,omitempty"`
	Geometry string   `json:"geometry,omitempty"`
	Fields   []string `json:"fields,omitempty"`
}
type Candidate struct {
	StoreKey             string                   `json:"store_key,omitempty"`
	Method               string                   `json:"method,omitempty"`
	Geometry             string                   `json:"geometry,omitempty"`
	Confidence           string                   `json:"confidence,omitempty"`
	RequiresConfirmation bool                     `json:"requires_confirmation,omitempty"`
	MatchReason          string                   `json:"match_reason,omitempty"`
	Sublayer             string                   `json:"sublayer,omitempty"`
	ID                   string                   `json:"id"`
	Kind                 string                   `json:"kind"`
	Name                 string                   `json:"name"`
	SourceID             string                   `json:"source_id,omitempty"`
	Available            bool                     `json:"available"`
	Reasons              []string                 `json:"reasons"`
	Notes                []string                 `json:"notes"`
	Handbook             string                   `json:"handbook,omitempty"`
	Path                 string                   `json:"path,omitempty"`
	LayerID              string                   `json:"layer_id,omitempty"`
	Tier                 int                      `json:"tier"`
	Network              bool                     `json:"network"`
	Choice               *datasource.SourceChoice `json:"choice,omitempty"`
}
type Exclusion struct {
	Path     string `json:"path,omitempty"`
	SourceID string `json:"source_id,omitempty"`
	Reason   string `json:"reason"`
}
type FindRow struct {
	Row         string      `json:"row"`
	Category    string      `json:"category"`
	Recommended string      `json:"recommended,omitempty"`
	Candidates  []Candidate `json:"candidates"`
	Excluded    []Exclusion `json:"excluded"`
}
type Resolved struct {
	Category         string                 `json:"category,omitempty"`
	Extent           *datasource.ExtentPlan `json:"extent"`
	ScaleDenominator int                    `json:"scale_denominator"`
}
type FindResult struct {
	State             string       `json:"state"`
	NextAction        string       `json:"next_action"`
	ProvidedMatches   []Candidate  `json:"provided_matches"`
	ProvidedUnmatched []Exclusion  `json:"provided_unmatched"`
	FindID            string       `json:"find_id,omitempty"`
	CreatedAt         time.Time    `json:"created_at"`
	ExpiresAt         time.Time    `json:"expires_at"`
	Resolved          Resolved     `json:"resolved"`
	Rows              []FindRow    `json:"rows"`
	Fallback          []Candidate  `json:"fallback"`
	Diagnostics       []Diagnostic `json:"diagnostics"`
}
type SelectedRow struct {
	Route     string                     `json:"route,omitempty"`
	Row       string                     `json:"row"`
	Candidate Candidate                  `json:"candidate"`
	Item      datasource.AcquisitionItem `json:"item"`
}
type Selection struct {
	FindID     string        `json:"find_id"`
	Request    FindRequest   `json:"request"`
	Rows       []SelectedRow `json:"rows"`
	Fallback   []Candidate   `json:"fallback,omitempty"`
	SelectedAt time.Time     `json:"selected_at"`
}
type Target struct {
	FolderName string `json:"folder_name,omitempty"`
}
type GetRequest struct {
	FindID       string            `json:"find_id"`
	Choice       map[string]string `json:"choice"`
	Target       Target            `json:"target"`
	ResolvedData map[string]string `json:"resolved_data,omitempty"`
}
type GetRow struct {
	FindID      string            `json:"find_id,omitempty"`
	Row         string            `json:"row"`
	Category    string            `json:"category"`
	Paths       []string          `json:"paths"`
	LayerID     string            `json:"layer_id,omitempty"`
	SourceID    string            `json:"source_id,omitempty"`
	Kind        string            `json:"kind"`
	RetrievedAt time.Time         `json:"retrieved_at"`
	SHA256      map[string]string `json:"sha256"`
	License     string            `json:"license"`
	Attribution string            `json:"attribution"`
	Notes       []string          `json:"notes"`
	Warnings    []string          `json:"warnings"`
	StoreKey    string            `json:"store_key,omitempty"`
	Count       int               `json:"count"`
}
type GetResult struct {
	Preparation    *Selection `json:"preparation,omitempty"`
	FindID         string     `json:"find_id"`
	Status         string     `json:"status"`
	Folder         string     `json:"folder,omitempty"`
	Rows           []GetRow   `json:"rows"`
	ProvenancePath string     `json:"provenance_path"`
}
type RegisterRequest struct {
	Need         *Need    `json:"need,omitempty"`
	NeedID       string   `json:"need_id,omitempty"`
	SourceTitle  string   `json:"source_title,omitempty"`
	TableRef     string   `json:"table_ref,omitempty"`
	Method       string   `json:"method,omitempty"`
	OriginalPath string   `json:"original_path,omitempty"`
	FindID       string   `json:"find_id"`
	Row          string   `json:"row"`
	Path         string   `json:"path"`
	Kind         string   `json:"kind"`
	URL          string   `json:"url"`
	License      string   `json:"license"`
	Attribution  string   `json:"attribution"`
	Notes        []string `json:"notes,omitempty"`
}
type Options struct {
	SessionID, SessionDir string
	Environment           datasource.PlanningEnvironment
	Engine                *acquire.Engine
	Memory                *flow.DataMemory
	UserData              []Binding
	Layers                func(context.Context) ([]Binding, error)
	ResolveExtent         acquire.ExtentResolver
	Config                config.AcquisitionConfig
	Now                   func() time.Time
	Progress              func(string, acquire.Progress)
	NetworkConfirmed      bool
	FallbackConfirmed     bool
}
type Service struct {
	options          Options
	argumentMu       sync.Mutex
	argumentFailures map[string]int
}

func New(options Options) *Service {
	if options.Now == nil {
		options.Now = time.Now
	}
	options.Config = options.Config.WithDefaults()
	return &Service{options: options, argumentFailures: map[string]int{}}
}
func (s *Service) WithExecution(networkConfirmed, fallbackConfirmed bool, progress func(string, acquire.Progress)) *Service {
	options := s.options
	options.NetworkConfirmed = networkConfirmed
	options.FallbackConfirmed = fallbackConfirmed
	options.Progress = progress
	return New(options)
}
func NormalizeChoice(value any) (map[string]string, error) {
	out := map[string]string{}
	switch v := value.(type) {
	case nil:
		out["*"] = "recommended"
	case string:
		if v == "auto" {
			v = "recommended"
		}
		out["*"] = v
	case map[string]string:
		for k, x := range v {
			out[k] = x
		}
	case map[string]any:
		for k, x := range v {
			text, ok := x.(string)
			if !ok {
				return nil, failure("DATA_CHOICE_INVALID", "来源选择须为字符串")
			}
			out[k] = text
		}
	default:
		return nil, fmt.Errorf("DATA_CHOICE_INVALID: 来源选择格式无效")
	}
	return out, nil
}
