package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/agentruntime"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/resource"
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

func (s *DeepServer) flowDependencies() (any, *flowrun.Controller) {
	s.flowMu.Lock()
	defer s.flowMu.Unlock()
	return nil, s.flowController
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

type stepConversation struct {
	agent    adk.Agent
	messages []*schema.Message
	styles   []flowrun.ToolSummary
}

func (binding flowRuntimeBinding) Run(ctx context.Context, request flowrun.StepRequest) (flowrun.StepResult, error) {
	ctx = resource.WithScope(ctx, resource.Scope{FlowID: request.FlowID, FlowDir: request.FlowDir, Lock: request.Lock, Linked: request.Resources})
	if request.Acquisition != nil {
		if len(request.Acquisition.Need) > 0 {
			var need dataservice.AcquisitionNeed
			if err := json.Unmarshal(request.Acquisition.Need, &need); err != nil {
				return flowrun.StepResult{}, err
			}
			ctx = dataservice.WithAcquisitionNeed(ctx, need)
		} else {
			var selection dataservice.Selection
			if err := json.Unmarshal(request.Acquisition.Selection, &selection); err != nil {
				return flowrun.StepResult{}, err
			}
			ctx = dataservice.WithAcquisitionSelection(ctx, selection)
		}
	}
	s := binding.server
	run, ok := s.flowContext(request.SessionID)
	if !ok {
		return flowrun.StepResult{}, fmt.Errorf("流程会话上下文不存在")
	}
	run.FlowRunID = request.FlowRunID
	s.setFlowContext(run)
	var agent adk.Agent
	var err error
	conversationKey := fmt.Sprintf("%s/%s/v%d/%s", request.FlowRunID, request.Step.ID, request.PlanVersion, request.Mode)
	if request.Acquisition != nil {
		conversationKey += "/" + request.Acquisition.DataID
	}
	var conversation *stepConversation
	if s.cartoRuntime != nil {
		s.cartoRuntime.mu.Lock()
		conversation = s.cartoRuntime.stepConversations[conversationKey]
		s.cartoRuntime.mu.Unlock()
	}
	if conversation != nil {
		agent = conversation.agent
	}

	if request.Confirmation != nil {
		agent = flowrun.NewConfirmAgent(*request.Confirmation)
	} else if agent == nil {
		builder, cfg, buildErr := s.flowBuilder()
		if buildErr != nil {
			return flowrun.StepResult{}, buildErr
		}
		agent, err = builder.BuildRole(ctx, run.Scheme, cfg, request.Role.ID)
		if err != nil {
			return flowrun.StepResult{}, err
		}
	}
	if conversation != nil {
		conversation.agent = agent
	}
	if conversation == nil {
		conversation = &stepConversation{agent: agent}
		if s.cartoRuntime != nil {
			s.cartoRuntime.mu.Lock()
			s.cartoRuntime.stepConversations[conversationKey] = conversation
			s.cartoRuntime.mu.Unlock()
		}
	}
	messages := request.Messages
	if len(conversation.messages) > 0 {
		repair := request.Repair
		if repair == "" {
			repair = "继续未完成的工作，复用已经取得的数据和工具结果。"
		}
		messages = append(append([]*schema.Message{}, conversation.messages...), schema.UserMessage(repair))
	}
	original := request.OriginalSource
	result := s.runAgentPhase(ctx, run.Conn, request.SessionID, run.ProjectPath, run.InstanceID, run.Session, agent, messages, consumeEventsOptions{runID: request.RunID, emitFinalResponse: false, persistAssistant: true, modePhase: modeStatePhaseBuilding, executionMode: executionModeBuild, flowRunID: request.FlowRunID, flowStepID: request.Step.ID, originalSource: &original})
	conversation.messages = append(messages, result.Messages...)
	conversation.styles = append(conversation.styles, result.StyleTools...)
	if len(result.Messages) == 0 && result.LastContent != "" {
		conversation.messages = append(conversation.messages, schema.AssistantMessage(result.LastContent, nil))
	}
	return flowrun.StepResult{LastContent: result.LastContent, Interrupted: result.Interrupted, SuccessfulTools: result.SuccessfulTools, StyleTools: conversation.styles, Err: result.Err}, result.Err
}

// An ADK resume starts with a fresh context. Restore the exact query from the
// controller's checked snapshot, rather than copying it into another checkpoint.
func withRunAcquisitionSnapshot(ctx context.Context, state *flowrun.RunState) (context.Context, error) {
	if state == nil || state.Cursor < 0 || state.Cursor >= len(state.Steps) {
		return ctx, nil
	}
	step := state.Steps[state.Cursor]
	if len(step.Attempts) == 0 {
		return ctx, nil
	}
	attempt := step.Attempts[len(step.Attempts)-1]
	if attempt.Mode == "prepare" {
		var need dataservice.AcquisitionNeed
		if err := json.Unmarshal(state.DataNeeds[attempt.DataID], &need); err != nil {
			return ctx, err
		}
		return dataservice.WithAcquisitionNeed(ctx, need), nil
	}
	if attempt.Mode != "acquire" {
		return ctx, nil
	}
	if state.CartoPlan == nil || attempt.DataID == "" {
		return ctx, fmt.Errorf("数据获取恢复缺少已确认的数据引用")
	}
	data, ok := state.CartoPlan.DataByID(attempt.DataID)
	var selection dataservice.Selection
	if !ok || json.Unmarshal(state.Selections[attempt.DataID], &selection) != nil || selection.FindID != data.FindID || !selection.RequiresDiscovery() {
		return ctx, fmt.Errorf("数据获取恢复缺少匹配的已确认来源")
	}
	return dataservice.WithAcquisitionSelection(ctx, selection), nil
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
	// Only lifecycle transitions update the whole plan. A completed data item
	// is still part of a running step and must not mark the plan completed.
	status := ""
	switch event.Event {
	case "flow.started", "flow.step_started", "flow.plan_revised":
		status = "running"
	case "flow.finished":
		status, _ = event.Payload["status"].(string)
	case "flow.step_waiting_user":
		status = "waiting_user"
		if event.Payload["status"] == "interrupted" {
			status = "interrupted"
		}
	case "flow.step_failed":
		if event.Payload["next"] == "replan" {
			status = "needs_replan"
		}
	}
	if event.PlanID != "" && status != "" {
		if store, e := planStore(run); e == nil {
			snapshot, e := store.UpdateStatus(event.PlanID, event.PlanVersion, func(v *plan.Snapshot) error {
				v.RunID = event.FlowRunID
				v.Status = status
				return nil
			})
			if e == nil {
				s.publishPlan(snapshot)
			}
		}
	}
	seq, _ := s.sendSessionMessageSequenced(run.Conn, Message{Type: MsgTypeFlowEvent, SessionID: event.SessionID, Data: data})
	if event.Event == "flow.step_waiting_user" && event.Payload["status"] == "interrupted" && s.cartoRuntime != nil {
		// Account/model settings may change while paused. Keep the conversation,
		// but create the model client from the updated configuration on resume.
		s.cartoRuntime.mu.Lock()
		for key, conversation := range s.cartoRuntime.stepConversations {
			if strings.HasPrefix(key, event.FlowRunID+"/") {
				conversation.agent = nil
			}
		}
		s.cartoRuntime.mu.Unlock()
	}
	if event.Event == "flow.finished" {
		if s.cartoRuntime != nil {
			s.cartoRuntime.mu.Lock()
			for key := range s.cartoRuntime.stepConversations {
				if strings.HasPrefix(key, event.FlowRunID+"/") {
					delete(s.cartoRuntime.stepConversations, key)
				}
			}
			s.cartoRuntime.mu.Unlock()
		}
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
				before, _ := json.Marshal(ref)
				after, _ := json.Marshal(flowrun.ConversationReference(messages[i].Content))
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
	if s.cartoRuntime != nil {
		if previous, e := controller.Snapshot(pending.SessionID); e == nil && previous != nil && previous.CartoPlan != nil && previous.Cursor < len(previous.Steps) {
			key := fmt.Sprintf("%s/%s/v%d/step", previous.ID, previous.Steps[previous.Cursor].ID, previous.CartoPlan.Version)
			s.cartoRuntime.mu.Lock()
			if conversation := s.cartoRuntime.stepConversations[key]; conversation != nil {
				conversation.styles = append(conversation.styles, result.StyleTools...)
				result.StyleTools = conversation.styles
			}
			s.cartoRuntime.mu.Unlock()
		}
	}
	state, err := controller.OnStepRunFinished(ctx, pending.CheckpointID, flowrun.StepResult{LastContent: result.LastContent, Interrupted: result.Interrupted, SuccessfulTools: result.SuccessfulTools, StyleTools: result.StyleTools, Err: result.Err})
	run, _ := s.flowContext(pending.SessionID)
	s.publishCartographicRun(run, state)
	if err != nil {
		s.sendSessionError(nil, pending.SessionID, fmt.Sprintf("继续计划失败: %v", err))
		return
	}
	if state != nil && state.CartoPlan != nil && state.Status == "needs_replan" {
		if err = s.replanCartographic(ctx, run, state); err == nil && s.hasPendingCartographicPlan(run.SessionID) {
			_, err = s.startPendingCartographicPlan(ctx, run.SessionID)
		}
		if err != nil {
			s.sendSessionError(nil, pending.SessionID, err.Error())
		}
	}
}
