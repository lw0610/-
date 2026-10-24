package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/autocarto/carto_agent/internal/agentruntime"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/runtimecontrol"
	"github.com/autocarto/carto_agent/internal/session"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/gorilla/websocket"
)

type flowSessionContext struct {
	Conn           *websocket.Conn
	Session        *session.Session
	SessionID      string
	ProjectPath    string
	InstanceID     string
	OriginalSource string
	Scheme         string
	Mode           executionMode
	FlowRunID      string
}
type flowRuntimeBinding struct{ server *DeepServer }
type sessionFlowService struct {
	server  *DeepServer
	session flowSessionContext
}
type flowTaskRequest struct {
	ID                  string         `json:"id"`
	Inputs              map[string]any `json:"inputs"`
	ResumeRunID         string         `json:"resume_run_id,omitempty"`
	ResourceChoice      string         `json:"resume_resource_choice,omitempty"`
	ResourceFingerprint string         `json:"resource_fingerprint,omitempty"`
}

// SetFlowCompiler attaches the same compiler used by the flow editor HTTP API.
func (s *DeepServer) SetFlowCompiler(compiler flowrun.Compiler) error {
	if compiler == nil {
		return fmt.Errorf("流程编译器不能为空")
	}
	registry := flowrun.NewExecutorRegistry()
	if err := registry.Register(flowrun.NewRouteKnowledgeExecutor()); err != nil {
		return err
	}
	binding := flowRuntimeBinding{server: s}
	controller, err := flowrun.NewController(flowrun.Options{Compiler: compiler, Runner: binding, Tools: binding, Model: binding, Events: binding, Store: flowrun.FileRunStore{}, Executors: registry})
	if err != nil {
		return err
	}
	if err := s.markStoredFlowsInterrupted(context.Background()); err != nil {
		return err
	}
	s.flowMu.Lock()
	defer s.flowMu.Unlock()
	s.flowCompiler = compiler
	s.flowController = controller
	if s.flowContexts == nil {
		s.flowContexts = map[string]flowSessionContext{}
	}
	if s.pendingFlows == nil {
		s.pendingFlows = map[string]flowrun.StartRequest{}
	}
	return nil
}

func (s *DeepServer) flowDependencies() (flowrun.Compiler, *flowrun.Controller) {
	s.flowMu.Lock()
	defer s.flowMu.Unlock()
	return s.flowCompiler, s.flowController
}
func (s *DeepServer) flowContext(id string) (flowSessionContext, bool) {
	s.flowMu.Lock()
	defer s.flowMu.Unlock()
	value, ok := s.flowContexts[id]
	return value, ok
}
func (s *DeepServer) setFlowContext(value flowSessionContext) {
	s.seedSessionEventSequence(value.SessionID, value.ProjectPath)
	s.flowMu.Lock()
	defer s.flowMu.Unlock()
	if s.flowContexts == nil {
		s.flowContexts = map[string]flowSessionContext{}
	}
	s.flowContexts[value.SessionID] = value
}
func (s *DeepServer) flowBuilder() (agentruntime.FlowBuilder, *config.AgentConfigFile, error) {
	s.mu.Lock()
	builder, ok := s.runtimeFactory.(agentruntime.FlowBuilder)
	runtime := s.runtime
	manager := s.agentConfigManager
	s.mu.Unlock()
	if !ok {
		return nil, nil, fmt.Errorf("当前运行时没有流程角色工厂")
	}
	var cfg *config.AgentConfigFile
	if manager != nil {
		cfg = manager.GetActiveConfig()
	} else if runtime != nil {
		cfg = runtime.ActiveConfig
	}
	if cfg == nil {
		return nil, nil, fmt.Errorf("当前智能体方案不可用")
	}
	return builder, cfg, nil
}

