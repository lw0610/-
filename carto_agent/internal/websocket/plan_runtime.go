package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"crypto/sha256"
	"encoding/hex"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/deepagent"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/llm"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/planexec"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/runtimecontrol"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/schema"
)

type PlanRuntimeOptions struct {
	Guides          *flow.GuideStore
	Resources       *resource.Library
	LoadEnvironment func(context.Context, string) (flow.Environment, error)
}
type cartographicRuntime struct {
	options PlanRuntimeOptions
	mu      sync.Mutex
	pending map[string]*preparedCartographicPlan
}
type preparedCartographicPlan struct {
	Snapshot   *plan.Snapshot
	Executable *flow.FlowPlan
	Inputs     map[string]any
	Selections map[string]json.RawMessage
	Guide      *flow.GuideDoc
}
type sessionPlanService struct {
	server *DeepServer
	run    flowSessionContext
}

func (s *DeepServer) SetPlanRuntime(options PlanRuntimeOptions) error {
	if options.Guides == nil || options.LoadEnvironment == nil {
		return fmt.Errorf("指南与能力目录未配置")
	}
	if s.flowLifecycle == nil {
		s.flowLifecycle = context.Background()
	}
	s.cartoRuntime = &cartographicRuntime{options: options, pending: map[string]*preparedCartographicPlan{}}
	registry := flowrun.NewExecutorRegistry()
	if err := registry.Register(flowrun.NewRouteKnowledgeExecutor()); err != nil {
		return err
	}
	binding := flowRuntimeBinding{server: s}
	controller, err := flowrun.NewController(flowrun.Options{Lifecycle: s.flowLifecycle, Runner: binding, Tools: binding, Model: binding, Events: binding, Store: flowrun.FileRunStore{}, Executors: registry, DataGet: s.getPlanData, FinalChecks: s.checkCartographicResult, ValidatePlanResume: s.validateCartographicResume})
	if err != nil {
		return err
	}
	s.flowMu.Lock()
	s.flowController = controller
	if s.flowContexts == nil {
		s.flowContexts = map[string]flowSessionContext{}
	}
	s.flowMu.Unlock()
	return s.markStoredFlowsInterrupted(context.Background())
}
func (s *DeepServer) withPlanService(ctx context.Context, sid string) context.Context {
	if run, ok := s.flowContext(sid); ok && s.cartoRuntime != nil {
		return runtimecontrol.WithPlanService(ctx, sessionPlanService{server: s, run: run})
	}
	return ctx
}
func (s *DeepServer) planContext(sid, project string) (flowSessionContext, error) {
	if _, err := config.ValidateSessionID(sid); err != nil {
		return flowSessionContext{}, err
	}
	if run, ok := s.flowContext(sid); ok {
		state := s.sessionRuntimeProjectionSnapshot(sid, nil).RuntimeState
		if state.ExecutionMode != "" {
			run.Mode = executionMode(state.ExecutionMode)
		}
		return run, nil
	}
	state := s.sessionRuntimeProjectionSnapshot(sid, nil).RuntimeState
	if project == "" {
		project = state.BoundProjectPath
	}
	sess := s.sessionManager.GetOrCreate(sid)
	s.hydrateSessionMessages(sess, sid, project)
	mode := executionModeBuild
	if state.ExecutionMode == string(executionModePlan) {
		mode = executionModePlan
	}
	run := flowSessionContext{Session: sess, SessionID: sid, ProjectPath: project, InstanceID: state.BoundInstanceID, Scheme: s.GetRuntimeActiveConfigName(), Mode: mode}
	s.setFlowContext(run)
	return run, nil
}
func planStore(run flowSessionContext) (*plan.Store, error) {
	dir, err := config.ResolveSessionDir(run.ProjectPath, run.SessionID)
	if err != nil {
		return nil, err
	}
	return plan.NewStore(dir)
}
func (service sessionPlanService) ProposePlan(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	p, err := plan.Decode(raw)
	if err != nil {
		return nil, err
	}
	if p.ID == "" {
		p.ID, err = plan.NewID()
		if err != nil {
			return nil, err
		}
	}
	if p.Version == 0 {
		p.Version = 1
	}
	if p.Version != 1 {
		return nil, fmt.Errorf("PLAN_VERSION_CONFLICT: 新计划版本必须为1")
	}
	prepared, err := service.server.prepareCartographicPlan(ctx, service.run, p, nil)
	if err != nil {
		return nil, err
	}
	store, err := planStore(service.run)
	if err != nil {
		return nil, err
	}
	prepared.Snapshot.OriginalSource = service.run.OriginalSource
	if err = store.Save(prepared.Snapshot, 0); err != nil {
		return nil, err
	}
	service.server.publishPlan(prepared.Snapshot)
	if prepared.Snapshot.Status == "ready" {
		service.server.queueCartographicPlan(service.run.SessionID, prepared)
	}
	return map[string]any{"accepted": true, "plan_id": p.ID, "version": p.Version, "status": prepared.Snapshot.Status, "needs_confirmation": prepared.Snapshot.NeedsConfirmation, "diagnostics": prepared.Snapshot.Diagnostics}, nil
}
func (service sessionPlanService) RevisePlan(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	p, err := plan.Decode(raw)
	if err != nil {
		return nil, err
	}
	store, err := planStore(service.run)
	if err != nil {
		return nil, err
	}
	prior, err := store.Load(p.ID)
	if err != nil {
		return nil, err
	}
	if prior.Plan.Version+1 != p.Version {
		return nil, plan.ErrVersionConflict
	}
	prepared, err := service.server.prepareCartographicPlan(ctx, service.run, p, prior)
	if err != nil {
		return nil, err
	}
	_, controller := service.server.flowDependencies()
	if prior.RunID != "" {
		run, err := service.server.planRunSnapshot(service.run, prior.RunID)
		if err != nil {
			return nil, err
		}
		if err = flowrun.CheckRevision(run, p, prepared.Executable); err != nil {
			if run.Revisions >= plan.MaxRevisions && controller != nil {
				_, _ = controller.Cancel(ctx, service.run.SessionID)
			}
			return nil, err
		}
		prepared.Snapshot.RunID = prior.RunID
	}
	if err = store.Save(prepared.Snapshot, prior.Plan.Version); err != nil {
		return nil, err
	}
	service.server.publishPlan(prepared.Snapshot)
	if prepared.Snapshot.Status == "ready" {
		service.server.queueCartographicPlan(service.run.SessionID, prepared)
	}
	return map[string]any{"accepted": true, "plan_id": p.ID, "version": p.Version, "status": prepared.Snapshot.Status, "diagnostics": prepared.Snapshot.Diagnostics}, nil
}
func (s *DeepServer) prepareCartographicPlan(ctx context.Context, run flowSessionContext, p *plan.Plan, prior *plan.Snapshot) (*preparedCartographicPlan, error) {
	ctx = withToolSessionContext(ctx, run.SessionID, run.ProjectPath, run.InstanceID)
	if s.cartoRuntime == nil {
		return nil, fmt.Errorf("计划服务未就绪")
	}
	env, err := s.cartoRuntime.options.LoadEnvironment(ctx, run.Scheme)
	if err != nil {
		return nil, err
	}
	// Persist canonical references, while keeping display names independent of
	// output keys. Validate below reports every unresolved/ambiguous reference.
	_ = plan.NormalizeReferences(p)
	var guide *flow.ResolvedGuide
	if p.Guide != nil {
		guide, err = s.cartoRuntime.options.Guides.Resolve(ctx, p.Guide.ID, s.cartoRuntime.options.Resources)
		if err != nil {
			return nil, err
		}
		if !guide.Package.Enabled {
			return nil, fmt.Errorf("指南已停用")
		}
		for _, diagnostic := range guide.Doc.Diagnostics {
			if diagnostic.Level == "error" {
				return nil, fmt.Errorf("GUIDE_INVALID: %s", diagnostic.Message)
			}
		}
		if p.Guide.Version != "" && p.Guide.Version != guide.Doc.Meta.Version {
			return nil, fmt.Errorf("指南已更新，请重新读取指南并生成计划")
		}
		p.Guide.Version = guide.Doc.Meta.Version
		digest := sha256.Sum256([]byte(guide.Package.GuideMD + "\n" + guide.LockDigest))
		p.Guide.Fingerprint = "sha256:" + hex.EncodeToString(digest[:])
	}
	root, err := config.ResolveProjectRoot()
	if err != nil {
		return nil, err
	}
	bindings := map[string]any{}
	resolvedSessionData := map[string]plan.Data{}
	capabilities := map[string]bool{"route_knowledge": true}
	for _, pkg := range env.Packages {
		if flow.FlowPackageGrantable(pkg.ID, env.Packages) {
			capabilities[pkg.ID] = true
		}
	}
	// Normalize supported display-name aliases once. Execution, validation and
	// repair diagnostics all use the same canonical package identifiers.
	for i := range p.Steps {
		for j, capability := range p.Steps[i].Capabilities {
			if capability == "路线知识抽取" {
				p.Steps[i].Capabilities[j] = "route_knowledge"
			} else if ids, err := flow.ResolvePlanCapabilities([]string{capability}, env); err == nil && len(ids) == 1 {
				p.Steps[i].Capabilities[j] = ids[0]
			}
		}
	}
	selections := map[string]json.RawMessage{}
	validatedSelections := map[string]json.RawMessage{}
	findResults := map[string]any{}
	network := false
	localConfirmation := []string{}
	var dataService *dataservice.Service
	validation := plan.Environment{ProjectRoot: root, Capabilities: capabilities}
	validation.StyleExists = func(name string) bool {
		if s.cartoRuntime.options.Resources == nil {
			return false
		}
		r := s.cartoRuntime.options.Resources.Resolve(ctx, resource.Ref{Type: "style", Name: name}, resource.Scope{})
		if r == nil {
			return false
		}
		for _, d := range r.Problems {
			if d.Level == "error" {
				return false
			}
		}
		return true
	}

	validation.ValidateSource = func(id string, choice any) error {
		if dataService == nil {
			var e error
			dataService, e = s.dataService(ctx, run.SessionID)
			if e != nil {
				return e
			}
		}
		choices, e := dataservice.NormalizeChoice(choice)
		if e != nil {
			return e
		}
		selected, e := dataService.ValidateChoice(ctx, id, choices)
		if e != nil {
			return e
		}
		raw, _ := json.Marshal(selected)
		validatedSelections[planDataChoiceKey(id, choice)] = raw
		for _, r := range selected.Rows {
			network = network || r.Candidate.Network
			if r.Candidate.Kind == "user" || r.Candidate.Kind == "user_memory" || r.Candidate.Kind == "session" {
				return fmt.Errorf("候选已由用户或会话提供，请写 from=user、path=%s，不要安排获取步骤", r.Candidate.Path)
			}
			if r.Candidate.Kind == "project_layer" {
				return fmt.Errorf("候选已在工程中，请写 from=project、layer=%s，不要安排获取步骤", r.Candidate.LayerID)
			}
		}
		found, e := dataService.FindResult(ctx, id)
		if e != nil {
			return e
		}
		findResults[id] = found
		return nil
	}
	validation.ValidateData = func(d plan.Data) error {
		if d.FindID != "" {
			if dataService == nil {
				var e error
				dataService, e = s.dataService(ctx, run.SessionID)
				if e != nil {
					return e
				}
			}
			choices, e := dataservice.NormalizeChoice(d.Choice)
			if e != nil {
				return e
			}
			selected, e := dataService.ValidateChoice(ctx, d.FindID, choices)
			if e != nil {
				return e
			}
			matches := false
			for _, row := range selected.Rows {
				c := row.Candidate
				candidatePath := c.Path
				if c.Sublayer != "" {
					candidatePath += "|layername=" + c.Sublayer
				}
				samePath := func(a, b string) bool {
					ap, as, _ := strings.Cut(a, "|layername=")
					bp, bs, _ := strings.Cut(b, "|layername=")
					aa, _ := filepath.EvalSymlinks(ap)
					bb, _ := filepath.EvalSymlinks(bp)
					return aa != "" && aa == bb && as == bs
				}
				if (d.From == "user" && samePath(d.Path, candidatePath)) || (d.From == "project" && d.Layer == c.LayerID) {
					matches = true
					if c.RequiresConfirmation {
						localConfirmation = append(localConfirmation, d.Name+"："+c.MatchReason)
					}
				}
			}
			if !matches && d.From != "session" {
				return fmt.Errorf("数据路径或工程图层与选中的data_find候选不一致")
			}
			raw, _ := json.Marshal(selected)
			selections[d.ID] = raw
			found, e := dataService.FindResult(ctx, d.FindID)
			if e != nil {
				return e
			}
			findResults[d.FindID] = found
		}

		if d.From == "user" {
			path := d.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, path)
			}
			physical := strings.Split(path, "|layername=")[0]
			dir, e := config.ResolveSessionDir(run.ProjectPath, run.SessionID)
			if e != nil {
				return e
			}
			supplied, e := dataservice.LoadBindings(dir)
			if e != nil {
				return e
			}
			allowed := false
			folderBinding := false
			real, _ := filepath.EvalSymlinks(physical)
			for _, b := range supplied {
				bound, e := filepath.EvalSymlinks(b.Path)
				if e != nil {
					continue
				}
				rel, e := filepath.Rel(bound, real)
				if e == nil && (rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")) {
					allowed = true
					if rel != "." {
						folderBinding = true
					} else {
						folderBinding = false
						break
					}
				}
			}
			if !allowed {
				return fmt.Errorf("路径不是用户消息或附件明确提供的数据：%s", physical)
			}
			if folderBinding && d.FindID == "" {
				return fmt.Errorf("文件夹内的数据须先用data_find匹配并保留find_id与choice")
			}
			if d.Geometry != "" {
				if _, e = dataservice.ValidateLocalData(ctx, path, d.Geometry, nil); e != nil {
					return e
				}
			}
			store, e := sessionfiles.New(run.ProjectPath, run.SessionID)
			if e != nil {
				return e
			}
			if info, e := os.Stat(physical); e == nil && info.Mode().IsRegular() {
				_, e = store.RegisterProvided(physical, sessionfiles.Origin{Type: "provided", Label: d.Name})
				if e != nil {
					return e
				}
			}
			return nil
		}
		if d.From == "session" {
			store, e := sessionfiles.New(run.ProjectPath, run.SessionID)
			if e != nil {
				return e
			}
			resolved, e := resolveSessionPlanData(ctx, store, d)
			if e != nil {
				return e
			}
			resolvedSessionData[d.ID] = resolved
			bindings[d.ID] = resolved.Path
			return nil
		}
		value, e := (flowRuntimeBinding{server: s}).Invoke(ctx, "get_layers_info", map[string]any{})
		if e != nil {
			return e
		}
		layers, e := dataLayerBindings(value)
		if e != nil {
			return e
		}
		for _, l := range layers {
			if l.LayerID == d.Layer || l.Name == d.Layer {
				if d.Geometry != "" && dataservice.NormalizeGeometry(d.Geometry) != dataservice.NormalizeGeometry(l.Geometry) {
					return fmt.Errorf("工程图层几何类型不符：需要%s，实际%s", d.Geometry, l.Geometry)
				}
				return nil
			}
		}
		return fmt.Errorf("工程图层不存在：%s", d.Layer)
	}
	diagnostics := plan.Validate(p, validation)
	if flow.HasErrors(diagnostics) {
		return nil, plan.NewValidationError(diagnostics, validation)
	}
	for _, data := range p.Data {
		if data.From == "acquire" {
			selections[data.ID] = validatedSelections[planDataChoiceKey(data.FindID, data.Choice)]
		}
	}
	for i := range p.Data {
		if resolved, ok := resolvedSessionData[p.Data[i].ID]; ok {
			p.Data[i] = resolved
		}
		if p.Data[i].From == "user" && !filepath.IsAbs(p.Data[i].Path) {
			p.Data[i].Path = filepath.Join(root, p.Data[i].Path)
		}
	}
	options := planexec.BuildOptions{Environment: env, Bindings: bindings, OriginalSource: run.OriginalSource}
	if prior != nil {
		options.OriginalSource = prior.OriginalSource
	}
	if guide != nil {
		options.Guide = guide.Doc
		options.Scope = guide.Scope
	}
	executable, inputs, err := planexec.Build(p, options)
	if err != nil {
		return nil, err
	}
	configPolicy := "when_needed"
	networkPolicy := "direct"
	if s.configService != nil {
		configPolicy = s.configService.GetCartographicPlanConfig().Confirm
		networkPolicy = s.configService.GetAcquisitionConfig().NetworkPolicy
	}
	confirm := planexec.ConfirmationFor(p, configPolicy, networkPolicy, network)
	if len(localConfirmation) > 0 {
		confirm.Required = true
		confirm.Reasons = append(confirm.Reasons, localConfirmation...)
	}
	snapshot := &plan.Snapshot{Diagnostics: diagnostics, Plan: p, PlanID: p.ID, PlanVersion: p.Version, SessionID: run.SessionID, Status: "ready", NeedsConfirmation: confirm.Required, ConfirmationReasons: confirm.Reasons, ExecutionMode: string(run.Mode), FindResults: findResults, OriginalSource: options.OriginalSource, Selections: selections}
	if guide != nil {
		snapshot.GuideSnapshot, _ = json.Marshal(guide)
	}
	if confirm.Required {
		snapshot.Status = "awaiting_confirmation"
	}
	if run.Mode == executionModePlan {
		snapshot.Status = "preview"
		snapshot.NeedsConfirmation = false
	}
	if prior != nil {
		snapshot.RunID = prior.RunID
	}
	return &preparedCartographicPlan{Snapshot: snapshot, Executable: executable, Inputs: inputs, Selections: selections, Guide: options.Guide}, nil
}

