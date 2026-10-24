package flow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/autocarto/carto_agent/internal/resource"
)

// Environment 由生产装配层从当前方案和正式工具目录创建，不含任何凭据。
type Environment struct {
	Scheme          string
	SchemeRevision  string
	CatalogRevision string
	Roles           []RoleSpec
	Packages        []PackageSpec
	Capabilities    []CapabilitySpec
	EnabledFlows    *[]string // nil 表示全部启用；空列表表示全部禁用。
	Model           JSONModel
	ModelName       string
}

type CompilerOptions struct {
	Store           *Store
	Resources       *resource.Library
	LoadEnvironment func(context.Context, string) (Environment, error)
	ListSchemes     func(context.Context) ([]string, error)
	CacheDir        string
	Version         string
	// RetryAfter 是非强制编译对失败增强的重试间隔；0 表示 DefaultEnrichmentRetryAfter。
	RetryAfter time.Duration
}

type Compiler struct {
	options    CompilerOptions
	linkMu     sync.Mutex
	links      map[string]linkedMemory
	enrichMu   sync.Mutex
	enrichMemo map[string]enrichmentMemo
	activeKeys map[string]map[string][]string
	legacyOnce sync.Once
	jobsMu     sync.Mutex
	jobs       *EnrichmentManager
}

func NewCompiler(options CompilerOptions) *Compiler {
	if options.Version == "" {
		options.Version = CompilerVersion
	}
	return &Compiler{options: options, links: map[string]linkedMemory{}, enrichMemo: map[string]enrichmentMemo{}, activeKeys: map[string]map[string][]string{}}
}

var ErrFlowDisabled = errors.New("当前方案未启用该流程")
var ErrPlanChanged = errors.New("生成执行计划期间流程、资源或方案发生变化，请重新打开流程后重试")
var errNotConfigured = errors.New("流程编译服务未配置")

type compilationSource struct {
	version   string
	lock      *resource.Lock
	stored    *StoredFlow
	doc       *FlowDoc
	env       Environment
	resources []*resource.ResolvedResource
	key       string
}

