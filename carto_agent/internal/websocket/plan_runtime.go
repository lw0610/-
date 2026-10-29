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
	options           PlanRuntimeOptions
	mu                sync.Mutex
	pending           map[string]*preparedCartographicPlan
	stepConversations map[string]*stepConversation
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
	s.cartoRuntime = &cartographicRuntime{options: options, pending: map[string]*preparedCartographicPlan{}, stepConversations: map[string]*stepConversation{}}
	registry := flowrun.NewExecutorRegistry()
	if err := registry.Register(flowrun.NewRouteKnowledgeExecutor()); err != nil {
		return err
	}
	binding := flowRuntimeBinding{server: s}
	controller, err := flowrun.NewController(flowrun.Options{Lifecycle: s.flowLifecycle, Runner: binding, Tools: binding, Model: binding, Events: binding, Store: flowrun.FileRunStore{}, Executors: registry, DataGet: s.getPlanData, AcquisitionConfig: s.runtimeAcquisitionConfig, FinalChecks: s.checkCartographicResult, ValidatePlanResume: s.validateCartographicResume})
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
	if !plan.Identifier.MatchString(p.ID) {
		p.ID, err = plan.NewID()
		if err != nil {
			return nil, err
		}
	}
	p.Version = 1
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
	return map[string]any{"accepted": true, "plan_id": p.ID, "version": p.Version, "status": prepared.Snapshot.Status, "needs_confirmation": prepared.Snapshot.NeedsConfirmation, "diagnostics": prepared.Snapshot.Diagnostics, "precheck": prepared.Snapshot.Precheck, "notes": prepared.Snapshot.Notes}, nil
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
	if p.ID == "" {
		items, e := store.List()
		if e != nil {
			return nil, e
		}
		for i := len(items) - 1; i >= 0; i-- {
			if items[i].Plan.APIVersion == plan.APIVersion && !flowrun.IsTerminal(items[i].Status) {
				p.ID = items[i].Plan.ID
				break
			}
		}
	}
	prior, err := store.Load(p.ID)
	if err != nil {
		return nil, err
	}
	if prior.Plan.APIVersion != plan.APIVersion {
		return nil, fmt.Errorf("旧计划只读，请重新提出 v2 计划")
	}
	p.Version = prior.Plan.Version + 1
	if prior.RunID != "" {
		state, e := service.server.planRunSnapshot(service.run, prior.RunID)
		if e != nil {
			return nil, e
		}
		flowrun.PreserveCompleted(state, p)
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
	return map[string]any{"accepted": true, "plan_id": p.ID, "version": p.Version, "status": prepared.Snapshot.Status, "diagnostics": prepared.Snapshot.Diagnostics, "precheck": prepared.Snapshot.Precheck, "notes": prepared.Snapshot.Notes}, nil
}
func (s *DeepServer) prepareCartographicPlan(ctx context.Context, run flowSessionContext, p *plan.Plan, prior *plan.Snapshot) (*preparedCartographicPlan, error) {
	if p.APIVersion == plan.APIVersion {
		return s.preparePlanV2(ctx, run, p, prior)
	}
	return s.prepareLegacyPlan(ctx, run, p, prior)
}