func (binding flowRuntimeBinding) Run(ctx context.Context, request flowrun.StepRequest) (flowrun.StepResult, error) {
	ctx = resource.WithScope(ctx, resource.Scope{FlowID: request.FlowID, FlowDir: request.FlowDir, Lock: request.Lock, Linked: request.Resources})
	s := binding.server
	run, ok := s.flowContext(request.SessionID)
	if !ok {
		return flowrun.StepResult{}, fmt.Errorf("流程会话上下文不存在")
	}
	run.FlowRunID = request.FlowRunID
	s.setFlowContext(run)
	var agent adk.Agent
	var err error
	if request.Confirmation != nil {
		agent = flowrun.NewConfirmAgent(*request.Confirmation)
	} else {
		builder, cfg, buildErr := s.flowBuilder()
		if buildErr != nil {
			return flowrun.StepResult{}, buildErr
		}
		agent, err = builder.BuildRole(ctx, run.Scheme, cfg, request.Role.ID)
		if err != nil {
			return flowrun.StepResult{}, err
		}
	}
	original := request.OriginalSource
	result := s.runAgentPhase(ctx, run.Conn, request.SessionID, run.ProjectPath, run.InstanceID, run.Session, agent, request.Messages, consumeEventsOptions{runID: request.RunID, emitFinalResponse: false, persistAssistant: true, modePhase: modeStatePhaseBuilding, executionMode: executionModeBuild, flowRunID: request.FlowRunID, flowStepID: request.Step.ID, originalSource: &original})
	return flowrun.StepResult{LastContent: result.LastContent, Interrupted: result.Interrupted, SuccessfulTools: result.SuccessfulTools, Err: result.Err}, result.Err
}
func (binding flowRuntimeBinding) Invoke(ctx context.Context, name string, args map[string]any) (any, error) {
	builder, _, err := binding.server.flowBuilder()
	if err != nil {
		return nil, err
	}
	return builder.InvokeFlowTool(ctx, name, args)
}
func (binding flowRuntimeBinding) CallJSON(ctx context.Context, prompt string) (string, error) {
	builder, cfg, err := binding.server.flowBuilder()
	if err != nil {
		return "", err
	}
	return builder.CallFlowJSON(ctx, cfg, prompt)
}
func (binding flowRuntimeBinding) Emit(ctx context.Context, event flowrun.Event) int64 {
	s := binding.server
	run, _ := s.flowContext(event.SessionID)
	raw, _ := json.Marshal(event)
	data := map[string]any{}
	_ = json.Unmarshal(raw, &data)
	seq, _ := s.sendSessionMessageSequenced(run.Conn, Message{Type: MsgTypeFlowEvent, SessionID: event.SessionID, Data: data})
	if event.Event == "flow.finished" {
		status, _ := event.Payload["status"].(string)
		runtimeStatus := sessionRuntimeStatusCompleted
		if status == "failed" {
			runtimeStatus = sessionRuntimeStatusFailed
		}
		if status == "cancelled" {
			runtimeStatus = sessionRuntimeStatusCancelled
		}
		s.markSessionRuntimeFinished(event.SessionID, runtimeStatus, "")
		summary, _ := event.Payload["summary"].(string)
		if summary != "" {
			message := schema.AssistantMessage(summary, nil)
			message.Extra = map[string]any{"flow_run_id": event.FlowRunID}
			if run.Session != nil {
				run.Session.AddMessage(message)
			}
			_ = s.sendSessionMessage(run.Conn, Message{Type: MsgTypeResponse, SessionID: event.SessionID, Content: summary, Data: map[string]any{"flow_run_id": event.FlowRunID}})
		}
		s.sendTaskRunningChanged(run.Conn, event.SessionID, false)
		s.sendModeState(run.Conn, event.SessionID, executionModeBuild, modeStatePhaseCompleted)
		s.persistManagedSessionState(event.SessionID, run.ProjectPath, executionModeBuild)
	}
	return int64(seq)
}

func (s *DeepServer) withFlowService(ctx context.Context, sessionID string) context.Context {
	if run, ok := s.flowContext(sessionID); ok {
		return runtimecontrol.WithFlowService(ctx, sessionFlowService{server: s, session: run})
	}
	return ctx
}