// Resolve both public reference forms through the same existing session index.
// A display name is never an artifact identity, and a directory is not a table.
func resolveSessionPlanData(ctx context.Context, store *sessionfiles.Store, d plan.Data) (plan.Data, error) {
	var item sessionfiles.Item
	var err error
	switch {
	case d.FileID != "":
		item, err = store.Get(d.FileID)
	case d.Path != "":
		if !filepath.IsAbs(d.Path) {
			return d, fmt.Errorf("from=session 的 path 必须使用工具返回的精确绝对路径，或提供 file_id；name 仅作显示")
		}
		item, err = store.GetPath(d.Path)
	default:
		return d, fmt.Errorf("from=session 缺少产物引用：请提供 file_id 或工具返回的精确绝对 path；name/layer_name 仅作显示，已完成产物不应改为 from=step 重复生产")
	}
	if err != nil {
		return d, fmt.Errorf("会话产物引用无法解析：%w；请使用本会话工具返回的精确 path 或文件索引中的 file_id", err)
	}
	if dataservice.RequiresRegistration(item.Origin.Tool) && !dataservice.IsRegistered(store.Dir, item.Path) {
		return d, fmt.Errorf("下载文件须先通过 data_register 核验")
	}
	if item.Kind == "folder" {
		if d.Kind == "file" || d.Geometry != "" {
			return d, fmt.Errorf("%s 是数据集目录：应使用 kind=dataset 并省略 geometry；若需单张表，请引用目录内已登记文件的精确 path 或 file_id", item.Path)
		}
		d.Kind = "dataset"
	} else {
		if d.Kind == "dataset" {
			return d, fmt.Errorf("%s 是单文件：应使用 kind=file；kind=dataset 用于整体交付的数据集目录", item.Path)
		}
		d.Kind = "file"
		if d.Geometry != "" {
			if _, err = dataservice.ValidateLocalData(ctx, item.Path, d.Geometry, nil); err != nil {
				return d, err
			}
		}
	}
	d.FileID, d.Path = item.ID, item.Path
	return d, nil
}