func (s *DeepServer) prepareLegacyPlan(ctx context.Context, run flowSessionContext, p *plan.Plan, prior *plan.Snapshot) (*preparedCartographicPlan, error) {
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
	for _, role := range env.Roles {
		if role.Enabled && !role.IsMain {
			for _, id := range role.ToolPackages {
				capabilities[id] = true
			}
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
	if prior.Plan.APIVersion == plan.APIVersion {
		return s.editPlanV2(ctx, run, prior, edit)
	}
	return nil, fmt.Errorf("旧计划只读，请按新格式重新生成")
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
			current, e := controller.Snapshot(sid)
			if e != nil {
				return nil, e
			}
			if current == nil {
				saved, e := s.planRunSnapshot(run, prior.RunID)
				if e != nil {
					return nil, e
				}
				if !flowrun.IsTerminal(saved.Status) {
					if e = controller.AttachPaused(ctx, saved, prior.OriginalSource); e != nil {
						return nil, e
					}
				}
			}
			if _, e = controller.Cancel(context.WithoutCancel(ctx), sid); e != nil {
				return nil, e
			}
		}
		updated, err := store.UpdateStatus(id, action.Version, func(v *plan.Snapshot) error { v.Status = "cancelled"; return nil })
		if err == nil {
			s.publishPlan(updated)
		}
		return updated, err
	case "provide_data":
		if action.Path != "" && action.DataID != "" {
			return s.EditPlan(ctx, sid, project, id, plan.EditRequest{Version: action.Version, DataID: action.DataID, Source: "user", Ref: action.Path})
		}
		return prior, nil
	case "start":
		if prior.Plan.APIVersion != plan.APIVersion && prior.RunID == "" {
			return nil, fmt.Errorf("旧计划仅供查看，请按新格式重新生成")
		}
		if run.Mode != executionModeBuild {
			return nil, fmt.Errorf("请切换到 Build 后执行同一计划")
		}
		var savedRun *flowrun.RunState
		if prior.RunID != "" {
			savedRun, err = s.planRunSnapshot(run, prior.RunID)
			if err != nil {
				return nil, err
			}
			// On restart the controller record is marked interrupted. The
			// plan card is a projection and may still contain its last status.
			prior.Status = savedRun.Status
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
		if savedRun != nil && savedRun.CartoPlan != nil && savedRun.CartoPlan.Version == prior.Plan.Version {
			return s.resumeCartographicPlan(ctx, run, prior, savedRun, action)
		}
		prepared, err := s.prepareCartographicPlan(ctx, run, mustClonePlan(prior.Plan), prior)
		if err != nil {
			return nil, err
		}
		if prepared.Snapshot.Status == "needs_data" {
			s.publishPlan(prepared.Snapshot)
			return prepared.Snapshot, fmt.Errorf("计划仍有不可用数据，请按预检建议处理")
		}
		if prepared.Snapshot.Plan.Guide != nil && prior.Plan.Guide != nil && prior.Plan.Guide.Fingerprint != "" && prepared.Snapshot.Plan.Guide.Fingerprint != prior.Plan.Guide.Fingerprint {
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
	input, err := cartographicReplanInput(run, state)
	if err != nil {
		return err
	}
	input, err = s.injectTaskContext(ctx, input, run.Scheme, run.SessionID, executionModePlan)
	if err != nil {
		return err
	}
	// Replanning must use the planning tool set. A Build delegate would execute
	// outside the controller and leave its completed work absent from run.Steps.
	agent, fallback := s.getAgentWithFallback(executionModePlan)
	if agent == nil || fallback {
		return fmt.Errorf("计划修订智能体未就绪，已保留当前步骤和成果")
	}
	options := consumeEventsOptions{runID: "replan-" + state.ID + fmt.Sprint(state.Revisions), replanPlanID: state.CartoPlan.ID, replanPlanVersion: state.CartoPlan.Version, modePhase: modeStatePhaseBuilding, executionMode: executionModeBuild, originalSource: &run.OriginalSource}
	result := s.runAgentPhase(ctx, run.Conn, run.SessionID, run.ProjectPath, run.InstanceID, run.Session, agent, input, options)
	return s.finishCartographicReplan(run.SessionID, run.ProjectPath, options, result)
}

// Project verified state once; old assistant claims and attempts are not facts for
// the next revision. User messages retain attachment/guide metadata for injection.
func cartographicReplanInput(run flowSessionContext, state *flowrun.RunState) ([]*schema.Message, error) {
	current, err := plan.Clone(state.CartoPlan)
	if err != nil {
		return nil, err
	}
	completed := []map[string]any{}
	completedIDs := map[string]bool{}
	for _, step := range state.Steps {
		if step.Status != "completed" {
			continue
		}
		completedIDs[step.ID] = true
		definition, _ := current.StepByID(step.ID)
		outputs := []map[string]any{}
		for _, product := range definition.Produces {
			if value, ok := step.Outputs[product.ID]; ok {
				outputs = append(outputs, map[string]any{"product": product, "value": value})
			}
		}
		completed = append(completed, map[string]any{"step_id": step.ID, "outputs": outputs})
	}
	remaining := make([]plan.Step, 0, len(current.Steps))
	for _, step := range current.Steps {
		if !completedIDs[step.ID] {
			remaining = append(remaining, step)
		}
	}
	current.Steps = remaining
	var failure map[string]any
	if state.Cursor >= 0 && state.Cursor < len(state.Steps) {
		step := state.Steps[state.Cursor]
		failure = map[string]any{"step_id": step.ID, "status": step.Status, "last_failure": step.LastFailure, "user_guidance": step.UserGuidance}
		if len(step.Attempts) > 0 {
			latest := step.Attempts[len(step.Attempts)-1]
			failure["latest_receipt"], failure["verification_errors"] = latest.Receipt, latest.Verification.Errors
		}
	}
	raw, err := json.Marshal(map[string]any{
		"current_plan": current, "failed_step": failure, "completed_steps": completed,
		"inputs": state.Inputs, "acquired_inputs": state.AcquiredInputs, "input_details": state.InputDetails,
		"data_preparation": state.DataPreparation, "data_selections": state.Selections, "source_notes": state.SourceNotes,
		"revisions_used": state.Revisions, "revisions_remaining": plan.MaxRevisions - state.Revisions,
	})
	if err != nil {
		return nil, fmt.Errorf("准备计划修订上下文失败: %w", err)
	}
	input := []*schema.Message{}
	originalIncluded := false
	for _, message := range run.Session.GetMessages() {
		if message != nil && message.Role == schema.User {
			copy := *message
			copy.Extra = cloneMap(message.Extra)
			input = append(input, &copy)
			originalIncluded = originalIncluded || message.Content == run.OriginalSource
		}
	}
	if !originalIncluded && run.OriginalSource != "" {
		input = append([]*schema.Message{schema.UserMessage(run.OriginalSource)}, input...)
	}
	if len(input) > 0 && current.Guide != nil {
		last := input[len(input)-1]
		if last.Extra == nil {
			last.Extra = map[string]any{}
		}
		last.Extra["guide_id"] = current.Guide.ID
	}
	message := schema.SystemMessage("当前计划需要调整。请依据以下真实回执、取数台账和已完成产出，仅修订尚未完成部分，调用 plan_revise；确需用户决定时调用 hitl_request。已完成步骤和产出由系统保留，不必照抄。此阶段只改计划，不执行抽取、下载或制图；执行器会在修订通过后继续。已有可用数据作为输入引用，不再声明重复生产步骤。只有实际提交修订或发起用户交互才算完成本轮，文字说明 ready 不能代替这些动作。\n" + string(raw))
	return append(input, message), nil
}

func (s *DeepServer) finishCartographicReplan(sid, projectPath string, options consumeEventsOptions, result consumeEventsResult) error {
	if result.Err != nil {
		return result.Err
	}
	if s.cartoRuntime != nil {
		s.cartoRuntime.mu.Lock()
		pending := s.cartoRuntime.pending[sid]
		accepted := pending != nil && pending.Snapshot.Plan.ID == options.replanPlanID && pending.Snapshot.Plan.Version > options.replanPlanVersion
		s.cartoRuntime.mu.Unlock()
		if accepted {
			return nil
		}
	}
	if result.Interrupted {
		pending, ok := s.getPendingInterrupt(sid)
		if ok && pending.Runner != nil && pending.CheckpointID != "" && len(pending.Contexts) > 0 && pending.FlowOptions.runID == options.runID && pending.FlowOptions.replanPlanID == options.replanPlanID && pending.FlowOptions.replanPlanVersion == options.replanPlanVersion {
			return nil
		}
	}
	// Accepted revisions can wait for confirmation or missing data without being
	// queued. Their persisted plan cards are real outcomes, not planner prose.
	if store, err := planStore(flowSessionContext{SessionID: sid, ProjectPath: projectPath}); err == nil {
		if snapshot, err := store.Load(options.replanPlanID); err == nil && snapshot.Plan.Version > options.replanPlanVersion && (snapshot.Status == "needs_data" || snapshot.Status == "awaiting_confirmation" && snapshot.NeedsConfirmation) {
			s.markSessionRuntimeInterrupted(sid, "")
			s.persistManagedSessionState(sid, projectPath, options.executionMode)
			return nil
		}
	}
	err := fmt.Errorf("未提交修订，已保留成果；请重新调整计划或补充要求后继续")
	s.markSessionRuntimeFinished(sid, sessionRuntimeStatusFailed, err.Error())
	s.persistManagedSessionState(sid, projectPath, options.executionMode)
	return err
}

func (s *DeepServer) planTaskActive(sid string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, active := s.sessionTaskCancels[sid]
	return active
}
func (s *DeepServer) checkCartographicResult(ctx context.Context, run *flowrun.RunState) flowrun.Verification {
	if run.CartoPlan != nil && run.CartoPlan.APIVersion == plan.APIVersion {
		return flowrun.Verification{OK: true}
	}
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
	if run.Status == "waiting_user" && run.Cursor >= 0 && run.Cursor < len(run.Steps) {
		step := run.Steps[run.Cursor]
		if len(step.Attempts) > 0 {
			attempt := step.Attempts[len(step.Attempts)-1]
			mode := attempt.Mode
			if mode == "acquire" || mode == "prepare" {
				roleID := attempt.Role
				if roleID == "" {
					if mode == "prepare" {
						return fmt.Errorf("旧取数断点未记录实际执行角色，不能用消费步骤角色替代恢复；请修订当前计划后继续，已登记的数据仍可复用")
					}
					roleID = step.Role
				}
				env, err := s.cartoRuntime.options.LoadEnvironment(ctx, run.Scheme)
				if err != nil {
					return err
				}
				role, err := flowrun.SelectRole(flow.StepPlan{Role: roleID}, env.Roles)
				if err != nil || !canAcquirePlanData(role) {
					return fmt.Errorf("取数断点绑定的角色 %s 已不属于当前方案的可用取数角色，不能用其他角色恢复旧断点；请修订当前计划后继续，已登记的数据仍可复用", roleID)
				}
			}
		}
	}
	return nil
}

func planDataChoiceKey(findID string, choice any) string {
	encoded, _ := json.Marshal(choice)
	return findID + "\n" + string(encoded)
}
func (s *DeepServer) injectTaskContext(ctx context.Context, input []*schema.Message, scheme, sid string, mode executionMode) ([]*schema.Message, error) {
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
	// Build keeps the main agent's task/delegate conversation alive. Only Plan
	// needs the declarative plan contract and controller state below.
	if s.cartoRuntime == nil || mode == executionModeBuild {
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
	var active map[string]any
	if run, ok := s.flowContext(sid); ok {
		if plans, e := s.ListPlans(ctx, sid, run.ProjectPath); e == nil {
			for i := len(plans) - 1; i >= 0; i-- {
				p := plans[i]
				if p.Plan.APIVersion == plan.APIVersion && !flowrun.IsTerminal(p.Status) {
					active = map[string]any{"plan_id": p.Plan.ID, "version": p.Plan.Version, "goal": p.Plan.Goal, "status": p.Status}
					break
				}
			}
		}
	}
	rule := boundedPlanningContext(env, enabled, active)
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
