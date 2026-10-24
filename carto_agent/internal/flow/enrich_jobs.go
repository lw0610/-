package flow

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/autocarto/carto_agent/internal/logger"
)

// EnrichmentOptions applies to a manager for its process lifetime. Model selection
// is loaded afresh with the environment when a task is queued.
type EnrichmentOptions struct {
	Concurrency             int
	CallTimeout, JobTimeout time.Duration
	Broadcast               func(CompileJobStatus)
}
type EnqueueOptions struct {
	Force  bool
	Reason string
}
type CompileJobStatus struct {
	JobID      string        `json:"job_id"`
	FlowID     string        `json:"flow_id"`
	Scheme     string        `json:"scheme"`
	Reason     string        `json:"reason"`
	State      string        `json:"state"`
	Done       int           `json:"done"`
	Total      int           `json:"total"`
	Failed     []StepFailure `json:"failed"`
	StartedAt  *time.Time    `json:"started_at,omitempty"`
	FinishedAt *time.Time    `json:"finished_at,omitempty"`
}
type enrichmentJob struct {
	status    CompileJobStatus
	signature string
	source    *compilationSource
	units     []enrichmentUnit
	pending   []enrichmentUnit
	force     bool
	cancel    context.CancelFunc
}

// EnrichmentManager owns background jobs. Only Start supplies its context;
// HTTP request cancellation never becomes a task's cancellation source.
type EnrichmentManager struct {
	compiler   *Compiler
	options    EnrichmentOptions
	mu         sync.Mutex
	ctx        context.Context
	jobs       map[string]*enrichmentJob
	slots      chan struct{}
	events     chan struct{}
	eventQueue []CompileJobStatus
	sequence   atomic.Uint64
}

