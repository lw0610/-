// Package knowledge coordinates bounded model extraction. Semantic relationships
// are model decisions with source evidence; no name/distance heuristic joins paths.
package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type Contract struct {
	ContractID            string          `json:"contract_id"`
	SourceSHA256          string          `json:"source_sha256"`
	Instructions          string          `json:"instructions"`
	JSONSchema            json.RawMessage `json:"json_schema"`
	RuntimeSessionDir     string          `json:"runtime_session_dir,omitempty"`
	ReviewProtocolVersion int             `json:"review_protocol_version,omitempty"`
}

// A typed context value is deliberately used instead of ADK SessionValues:
// nonempty SessionValues enable FString interpolation of ALL agent prompts,
// which would misinterpret literal JSON examples as template variables.
type sourceContextKey struct{}

func WithOriginalSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, sourceContextKey{}, source)
}

func OriginalSource(ctx context.Context) (string, bool) {
	source, ok := ctx.Value(sourceContextKey{}).(string)
	return source, ok
}

type Config struct {
	// Positive character limits are an explicit override for callers/tests.
	// Otherwise chunks are planned from the configured model budgets.
	TargetChars, MaxChars, MaxOutputTokens, MaxContextTokens int
	// Retained for legacy configuration compatibility. Extraction no longer runs
	// an additional model review, regardless of this flag.
	ReviewAllRoutes bool
	Checkpoint      func(*Result) error
}

func DefaultConfig() Config { return Config{MaxOutputTokens: 8192} }

type Progress struct {
	Stage          string
	Current, Total int
	ID             string
	Attempt        int
	Status         string
	StartedAt      string
	ElapsedMillis  int64
}
type Fragment struct {
	Chunk      Chunk   `json:"chunk"`
	Graph      Graph   `json:"graph"`
	Unresolved []Issue `json:"unresolved"`
}
type Attempt struct {
	Stage  string `json:"stage"`
	ID     string `json:"id"`
	Number int    `json:"attempt"`
	// Status describes the request, not semantic validation of its response.
	Status            string                    `json:"status"`
	StartedAt         string                    `json:"started_at,omitempty"`
	FinishedAt        string                    `json:"finished_at,omitempty"`
	DurationMillis    int64                     `json:"duration_ms"`
	FinishReason      string                    `json:"finish_reason,omitempty"`
	Response          string                    `json:"response,omitempty"`
	Error             string                    `json:"error,omitempty"`
	ResponseShape     string                    `json:"response_shape,omitempty"`
	InputEstimate     int                       `json:"input_token_estimate"`
	OutputLimit       int                       `json:"max_output_tokens"`
	CandidateRevision int                       `json:"candidate_revision,omitempty"`
	Validation        *FragmentValidationReport `json:"validation,omitempty"`
}
type BoundaryRecord struct {
	Left            string           `json:"left"`
	Right           string           `json:"right"`
	Plan            BoundaryPlan     `json:"plan"`
	Applied         bool             `json:"applied"`
	ContextChunks   []string         `json:"context_chunks,omitempty"`
	EvidenceSources []EvidenceSource `json:"evidence_sources,omitempty"`
	Error           string           `json:"error,omitempty"`
}
type Result struct {
	Status          string            `json:"status"`
	Error           string            `json:"error,omitempty"`
	ContractID      string            `json:"contract_id"`
	SourceSHA256    string            `json:"source_sha256"`
	Chunks          []Fragment        `json:"chunks"`
	Attempts        []Attempt         `json:"attempts"`
	Boundaries      []BoundaryRecord  `json:"boundaries"`
	RouteReviews    []RouteReview     `json:"route_reviews,omitempty"`
	SemanticReviews []SemanticReview  `json:"semantic_reviews,omitempty"`
	Issues          []Issue           `json:"issues"`
	Aliases         map[string]string `json:"aliases"`
	Graph           Graph             `json:"graph"`
	// Preserve semantic extraction independently from the six-table adaptation.
	SemanticGraph      Graph                 `json:"semantic_graph,omitempty"`
	Budget             *Budget               `json:"budget,omitempty"`
	FragmentCandidates []FragmentCandidate   `json:"fragment_candidates,omitempty"`
	Completion         *ExtractionCompletion `json:"completion,omitempty"`
}

type Pipeline struct {
	cm  model.BaseChatModel
	cfg Config
}