func (s *DeepServer) publishPlan(snapshot *plan.Snapshot) {
	raw, _ := json.Marshal(snapshot)
	data := map[string]any{}
	_ = json.Unmarshal(raw, &data)
	delete(data, "original_source")
	delete(data, "guide_snapshot")
	delete(data, "data_selections")
	_ = s.sendSessionMessage(nil, Message{Type: "plan_event", SessionID: snapshot.SessionID, Data: data})
}
func (s *DeepServer) queueCartographicPlan(sid string, p *preparedCartographicPlan) {
	s.cartoRuntime.mu.Lock()
	defer s.cartoRuntime.mu.Unlock()
	s.cartoRuntime.pending[sid] = p
}
func (s *DeepServer) takeCartographicPlan(sid string) *preparedCartographicPlan {
	if s.cartoRuntime == nil {
		return nil
	}
	s.cartoRuntime.mu.Lock()
	defer s.cartoRuntime.mu.Unlock()
	p := s.cartoRuntime.pending[sid]
	delete(s.cartoRuntime.pending, sid)
	return p
}
func (s *DeepServer) hasPendingCartographicPlan(sid string) bool {
	if s.cartoRuntime == nil {
		return false
	}
	s.cartoRuntime.mu.Lock()
	defer s.cartoRuntime.mu.Unlock()
	return s.cartoRuntime.pending[sid] != nil
}
func (s *DeepServer) startPendingCartographicPlan(ctx context.Context, sid string) (*flowrun.RunState, error) {
	p := s.takeCartographicPlan(sid)
	if p == nil {
		return nil, nil
	}
	run, ok := s.flowContext(sid)
	if !ok {
		return nil, fmt.Errorf("会话上下文不存在")
	}
	return s.runCartographicPlan(ctx, run, p)
}
func (s *DeepServer) runCartographicPlan(ctx context.Context, run flowSessionContext, p *preparedCartographicPlan) (*flowrun.RunState, error) {
	ctx = withToolSessionContext(ctx, run.SessionID, run.ProjectPath, run.InstanceID)
	_, controller := s.flowDependencies()
	if controller == nil {
		return nil, fmt.Errorf("执行器未就绪")
	}
	store, err := planStore(run)
	if err != nil {
		return nil, err
	}
	dir, err := config.ResolveSessionDir(run.ProjectPath, run.SessionID)
	if err != nil {
		return nil, err
	}
	root, err := config.ResolveProjectRoot()
	if err != nil {
		return nil, err
	}
	var state *flowrun.RunState
	for {
		snapshot, err := store.UpdateStatus(p.Snapshot.Plan.ID, p.Snapshot.Plan.Version, func(v *plan.Snapshot) error {
			if v.Status == "cancelled" {
				return fmt.Errorf("计划已取消")
			}
			v.Status = "running"
			v.NeedsConfirmation = false
			v.ExecutionMode = "build"
			return nil
		})
		if err != nil {
			return nil, err
		}
		s.publishPlan(snapshot)
		run.Mode = executionModeBuild
		run.OriginalSource = p.Snapshot.OriginalSource
		s.setFlowContext(run)
		if p.Snapshot.RunID != "" {
			saved, e := s.planRunSnapshot(run, p.Snapshot.RunID)
			if e != nil {
				return nil, e
			}
			if e = controller.AttachPaused(ctx, saved, run.OriginalSource); e != nil {
				return nil, e
			}
			state, err = controller.Revise(ctx, run.SessionID, p.Snapshot.Plan, p.Executable, p.Inputs, p.Selections)
		} else {
			state, err = controller.StartFromPlan(ctx, flowrun.StartRequest{SessionID: run.SessionID, SessionDir: dir, ProjectRoot: root, ProjectPath: run.ProjectPath, FlowID: p.Executable.FlowID, Scheme: run.Scheme, Inputs: p.Inputs, OriginalSource: run.OriginalSource, Origin: "orchestrator", CartoPlan: p.Snapshot.Plan, Guide: p.Guide, Selections: p.Selections}, p.Executable)
		}
		if state != nil {
			s.publishCartographicRun(run, state)
		}
		if err != nil {
			if state == nil {
				failed, e := store.UpdateStatus(p.Snapshot.Plan.ID, p.Snapshot.Plan.Version, func(v *plan.Snapshot) error { v.Status = "failed"; return nil })
				if e == nil {
					s.publishPlan(failed)
				}
			}
			return state, err
		}
		if state == nil || state.Status != "needs_replan" {
			return state, nil
		}
		if err = s.replanCartographic(ctx, run, state); err != nil {
			return state, err
		}
		next := s.takeCartographicPlan(run.SessionID)
		if next == nil {
			return state, nil
		}
		p = next
	}
}
func (s *DeepServer) publishCartographicRun(run flowSessionContext, state *flowrun.RunState) {
	if state == nil || state.CartoPlan == nil {
		return
	}
	store, err := planStore(run)
	if err != nil {
		return
	}
	snapshot, err := store.UpdateStatus(state.CartoPlan.ID, state.CartoPlan.Version, func(v *plan.Snapshot) error {
		v.Status = state.Status
		v.RunID = state.ID
		v.Run = state
		v.NeedsConfirmation = false
		return nil
	})
	if err == nil {
		s.publishPlan(snapshot)
	}
}
func (s *DeepServer) planRunSnapshot(run flowSessionContext, id string) (*flowrun.RunState, error) {
	_, c := s.flowDependencies()
	if c != nil {
		if current, e := c.Snapshot(run.SessionID); e != nil {
			return nil, e
		} else if current != nil && current.ID == id {
			return current, nil
		}
	}
	dir, e := config.ResolveSessionDir(run.ProjectPath, run.SessionID)
	if e != nil {
		return nil, e
	}
	return (flowrun.FileRunStore{}).Load(context.Background(), dir, id)
}
func (s *DeepServer) ListPlans(ctx context.Context, sid, project string) ([]*plan.Snapshot, error) {
	dir, err := config.ResolveSessionDir(project, sid)
	if err != nil {
		return nil, err
	}
	store, err := plan.NewStore(dir)
	if err != nil {
		return nil, err
	}
	list, err := store.List()
	if err != nil {
		return nil, err
	}
	for _, p := range list {
		p.Run = nil
		if !s.planTaskActive(sid) {
			p.Replanning = false
		}
		if p.RunID != "" {
			run := flowSessionContext{SessionID: sid, ProjectPath: project}
			state, e := s.planRunSnapshot(run, p.RunID)
			if e == nil && state.CartoPlan != nil && state.CartoPlan.Version == p.PlanVersion {
				p.Run = state
				p.Status = state.Status
			}
		}
	}
	return list, nil
}
func (s *DeepServer) EditPlan(ctx context.Context, sid, project, id string, edit plan.EditRequest) (*plan.Snapshot, error) {
	if s.planTaskActive(sid) {
		return nil, fmt.Errorf("计划仍在执行或调整中，请稍后修改")
	}
	run, err := s.planContext(sid, project)
	if err != nil {
		return nil, err
	}
	store, err := planStore(run)
	if err != nil {
		return nil, err
	}
	prior, err := store.Load(id)
	if err != nil {
		return nil, err
	}
	if prior.Plan.Version != edit.Version {
		return nil, plan.ErrVersionConflict
	}
	if prior.Status == "running" || flowrun.IsTerminal(prior.Status) {
		return nil, fmt.Errorf("当前计划不能修改")
	}
	if edit.From == "user" && !filepath.IsAbs(edit.Path) {
		root, e := config.ResolveProjectRoot()
		if e != nil {
			return nil, e
		}
		edit.Path = filepath.Join(root, edit.Path)
	}
	next, err := plan.Clone(prior.Plan)
	if err != nil {
		return nil, err
	}
	found := false
	effectiveFrom := edit.From
	selectedLocal := false
	for i := range next.Data {
		d := &next.Data[i]
		if d.ID != edit.DataID {
			continue
		}
		found = true
		switch edit.From {
		case "acquire":
			if d.FindID == "" {
				return nil, fmt.Errorf("没有可选择的来源，请先查询数据")
			}
			d.From = "acquire"
			d.Choice = edit.Choice
			d.Path = ""
			d.Layer = ""
			d.FileID = ""
			d.Step = ""
			svc, e := s.dataService(ctx, sid)
			if e != nil {
				return nil, e
			}
			choice, e := dataservice.NormalizeChoice(edit.Choice)
			if e != nil {
				return nil, e
			}
			selected, e := svc.ValidateChoice(ctx, d.FindID, choice)
			if e != nil {
				return nil, e
			}
			if len(selected.Rows) == 1 {
				c := selected.Rows[0].Candidate
				if c.LayerID != "" {
					d.From = "project"
					d.Layer = c.LayerID
					effectiveFrom = "project"
				} else if c.Path != "" {
					d.From = "user"
					d.Path = c.Path
					if c.Sublayer != "" {
						d.Path += "|layername=" + c.Sublayer
					}
					effectiveFrom = "user"
					edit.Path = d.Path
					selectedLocal = true
				}
			}
		case "user", "project", "session":
			d.From = edit.From
			d.Path = edit.Path
			d.Layer = edit.Layer
			d.FileID = edit.FileID
			d.Step = ""
			d.FindID = ""
			d.Choice = nil
		default:
			return nil, fmt.Errorf("数据修改方式无效")
		}
	}
	if !found {
		return nil, fmt.Errorf("计划数据不存在")
	}
	if effectiveFrom == "user" {
		// This is an explicit human plan-card selection, not an LLM-proposed path.
		physical := strings.Split(edit.Path, "|layername=")[0]
		info, e := os.Stat(physical)
		if e != nil || (!info.Mode().IsRegular() && !info.IsDir()) {
			return nil, fmt.Errorf("用户指定的数据路径不可读取")
		}
		dir, e := config.ResolveSessionDir(run.ProjectPath, run.SessionID)
		if e != nil {
			return nil, e
		}
		supplied, e := dataservice.LoadBindings(dir)
		if e != nil {
			return nil, e
		}
		supplied = append(supplied, dataservice.Binding{Name: filepath.Base(physical), Path: physical, Explicit: true})
		if e = dataservice.SaveBindings(dir, supplied); e != nil {
			return nil, e
		}
		for i := range next.Data {
			if next.Data[i].ID == edit.DataID && !selectedLocal {
				next.Data[i].FindID = ""
				next.Data[i].Choice = nil
			}
		}
	}
	next.Version++
	// Binding data replaces its acquire operation. Keep other rows/steps intact.
	if effectiveFrom != "acquire" {
		steps := []plan.Step{}
		removed := map[string]bool{}
		for _, st := range next.Steps {
			if st.Kind == "acquire" {
				ids := []string{}
				for _, d := range st.Data {
					if d != edit.DataID {
						ids = append(ids, d)
					}
				}
				st.Data = ids
				if len(ids) == 0 {
					removed[st.ID] = true
					continue
				}
			}
			steps = append(steps, st)
		}
		for i := range steps {
			deps := []string{}
			for _, dep := range steps[i].DependsOn {
				if !removed[dep] {
					deps = append(deps, dep)
				}
			}
			steps[i].DependsOn = deps
		}
		next.Steps = steps
	}
	prepared, err := s.prepareCartographicPlan(ctx, run, next, prior)
	if err != nil {
		return nil, err
	}
	if prior.RunID != "" {
		state, e := s.planRunSnapshot(run, prior.RunID)
		if e != nil {
			return nil, e
		}
		if e = flowrun.CheckRevision(state, next, prepared.Executable); e != nil {
			return nil, e
		}
		prepared.Snapshot.Status = "awaiting_confirmation"
		prepared.Snapshot.NeedsConfirmation = true
	}
	if run.Mode == executionModeBuild {
		prepared.Snapshot.Status = "awaiting_confirmation"
		prepared.Snapshot.NeedsConfirmation = true
	}
	if err = store.Save(prepared.Snapshot, prior.Plan.Version); err != nil {
		return nil, err
	}
	s.publishPlan(prepared.Snapshot)
	return prepared.Snapshot, nil
}
func (s *DeepServer) ActOnPlan(ctx context.Context, sid, project, id string, action plan.ActionRequest) (*plan.Snapshot, error) {
	run, err := s.planContext(sid, project)
	if err != nil {
		return nil, err
	}
	store, err := planStore(run)
	if err != nil {
		return nil, err
	}
	prior, err := store.Load(id)
	if err != nil {
		return nil, err
	}
	if prior.Plan.Version != action.Version {
		return nil, plan.ErrVersionConflict
	}
	switch action.Action {
	case "cancel", "stop":
		s.takeCartographicPlan(sid)
		s.cancelSessionTask(run.Conn, sid)
		_, controller := s.flowDependencies()
		if prior.RunID != "" && controller != nil {
			_, _ = controller.Cancel(context.WithoutCancel(ctx), sid)
		}
		updated, err := store.UpdateStatus(id, action.Version, func(v *plan.Snapshot) error { v.Status = "cancelled"; return nil })
		if err == nil {
			s.publishPlan(updated)
		}
		return updated, err
	case "provide_data":
		if action.Path != "" && action.DataID != "" {
			return s.EditPlan(ctx, sid, project, id, plan.EditRequest{Version: action.Version, DataID: action.DataID, From: "user", Path: action.Path})
		}
		return prior, nil
	case "start":
		if run.Mode != executionModeBuild {
			return nil, fmt.Errorf("请切换到 Build 后执行同一计划")
		}
		if prior.Status == "running" || flowrun.IsTerminal(prior.Status) {
			return nil, fmt.Errorf("计划已启动或结束")
		}
		s.mu.Lock()
		_, working := s.sessionTaskCancels[sid]
		s.mu.Unlock()
		if working {
			return nil, fmt.Errorf("编排仍在处理中，请稍后开始")
		}
		if prior.RunID != "" {
			state, e := s.planRunSnapshot(run, prior.RunID)
			if e != nil {
				return nil, e
			}
			if state.CartoPlan != nil && state.CartoPlan.Version == prior.Plan.Version {
				return s.resumeCartographicPlan(ctx, run, prior, state, action)
			}
		}
		prepared, err := s.prepareCartographicPlan(ctx, run, mustClonePlan(prior.Plan), prior)
		if err != nil {
			return nil, err
		}
		if prepared.Snapshot.Plan.Guide != nil && prior.Plan.Guide != nil && prepared.Snapshot.Plan.Guide.Fingerprint != prior.Plan.Guide.Fingerprint {
			return nil, fmt.Errorf("指南已更新，请重新生成计划")
		}
		updated, err := store.UpdateStatus(id, action.Version, func(v *plan.Snapshot) error {
			if v.Status == "running" {
				return fmt.Errorf("计划已启动")
			}
			v.Status = "running"
			v.NeedsConfirmation = false
			return nil
		})
		if err != nil {
			return nil, err
		}
		s.publishPlan(updated)
		taskCtx, cancel := context.WithCancel(s.flowLifecycle)
		if !s.startSessionTask(taskCtx, run.Conn, sid, cancel, func() {
			if _, e := s.runCartographicPlan(taskCtx, run, prepared); e != nil {
				s.sendSessionError(run.Conn, sid, e.Error())
			}
		}) {
			return nil, fmt.Errorf("会话已删除")
		}
		return updated, nil
	case "replan", "skip":
		if s.planTaskActive(sid) {
			return nil, fmt.Errorf("计划正在自动调整，请稍后操作")
		}
		state, err := s.planRunSnapshot(run, prior.RunID)
		if err != nil {
			return nil, err
		}
		if state.Status != "needs_replan" {
			return nil, fmt.Errorf("当前计划没有待处理失败")
		}
		_, controller := s.flowDependencies()
		if controller == nil {
			return nil, fmt.Errorf("执行器未就绪")
		}
		if err = controller.AttachPaused(ctx, state, prior.OriginalSource); err != nil {
			return nil, err
		}
		run.OriginalSource = prior.OriginalSource
		s.setFlowContext(run)
		taskCtx, cancel := context.WithCancel(s.flowLifecycle)
		if !s.startSessionTask(taskCtx, run.Conn, sid, cancel, func() {
			var e error
			if action.Action == "skip" {
				_, controller := s.flowDependencies()
				var result *flowrun.RunState
				result, e = controller.SkipOptional(taskCtx, sid)
				s.publishCartographicRun(run, result)
			} else {
				e = s.replanCartographic(taskCtx, run, state)
				if e == nil && s.hasPendingCartographicPlan(sid) {
					_, e = s.startPendingCartographicPlan(taskCtx, sid)
				}
			}
			if e != nil {
				s.sendSessionError(run.Conn, sid, e.Error())
			}
		}) {
			return nil, fmt.Errorf("会话已删除")
		}
		return prior, nil
	default:
		return nil, fmt.Errorf("计划操作无效")
	}
}
func (s *DeepServer) replanCartographic(ctx context.Context, run flowSessionContext, state *flowrun.RunState) error {
	// Older persisted runs may still mark provider account failures needs_replan.
	// Preserve the real failure instead of spending another call on the same service.
	if state.Cursor >= 0 && state.Cursor < len(state.Steps) {
		attempts := state.Steps[state.Cursor].Attempts
		if len(attempts) > 0 {
			err := fmt.Errorf("%s", strings.Join(attempts[len(attempts)-1].Verification.Errors, "; "))
			if llm.IsNonRecoverableServiceError(err) {
				return err
			}
		}
	}
	if state.Revisions >= plan.MaxRevisions {
		return fmt.Errorf("已达到三次修订上限")
	}
	if store, err := planStore(run); err == nil {
		set := func(active bool) {
			snapshot, e := store.UpdateStatus(state.CartoPlan.ID, state.CartoPlan.Version, func(v *plan.Snapshot) error { v.Replanning = active; return nil })
			if e == nil {
				s.publishPlan(snapshot)
			}
		}
		set(true)
		defer set(false)
	}
	raw, _ := json.Marshal(state)
	message := schema.SystemMessage("计划执行失败，系统已自动重试一次。请依据以下真实回执和产出核验修订尚未完成部分，调用plan_revise或hitl_request请用户决定。已完成步骤与产出不得改写。最多三次修订。\n" + string(raw))
	input := append(append([]*schema.Message{}, run.Session.GetMessages()...), message)
	input, err := s.injectCartographicRules(ctx, input, run.Scheme, run.SessionID)
	if err != nil {
		return err
	}
	agent := s.getModeAgentWithWarning(run.Conn, run.SessionID, executionModeBuild)
	result := s.runAgentPhase(ctx, run.Conn, run.SessionID, run.ProjectPath, run.InstanceID, run.Session, agent, input, consumeEventsOptions{runID: "replan-" + state.ID + fmt.Sprint(state.Revisions), emitFinalResponse: true, persistAssistant: true, modePhase: modeStatePhaseBuilding, executionMode: executionModeBuild, originalSource: &run.OriginalSource})
	return result.Err
}

