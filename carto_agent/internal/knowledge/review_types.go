package knowledge

import (
	"errors"
	"github.com/autocarto/carto_agent/internal/resource"
)

const (
	ReviewAwaiting      = "awaiting_review"
	ReviewCompiling     = "compiling"
	ReviewCompiled      = "compiled"
	ReviewCompileFailed = "compile_failed"
	MaxReviewGraphBytes = 16 << 20
	MaxReviewImageBytes = 20 << 20
)

var (
	ErrReviewNotFound = errors.New("知识审核草稿不存在")
	ErrReviewConflict = errors.New("知识审核草稿版本或状态已变化，请刷新后重试")
	ErrReviewInvalid  = errors.New("知识审核数据不合法")
	ErrReviewCorrupt  = errors.New("知识审核文件完整性校验失败")
)

type ReviewError struct {
	Path    string `json:"path,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// ReviewPendingIssue is a bounded, user-facing semantic diagnostic. Model
// request/response transcripts remain only in OriginalResult and trace files.
type ReviewPendingIssue struct {
	ID             string   `json:"id"`
	Kind           string   `json:"kind"`
	Phase          string   `json:"phase,omitempty"`
	Message        string   `json:"message"`
	SubjectRefs    []string `json:"subject_refs,omitempty"`
	Field          string   `json:"field,omitempty"`
	EvidenceQuotes []string `json:"evidence_quotes,omitempty"`
	Warning        bool     `json:"warning,omitempty"`
}

// ReviewIssueResolutionInput cannot supply provenance, hashes or revision.
// The server derives those from the saved graph and the immutable issue scope.
type ReviewIssueResolutionInput struct {
	IssueID     string `json:"issue_id"`
	Action      string `json:"action"` // confirmed, edited or excluded
	Explanation string `json:"explanation"`
}

type ReviewIssueResolution struct {
	IssueID         string   `json:"issue_id"`
	Action          string   `json:"action"`
	Explanation     string   `json:"explanation"`
	SubjectRefs     []string `json:"subject_refs"`
	SubjectSHA256   string   `json:"subject_sha256"`
	SnapshotVersion int      `json:"snapshot_version,omitempty"`
	Revision        int64    `json:"revision"`
	ResolvedAt      string   `json:"resolved_at"`
	Source          string   `json:"source"`
}

type ReviewManualRecord struct {
	Collection string `json:"collection"`
	ID         string `json:"id"`
	Origin     string `json:"origin"`
}

// ReviewProvenance is a flat historical audit, not active pending questions.
// Its revision belongs to ReviewID, never to the newly created review.
type ReviewProvenance struct {
	ReviewID         string                  `json:"review_id"`
	Revision         int64                   `json:"revision"`
	ManualRecords    []ReviewManualRecord    `json:"manual_records"`
	IssueResolutions []ReviewIssueResolution `json:"issue_resolutions"`
}

// ReviewResumeDescriptor contains only durable references, never Runner pointers.
type ReviewResumeDescriptor struct {
	CheckpointID    string `json:"checkpoint_id"`
	InterruptID     string `json:"interrupt_id,omitempty"`
	RuntimeRevision string `json:"runtime_revision,omitempty"`
	ExecutionMode   string `json:"execution_mode,omitempty"`
	ModePhase       string `json:"mode_phase,omitempty"`
	InstanceID      string `json:"instance_id,omitempty"`
	ProjectPath     string `json:"project_path,omitempty"`
}

// Review is the on-disk record. HTTP handlers intentionally return ReviewView.
type Review struct {
	Version           int             `json:"version"`
	ReviewID          string          `json:"review_id"`
	SessionID         string          `json:"session_id"`
	ProjectPath       string          `json:"project_path"`
	SessionDir        string          `json:"session_dir"`
	InstanceID        string          `json:"instance_id,omitempty"`
	SourceText        string          `json:"source_text"`
	SourceSHA256      string          `json:"source_sha256"`
	CompatibilityKey  string          `json:"compatibility_key"`
	Contract          Contract        `json:"contract"`
	OriginalResult    *Result         `json:"original_result"`
	OriginalCacheInfo map[string]any  `json:"original_cache_info,omitempty"`
	CompileArgs       map[string]any  `json:"compile_args,omitempty"`
	ResourceScope     *resource.Scope `json:"resource_scope,omitempty"`
	TraceFile         string          `json:"trace_file,omitempty"`
	Graph             Graph           `json:"graph"`
	InitialGraph      Graph           `json:"initial_graph,omitempty"`
	// Backend-owned snapshots keep automatic coordinate enrichment distinct from
	// human edits when generating signed review provenance.
	CoordinateLookups   map[string]map[string]any      `json:"coordinate_lookups,omitempty"`
	ExtractionStatus    string                         `json:"extraction_status,omitempty"`
	PendingIssues       []ReviewPendingIssue           `json:"pending_issues,omitempty"`
	Resolutions         []ReviewIssueResolution        `json:"resolutions,omitempty"`
	InheritedProvenance []ReviewProvenance             `json:"inherited_provenance,omitempty"`
	Revision            int64                          `json:"revision"`
	Status              string                         `json:"status"`
	Errors              []ReviewError                  `json:"errors"`
	Resume              *ReviewResumeDescriptor        `json:"resume,omitempty"`
	ClaimRequestID      string                         `json:"claim_request_id,omitempty"`
	ClaimedRevision     int64                          `json:"claimed_revision,omitempty"`
	ClaimedAt           string                         `json:"claimed_at,omitempty"`
	ApprovedGraph       Graph                          `json:"approved_graph,omitempty"`
	ApprovedRevision    int64                          `json:"approved_revision,omitempty"`
	ApprovedAt          string                         `json:"approved_at,omitempty"`
	Dataset             map[string]any                 `json:"dataset,omitempty"`
	Assets              []ReviewAsset                  `json:"assets,omitempty"`
	CalloutImports      map[string]CalloutImportSource `json:"callout_imports,omitempty"`
	CreatedAt           string                         `json:"created_at"`
	UpdatedAt           string                         `json:"updated_at"`
}

type ReviewCreateInput struct {
	SessionID, ProjectPath, SessionDir, InstanceID, SourceText string
	Contract                                                   Contract
	OriginalResult                                             *Result
	Graph                                                      Graph
	CompileArgs                                                map[string]any
	ResourceScope                                              *resource.Scope
	TraceFile                                                  string
	OriginalCacheInfo                                          map[string]any
	Assets                                                     []ReviewAsset
	ExtractionStatus                                           string
	PendingIssues                                              []ReviewPendingIssue
	ReusedFrom                                                 *Review
}

type ReviewView struct {
	ReviewID             string                         `json:"review_id"`
	SessionID            string                         `json:"session_id"`
	Revision             int64                          `json:"revision"`
	Status               string                         `json:"status"`
	Graph                Graph                          `json:"graph"`
	ExtractionStatus     string                         `json:"extraction_status,omitempty"`
	ExtractionCompletion *ExtractionCompletion          `json:"extraction_completion,omitempty"`
	PendingIssues        []ReviewPendingIssue           `json:"pending_issues,omitempty"`
	Resolutions          []ReviewIssueResolution        `json:"resolutions,omitempty"`
	SourceText           string                         `json:"source_text"`
	SourceSHA256         string                         `json:"source_sha256"`
	Errors               []ReviewError                  `json:"errors"`
	ApprovedRevision     int64                          `json:"approved_revision,omitempty"`
	Dataset              map[string]any                 `json:"dataset,omitempty"`
	Assets               []ReviewAsset                  `json:"assets,omitempty"`
	CalloutImports       map[string]CalloutImportSource `json:"callout_imports,omitempty"`
	UpdatedAt            string                         `json:"updated_at"`
}

func (r *Review) View() ReviewView {
	// Older drafts may contain a persisted error from the removed manual
	// resolution gate. Do not present that stale error as a current failure.
	errors := make([]ReviewError, 0, len(r.Errors))
	for _, issue := range r.Errors {
		if issue.Code != "semantic_review_pending" {
			errors = append(errors, issue)
		}
	}
	view := ReviewView{ReviewID: r.ReviewID, SessionID: r.SessionID, Revision: r.Revision, Status: r.Status, Graph: r.Graph, ExtractionStatus: r.ExtractionStatus, PendingIssues: visiblePendingIssues(r), Resolutions: r.Resolutions, SourceText: r.SourceText, SourceSHA256: r.SourceSHA256, Errors: errors, ApprovedRevision: r.ApprovedRevision, Dataset: r.Dataset, Assets: r.Assets, UpdatedAt: r.UpdatedAt}
	view.CalloutImports = r.CalloutImports
	if r.ExtractionStatus == "needs_review" && r.OriginalResult != nil {
		view.ExtractionCompletion = r.OriginalResult.Completion
	}
	return view
}

type ReviewAsset struct {
	AssetID    string `json:"asset_id"`
	ImagePath  string `json:"image_path"`
	MediaType  string `json:"media_type"`
	Size       int64  `json:"size"`
	PreviewURL string `json:"preview_url,omitempty"`
}