func NewEnrichmentManager(compiler *Compiler, opts EnrichmentOptions) *EnrichmentManager {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 3
	}
	if opts.CallTimeout <= 0 {
		opts.CallTimeout = 90 * time.Second
	}
	if opts.JobTimeout <= 0 {
		opts.JobTimeout = 15 * time.Minute
	}
	m := &EnrichmentManager{compiler: compiler, options: opts, jobs: map[string]*enrichmentJob{}, slots: make(chan struct{}, opts.Concurrency), events: make(chan struct{}, 1)}
	if compiler != nil {
		compiler.jobsMu.Lock()
		compiler.jobs = m
		compiler.jobsMu.Unlock()
	}
	return m
}
func (m *EnrichmentManager) Start(ctx context.Context) {
	m.mu.Lock()
	if m.ctx != nil {
		m.mu.Unlock()
		return
	}
	m.ctx = ctx
	m.mu.Unlock()
	go func() {
		for {
			select {
			case <-m.events:
				for {
					m.mu.Lock()
					if len(m.eventQueue) == 0 {
						m.mu.Unlock()
						break
					}
					status := m.eventQueue[0]
					m.eventQueue = m.eventQueue[1:]
					m.mu.Unlock()
					if m.options.Broadcast != nil {
						m.options.Broadcast(status)
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
func activeJob(state string) bool     { return state == "queued" || state == "running" }
func jobKey(id, scheme string) string { return id + "\x00" + scheme }
func cloneJobStatus(status CompileJobStatus) CompileJobStatus {
	status.Failed = append([]StepFailure{}, status.Failed...)
	if status.StartedAt != nil {
		at := *status.StartedAt
		status.StartedAt = &at
	}
	if status.FinishedAt != nil {
		at := *status.FinishedAt
		status.FinishedAt = &at
	}
	return status
}
func (m *EnrichmentManager) emitLocked(job *enrichmentJob) {
	// Queue delivery independently: a slow/disconnected websocket client must not
	// delay HTTP responses or block cache persistence while holding the job lock.
	if m.options.Broadcast == nil {
		return
	}
	m.eventQueue = append(m.eventQueue, cloneJobStatus(job.status))
	select {
	case m.events <- struct{}{}:
	default:
	}
}
func (m *EnrichmentManager) canonicalScheme(scheme string) string {
	if strings.TrimSpace(scheme) != "" {
		return scheme
	}
	m.mu.Lock()
	ctx := m.ctx
	m.mu.Unlock()
	if ctx != nil && m.compiler != nil && m.compiler.options.LoadEnvironment != nil {
		if env, err := m.compiler.options.LoadEnvironment(ctx, scheme); err == nil {
			return env.Scheme
		}
	}
	return scheme
}
func (m *EnrichmentManager) Status(id, scheme string) (CompileJobStatus, bool) {
	scheme = m.canonicalScheme(scheme)
	m.mu.Lock()
	defer m.mu.Unlock()
	if j := m.jobs[jobKey(id, scheme)]; j != nil {
		return cloneJobStatus(j.status), true
	}
	return CompileJobStatus{}, false
}
func (m *EnrichmentManager) Cancel(id, scheme string) bool {
	scheme = m.canonicalScheme(scheme)
	m.mu.Lock()
	defer m.mu.Unlock()
	if j := m.jobs[jobKey(id, scheme)]; j != nil && activeJob(j.status.State) {
		j.cancel()
		m.finishLocked(j, "cancelled")
		return true
	}
	return false
}
func (m *EnrichmentManager) finishLocked(j *enrichmentJob, state string) {
	at := time.Now().UTC()
	j.status.State = state
	j.status.FinishedAt = &at
	m.emitLocked(j)
}

// Enqueue only resolves local structure and cache metadata. It never calls a
// model. A zero JobID means no model is configured or lifecycle has stopped.
func (m *EnrichmentManager) Enqueue(id, scheme string, opts EnqueueOptions) CompileJobStatus {
	m.mu.Lock()
	ctx := m.ctx
	m.mu.Unlock()
	if ctx == nil || ctx.Err() != nil || m.compiler == nil {
		return CompileJobStatus{}
	}
	switch opts.Reason {
	case "saved", "created", "imported", "copied", "selected", "opened", "manual", "startup":
	default:
		opts.Reason = "manual"
	}
	source, err := m.compiler.source(ctx, id, scheme)
	if err != nil || source.env.Model == nil {
		return CompileJobStatus{}
	}
	m.compiler.removeLegacyPlanCache()
	units := m.compiler.enrichmentUnits(source)
	keys := make([]string, 0, len(units))
	pending := []enrichmentUnit{}
	status := CompileJobStatus{FlowID: id, Scheme: source.env.Scheme, Reason: opts.Reason, State: "queued", Total: len(units), Failed: []StepFailure{}}
	for _, u := range units {
		keys = append(keys, u.Key)
		record := m.compiler.loadEnrichment(id, u)
		if m.compiler.needsEnrichment(record, opts.Force) {
			pending = append(pending, u)
		} else {
			status.Done++
			if record != nil && record.Failed {
				status.Failed = append(status.Failed, unitFailure(u, record.Reason))
			}
		}
	}
	// Full current keys, not the shrinking pending set, identify an in-flight
	// task. Completed steps must not make a duplicate request cancel that task.
	signature := hashKey(struct {
		Raw  string
		Keys []string
	}{source.stored.FlowMD, keys})
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return CompileJobStatus{}
	}
	key := jobKey(id, source.env.Scheme)
	if prior := m.jobs[key]; prior != nil && activeJob(prior.status.State) {
		if prior.signature == signature && (!opts.Force || prior.force) {
			return cloneJobStatus(prior.status)
		}
		prior.cancel()
		m.finishLocked(prior, "cancelled")
	}
	if len(pending) == 0 {
		if prior := m.jobs[key]; prior != nil && prior.signature == signature && !opts.Force {
			return cloneJobStatus(prior.status)
		}
		status.State = "succeeded"
		if len(status.Failed) > 0 {
			status.State = "failed"
		}
	}
	status.JobID = fmt.Sprintf("compile-%d-%d", time.Now().UnixNano(), m.sequence.Add(1))
	jobCtx, cancel := context.WithTimeout(ctx, m.options.JobTimeout)
	job := &enrichmentJob{status: status, signature: signature, source: source, units: units, pending: pending, force: opts.Force, cancel: cancel}
	m.jobs[key] = job
	m.emitLocked(job)
	if len(pending) == 0 {
		m.finishLocked(job, status.State)
		cancel()
	} else {
		go m.run(jobCtx, job)
	}
	return cloneJobStatus(job.status)
}
func unitFailure(unit enrichmentUnit, reason string) StepFailure {
	title := unit.StepTitle
	if unit.Kind == "flow" {
		title = "流程输入与触发词"
	}
	return StepFailure{StepID: unit.StepID, Title: title, Reason: reason}
}
func (m *EnrichmentManager) run(ctx context.Context, j *enrichmentJob) {
	defer j.cancel()
	started := time.Now()
	m.mu.Lock()
	if !activeJob(j.status.State) {
		m.mu.Unlock()
		return
	}
	at := started.UTC()
	j.status.StartedAt = &at
	j.status.State = "running"
	m.emitLocked(j)
	logger.Info("流程编译任务开始 flow=%s scheme=%s job=%s reason=%s missing=%d/%d", j.status.FlowID, j.status.Scheme, j.status.JobID, j.status.Reason, len(j.pending), j.status.Total)
	m.mu.Unlock()
	// The flow unit finishes first. Step units only depend on deterministic
	// inputs and declared upstream outputs, so independent steps run concurrently.
	var wg sync.WaitGroup
	for _, unit := range j.pending {
		if ctx.Err() != nil {
			break
		}
		if unit.Kind == "flow" {
			m.runUnit(ctx, j, unit)
			continue
		}
		wg.Add(1)
		go func(u enrichmentUnit) { defer wg.Done(); m.runUnit(ctx, j, u) }(unit)
	}
	wg.Wait()
	keep := map[string]bool{}
	for _, unit := range j.units {
		keep[enrichmentFile(m.compiler.enrichmentDir(j.status.FlowID), unit.Kind, unit.Key)] = true
	}
	m.compiler.protectSchemeKeys(ctx, j.source.stored.Meta.ID)
	if err := m.compiler.pruneEnrichments(j.status.FlowID, keep); err != nil {
		logger.Warn("流程编译缓存清理失败 flow=%s reason=cache_io", j.status.FlowID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state := "succeeded"
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		state = "failed"
		j.status.Failed = append(j.status.Failed, StepFailure{Title: "后台编译任务", Reason: reasonTimeout})
	} else if ctx.Err() != nil {
		state = "cancelled"
	} else if len(j.status.Failed) > 0 {
		state = "failed"
	}
	if activeJob(j.status.State) {
		m.finishLocked(j, state)
	}
	logger.Info("流程编译任务结束 flow=%s job=%s state=%s done=%d failed=%d elapsed_ms=%d", j.status.FlowID, j.status.JobID, j.status.State, j.status.Done, len(j.status.Failed), time.Since(started).Milliseconds())
}
func (m *EnrichmentManager) runUnit(ctx context.Context, j *enrichmentJob, unit enrichmentUnit) {
	if ctx.Err() != nil {
		return
	}
	source := *j.source
	source.env.Model = &limitedEnrichmentModel{manager: m, model: source.env.Model, flowID: source.stored.Meta.ID, modelName: source.env.ModelName}
	record, err := m.compiler.runEnrichment(ctx, &source, unit)
	if err != nil {
		return
	} // lifecycle/job cancellation leaves this key missing for restart
	// Serialize cancellation/replacement against persistence. A superseded task
	// cannot overwrite newer forced results after it has been cancelled.
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil || !activeJob(j.status.State) {
		return
	}
	if err = m.compiler.storeEnrichment(record); err != nil {
		j.status.Failed = append(j.status.Failed, unitFailure(unit, "cache_io"))
	} else if record.Failed {
		j.status.Failed = append(j.status.Failed, unitFailure(unit, record.Reason))
	}
	j.status.Done++
	sort.SliceStable(j.status.Failed, func(a, b int) bool { return j.status.Failed[a].StepID < j.status.Failed[b].StepID })
	m.emitLocked(j)
}

type limitedEnrichmentModel struct {
	manager   *EnrichmentManager
	model     JSONModel
	flowID    string
	modelName string
	attempt   int
}

func (m *limitedEnrichmentModel) Complete(ctx context.Context, request ModelRequest) (string, error) {
	select {
	case m.manager.slots <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-m.manager.slots }()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	callCtx, cancel := context.WithTimeout(ctx, m.manager.options.CallTimeout)
	defer cancel()
	m.attempt++
	started := time.Now()
	raw, err := m.model.Complete(callCtx, request)
	if err == nil && callCtx.Err() != nil {
		err = callCtx.Err()
	}
	logger.Info("流程编译调用 flow=%s step=%s model=%s attempt=%d elapsed_ms=%d ok=%t", m.flowID, request.Step, m.modelName, m.attempt, time.Since(started).Milliseconds(), err == nil)
	if err != nil {
		reason := reasonProviderError
		if errors.Is(err, context.DeadlineExceeded) {
			reason = reasonTimeout
		}
		if errors.Is(err, context.Canceled) {
			reason = "cancelled"
		}
		logger.Warn("流程编译调用失败 flow=%s step=%s reason=%s elapsed_ms=%d", m.flowID, request.Step, reason, time.Since(started).Milliseconds())
	}
	return raw, err
}

func (c *Compiler) protectSchemeKeys(ctx context.Context, id string) {
	if c.options.ListSchemes == nil {
		return
	}
	schemes, err := c.options.ListSchemes(ctx)
	if err != nil {
		return
	}
	for _, scheme := range schemes {
		if source, err := c.source(ctx, id, scheme); err == nil && source.env.Model != nil {
			c.enrichmentUnits(source)
		}
	}
}
func (c *Compiler) withJobStatus(plan *FlowPlan) *FlowPlan {
	c.jobsMu.Lock()
	manager := c.jobs
	c.jobsMu.Unlock()
	if manager == nil || plan == nil || plan.PlanStatus == PlanStatusBasic {
		return plan
	}
	manager.mu.Lock()
	current := manager.jobs[jobKey(plan.FlowID, plan.Scheme)]
	var job CompileJobStatus
	matching := current != nil && current.source.key == plan.CompileKey && current.source.env.ModelName == plan.Compiler.Model
	if matching {
		job = cloneJobStatus(current.status)
	}
	manager.mu.Unlock()
	if matching {
		plan.Enrichment.JobID = job.JobID
		plan.Enrichment.JobState = job.State
		seen := map[string]bool{}
		for _, failure := range plan.Enrichment.Failed {
			seen[failure.StepID+"\x00"+failure.Reason] = true
		}
		for _, failure := range job.Failed {
			key := failure.StepID + "\x00" + failure.Reason
			if !seen[key] {
				plan.Enrichment.Failed = append(plan.Enrichment.Failed, failure)
				plan.Diagnostics = append(plan.Diagnostics, llmFailure(failure.StepID, failure.Reason))
				seen[key] = true
			}
		}
		if activeJob(job.State) {
			plan.PlanStatus = PlanStatusEnriching
			plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "info", Code: "FLOW_PLAN_ENRICHING", Message: "执行计划正在后台完善，可直接按当前计划运行。"})
		} else if len(job.Failed) > 0 {
			plan.PlanStatus = PlanStatusPartial
		}
	}
	return plan
}