func (s *DeepServer) planTaskActive(sid string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, active := s.sessionTaskCancels[sid]
	return active
}
func (s *DeepServer) checkCartographicResult(ctx context.Context, run *flowrun.RunState) flowrun.Verification {
	outputs := map[string]any{}
	for _, step := range run.Steps {
		if step.Status == "completed" {
			for k, v := range step.Outputs {
				outputs[k] = v
			}
		}
	}
	result := planexec.Check(ctx, planexec.CheckInput{Plan: run.CartoPlan, Guide: run.Guide, Outputs: outputs, SourceNotes: run.SourceNotes}, flowRuntimeBinding{server: s})
	for _, manual := range result.Manual {
		run.Result.Issues = append(run.Result.Issues, "人工检查项："+manual)
	}
	return flowrun.Verification{OK: result.OK, Errors: result.Errors}
}
func (s *DeepServer) validateCartographicResume(ctx context.Context, run *flowrun.RunState) error {
	if run.CartoPlan == nil {
		return fmt.Errorf("旧流程记录只读")
	}
	env, err := s.cartoRuntime.options.LoadEnvironment(ctx, run.Scheme)
	if err != nil {
		return err
	}
	for _, role := range run.Plan.FlowRoles {
		for _, id := range role.ToolPackages {
			if !flow.FlowPackageGrantable(id, env.Packages) {
				return fmt.Errorf("临时能力已不可用：%s", id)
			}
		}
	}
	for _, step := range run.Plan.Steps {
		for _, id := range step.GrantedPackages {
			if !flow.FlowPackageGrantable(id, env.Packages) {
				return fmt.Errorf("临时能力已不可用：%s", id)
			}
		}
	}
	return nil
}