func NewPipeline(cm model.BaseChatModel, cfg Config) *Pipeline {
	defaults := DefaultConfig()
	if cfg.TargetChars <= 0 && cfg.MaxChars > 0 {
		cfg.TargetChars = max(1, cfg.MaxChars-cfg.MaxChars/4)
	}
	if cfg.TargetChars > 0 && cfg.MaxChars < cfg.TargetChars {
		cfg.MaxChars = cfg.TargetChars * 3 / 2
	}
	if cfg.MaxOutputTokens <= 0 {
		cfg.MaxOutputTokens = defaults.MaxOutputTokens
	}
	return &Pipeline{cm: cm, cfg: cfg}
}

func (p *Pipeline) save(r *Result) error {
	if p.cfg.Checkpoint != nil {
		return p.cfg.Checkpoint(r)
	}
	return nil
}

func (p *Pipeline) Run(ctx context.Context, source string, contract Contract, progress func(Progress)) (r *Result, err error) {
	ctx = withRequestProgress(ctx, progress)
	r = &Result{Status: "extracting", ContractID: contract.ContractID, SourceSHA256: contract.SourceSHA256, Aliases: map[string]string{}, Completion: &ExtractionCompletion{Version: ExtractionProtocolVersion, SemanticSkipped: true}}
	defer func() {
		if err != nil {
			if r.Status != "compile_blocked" && r.Status != "needs_review" {
				r.Status = "blocked"
			}
			r.Error = err.Error()
		}
		if e := p.save(r); e != nil {
			err = errors.Join(err, fmt.Errorf("保存抽取记录: %w", e))
			r.Status, r.Error = "blocked", err.Error()
		}
	}()
	if strings.TrimSpace(source) == "" {
		return r, errors.New("抽取原文为空")
	}
	if p.cm == nil {
		return r, errors.New("知识抽取模型未配置")
	}
	if contract.ContractID == "" || contract.Instructions == "" || !json.Valid(contract.JSONSchema) {
		return r, errors.New("知识抽取契约不完整，请更新 QGIS 知识工具以支持 compact 契约")
	}
	if fmt.Sprintf("%x", sha256.Sum256([]byte(source))) != contract.SourceSHA256 {
		return r, errors.New("原文与抽取契约摘要不一致")
	}
	budget, err := planBudget(p.cfg, source, contract)
	r.Budget = &budget
	if err != nil {
		return r, err
	}
	var chunks []Chunk
	if p.cfg.TargetChars > 0 {
		chunks = SplitText(source, p.cfg.TargetChars, p.cfg.MaxChars)
	} else {
		chunks = splitTextByBudget(source, budget.SourceBudget)
	}
	for i, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		chunk.ID = fmt.Sprintf("c%04d", i+1)
		if progress != nil {
			progress(Progress{Stage: "分段抽取", Current: i + 1, Total: len(chunks)})
		}
		if err := p.extract(ctx, r, chunk, contract, 0); err != nil {
			return r, err
		}
	}
	r.Completion.ExtractionComplete = true
	graphs := make([]Graph, 0, len(r.Chunks))
	for _, fragment := range r.Chunks {
		graphs = append(graphs, fragment.Graph)
		r.Issues = append(r.Issues, fragment.Unresolved...)
	}
	r.Graph, err = MergeGraphs(graphs)
	if err != nil {
		return r, err
	}
	if HasPartialExtraction(r) {
		// A local format failure must not hide the other fragments. The draft
		// keeps unknown references for the editable results page.
		r.SemanticGraph, err = cloneGraph(r.Graph)
		if err != nil {
			return r, err
		}
		r.Status = "needs_review"
		return r, &ExtractionNeedsReviewError{Message: "已保留可读取的抽取内容；局部格式问题记录在过程文件中"}
	}
	// Keep only deterministic graph preparation after extraction. In particular,
	// do not ask the model to rewrite the complete graph a second time.
	// A secondary path can repeat its unique main-path junction while naming
	// the main path as attach_ref. This is an unambiguous representation error,
	// not a new movement inference; repair it before consistency inspection.
	NormalizeRouteAttachments(r.Graph)
	r.SemanticGraph, err = cloneGraph(r.Graph)
	if err != nil {
		return r, err
	}
	if hasBlockingExtractionIssues(r.Issues) || len(graphConsistencyPendingIssues(r.Graph)) > 0 {
		r.Status = "needs_review"
		return r, &ExtractionNeedsReviewError{Message: "知识抽取结果已保留，可在结果页面修改"}
	}
	r.Status = "adapting"
	if err = deriveRouteRendering(r.Graph); err != nil {
		// Rendering may have filled fields on earlier routes before finding a
		// later unsupported one. Hand review the untouched semantic graph.
		cleanGraph, cloneErr := cloneGraph(r.SemanticGraph)
		if cloneErr != nil {
			return r, cloneErr
		}
		r.Graph = cleanGraph
		if pending := routeAdapterIssues(r.Graph); len(pending) > 0 {
			r.Issues = append(r.Issues, pending...)
			r.Status = "needs_review"
			return r, &ExtractionNeedsReviewError{Message: "知识事实已抽取，路径结构已保留供结果页面编辑"}
		}
		r.Status = "compile_blocked"
		return r, fmt.Errorf("知识已抽取，制图结构需要调整: %w", err)
	}
	r.Status = "ready_to_compile"
	r.Completion.AdaptationComplete = true
	return r, nil
}