func (service sessionFlowService) PreviewFlow(ctx context.Context, id string, inputs map[string]any) (map[string]any, error) {
	compiler, controller := service.server.flowDependencies()
	if compiler == nil || controller == nil {
		return nil, fmt.Errorf("流程服务未就绪")
	}
	plan, err := compiler.EnsureRunnable(ctx, id, service.session.Scheme)
	var invalid *flow.ValidationError
	if err != nil && !errors.As(err, &invalid) {
		return nil, err
	}
	if plan == nil && invalid != nil {
		plan = invalid.Plan
	}
	if plan == nil {
		return nil, fmt.Errorf("流程计划不可用")
	}
	root, rootErr := config.ResolveProjectRoot()
	if rootErr != nil {
		return nil, rootErr
	}
	_, inputErr := flowrun.ResolveInputs(plan, inputs, service.session.OriginalSource, root)
	missing := []string{}
	if inputErr != nil {
		var bad *flowrun.InputError
		if errors.As(inputErr, &bad) {
			for _, reason := range bad.Reasons {
				missing = append(missing, reason.Message)
			}
		} else {
			missing = append(missing, inputErr.Error())
		}
	}
	return map[string]any{"flow_id": plan.FlowID, "name": plan.Name, "plan": plan, "diagnostics": plan.Diagnostics, "input_check": map[string]any{"missing": missing, "valid": inputErr == nil && !plan.HasErrors()}}, nil
}

func (service sessionFlowService) StartFlow(ctx context.Context, id string, inputs map[string]any) (map[string]any, error) {
	if service.session.Mode != executionModeBuild {
		return nil, fmt.Errorf("规划模式只可预览流程")
	}
	if service.session.FlowRunID != "" {
		return nil, fmt.Errorf("流程步骤中不能启动另一个流程")
	}
	preview, err := service.PreviewFlow(ctx, id, inputs)
	if err != nil {
		return nil, err
	}
	plan := preview["plan"].(*flow.FlowPlan)
	if plan.HasErrors() {
		return nil, &flow.ValidationError{Plan: plan, Diagnostics: plan.Diagnostics}
	}
	if check := preview["input_check"].(map[string]any); check["valid"] != true {
		return nil, fmt.Errorf("流程输入未齐全：%v", check["missing"])
	}
	_, controller := service.server.flowDependencies()
	runID, err := controller.NewRunID()
	if err != nil {
		return nil, err
	}
	sessionDir, err := config.ResolveSessionDir(service.session.ProjectPath, service.session.SessionID)
	if err != nil {
		return nil, err
	}
	root, err := config.ResolveProjectRoot()
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(inputs)
	if err != nil {
		return nil, err
	}
	var copied map[string]any
	if err = json.Unmarshal(raw, &copied); err != nil {
		return nil, err
	}
	request := flowrun.StartRequest{FlowRunID: runID, SessionID: service.session.SessionID, SessionDir: sessionDir, ProjectRoot: root, FlowID: id, Scheme: service.session.Scheme, Inputs: copied, OriginalSource: service.session.OriginalSource, Origin: "orchestrator"}
	s := service.server
	s.flowMu.Lock()
	defer s.flowMu.Unlock()
	if existing, ok := s.pendingFlows[request.SessionID]; ok {
		if existing.FlowID == id && reflect.DeepEqual(existing.Inputs, copied) {
			return map[string]any{"accepted": true, "flow_run_id": existing.FlowRunID}, nil
		}
		return nil, fmt.Errorf("本轮已登记另一个待启动流程")
	}
	s.pendingFlows[request.SessionID] = request
	return map[string]any{"accepted": true, "flow_run_id": runID}, nil
}

func (s *DeepServer) hasPendingFlow(sessionID string) bool {
	s.flowMu.Lock()
	defer s.flowMu.Unlock()
	_, ok := s.pendingFlows[sessionID]
	return ok
}
func (s *DeepServer) startPendingFlow(ctx context.Context, sessionID string) (*flowrun.RunState, error) {
	s.flowMu.Lock()
	request, ok := s.pendingFlows[sessionID]
	delete(s.pendingFlows, sessionID)
	run := s.flowContexts[sessionID]
	if ok {
		run.FlowRunID = request.FlowRunID
		s.flowContexts[sessionID] = run
	}
	controller := s.flowController
	s.flowMu.Unlock()
	if !ok {
		return nil, nil
	}
	return controller.Start(withToolSessionContext(ctx, run.SessionID, run.ProjectPath, run.InstanceID), request)
}