func (c *Compiler) source(ctx context.Context, id, scheme string) (*compilationSource, error) {
	if c == nil || c.options.Store == nil || c.options.LoadEnvironment == nil {
		return nil, errNotConfigured
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	env, err := c.options.LoadEnvironment(ctx, scheme)
	if err != nil {
		return nil, err
	}
	return c.sourceWithEnvironment(ctx, id, env)
}

func (c *Compiler) sourceWithEnvironment(ctx context.Context, id string, env Environment) (*compilationSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stored, doc, lock, resources, err := c.options.Store.ResolveResources(ctx, id, c.options.Resources)
	if err != nil {
		return nil, err
	}
	return c.newSource(ctx, env, stored, doc, lock, resources)
}

func (c *Compiler) newSource(ctx context.Context, env Environment, stored *StoredFlow, doc *FlowDoc, lock *resource.Lock, resources []*resource.ResolvedResource) (*compilationSource, error) {
	if strings.TrimSpace(env.Scheme) == "" {
		return nil, errors.New("方案名称不能为空")
	}
	source := &compilationSource{version: c.options.Version, stored: stored, doc: doc, env: env, lock: lock, resources: resources}
	var err error
	source.key, err = c.fingerprint(ctx, source)
	return source, err
}

// Compile 补做缺失的模型增强后返回执行计划，允许含错误（供预览）；force 时全部重做。
// 执行入口必须使用 EnsureRunnable。
func (c *Compiler) Compile(ctx context.Context, id, scheme string, force bool) (*FlowPlan, error) {
	source, err := c.source(ctx, id, scheme)
	if err != nil {
		return nil, err
	}
	plan, err := c.enrich(ctx, source, force)
	if err != nil {
		return nil, err
	}
	return c.linkSource(ctx, source, plan)
}

// enrich 逐项补做当前内容键缺失、或失败已超过重试间隔的增强。每项结果立即落盘，
// 中途取消时已完成的项保留；没有编译模型时直接得到基础计划。
func (c *Compiler) enrich(ctx context.Context, source *compilationSource, force bool) (*FlowPlan, error) {
	c.removeLegacyPlanCache()
	if source.env.Model != nil {
		id := source.stored.Meta.ID
		unlock := c.lockFlow(id)
		defer unlock()
		keep, written := map[string]bool{}, 0
		for _, unit := range c.enrichmentUnits(source) {
			keep[enrichmentFile(c.enrichmentDir(id), unit.Kind, unit.Key)] = true
			if !c.needsEnrichment(c.loadEnrichment(id, unit), force) {
				continue
			}
			record, err := c.runEnrichment(ctx, source, unit)
			if err != nil {
				return nil, err
			}
			if err = c.storeEnrichment(record); err != nil {
				return nil, err
			}
			written++
		}
		// 只有写入了新记录才可能超出上限。
		if written > 0 {
			if err := c.pruneEnrichments(id, keep); err != nil {
				return nil, err
			}
		}
	}
	return c.buildPlan(ctx, source, enrichCached)
}

// EnsureRunnable checks structure identity and resources before execution.
func (c *Compiler) EnsureRunnable(ctx context.Context, id, scheme string) (*FlowPlan, error) {
	source, err := c.source(ctx, id, scheme)
	if err != nil {
		return nil, err
	}
	if !source.env.FlowEnabled(id) {
		return nil, ErrFlowDisabled
	}
	plan, err := c.buildPlan(ctx, source, enrichSaved)
	if err != nil {
		return nil, err
	}
	plan, err = c.linkSource(ctx, source, plan)
	if err != nil {
		return nil, err
	}
	// 再次确认本地解析期间未发生编辑，旧快照不能进入执行。
	current, err := c.source(ctx, id, source.env.Scheme)
	if err != nil {
		return nil, err
	}
	currentFingerprint, err := c.linkFingerprint(ctx, current)
	if err != nil {
		return nil, err
	}
	if current.key != source.key || currentFingerprint != plan.LinkFingerprint {
		return plan, ErrPlanChanged
	}
	if plan.HasErrors() {
		return plan, &ValidationError{Plan: plan, Diagnostics: plan.Diagnostics}
	}
	return plan, nil
}

// EnsureCompiled is retained for one release for existing integrations.
func (c *Compiler) EnsureCompiled(ctx context.Context, id, scheme string) (*FlowPlan, error) {
	return c.EnsureRunnable(ctx, id, scheme)
}

// AvailablePlans 提供主编排角色可用的已校验计划，坏流程不阻断其他流程。
// 返回完整只读计划；提示词装配层只摘取编号、名称、适用场景和输入摘要。
func (c *Compiler) AvailablePlans(ctx context.Context, scheme string) ([]*FlowPlan, error) {
	if c == nil || c.options.Store == nil || c.options.LoadEnvironment == nil {
		return nil, errNotConfigured
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	env, err := c.options.LoadEnvironment(ctx, scheme)
	if err != nil {
		return nil, err
	}
	ids, err := c.options.Store.compilableIDs()
	if err != nil {
		return nil, err
	}
	plans := []*FlowPlan{}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !env.FlowEnabled(id) {
			continue
		}
		source, err := c.sourceWithEnvironment(ctx, id, env)
		var plan *FlowPlan
		if err == nil {
			plan, err = c.buildPlan(ctx, source, enrichSaved)
		}
		if err == nil {
			plan, err = c.linkSource(ctx, source, plan)
		}
		if err == nil && plan.HasErrors() {
			err = &ValidationError{Plan: plan, Diagnostics: plan.Diagnostics}
		}
		if err == nil {
			plans = append(plans, plan)
			continue
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var invalid *ValidationError
		if errors.As(err, &invalid) || errors.Is(err, ErrFlowDisabled) || errors.Is(err, ErrNotFound) {
			continue
		}
	}
	return plans, nil
}

// 只枚举编号，单包内容读取失败交由 AvailablePlans 跳过；根目录读取失败仍返回。
func (s *Store) compilableIDs() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	ids := []string{}
	for _, root := range []string{s.BuiltinDir, s.UserDir} {
		if root == "" {
			continue
		}
		entries, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() && ValidateID(entry.Name()) == nil && !seen[entry.Name()] {
				seen[entry.Name()] = true
				ids = append(ids, entry.Name())
			}
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func (e Environment) FlowEnabled(id string) bool {
	if e.EnabledFlows == nil {
		return true
	}
	for _, enabled := range *e.EnabledFlows {
		if enabled == id {
			return true
		}
	}
	return false
}

// buildPlan 生成执行计划。enrichNone 只用确定性规则；enrichCached 再合并当前内容键已缓存的增强结果。
// 两种模式都不调用模型、不写文件；没有模型结果的项保持基础计划的值。
func (c *Compiler) buildPlan(ctx context.Context, source *compilationSource, mode enrichMode) (*FlowPlan, error) {
	doc, env := source.doc, source.env
	plan := &FlowPlan{PlanStatus: PlanStatusBasic, Enrichment: EnrichmentStatus{Flow: StepEnrichmentNone, Failed: []StepFailure{}}, APIVersion: PlanAPIVersion, FlowID: source.stored.Meta.ID, FlowDir: source.stored.Dir, FlowVersion: doc.Meta.Version, Name: doc.Meta.Name, Description: doc.Meta.Description, CompileKey: source.key, CompileDiagnostics: []Diagnostic{}, LinkDiagnostics: []Diagnostic{}, Scheme: env.Scheme, SchemeRevision: env.SchemeRevision, CatalogRevision: env.CatalogRevision, CompiledAt: source.stored.UpdatedAt.UTC(), Compiler: CompilerInfo{Version: c.options.Version, Model: env.ModelName}, Triggers: []string{}, Applicability: doc.Applicability, Inputs: []InputPlan{}, InputConstraints: []InputConstraint{}, Resources: []ResourcePlan{}, Rules: append([]string{}, doc.Rules...), Notes: []string{}, Deliverables: doc.Deliverables, Steps: []StepPlan{}, Diagnostics: []Diagnostic{}, Roles: append([]RoleSpec{}, env.Roles...)}
	for _, section := range doc.Notes {
		plan.Notes = append(plan.Notes, strings.TrimSpace(section.Title+"\n"+section.Content))
	}
	for _, item := range source.resources {
		plan.Resources = append(plan.Resources, resourceDeclaration(item))
	}
	// 引用和条件在辅助推断后统一检查，避免保留已被推断产出解决的旧诊断。
	for _, d := range doc.Diagnostics {
		switch d.Code {
		case "FLOW_REF_UNRESOLVED", "FLOW_REF_FORWARD", "FLOW_OUTPUT_DUPLICATE", "FLOW_CONDITION_UNPARSED":
			continue
		}
		plan.Diagnostics = append(plan.Diagnostics, d)
	}
	plan.Inputs = baseInputs(doc.Inputs)
	implicit := []string{}
	for _, input := range doc.Inputs {
		if _, isImplicit := deterministicInputType(input); isImplicit {
			implicit = append(implicit, input.Label)
		}
	}
	if len(implicit) > 0 {
		plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "info", Code: "FLOW_INPUT_TYPE_IMPLICIT", Message: "以下输入没有写明类型，暂按文本处理：" + strings.Join(implicit, "、") + "。需要文件、文件夹或图层时，请在括号里写明，例如（可选，文件）。"})
	}
	var units []enrichmentUnit
	if env.Model != nil {
		if mode != enrichSaved {
			plan.Enrichment.Flow = StepEnrichmentPending
		}
		if mode == enrichCached || mode == enrichSaved {
			units = c.enrichmentUnits(source)
		}
	}
	// cached 读取一项的记录，并把时间与失败情况计入计划。
	cached := func(unit enrichmentUnit) *enrichmentRecord {
		record := c.loadEnrichment(plan.FlowID, unit)
		if record == nil || (mode == enrichSaved && record.Failed) {
			return nil
		}
		at := record.CreatedAt.UTC()
		if plan.Enrichment.UpdatedAt == nil || at.After(*plan.Enrichment.UpdatedAt) {
			plan.Enrichment.UpdatedAt = &at
		}
		if at.After(plan.CompiledAt) {
			plan.CompiledAt = at
		}
		if record.Failed {
			title := unit.StepTitle
			if unit.Kind == "flow" {
				title = "流程输入与触发词"
			}
			plan.Enrichment.Failed = append(plan.Enrichment.Failed, StepFailure{StepID: unit.StepID, Title: title, Reason: record.Reason})
			plan.Diagnostics = append(plan.Diagnostics, llmFailure(unit.StepID, record.Reason))
		}
		return record
	}
	if len(units) > 0 {
		if record := cached(units[0]); record != nil && record.Failed {
			plan.Enrichment.Flow = StepEnrichmentFailed
		} else if record != nil {
			var result inferredFlow
			if json.Unmarshal(record.Result, &result) == nil {
				applyFlowEnrichment(doc, plan, result)
				plan.Enrichment.Flow = StepEnrichmentReady
			}
		}
	}
	plan.InputConstraints = declaredConstraints(doc.Inputs, plan.Inputs, plan.InputConstraints)
	for index, step := range doc.Steps {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		compiled := StepPlan{ID: step.ID, Index: step.Index, Title: step.Title, RoleLabel: step.Role, Kind: "auto", Instructions: step.Instructions, Completion: step.Completion, ExpectedOutputs: append([]OutputDeclaration{}, step.ExpectedOutputs...), OnFailure: step.OnFailure, Refs: PlanRefs{Resources: []string{}, Inputs: []string{}, Outputs: []OutputRef{}}, SuggestedTools: []string{}, MissingCapabilities: []string{}}
		if step.Condition != nil {
			compiled.Condition = &ConditionDoc{Text: step.Condition.Text, Rule: cloneRule(step.Condition.Rule)}
		}
		role, ds := bindRole(step, env.Roles)
		plan.Diagnostics = append(plan.Diagnostics, ds...)
		if step.Role == "用户" || step.Role == "人工" {
			compiled.Kind = "confirm"
		} else if role != nil {
			compiled.Role, compiled.RoleLabel, compiled.Executor = role.ID, role.DisplayName, role.Executor
			compiled.Kind = "agent"
			if role.Kind == "executor" {
				compiled.Kind = "executor"
			}
		}
		compiled.Enrichment = StepEnrichmentNone
		if env.Model != nil && mode != enrichSaved {
			compiled.Enrichment = StepEnrichmentPending
		}
		if len(units) > 0 {
			if record := cached(units[index+1]); record != nil && record.Failed {
				compiled.Enrichment = StepEnrichmentFailed
			} else if record != nil {
				var result inferredStep
				if json.Unmarshal(record.Result, &result) == nil {
					applyStepEnrichment(plan, step, &compiled, result)
					compiled.Enrichment = StepEnrichmentReady
				}
			}
		}
		plan.Steps = append(plan.Steps, compiled)
	}
	plan.Enrichment.Total = len(plan.Steps)
	for _, step := range plan.Steps {
		if step.Enrichment == StepEnrichmentReady {
			plan.Enrichment.Done++
		}
	}
	switch {
	case mode == enrichSaved:
		plan.PlanStatus = PlanStatusBasic
		if plan.Enrichment.Flow == StepEnrichmentReady && plan.Enrichment.Done == plan.Enrichment.Total {
			plan.PlanStatus = PlanStatusReady
		}
	case env.Model == nil:
		plan.PlanStatus = PlanStatusBasic
		plan.Diagnostics = append(plan.Diagnostics, Diagnostic{Level: "info", Code: "FLOW_PLAN_BASIC", Message: "没有可用的编译模型，执行计划只包含流程原文能确定的内容（不含建议能力），可以照常运行。"})
	case plan.Enrichment.Flow == StepEnrichmentReady && plan.Enrichment.Done == plan.Enrichment.Total:
		plan.PlanStatus = PlanStatusReady
	default:
		plan.PlanStatus = PlanStatusPartial
	}
	completeDiagnostics(doc, plan, env)
	plan.CompileDiagnostics = append([]Diagnostic{}, plan.Diagnostics...)
	return plan, nil
}

func cloneRule(rule *ConditionRule) *ConditionRule {
	if rule == nil {
		return nil
	}
	copy := *rule
	copy.Args = append([]ConditionRule(nil), rule.Args...)
	for index := range copy.Args {
		copy.Args[index] = *cloneRule(&rule.Args[index])
	}
	copy.Arg = cloneRule(rule.Arg)
	return &copy
}