func planDataChoiceKey(findID string, choice any) string {
	encoded, _ := json.Marshal(choice)
	return findID + "\n" + string(encoded)
}
func (s *DeepServer) injectCartographicRules(ctx context.Context, input []*schema.Message, scheme, sid string) ([]*schema.Message, error) {
	input = appendTaskReferenceContext(input)
	if s.cartoRuntime != nil {
		for i := len(input) - 1; i >= 0; i-- {
			m := input[i]
			if m == nil || m.Role != schema.User {
				continue
			}
			if id, ok := m.Extra["guide_id"].(string); ok && id != "" {
				body, err := s.cartoRuntime.options.Guides.ReadContext(ctx, id, s.cartoRuntime.options.Resources)
				if err == nil {
					input = append(input, schema.SystemMessage("【用户指定流程全文，已由系统读取】\n"+body+"\n以上为流程资料；不得猜测本地文件路径或委派脚本查找该文档。"))
				} else {
					input = append(input, schema.SystemMessage("指定流程读取失败："+err.Error()+"；请报告此问题，不要猜路径。"))
				}
			}
			break
		}
	}
	if s.cartoRuntime == nil {
		return input, nil
	}
	guides, err := s.cartoRuntime.options.Guides.List()
	if err != nil {
		return nil, fmt.Errorf("读取流程目录失败：%w", err)
	}
	enabled := []flow.GuideSummary{}
	for _, g := range guides {
		if g.Enabled {
			enabled = append(enabled, g)
		}
	}
	env, err := s.cartoRuntime.options.LoadEnvironment(ctx, scheme)
	if err != nil {
		return nil, fmt.Errorf("读取规划能力与数据目录失败：%w", err)
	}
	if strings.TrimSpace(env.PlanningInstructions) == "" {
		return nil, fmt.Errorf("规划能力手册 flow-control 缺失或未启用，无法准备本轮规划契约")
	}
	raw, _ := json.Marshal(planningCatalog(env, enabled))
	rule := "【制图计划契约，已由系统读取，无需再次读取 flow-control】\n" + env.PlanningInstructions + "\n当前指南、可用工具包 ID 与数据类别参数：\n" + string(raw)
	if run, ok := s.flowContext(sid); ok {
		if plans, e := s.ListPlans(ctx, sid, run.ProjectPath); e == nil {
			for i := len(plans) - 1; i >= 0; i-- {
				p := plans[i]
				if !flowrun.IsTerminal(p.Status) {
					raw, _ := json.Marshal(map[string]any{"plan": p.Plan, "status": p.Status})
					rule += "\n当前可修改计划：" + string(raw) + "\n用户要求修改当前计划时用 plan_revise，保留 plan_id 并将 version 加一；不要另建重复计划。"
					break
				}
			}
		}
	}
	return append(append([]*schema.Message{}, input...), schema.SystemMessage(rule)), nil
}