func parseFlowRequest(data map[string]any) (*flowTaskRequest, error) {
	raw, exists := data["flow"]
	if !exists || raw == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var request flowTaskRequest
	if err = json.Unmarshal(encoded, &request); err != nil {
		return nil, fmt.Errorf("流程参数格式无效: %w", err)
	}
	if strings.TrimSpace(request.ID) == "" && strings.TrimSpace(request.ResumeRunID) == "" {
		return nil, fmt.Errorf("流程编号不能为空")
	}
	request.ID = strings.TrimSpace(request.ID)
	return &request, nil
}

func (s *DeepServer) runDirectFlow(ctx context.Context, run flowSessionContext, request flowTaskRequest) (*flowrun.RunState, error) {
	ctx = withToolSessionContext(ctx, run.SessionID, run.ProjectPath, run.InstanceID)
	_, controller := s.flowDependencies()
	if controller == nil {
		return nil, fmt.Errorf("流程服务尚未初始化")
	}
	sessionDir, err := config.ResolveSessionDir(run.ProjectPath, run.SessionID)
	if err != nil {
		return nil, err
	}
	if request.ResumeRunID != "" {
		state, loadErr := (flowrun.FileRunStore{}).Load(ctx, sessionDir, request.ResumeRunID)
		if loadErr != nil {
			return nil, loadErr
		}
		source, err := s.findOriginalFlowSource(ctx, run, sessionDir, request.ResumeRunID)
		if err != nil {
			return nil, err
		}
		run.OriginalSource = source
		run.FlowRunID = request.ResumeRunID
		run.Scheme = state.Scheme
		s.setFlowContext(run)
		resume := flowrun.ResumeRequest{SessionID: run.SessionID, SessionDir: sessionDir, FlowRunID: request.ResumeRunID, OriginalSource: source, ResourceChoice: request.ResourceChoice, ResourceFingerprint: request.ResourceFingerprint}
		if request.ResourceChoice == "restart" {
			previous, _ := s.getPendingInterrupt(run.SessionID)
			restarted, err := controller.Restart(ctx, resume)
			if resourceChangePending(err) {
				return restarted, nil
			}
			if restarted != nil && restarted.ID != request.ResumeRunID {
				s.discardOldFlowCheckpoint(ctx, run.SessionID, run.ProjectPath, request.ResumeRunID, previous)
			}
			return restarted, err
		}
		if state.Status == "waiting_user" {
			pending, err := s.restoreFlowPending(ctx, run.SessionID, run.ProjectPath, request.ResumeRunID, resume)
			if resourceChangePending(err) {
				paused, _ := (flowrun.FileRunStore{}).Load(ctx, sessionDir, request.ResumeRunID)
				return paused, nil
			}
			if err == nil {
				s.sendSessionMessage(run.Conn, Message{Type: MsgTypeInterruptRequest, SessionID: run.SessionID, Content: "任务需要您的确认才能继续", Data: pendingInterruptPayload(pending)})
			}
			return state, err
		}
		resumed, err := controller.Resume(ctx, resume)
		if resourceChangePending(err) {
			return resumed, nil
		}
		return resumed, err
	}
	root, err := config.ResolveProjectRoot()
	if err != nil {
		return nil, err
	}
	id, err := controller.NewRunID()
	if err != nil {
		return nil, err
	}
	run.FlowRunID = id
	s.setFlowContext(run)
	return controller.Start(ctx, flowrun.StartRequest{FlowRunID: id, SessionID: run.SessionID, SessionDir: sessionDir, ProjectRoot: root, FlowID: request.ID, Scheme: run.Scheme, Inputs: request.Inputs, OriginalSource: run.OriginalSource, Origin: "direct"})
}