var errTruncated = errors.New("模型输出达到长度上限，未接收部分结果")
var errInputBudget = errors.New("模型上下文预算不足")

// Each request has a fresh, bounded context. UTF-8 bytes are a deliberately
// conservative budget guard, NOT a claim that characters equal model tokens.
func (p *Pipeline) request(ctx context.Context, r *Result, stage, id, system string, input any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	started := time.Now()
	attempt := Attempt{Stage: stage, ID: id, Number: 1, Status: "running", StartedAt: started.UTC().Format(time.RFC3339Nano), InputEstimate: len(system) + len(body), OutputLimit: p.cfg.MaxOutputTokens}
	for _, previous := range r.Attempts {
		if previous.Stage == stage && previous.ID == id {
			attempt.Number++
		}
	}
	index := len(r.Attempts)
	r.Attempts = append(r.Attempts, attempt)
	// Persist before Generate: a long or interrupted request must not look like
	// the last completed attempt is still being processed locally.
	if err := p.save(r); err != nil {
		attempt.Status, attempt.Error = "failed", err.Error()
		attempt.FinishedAt, attempt.DurationMillis = time.Now().UTC().Format(time.RFC3339Nano), time.Since(started).Milliseconds()
		r.Attempts[index] = attempt
		return "", err
	}
	observer := requestProgressFromContext(ctx)
	var msg *schema.Message
	var callErr error
	if p.cfg.MaxContextTokens > 0 && attempt.InputEstimate > p.cfg.MaxContextTokens-p.cfg.MaxOutputTokens-requestReserve {
		callErr = fmt.Errorf("%w（%s/%s）：输入保守估算 %d，最大输出 %d，额外预留 %d，上下文 Token %d；请核对模型设置", errInputBudget, stage, id, attempt.InputEstimate, p.cfg.MaxOutputTokens, requestReserve, p.cfg.MaxContextTokens)
	} else if ctx.Err() != nil {
		callErr = ctx.Err()
	} else {
		stopHeartbeat := startRequestHeartbeat(ctx, observer, attempt, started, requestHeartbeatInterval)
		defer stopHeartbeat()
		msg, callErr = p.cm.Generate(ctx, []*schema.Message{schema.SystemMessage(system), schema.UserMessage(string(body))}, model.WithMaxTokens(p.cfg.MaxOutputTokens))
		stopHeartbeat()
		// Do not accept a late response if cancellation raced with Generate.
		if ctx.Err() != nil {
			callErr = ctx.Err()
		}
	}
	if msg != nil {
		attempt.Response = msg.Content
		if msg.ResponseMeta != nil {
			attempt.FinishReason = msg.ResponseMeta.FinishReason
		}
	}
	if callErr == nil && msg == nil {
		callErr = errors.New("模型返回为空")
	}
	if callErr == nil {
		switch strings.ToLower(attempt.FinishReason) {
		case "length", "max_tokens", "max_output_tokens", "model_length":
			callErr = errTruncated
		case "content_filter", "safety", "refusal":
			callErr = errors.New("模型未返回可用的抽取结果")
		}
	}
	attempt.Status = "response_received"
	if callErr != nil {
		attempt.Status = "failed"
		if errors.Is(callErr, context.Canceled) || errors.Is(callErr, context.DeadlineExceeded) {
			attempt.Status = "cancelled"
		}
		attempt.Error = callErr.Error()
	}
	attempt.FinishedAt, attempt.DurationMillis = time.Now().UTC().Format(time.RFC3339Nano), time.Since(started).Milliseconds()
	r.Attempts[index] = attempt
	if err := p.save(r); err != nil {
		return "", err
	}
	if observer != nil {
		observer(progressForAttempt(attempt, attempt.DurationMillis))
	}
	return attempt.Response, callErr
}

func decodeObject(text string, dest any) error {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```json") && strings.HasSuffix(text, "```") {
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "```json"), "```"))
	} else if strings.HasPrefix(text, "```") && strings.HasSuffix(text, "```") {
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "```"), "```"))
	}
	if !strings.HasPrefix(text, "{") || !json.Valid([]byte(text)) {
		return errors.New("模型输出不是完整 JSON 对象")
	}
	if err := rejectDuplicateJSONKeys(text); err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dest)
}