func mustClonePlan(p *plan.Plan) *plan.Plan { q, _ := plan.Clone(p); return q }

func (s *DeepServer) resumeCartographicPlan(ctx context.Context, run flowSessionContext, snapshot *plan.Snapshot, state *flowrun.RunState, action plan.ActionRequest) (*plan.Snapshot, error) {
	if action.ResumeRunID != "" && action.ResumeRunID != state.ID {
		return nil, fmt.Errorf("恢复记录与计划不匹配")
	}
	_, controller := s.flowDependencies()
	if controller == nil {
		return nil, fmt.Errorf("执行器未就绪")
	}
	run.OriginalSource = snapshot.OriginalSource
	s.setFlowContext(run)
	request := flowrun.ResumeRequest{SessionID: run.SessionID, SessionDir: state.SessionDir, FlowRunID: state.ID, OriginalSource: snapshot.OriginalSource, ResourceChoice: action.ResourceChoice, ResourceFingerprint: action.ResourceFingerprint}
	checked, err := controller.CheckResume(ctx, request)
	if err != nil {
		s.publishCartographicRun(run, checked)
		return nil, err
	}
	if checked.Status == "waiting_user" {
		pending, e := s.restoreFlowPending(ctx, run.SessionID, run.ProjectPath, state.ID, request)
		if e != nil {
			return nil, e
		}
		s.sendSessionMessage(run.Conn, Message{Type: MsgTypeInterruptRequest, SessionID: run.SessionID, Content: "请继续回答本次计划的问题", Data: pendingInterruptPayload(pending)})
		return snapshot, nil
	}
	if checked.Status == "needs_replan" {
		return nil, fmt.Errorf("请使用重新规划、提供数据、跳过或停止处理失败")
	}
	taskCtx, cancel := context.WithCancel(s.flowLifecycle)
	if !s.startSessionTask(taskCtx, run.Conn, run.SessionID, cancel, func() {
		result, e := controller.Resume(withToolSessionContext(taskCtx, run.SessionID, run.ProjectPath, run.InstanceID), request)
		s.publishCartographicRun(run, result)
		if e != nil {
			s.sendSessionError(run.Conn, run.SessionID, e.Error())
		}
	}) {
		return nil, fmt.Errorf("会话已删除")
	}
	snapshot.Status = "running"
	return snapshot, nil
}

func (s *DeepServer) runtimeGuideBackend() einoskill.Backend {
	if s.cartoRuntime == nil {
		return nil
	}
	return &deepagent.GuideBackend{Store: s.cartoRuntime.options.Guides, Resources: s.cartoRuntime.options.Resources}
}
func (s *DeepServer) runtimeAcquisitionConfig() config.AcquisitionConfig {
	s.mu.Lock()
	service := s.configService
	s.mu.Unlock()
	if service == nil {
		return (config.AcquisitionConfig{}).WithDefaults()
	}
	return service.GetAcquisitionConfig()
}

func (s *DeepServer) hasPlanAwaitingRevision(sid string) bool {
	_, controller := s.flowDependencies()
	if controller == nil {
		return false
	}
	state, err := controller.Snapshot(sid)
	return err == nil && state != nil && state.CartoPlan != nil && state.Status == "needs_replan"
}