func (s *DeepServer) findOriginalFlowSource(ctx context.Context, run flowSessionContext, dir, id string) (string, error) {
	state, err := (flowrun.FileRunStore{}).Load(ctx, dir, id)
	if err != nil {
		return "", err
	}
	if state.SessionID != run.SessionID {
		return "", fmt.Errorf("流程与会话不匹配")
	}
	if run.Session == nil {
		return "", fmt.Errorf("会话历史不可用")
	}
	messages := run.Session.GetMessages()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != schema.User {
			continue
		}
		valid := true
		for _, input := range state.Plan.Inputs {
			if input.Type != "conversation_text" {
				continue
			}
			if ref, ok := state.Inputs[input.Key].(map[string]any); ok && ref["$ref"] == "current_user_message" {
				resolved, resolveErr := flowrun.ResolveInputs(&flow.FlowPlan{Inputs: []flow.InputPlan{input}}, nil, messages[i].Content, filepath.Dir(dir))
				if resolveErr != nil {
					valid = false
					break
				}
				before, _ := json.Marshal(ref)
				after, _ := json.Marshal(resolved[input.Key])
				valid = string(before) == string(after)
				if !valid {
					break
				}
			}
		}
		if valid {
			return messages[i].Content, nil
		}
	}
	return "", fmt.Errorf("找不到运行记录对应的原始消息，不能用新的继续指令替代原文")
}

// Flow metadata decorates helper-generated progress frames as well as direct frames.
func (s *DeepServer) setFlowMessageMetadata(sessionID string, options consumeEventsOptions) func() {
	if options.flowRunID == "" {
		return func() {}
	}
	s.flowMu.Lock()
	if s.flowMessageMeta == nil {
		s.flowMessageMeta = map[string]map[string]any{}
	}
	s.flowMessageMeta[sessionID] = map[string]any{"flow_run_id": options.flowRunID, "step_id": options.flowStepID}
	s.flowMu.Unlock()
	return func() { s.flowMu.Lock(); delete(s.flowMessageMeta, sessionID); s.flowMu.Unlock() }
}
func (s *DeepServer) finishFlowResume(ctx context.Context, pending *PendingInterrupt, result consumeEventsResult) {
	if pending.FlowOptions.flowRunID == "" {
		return
	}
	_, controller := s.flowDependencies()
	if controller == nil {
		return
	}
	if _, err := controller.OnStepRunFinished(ctx, pending.CheckpointID, flowrun.StepResult{LastContent: result.LastContent, Interrupted: result.Interrupted, SuccessfulTools: result.SuccessfulTools, Err: result.Err}); err != nil {
		s.sendSessionError(nil, pending.SessionID, fmt.Sprintf("继续流程失败: %v", err))
	}
}
func (s *DeepServer) injectAvailableFlows(ctx context.Context, input []*schema.Message, scheme string) []*schema.Message {
	compiler, _ := s.flowDependencies()
	catalog, ok := compiler.(interface {
		AvailablePlans(context.Context, string) ([]*flow.FlowPlan, error)
	})
	if !ok {
		return input
	}
	plans, err := catalog.AvailablePlans(ctx, scheme)
	if err != nil || len(plans) == 0 {
		return input
	}
	summaries := []map[string]any{}
	for _, p := range plans {
		app := []rune(p.Applicability)
		if len(app) > 300 {
			app = app[:300]
		}
		summaries = append(summaries, map[string]any{"id": p.FlowID, "name": p.Name, "triggers": p.Triggers, "applicability": string(app), "inputs": p.Inputs})
	}
	raw, _ := json.Marshal(summaries)
	message := schema.SystemMessage("当前方案可运行流程：" + string(raw) + "\n匹配用户任务时可使用 flow_preview 查看步骤与输入、flow_start 登记执行。conversation_text 输入引用当前用户消息。输入不足先询问；规划模式只预览。")
	result := append([]*schema.Message{}, input...)
	return append(result, message)
}
