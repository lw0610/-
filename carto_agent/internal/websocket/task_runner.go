package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/deep"
	"github.com/cloudwego/eino/schema"
	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/deepagent"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/autocarto/carto_agent/internal/logger"
	"github.com/autocarto/carto_agent/internal/prompts"
	"github.com/autocarto/carto_agent/internal/runtimepersist"
	"github.com/autocarto/carto_agent/internal/session"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"github.com/autocarto/carto_agent/internal/sessionmeta"
	tooldefinition "github.com/autocarto/carto_agent/internal/tool"
	"github.com/autocarto/carto_agent/internal/toolsession"
)

type executionMode string

const (
	executionModePlan  executionMode = "plan"
	executionModeBuild executionMode = "build"
)

var runtimePromptRegistry = prompts.NewRegistry()

var runtimeExecutionModeInstructionPlan atomic.Value
var runtimeExecutionModeInstructionBuild = renderRuntimePromptTemplate(prompts.TemplateModeBuild, nil)

func init() {
	setPlanRuntimeExecutionModeInstruction("")
}

const (
	modeStatePhasePlanning  = "planning"
	modeStatePhaseBuilding  = "building"
	modeStatePhaseCompleted = "completed"
)

type consumeEventsOptions struct {
	flowRunID         string
	flowStepID        string
	originalSource    *string
	titleSeed         string
	emitFinalResponse bool
	persistAssistant  bool
	modePhase         string
	executionMode     executionMode
	runID             string
}

type consumeEventsResult struct {
	SuccessfulTools []flowrun.ToolSummary
	LastContent     string
	Interrupted     bool
	Err             error
}

var errSkipAutoSessionTitle = errors.New("skip auto session title")

func parseProjectPathFromMessageData(data map[string]any) string {
	if len(data) == 0 {
		return ""
	}
	projectPath, _ := data["project_path"].(string)
	return strings.TrimSpace(projectPath)
}

func parseInstanceIDFromMessageData(data map[string]any) string {
	if len(data) == 0 {
		return ""
	}
	instanceID, _ := data["instance_id"].(string)
	return strings.TrimSpace(instanceID)
}

func withToolSessionContext(ctx context.Context, sessionID, projectPath, instanceID string) context.Context {
	return toolsession.WithContext(ctx, sessionID, projectPath, instanceID)
}

func sessionIDFromContext(ctx context.Context) string {
	return toolsession.SessionIDFromContext(ctx)
}

func projectPathFromContext(ctx context.Context) string {
	return toolsession.ProjectPathFromContext(ctx)
}

func instanceIDFromContext(ctx context.Context) string {
	return toolsession.InstanceIDFromContext(ctx)
}

func parseExecutionModeFromMessageData(data map[string]any) (executionMode, error) {
	if len(data) == 0 {
		return executionModeBuild, nil
	}

	if intent, ok := data["run_intent"]; ok && hasMeaningfulExecutionModeValue(intent) {
		return parseRunIntentValue(intent)
	}

	if mode, ok := data["mode"]; ok && hasMeaningfulExecutionModeValue(mode) {
		return parseExecutionModeValue(mode)
	}

	if mode, ok := data["execution_mode"]; ok && hasMeaningfulExecutionModeValue(mode) {
		return parseExecutionModeValue(mode)
	}

	return executionModeBuild, nil
}

func hasMeaningfulExecutionModeValue(raw any) bool {
	if raw == nil {
		return false
	}
	if text, ok := raw.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return true
}

func parseExecutionModeValue(raw any) (executionMode, error) {
	if text, ok := raw.(string); ok {
		normalized := strings.TrimSpace(strings.ToLower(text))
		switch normalized {
		case "plan", "plan_only":
			return executionModePlan, nil
		case "build", "execute":
			return executionModeBuild, nil
		case "auto":
			return "", fmt.Errorf("execution_mode=auto 已废弃，不再支持")
		case "":
			return executionModeBuild, nil
		default:
			return "", fmt.Errorf("execution_mode=%s 无效，仅支持 build / plan", normalized)
		}
	}

	if parseBoolLike(raw, true) {
		return executionModePlan, nil
	}
	return executionModeBuild, nil
}

func parseRunIntentValue(raw any) (executionMode, error) {
	if text, ok := raw.(string); ok {
		normalized := strings.TrimSpace(strings.ToLower(text))
		switch normalized {
		case "plan_only", "plan":
			return executionModePlan, nil
		case "execute", "build":
			return executionModeBuild, nil
		case "auto":
			return "", fmt.Errorf("run_intent=auto 已废弃，不再支持")
		case "":
			return executionModeBuild, nil
		default:
			return "", fmt.Errorf("run_intent=%s 无效，仅支持 execute / plan_only", normalized)
		}
	}

	if parseBoolLike(raw, true) {
		return executionModePlan, nil
	}
	return executionModeBuild, nil
}

func parseBoolLike(raw any, fallback bool) bool {
	switch v := raw.(type) {
	case nil:
		return fallback
	case bool:
		return v
	case string:
		text := strings.TrimSpace(strings.ToLower(v))
		if text == "" {
			return fallback
		}
		switch text {
		case "false", "0", "off", "no", "n", "关", "关闭":
			return false
		case "true", "1", "on", "yes", "y", "开", "开启":
			return true
		default:
			return fallback
		}
	case int:
		return v != 0
	case int8:
		return v != 0
	case int16:
		return v != 0
	case int32:
		return v != 0
	case int64:
		return v != 0
	case uint:
		return v != 0
	case uint8:
		return v != 0
	case uint16:
		return v != 0
	case uint32:
		return v != 0
	case uint64:
		return v != 0
	case float32:
		return v != 0
	case float64:
		return v != 0
	default:
		return fallback
	}
}

func buildRuntimeExecutionModeInstruction(mode executionMode) string {
	switch mode {
	case executionModePlan:
		return getPlanRuntimeExecutionModeInstruction()
	default:
		return runtimeExecutionModeInstructionBuild
	}
}

func getPlanRuntimeExecutionModeInstruction() string {
	if value, ok := runtimeExecutionModeInstructionPlan.Load().(string); ok {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return renderRuntimePromptTemplate(prompts.TemplateModePlan, nil)
}

func setPlanRuntimeExecutionModeInstruction(instruction string) {
	normalized := strings.TrimSpace(instruction)
	if normalized == "" {
		normalized = renderRuntimePromptTemplate(prompts.TemplateModePlan, nil)
	}
	runtimeExecutionModeInstructionPlan.Store(normalized)
}

func renderRuntimePromptTemplate(templateName string, data any) string {
	if strings.TrimSpace(templateName) == "" {
		return ""
	}
	if runtimePromptRegistry == nil {
		return ""
	}
	text, err := runtimePromptRegistry.Render(templateName, data)
	if err != nil {
		return ""
	}
	return text
}

func injectRuntimeInstruction(messages []*schema.Message, instruction string, thinkingEnabled bool) []*schema.Message {
	if thinkingEnabled {
		instruction += "\n- Think 模式已开启：优先输出结构化推理与关键依据。"
	}
	input := make([]*schema.Message, 0, len(messages)+1)
	input = append(input, &schema.Message{
		Role:    schema.System,
		Content: instruction,
	})
	input = append(input, messages...)
	return input
}

func injectRuntimeExecutionMode(messages []*schema.Message, mode executionMode, thinkingEnabled bool) []*schema.Message {
	return injectRuntimeInstruction(messages, buildRuntimeExecutionModeInstruction(mode), thinkingEnabled)
}

func withModePhaseData(data map[string]interface{}, modePhase string) map[string]interface{} {
	if strings.TrimSpace(modePhase) == "" {
		return data
	}
	if data == nil {
		data = map[string]interface{}{}
	}
	data["mode_phase"] = modePhase
	return data
}

func (s *DeepServer) processTask(ctx context.Context, conn *websocket.Conn, msg Message) {
	logger.Info("========== processTask 方法被调用 ==========")
	logger.Info("收到消息类型: %s, 内容: %s", msg.Type, msg.Content)

	// 提前确定会话 ID，确保错误和状态消息也能按会话路由
	sessionID := normalizeSessionID(conn, msg.SessionID)
	if sessionID == "" {
		sessionID = msg.SessionID
	}
	msg.SessionID = sessionID
	s.seedSessionEventSequence(sessionID, parseProjectPathFromMessageData(msg.Data))

	keepRunningState := false
	boundInstanceID := ""
	runID := fmt.Sprintf("task-%s-%d", sessionID, time.Now().UnixNano())

	defer func() {
		s.knowledgeConfirmMu.Lock()
		defer s.knowledgeConfirmMu.Unlock()
		if state, active := s.activeSessionRuntime(sessionID); active && state.RunID != runID {
			return
		}
		if ctx.Err() != nil && !keepRunningState {
			s.markSessionRuntimeCancelled(sessionID, runID)
		}
		if !keepRunningState {
			s.sendTaskRunningChanged(conn, sessionID, false)
		}
		logger.Info("========== processTask 方法结束 ==========")
	}()

	// 发送开始状态和思考消息
	s.sendStatus(conn, sessionID, "thinking", "正在分析您的请求...")
	s.sendThinking(conn, sessionID, "收到用户请求，开始分析任务需求...")

	// 获取或创建会话
	sess := s.sessionManager.GetOrCreate(sessionID)
	projectPath := parseProjectPathFromMessageData(msg.Data)
	if s.hasActiveSessionRuntime(sessionID) && !s.isPausedFlowResume(sessionID, projectPath, msg.Data) {
		s.sendSessionError(conn, sessionID, "当前会话已有运行中的任务，请等待完成或先取消。")
		return
	}
	execMode, err := parseExecutionModeFromMessageData(msg.Data)
	if err != nil {
		s.sendSessionErrorWithCode(conn, sessionID, err.Error(), ErrorCodeInvalidExecutionMode)
		return
	}
	thinkingEnabled := parseBoolLike(msg.Data["thinking_enabled"], false)
	explicitInstanceID := parseInstanceIDFromMessageData(msg.Data)
	binding, bindErr := s.resolveSessionBinding(explicitInstanceID, projectPath)
	if bindErr != nil {
		s.sendSessionErrorWithCode(conn, sessionID, bindErr.Error(), ErrorCodeBoundInstanceUnavailable)
		return
	}
	boundInstanceID = binding.InstanceID
	if strings.TrimSpace(binding.ProjectPath) != "" {
		projectPath = strings.TrimSpace(binding.ProjectPath)
	}
	// Bind persistence before checking agent availability. This is request
	// context only: no runner or checkpoint exists until runAgentPhase starts.
	s.updateSessionRuntimeProjectionState(SessionRuntimeState{
		SessionID:         sessionID,
		ExecutionMode:     string(execMode),
		BoundInstanceID:   boundInstanceID,
		BoundProjectPath:  projectPath,
		RuntimeRevision:   strings.TrimSpace(s.GetRuntimeRevision()),
		ActiveAgentConfig: strings.TrimSpace(s.GetRuntimeActiveConfigName()),
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
	})
	s.hydrateSessionMessages(sess, sessionID, projectPath)
	logger.Info("任务运行模式: session_id=%s, execution_mode=%s, thinking=%t, bound_instance_id=%s, bound_project_path=%s, binding_reason=%s", sessionID, execMode, thinkingEnabled, boundInstanceID, projectPath, strings.TrimSpace(binding.Reason))

	// 添加用户消息到会话历史
	userMsg := schema.UserMessage(msg.Content)
	sess.AddMessage(userMsg)
	s.persistManagedSessionState(sessionID, projectPath, execMode)

	flowContext := flowSessionContext{Conn: conn, Session: sess, SessionID: sessionID, ProjectPath: projectPath, InstanceID: boundInstanceID, OriginalSource: msg.Content, Scheme: s.GetRuntimeActiveConfigName(), Mode: execMode}
	s.setFlowContext(flowContext)
	flowRequest, flowErr := parseFlowRequest(msg.Data)
	if flowErr != nil {
		s.sendSessionError(conn, sessionID, flowErr.Error())
		return
	}
	if flowRequest != nil {
		if execMode == executionModePlan {
			preview, err := (sessionFlowService{server: s, session: flowContext}).PreviewFlow(ctx, flowRequest.ID, flowRequest.Inputs)
			if err != nil {
				s.sendSessionError(conn, sessionID, err.Error())
				return
			}
			s.sendSessionMessage(conn, Message{Type: MsgTypeAgentWorkflow, SessionID: sessionID, Phase: "tool_result", Data: withModePhaseData(map[string]any{"tool": "flow_preview", "status": "completed", "result": preview}, modeStatePhasePlanning)})
			s.sendModeState(conn, sessionID, execMode, modeStatePhaseCompleted)
			return
		}
		s.sendTaskRunningChanged(conn, sessionID, true)
		state, err := s.runDirectFlow(ctx, flowContext, *flowRequest)
		if err != nil {
			s.sendSessionError(conn, sessionID, err.Error())
			s.markSessionRuntimeFinished(sessionID, sessionRuntimeStatusFailed, err.Error())
		}
		keepRunningState = state != nil && !flowrun.IsTerminal(state.Status)
		return
	}

	switch execMode {
	case executionModePlan:
		input := s.injectAvailableFlows(ctx, injectRuntimeExecutionMode(sess.GetMessages(), executionModePlan, thinkingEnabled), flowContext.Scheme)
		runtimeAgent := s.getModeAgentWithWarning(conn, sessionID, executionModePlan)
		if runtimeAgent == nil {
			s.sendModeState(conn, sessionID, execMode, modeStatePhaseCompleted)
			return
		}
		s.sendTaskRunningChanged(conn, sessionID, true)
		s.sendModeState(conn, sessionID, execMode, modeStatePhasePlanning)
		result := s.runAgentPhase(ctx, conn, sessionID, projectPath, boundInstanceID, sess, runtimeAgent, input, consumeEventsOptions{
			runID:             runID,
			titleSeed:         msg.Content,
			emitFinalResponse: true,
			persistAssistant:  true,
			modePhase:         modeStatePhasePlanning,
			executionMode:     executionModePlan,
		})
		if ctx.Err() != nil || errors.Is(result.Err, context.Canceled) {
			return
		}
		if !result.Interrupted {
			s.sendModeState(conn, sessionID, execMode, modeStatePhaseCompleted)
		} else {
			keepRunningState = true
		}
		return

	case executionModeBuild:
		buildInput := s.injectAvailableFlows(ctx, injectRuntimeExecutionMode(sess.GetMessages(), executionModeBuild, thinkingEnabled), flowContext.Scheme)
		buildAgent := s.getModeAgentWithWarning(conn, sessionID, executionModeBuild)
		if buildAgent == nil {
			s.sendModeState(conn, sessionID, execMode, modeStatePhaseCompleted)
			return
		}
		s.sendTaskRunningChanged(conn, sessionID, true)
		s.sendModeState(conn, sessionID, execMode, modeStatePhaseBuilding)
		result := s.runAgentPhase(ctx, conn, sessionID, projectPath, boundInstanceID, sess, buildAgent, buildInput, consumeEventsOptions{
			runID:             runID,
			titleSeed:         msg.Content,
			emitFinalResponse: true,
			persistAssistant:  true,
			modePhase:         modeStatePhaseBuilding,
			executionMode:     executionModeBuild,
		})
		if ctx.Err() != nil || errors.Is(result.Err, context.Canceled) {
			return
		}
		if result.Err == nil && !result.Interrupted && s.hasPendingFlow(sessionID) {
			state, err := s.startPendingFlow(ctx, sessionID)
			if err != nil {
				s.sendSessionError(conn, sessionID, err.Error())
				s.markSessionRuntimeFinished(sessionID, sessionRuntimeStatusFailed, err.Error())
			}
			keepRunningState = state != nil && !flowrun.IsTerminal(state.Status)
			return
		}

		if !result.Interrupted {
			s.sendModeState(conn, sessionID, execMode, modeStatePhaseCompleted)
		} else {
			keepRunningState = true
		}
		return

	default:
		s.sendSessionErrorWithCode(conn, sessionID, fmt.Sprintf("execution_mode=%s 无效，仅支持 build / plan", execMode), ErrorCodeInvalidExecutionMode)
		return
	}
}

func (s *DeepServer) consumeEvents(
	ctx context.Context,
	conn *websocket.Conn,
	iter *adk.AsyncIterator[*adk.AgentEvent],
	runner *adk.Runner,
	checkpointID string,
	sessionID string,
	projectPath string,
	sess *session.Session,
	options consumeEventsOptions,
) consumeEventsResult {
	cleanup := s.setFlowMessageMetadata(sessionID, options)
	defer cleanup()
	var lastAssistant string
	var lastContent string
	var lastAgentName string
	toolActivitySeen := false
	emittedWorkflowTexts := make(map[string]struct{})
	result := consumeEventsResult{}
	fileStore, _ := sessionfiles.New(projectPath, sessionID)
	artifactDefinitions := map[string]tooldefinition.ToolDefinition{}
	if fileStore != nil {
		fileStore.OnChange(func(change sessionfiles.Change) {
			s.sendSessionMessage(nil, Message{Type: MsgTypeSessionFilesChanged, SessionID: sessionID, Data: map[string]any{"session_id": sessionID, "added": change.Added, "updated": change.Updated}})
		})
		if definitions, err := tooldefinition.LoadCanonicalDefinitions(); err == nil {
			for _, definition := range definitions {
				artifactDefinitions[definition.Metadata.Name] = definition
			}
		}
	}

	if strings.TrimSpace(options.titleSeed) != "" {
		go s.tryAutoTitleSession(sessionID, projectPath, options.titleSeed)
	}

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}

		if ctx.Err() != nil {
			result.LastContent = lastContent
			result.Err = ctx.Err()
			return result
		}

		// 处理智能体切换事件
		if event.AgentName != "" && event.AgentName != lastAgentName {
			lastAgentName = event.AgentName
			s.setSessionAgent(conn, sessionID, event.AgentName)

			friendlyName := getAgentFriendlyName(event.AgentName)
			s.sendSessionMessage(conn, Message{
				Type:             "agent_workflow",
				SessionID:        sessionID,
				Agent:            event.AgentName,
				AgentDisplayName: friendlyName,
				Phase:            "thinking",
				Content:          fmt.Sprintf("识别到需要 %s 处理此任务", friendlyName),
				Data:             withModePhaseData(nil, options.modePhase),
			})
		}

		// 处理输出事件
		if event.Output != nil && event.Output.MessageOutput != nil {
			mv := event.Output.MessageOutput
			if mv.Message != nil {
				if !mv.IsStreaming && !isRuntimeProgressMessage(mv.Message) {
					lastContent = summarizeSchemaMessageContent(mv.Message)
				}

				if mv.Role == schema.Assistant {
					if !mv.IsStreaming && !isRuntimeProgressMessage(mv.Message) && len(mv.Message.ToolCalls) == 0 {
						lastAssistant = summarizeSchemaMessageContent(mv.Message)
					}
					s.emitAssistantWorkflowEvents(conn, sessionID, mv.Message, options.modePhase, toolActivitySeen, mv.IsStreaming, emittedWorkflowTexts)
				}
			}

			if !mv.IsStreaming && mv.Message != nil {

				if mv.Role == schema.Assistant && mv.Message.ToolCalls != nil {
					toolActivitySeen = true
					currentAgent := s.getSessionAgent(conn, sessionID)
					if currentAgent == "" {
						currentAgent = "orchestrator"
					}
					friendlyName := getAgentFriendlyName(currentAgent)

					for _, toolCall := range mv.Message.ToolCalls {
						toolName := toolCall.Function.Name
						var toolArgs map[string]interface{}
						json.Unmarshal([]byte(toolCall.Function.Arguments), &toolArgs)
						logger.Info(
							"运行时准备调用工具: session_id=%s agent=%s tool=%s",
							sessionID,
							currentAgent,
							toolName,
						)

						s.sendSessionMessage(conn, Message{
							Type:             "agent_workflow",
							SessionID:        sessionID,
							Agent:            currentAgent,
							AgentDisplayName: friendlyName,
							Phase:            "tool_call",
							Content:          fmt.Sprintf("调用工具: %s", toolName),
							Data: withModePhaseData(map[string]interface{}{
								"tool":      toolName,
								"call_id":   toolCall.ID,
								"tool_args": toolArgs,
								"status":    "running",
							}, options.modePhase),
						})

						if toolName == "write_todos" {
							logger.Info("拦截到 write_todos 工具调用请求")
							s.parseTodosFromArguments(toolCall.Function.Arguments, sessionID)
						}
					}
				}

				if mv.Role == schema.Tool && mv.ToolName != "" {
					toolActivitySeen = true
					currentAgent := s.getSessionAgent(conn, sessionID)
					if currentAgent == "" {
						currentAgent = "orchestrator"
					}
					friendlyName := getAgentFriendlyName(currentAgent)

					assessment := determineToolStatus(mv.ToolName, lastContent)
					if assessment.Status == "completed" {
						if fileStore != nil {
							registerToolArtifacts(fileStore, artifactDefinitions[mv.ToolName], mv.ToolName, currentAgent, mv.Message, options)
						}
						summary := lastContent
						if len(summary) > 2048 {
							summary = summary[:2048]
						}
						result.SuccessfulTools = append(result.SuccessfulTools, flowrun.ToolSummary{Name: mv.ToolName, Result: summary})
					}
					statusText := "执行完成（状态未判定）"
					switch assessment.Status {
					case "completed":
						statusText = "执行成功"
					case "failed":
						statusText = "执行失败"
					}

					logger.Debug(
						"工具状态判定: tool=%s status=%s source=%s",
						mv.ToolName, assessment.Status, assessment.Source,
					)
					logger.Info(
						"运行时收到工具结果: session_id=%s agent=%s tool=%s status=%s",
						sessionID,
						currentAgent,
						mv.ToolName,
						assessment.Status,
					)

					toolData := map[string]interface{}{
						"tool":          mv.ToolName,
						"call_id":       mv.Message.ToolCallID,
						"status":        assessment.Status,
						"result":        lastContent,
						"status_source": assessment.Source,
					}
					if parts := messageInputPartsPayload(mv.Message.UserInputMultiContent); len(parts) > 0 {
						toolData["result_parts"] = parts
					}
					if assessment.Success != nil {
						toolData["success"] = *assessment.Success
					}
					if payload, ok := parseToolPayload(lastContent); ok {
						if code, retryable, diagnostics := extractToolPayloadErrorMetadata(payload); code != "" || retryable != nil || len(diagnostics) > 0 {
							if code != "" {
								toolData["error_code"] = code
							}
							if retryable != nil {
								toolData["retryable"] = *retryable
							}
							if len(diagnostics) > 0 {
								toolData["provider_diagnostics"] = diagnostics
							}
						}
					}

					s.sendSessionMessage(conn, Message{
						Type:             "agent_workflow",
						SessionID:        sessionID,
						Agent:            currentAgent,
						AgentDisplayName: friendlyName,
						Phase:            "tool_result",
						Content:          fmt.Sprintf("工具 %s %s", mv.ToolName, statusText),
						Data:             withModePhaseData(toolData, options.modePhase),
					})
				}
			}
		}

		// 处理动作事件
		if event.Action != nil {
			// 处理 Interrupt 事件
			if event.Action.Interrupted != nil {
				interruptContexts := event.Action.Interrupted.InterruptContexts

				// 构建中断信息
				contexts := make([]map[string]interface{}, 0, len(interruptContexts))
				for _, ic := range interruptContexts {
					ictx := map[string]interface{}{
						"id":            ic.ID,
						"info":          ic.Info,
						"is_root_cause": ic.IsRootCause,
					}
					// 构建地址链
					addrParts := make([]string, 0, len(ic.Address))
					for _, seg := range ic.Address {
						addrParts = append(addrParts, fmt.Sprintf("%s:%s", seg.Type, seg.ID))
					}
					ictx["address"] = strings.Join(addrParts, " → ")
					contexts = append(contexts, ictx)
				}

				// Register/persist before notification; an immediate confirmation must
				// not race an as-yet unregistered pending request.
				pending := &PendingInterrupt{
					FlowOptions:     options,
					CheckpointID:    checkpointID,
					Runner:          runner,
					Contexts:        interruptContexts,
					SessionID:       sessionID,
					ProjectPath:     projectPath,
					BoundInstanceID: instanceIDFromContext(ctx),
					Session:         sess,
					ModePhase:       strings.TrimSpace(options.modePhase),
				}
				interruptData := withModePhaseData(map[string]any{
					"checkpoint_id": checkpointID, "interrupt_contexts": contexts,
				}, options.modePhase)
				pending.InterruptData = copyMessageData(interruptData)
				s.knowledgeConfirmMu.Lock()
				if ctx.Err() != nil {
					s.knowledgeConfirmMu.Unlock()
					result.Err = ctx.Err()
					return result
				}
				if err := s.persistFlowInterrupt(ctx, pending, interruptData); err != nil {
					s.knowledgeConfirmMu.Unlock()
					result.Err = err
					return result
				}
				if err := s.persistKnowledgeReviewInterrupt(pending, interruptData, options.executionMode); err != nil {
					s.knowledgeConfirmMu.Unlock()
					s.sendSessionError(conn, sessionID, fmt.Sprintf("保存核对断点失败: %v", err))
					result.Err = err
					return result
				}
				s.setPendingInterrupt(conn, sessionID, pending)
				s.markSessionRuntimeInterrupted(sessionID, checkpointID)
				s.persistManagedSessionState(sessionID, projectPath, options.executionMode)
				s.sendSessionMessage(conn, Message{
					Type: MsgTypeInterruptRequest, SessionID: sessionID,
					Content: "任务需要您的确认才能继续", Data: interruptData,
				})
				s.knowledgeConfirmMu.Unlock()

				logger.Info("任务已中断，等待用户确认: checkpoint=%s, contexts=%d", checkpointID, len(interruptContexts))
				result.LastContent = lastContent
				result.Interrupted = true
				return result // 退出事件循环，等待用户响应
			}

			if event.Action.TransferToAgent != nil {
				destAgent := event.Action.TransferToAgent.DestAgentName

				s.setSessionAgent(conn, sessionID, destAgent)

				lastAgentName = destAgent
				friendlyName := getAgentFriendlyName(destAgent)

				s.sendSessionMessage(conn, Message{
					Type:             "agent_workflow",
					SessionID:        sessionID,
					Agent:            destAgent,
					AgentDisplayName: friendlyName,
					Phase:            "thinking",
					Content:          "开始处理任务...",
					Data:             withModePhaseData(nil, options.modePhase),
				})
			}
		}

		// 处理错误
		if event.Err != nil {
			message, code, failureReason, details := s.resolveSessionRuntimeFailure(sessionID, event.Err)
			runtimeState, _ := s.activeSessionRuntime(sessionID)
			logger.Error("处理事件时发生错误，提前返回: %v", event.Err)
			s.sendSessionErrorWithDetails(conn, sessionID, message, code, details)
			if options.flowRunID == "" {
				s.markSessionRuntimeFailed(sessionID, message, code, failureReason)
				s.persistSessionRuntimeFailure(sessionID, projectPath, runtimeState.BoundInstanceID, options.executionMode, message, code, failureReason)
			}
			result.LastContent = lastContent
			result.Err = event.Err
			return result
		}
	}

	if ctx.Err() != nil {
		result.LastContent = lastContent
		result.Err = ctx.Err()
		return result
	}

	if override, ok := s.consumeSessionFailureOverride(sessionID); ok {
		message := strings.TrimSpace(override.Reason)
		if message == "" {
			message = "运行失败"
		}
		code := strings.TrimSpace(override.Code)
		runtimeState, _ := s.activeSessionRuntime(sessionID)
		s.sendSessionErrorWithCode(conn, sessionID, message, code)
		if options.flowRunID == "" {
			s.markSessionRuntimeFailed(sessionID, message, code, message)
			s.persistSessionRuntimeFailure(sessionID, projectPath, runtimeState.BoundInstanceID, options.executionMode, message, code, message)
		}
		result.LastContent = lastContent
		result.Err = errors.New(message)
		return result
	}

	if options.flowRunID != "" {
		lastContent = lastAssistant
	}
	logger.Info("事件循环结束，准备保存会话历史...")
	if options.persistAssistant && lastContent != "" {
		assistantMsg := schema.AssistantMessage(lastContent, nil)
		if options.flowRunID != "" {
			assistantMsg.Extra = map[string]any{"flow_run_id": options.flowRunID, "step_id": options.flowStepID}
		}
		sess.AddMessage(assistantMsg)
	}

	if options.emitFinalResponse && options.flowRunID == "" && !s.hasPendingFlow(sessionID) {
		s.sendSessionMessage(conn, Message{
			Type:      MsgTypeResponse,
			SessionID: sessionID,
			Content:   lastContent,
			Data:      withModePhaseData(nil, options.modePhase),
		})
	}

	if options.flowRunID == "" && !s.hasPendingFlow(sessionID) {
		s.markSessionRuntimeFinished(sessionID, sessionRuntimeStatusCompleted, "")
	}
	s.persistManagedSessionState(sessionID, projectPath, options.executionMode)
	if options.flowRunID == "" {
		s.maybeAutoCompressSessionContext(ctx, conn, sessionID, projectPath, sess, options.executionMode)
	}
	result.LastContent = lastContent
	return result
}

func (s *DeepServer) emitAssistantWorkflowEvents(
	conn *websocket.Conn,
	sessionID string,
	msg *schema.Message,
	modePhase string,
	toolActivitySeen bool,
	isStreaming bool,
	emitted map[string]struct{},
) {
	if s == nil || msg == nil {
		return
	}
	// Explicit application progress is not reasoning or an assistant answer.
	// Tool activity must not hide long-running subagent requests and retries.
	if isRuntimeProgressMessage(msg) {
		data := cloneMap(msg.Extra[knowledge.ProgressExtraKey].(map[string]any))
		s.sendAgentWorkflowPhase(conn, sessionID, "progress", strings.TrimSpace(msg.Content), withModePhaseData(data, modePhase))
		return
	}
	reasoningTexts := collectReasoningTexts(msg)
	for _, text := range reasoningTexts {
		if isStreaming && len([]rune(text)) < 12 {
			continue
		}
		if !markWorkflowText(emitted, "reasoning", text) {
			continue
		}
		s.sendAgentWorkflowPhase(conn, sessionID, "thinking", text, withModePhaseData(map[string]any{
			"thinking_kind": "reasoning",
		}, modePhase))
	}
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		return
	}
	if isStreaming && len([]rune(content)) < 12 {
		return
	}
	phase := "thinking"
	kind := "analysis"
	if len(msg.ToolCalls) > 0 {
		phase = "decision"
		kind = "decision"
	} else if toolActivitySeen {
		return
	}
	if !markWorkflowText(emitted, kind, content) {
		return
	}
	s.sendAgentWorkflowPhase(conn, sessionID, phase, content, withModePhaseData(map[string]any{
		"thinking_kind": kind,
	}, modePhase))
}

func isRuntimeProgressMessage(msg *schema.Message) bool {
	if msg == nil || strings.TrimSpace(msg.Content) == "" {
		return false
	}
	data, ok := msg.Extra[knowledge.ProgressExtraKey].(map[string]any)
	return ok && strings.TrimSpace(stringValue(data["stage"])) != ""
}

func markWorkflowText(emitted map[string]struct{}, kind, content string) bool {
	if emitted == nil {
		return true
	}
	key := strings.TrimSpace(kind) + "::" + strings.TrimSpace(content)
	if key == "::" {
		return false
	}
	if _, exists := emitted[key]; exists {
		return false
	}
	emitted[key] = struct{}{}
	return true
}

func collectReasoningTexts(msg *schema.Message) []string {
	if msg == nil {
		return nil
	}
	seen := make(map[string]struct{})
	result := make([]string, 0, 2)
	appendUnique := func(text string) {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	appendUnique(msg.ReasoningContent)
	for _, part := range msg.AssistantGenMultiContent {
		if part.Type == schema.ChatMessagePartTypeReasoning && part.Reasoning != nil {
			appendUnique(part.Reasoning.Text)
		}
	}
	return result
}

func (s *DeepServer) sendAgentWorkflowPhase(conn *websocket.Conn, sessionID, phase, content string, data map[string]any) {
	currentAgent := s.getSessionAgent(conn, sessionID)
	if currentAgent == "" {
		currentAgent = "orchestrator"
	}
	friendlyName := getAgentFriendlyName(currentAgent)
	s.sendSessionMessage(conn, Message{
		Type:             MsgTypeAgentWorkflow,
		SessionID:        sessionID,
		Agent:            currentAgent,
		AgentDisplayName: friendlyName,
		Phase:            phase,
		Content:          content,
		Data:             data,
	})
}

func (s *DeepServer) handleInterruptResponse(conn *websocket.Conn, msg Message) {
	sessionID := normalizeSessionID(conn, msg.SessionID)
	if pending, ok := s.getPendingInterrupt(sessionID); ok {
		if rid, _ := knowledgeReviewRef(pending.Contexts); rid != "" {
			if approved, _ := msg.Data["approved"].(bool); approved {
				var target struct {
					ReviewID  string `json:"review_id"`
					Revision  int64  `json:"revision"`
					RequestID string `json:"request_id"`
				}
				raw, _ := json.Marshal(msg.Data["user_data"])
				if json.Unmarshal(raw, &target) != nil || target.ReviewID != rid {
					s.sendSessionError(conn, sessionID, "请通过知识核对卡片确认已保存的版本")
					return
				}
				if _, err := s.ConfirmKnowledgeReview(context.Background(), sessionID, rid, target.Revision, target.RequestID); err != nil {
					s.sendSessionError(conn, sessionID, err.Error())
				}
				return
			}
		}
	}
	pending, exists := s.popPendingInterrupt(conn, sessionID)

	if !exists {
		s.sendSessionError(conn, sessionID, "没有待确认的中断请求")
		return
	}

	data := msg.Data
	approved, _ := data["approved"].(bool)

	if !approved {
		if pending.FlowOptions.flowRunID != "" {
			_, controller := s.flowDependencies()
			if controller != nil {
				_, _ = controller.Cancel(context.Background(), pending.SessionID)
			}
			_ = s.checkpointStore.Delete(context.Background(), pending.CheckpointID)
			return
		}
		s.sendSessionMessage(conn, Message{
			Type:      MsgTypeResponse,
			SessionID: pending.SessionID,
			Content:   "任务已取消。",
			Data:      withModePhaseData(nil, pending.ModePhase),
		})
		s.sendTaskRunningChanged(conn, pending.SessionID, false)
		s.markSessionRuntimeFinished(pending.SessionID, sessionRuntimeStatusCancelled, "")
		s.persistManagedSessionState(pending.SessionID, pending.ProjectPath, executionModeBuild)
		// 清理 checkpoint
		if err := s.checkpointStore.Delete(context.Background(), pending.CheckpointID); err != nil {
			logger.Warn("删除 checkpoint 失败: checkpoint=%s err=%v", pending.CheckpointID, err)
		}
		return
	}

	// 用户确认，构建 ResumeParams
	targets := make(map[string]any)
	userData := data["user_data"]

	for _, ic := range pending.Contexts {
		if ic.IsRootCause {
			if userData != nil {
				targets[ic.ID] = userData
			} else {
				targets[ic.ID] = nil
			}
		}
	}

	// 创建新的可取消 context
	ctx, cancel := context.WithCancel(context.Background())

	// 异步恢复执行
	if !s.startSessionTask(ctx, conn, pending.SessionID, cancel, func() {
		s.markSessionRuntimeResumed(pending.SessionID)
		s.resumeTask(ctx, conn, pending, targets)
	}) {
		s.sendSessionError(conn, pending.SessionID, "会话已删除，不能恢复执行")
	}
}

func (s *DeepServer) resumeTask(ctx context.Context, conn *websocket.Conn, pending *PendingInterrupt, targets map[string]any) {
	snapshot, checkErr := s.checkFlowPendingResources(ctx, pending, "", "")
	if checkErr != nil {
		// Keep the exact ADK checkpoint and user's pending step available while a
		// resource decision is shown; no part of the old agent is resumed yet.
		s.setPendingInterrupt(conn, pending.SessionID, pending)
		s.markSessionRuntimeInterrupted(pending.SessionID, pending.CheckpointID)
		s.sendTaskRunningChanged(conn, pending.SessionID, false)
		if !resourceChangePending(checkErr) {
			s.sendSessionError(conn, pending.SessionID, checkErr.Error())
		}
		return
	}
	ctx = withRunResourceSnapshot(ctx, snapshot)
	keepRunningState := false
	s.sendTaskRunningChanged(conn, pending.SessionID, true)

	defer func() {
		s.knowledgeConfirmMu.Lock()
		defer s.knowledgeConfirmMu.Unlock()
		if state, active := s.activeSessionRuntime(pending.SessionID); active && state.RunID != pending.CheckpointID {
			return
		}
		if ctx.Err() != nil {
			keepRunningState = false
			s.markSessionRuntimeCancelled(pending.SessionID, pending.CheckpointID)
		}
		if !keepRunningState {
			s.sendTaskRunningChanged(conn, pending.SessionID, false)
		}
	}()

	logger.Info("恢复任务执行: checkpoint=%s, targets=%d", pending.CheckpointID, len(targets))
	resumeCtx := withToolSessionContext(ctx, pending.SessionID, pending.ProjectPath, pending.BoundInstanceID)
	resumeCtx = s.withGEEProgress(resumeCtx, conn, pending.SessionID, pending.ModePhase)
	resumeCtx = s.withOSMProgress(resumeCtx, conn, pending.SessionID, pending.ModePhase)
	resumeCtx = deepagent.WithKnowledgeReview(resumeCtx, s.KnowledgeReviewStore())
	resumeCtx = s.withFlowService(resumeCtx, pending.SessionID)
	if pending.FlowOptions.originalSource != nil {
		resumeCtx = knowledge.WithOriginalSource(resumeCtx, *pending.FlowOptions.originalSource)
	}

	iter, err := pending.Runner.ResumeWithParams(resumeCtx, pending.CheckpointID, &adk.ResumeParams{
		Targets: targets,
	})
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		logger.Error("恢复执行失败: %v", err)
		if s.failKnowledgeReviewResume(pending, err) {
			keepRunningState = true
		}
		var staleErr *runtimepersist.ErrCheckpointRevisionMismatch
		if errors.As(err, &staleErr) {
			s.sendSessionErrorWithCode(
				conn,
				pending.SessionID,
				"恢复失败：当前智能体配置已变更，旧 checkpoint 已失效，请重新发起任务。",
				ErrorCodeCheckpointStale,
			)
			return
		}
		s.sendSessionError(conn, pending.SessionID, fmt.Sprintf("恢复执行失败: %v", err))
		return
	}

	// 复用事件消费逻辑（支持多次中断场景）
	options := pending.FlowOptions
	options.emitFinalResponse = options.flowRunID == ""
	options.persistAssistant = true
	options.modePhase = strings.TrimSpace(pending.ModePhase)
	resumeResult := s.consumeEvents(resumeCtx, conn, iter, pending.Runner, pending.CheckpointID, pending.SessionID, pending.ProjectPath, pending.Session, options)
	s.finishFlowResume(resumeCtx, pending, resumeResult)
	if pending.FlowOptions.flowRunID == "" && resumeResult.Err == nil && !resumeResult.Interrupted && s.hasPendingFlow(pending.SessionID) {
		state, err := s.startPendingFlow(resumeCtx, pending.SessionID)
		if err != nil {
			s.sendSessionError(conn, pending.SessionID, err.Error())
		}
		keepRunningState = state != nil && !flowrun.IsTerminal(state.Status)
	}

	if resumeResult.Interrupted {
		keepRunningState = true
	}
}

func (s *DeepServer) sendStatus(conn *websocket.Conn, sessionID, status, content string) {
	s.sendSessionMessage(conn, Message{
		Type:      MsgTypeStatus,
		SessionID: sessionID,
		Content:   content,
		Data: map[string]interface{}{
			"status": status,
		},
	})
}

func (s *DeepServer) sendTaskRunningChanged(conn *websocket.Conn, sessionID string, running bool) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	s.sendSessionMessage(conn, Message{
		Type:      MsgTypeTaskRunningChange,
		SessionID: sessionID,
		Data: map[string]interface{}{
			"running": running,
		},
	})
}

func (s *DeepServer) sendModeState(conn *websocket.Conn, sessionID string, mode executionMode, phase string) {
	s.sendSessionMessage(conn, Message{
		Type:      MsgTypeModeState,
		SessionID: sessionID,
		Data: map[string]interface{}{
			"mode":  string(mode),
			"phase": strings.TrimSpace(phase),
		},
	})
}

func (s *DeepServer) sendPhaseTransition(conn *websocket.Conn, sessionID, from, to, planSummary string) {
	s.sendSessionMessage(conn, Message{
		Type:      MsgTypePhaseTransition,
		SessionID: sessionID,
		Content:   "计划完成，开始执行...",
		Data: map[string]interface{}{
			"from":         strings.TrimSpace(from),
			"to":           strings.TrimSpace(to),
			"plan_summary": strings.TrimSpace(planSummary),
		},
	})
}

func (s *DeepServer) sendSessionError(conn *websocket.Conn, sessionID, errMsg string) {
	s.sendSessionErrorWithCode(conn, sessionID, errMsg, "")
}

func (s *DeepServer) sendSessionErrorWithCode(conn *websocket.Conn, sessionID, errMsg, code string) {
	s.sendSessionErrorWithDetails(conn, sessionID, errMsg, code, toolErrorDetails{})
}

func (s *DeepServer) sendSessionErrorWithDetails(conn *websocket.Conn, sessionID, errMsg, code string, details toolErrorDetails) {
	var data map[string]interface{}
	if strings.TrimSpace(code) != "" || details.Retryable != nil || len(details.ProviderDiagnostics) > 0 {
		data = map[string]interface{}{}
		if strings.TrimSpace(code) != "" {
			data["code"] = strings.TrimSpace(code)
			data["error_code"] = strings.TrimSpace(code)
		}
		if details.Retryable != nil {
			data["retryable"] = *details.Retryable
		}
		if len(details.ProviderDiagnostics) > 0 {
			data["provider_diagnostics"] = details.ProviderDiagnostics
		}
	}
	s.sendSessionMessage(conn, Message{
		Type:      "error",
		SessionID: sessionID,
		Content:   errMsg,
		Data:      data,
	})
}

func (s *DeepServer) sendSessionWarning(conn *websocket.Conn, sessionID, warning string) {
	s.sendSessionMessage(conn, Message{
		Type:      MsgTypeStatus,
		SessionID: sessionID,
		Content:   warning,
		Data: map[string]interface{}{
			"status": "warning",
		},
	})
}

func (s *DeepServer) getModeAgentWithWarning(conn *websocket.Conn, sessionID string, mode executionMode) adk.Agent {
	modeLabel := strings.TrimSpace(string(mode))
	if modeLabel == "" {
		modeLabel = "build"
	}
	modeLabel = strings.ToUpper(modeLabel[:1]) + strings.ToLower(modeLabel[1:])

	if issue, ok := s.runtimeModeIssue(mode); ok {
		code := strings.TrimSpace(issue.Code)
		if code == "" {
			code = ErrorCodePlanProtocolViolation
		}
		message := strings.TrimSpace(issue.Message)
		if message == "" {
			message = fmt.Sprintf("%s 模式当前不可用。", modeLabel)
		}
		s.failSessionBeforeAgentRun(conn, sessionID, mode, message, code)
		return nil
	}

	agent, fallback := s.getAgentWithFallback(mode)
	if agent == nil {
		if message := s.runtimeUnavailableMessage(modeLabel); message != "" {
			s.failSessionBeforeAgentRun(conn, sessionID, mode, message, ErrorCodeAgentUnavailable)
			return nil
		}
		s.failSessionBeforeAgentRun(conn, sessionID, mode, fmt.Sprintf("%s 模式不可用：智能体未初始化。", modeLabel), ErrorCodeAgentUnavailable)
		return nil
	}
	if !fallback {
		return agent
	}
	s.sendSessionWarning(conn, sessionID, fmt.Sprintf("%s 模式已回退到统一运行内核。", strings.ToLower(string(mode))))
	return agent
}

func (s *DeepServer) failSessionBeforeAgentRun(conn *websocket.Conn, sessionID string, mode executionMode, message, code string) {
	state := s.sessionRuntimeProjectionSnapshot(sessionID, nil).RuntimeState
	s.markSessionRuntimeFailed(sessionID, message, code, message)
	s.persistSessionRuntimeFailure(sessionID, state.BoundProjectPath, state.BoundInstanceID, mode, message, code, message)
	s.sendSessionErrorWithCode(conn, sessionID, message, code)
}

func (s *DeepServer) runtimeUnavailableMessage(modeLabel string) string {
	rebuildConfig := s.agentRebuildConfigSnapshot()
	if rebuildConfig == nil || rebuildConfig.ChatModel != nil {
		return ""
	}

	reason := "当前 LLM 未就绪。请在设置中完成模型配置后重试。"
	if base := rebuildConfig.BaseConfig; base != nil {
		if provider := base.GetActiveProvider(); provider == nil {
			reason = "当前未找到可用的 LLM 提供商。请在设置中配置模型后重试。"
		}
	}
	return fmt.Sprintf("%s 模式不可用：%s", modeLabel, reason)
}

func (s *DeepServer) isAutoTitleEnabled() bool {
	if s.configService == nil {
		return true
	}
	cfg, err := s.configService.GetFullConfig()
	if err != nil || cfg == nil {
		return true
	}
	if cfg.Session.AutoTitleEnabled == nil {
		return true
	}
	return *cfg.Session.AutoTitleEnabled
}

func (s *DeepServer) getActiveInstanceProjectPath() string {
	snapshot := s.GetRuntimeSnapshot()
	activeID := strings.TrimSpace(snapshot.Instances.ActiveID)
	if activeID == "" {
		return ""
	}
	for _, instance := range snapshot.Instances.Items {
		if instance == nil || strings.TrimSpace(instance.ID) != activeID {
			continue
		}
		return strings.TrimSpace(instance.ProjectPath)
	}
	return ""
}

func resolveAutoTitleProjectCandidates(paths ...string) []string {
	seen := make(map[string]bool, len(paths)+1)
	result := make([]string, 0, len(paths)+1)
	for _, raw := range paths {
		candidate := strings.TrimSpace(raw)
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		result = append(result, candidate)
	}
	if !seen[""] {
		result = append(result, "")
	}
	return result
}

func shouldAutoTitleSessionName(sessionData map[string]any) bool {
	if autoGenerated, ok := sessionData["name_auto_generated"].(bool); ok {
		return autoGenerated
	}
	name, _ := sessionData["name"].(string)
	return isLikelyDefaultSessionName(name)
}

func isLikelyDefaultSessionName(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(trimmed, "会话 ") || strings.HasPrefix(lower, "session ")
}

func (s *DeepServer) tryAutoTitleSession(sessionID, hintedProjectPath, titleSeed string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || !s.isAutoTitleEnabled() {
		return
	}

	result, err := s.generateAutoSessionTitle(titleSeed)
	title := ""
	titleSource := sessionmeta.TitleSourceLLM
	if err != nil {
		logger.Warn("自动标题生成失败，尝试规则降级: session_id=%s, err=%v", sessionID, err)
		title = generateFallbackAutoSessionTitle(titleSeed)
		titleSource = sessionmeta.TitleSourceFallback
		if strings.TrimSpace(title) == "" {
			logger.Warn("自动标题降级失败，保持默认标题: session_id=%s", sessionID)
			return
		}
		logger.Info("自动标题使用规则降级: session_id=%s, title=%s", sessionID, title)
	} else if result != nil {
		title = strings.TrimSpace(result.Title)
		titleSource = sessionmeta.NormalizeTitleSource(result.Source, sessionmeta.TitleSourceLLM)
		logger.Info("自动标题生成成功: session_id=%s, provider=%s, title=%s", sessionID, strings.TrimSpace(result.ProviderName), title)
	}
	if strings.TrimSpace(title) == "" {
		logger.Warn("自动标题为空，保持默认标题: session_id=%s", sessionID)
		return
	}

	candidates := resolveAutoTitleProjectCandidates(hintedProjectPath, s.getActiveInstanceProjectPath())
	var lastErr error
	for _, projectPath := range candidates {
		renamed := false
		_, err := s.SaveSessionAtomically(sessionID, projectPath, func(sessionData map[string]any) error {
			if !shouldAutoTitleSessionName(sessionData) {
				return errSkipAutoSessionTitle
			}
			currentName, _ := sessionData["name"].(string)
			if strings.TrimSpace(currentName) == title {
				return errSkipAutoSessionTitle
			}
			sessionData["name"] = title
			sessionData["name_auto_generated"] = false
			sessionData["title_source"] = titleSource
			sessionData["updated_at"] = time.Now().Format(time.RFC3339)
			renamed = true
			return nil
		})
		if err == nil {
			if renamed {
				s.realtimeService().BroadcastSessionChange("updated", sessionID, nil)
				logger.Info("自动更新会话标题: session_id=%s, title=%s", sessionID, title)
			}
			return
		}
		if errors.Is(err, errSkipAutoSessionTitle) {
			return
		}
		lastErr = err
	}

	if lastErr != nil {
		logger.Warn("自动标题更新失败: session_id=%s, err=%v", sessionID, lastErr)
	}
}

func (s *DeepServer) sendAgentStatus(conn *websocket.Conn, agentName string, runPath []adk.RunStep) {
	// 构建运行路径字符串
	var pathParts []string
	for _, step := range runPath {
		pathParts = append(pathParts, step.String())
	}
	path := strings.Join(pathParts, " → ")

	// 获取智能体友好名称
	friendlyName := getAgentFriendlyName(agentName)

	s.sendSessionMessage(conn, Message{
		Type:    MsgTypeAgent,
		Content: fmt.Sprintf("%s 正在处理...", friendlyName),
		Data: map[string]interface{}{
			"agent": agentName,
			"path":  path,
		},
	})
}

func (s *DeepServer) sendToolResult(conn *websocket.Conn, toolName, result string) {
	// 截断过长的结果
	displayResult := result
	if len(displayResult) > 200 {
		displayResult = displayResult[:200] + "..."
	}

	s.sendSessionMessage(conn, Message{
		Type:    MsgTypeTool,
		Content: fmt.Sprintf("工具 %s 执行完成", toolName),
		Data: map[string]interface{}{
			"tool":   toolName,
			"result": displayResult,
		},
	})
}

type toolStatusAssessment struct {
	Status  string
	Success *bool
	Source  string
}

func determineToolStatus(toolName string, content string) toolStatusAssessment {
	if payload, ok := parseToolPayload(content); ok {
		if assessment, matched := determineToolStatusFromPayload(payload, "payload"); matched {
			return assessment
		}
	}

	lower := strings.ToLower(strings.TrimSpace(content))
	if toolName == "skill" {
		if strings.HasPrefix(lower, "正在启动 skill") ||
			strings.HasPrefix(lower, "launching skill") {
			return buildToolStatusAssessment("completed", true, "skill_prefix")
		}
		if hasSkillFailurePrefix(lower) || hasStrongFailurePrefix(lower) {
			return buildToolStatusAssessment("failed", false, "skill_prefix")
		}
		return toolStatusAssessment{Status: "unknown", Source: "unknown"}
	}

	if hasStrongFailurePrefix(lower) {
		return buildToolStatusAssessment("failed", false, "text_fallback")
	}
	if hasStrongSuccessPrefix(lower) {
		return buildToolStatusAssessment("completed", true, "text_fallback")
	}

	return toolStatusAssessment{Status: "unknown", Source: "unknown"}
}

func parseToolPayload(content string) (map[string]interface{}, bool) {
	content = strings.TrimSpace(content)
	if content == "" || !strings.HasPrefix(content, "{") {
		return nil, false
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return nil, false
	}
	if len(payload) == 0 {
		return nil, false
	}
	return payload, true
}

func determineToolStatusFromPayload(payload map[string]interface{}, sourcePrefix string) (toolStatusAssessment, bool) {
	if payload == nil {
		return toolStatusAssessment{}, false
	}

	if payloadSuccess, ok := payload["success"].(bool); ok {
		if payloadSuccess {
			return buildToolStatusAssessment("completed", payloadSuccess, sourcePrefix+"_success"), true
		}
		return buildToolStatusAssessment("failed", payloadSuccess, sourcePrefix+"_success"), true
	}

	if status, ok := payload["status"].(string); ok {
		if normalized, successPtr, matched := normalizeStructuredToolStatus(status); matched {
			return toolStatusAssessment{Status: normalized, Success: successPtr, Source: sourcePrefix + "_status"}, true
		}
	}

	for _, key := range []string{"error", "err", "reason", "unavailable_reason"} {
		if text := strings.TrimSpace(toString(payload[key])); text != "" {
			return buildToolStatusAssessment("failed", false, sourcePrefix+"_error"), true
		}
	}

	for _, key := range []string{"result", "output", "info", "data"} {
		nested, ok := payload[key].(map[string]interface{})
		if !ok {
			continue
		}
		if assessment, matched := determineToolStatusFromPayload(nested, sourcePrefix+"_"+key); matched {
			return assessment, true
		}
	}

	for _, key := range []string{"output", "info", "message"} {
		text := strings.ToLower(strings.TrimSpace(toString(payload[key])))
		if text == "" {
			continue
		}
		if hasStrongFailurePrefix(text) {
			return buildToolStatusAssessment("failed", false, sourcePrefix+"_text"), true
		}
		if hasStrongSuccessPrefix(text) {
			return buildToolStatusAssessment("completed", true, sourcePrefix+"_text"), true
		}
	}

	return toolStatusAssessment{}, false
}

func extractToolPayloadErrorMetadata(payload map[string]interface{}) (string, *bool, map[string]any) {
	if payload == nil {
		return "", nil, nil
	}
	code := strings.TrimSpace(toString(firstNonNil(payload["error_code"], payload["code"])))
	var retryable *bool
	if value, ok := payload["retryable"].(bool); ok {
		retryable = &value
	}
	diagnostics := cloneToolPayloadDiagnostics(payload["provider_diagnostics"])
	if code != "" || retryable != nil || len(diagnostics) > 0 {
		return code, retryable, diagnostics
	}
	for _, key := range []string{"result", "output", "data"} {
		nested, ok := payload[key].(map[string]interface{})
		if !ok {
			continue
		}
		if nestedCode, nestedRetryable, nestedDiagnostics := extractToolPayloadErrorMetadata(nested); nestedCode != "" || nestedRetryable != nil || len(nestedDiagnostics) > 0 {
			return nestedCode, nestedRetryable, nestedDiagnostics
		}
	}
	return "", nil, nil
}

func cloneToolPayloadDiagnostics(raw any) map[string]any {
	values, ok := raw.(map[string]interface{})
	if !ok || len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func normalizeStructuredToolStatus(raw string) (string, *bool, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "", nil, false
	}

	completed := []string{"completed", "done", "success", "succeeded", "ok", "available", "resolved", "approved", "finished"}
	for _, candidate := range completed {
		if value == candidate {
			success := true
			return "completed", &success, true
		}
	}

	failed := []string{"failed", "error", "unavailable", "cancelled", "canceled", "aborted", "timeout", "timed_out", "invalid", "missing", "rejected", "denied"}
	for _, candidate := range failed {
		if value == candidate {
			success := false
			return "failed", &success, true
		}
	}

	running := []string{"running", "active", "in_progress", "processing", "pending", "queued", "starting"}
	for _, candidate := range running {
		if value == candidate {
			return "running", nil, true
		}
	}

	return "", nil, false
}

func buildToolStatusAssessment(status string, success bool, source string) toolStatusAssessment {
	successCopy := success
	return toolStatusAssessment{
		Status:  status,
		Success: &successCopy,
		Source:  source,
	}
}

func extractToolPayloadSuccess(content string) (bool, bool) {
	content = strings.TrimSpace(content)
	if content == "" || !strings.HasPrefix(content, "{") {
		return false, false
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return false, false
	}

	success, ok := payload["success"].(bool)
	return success, ok
}

func hasStrongFailurePrefix(lower string) bool {
	if lower == "" {
		return false
	}

	strongPrefixes := []string{
		"error:",
		"error：",
		"错误:",
		"错误：",
		"执行失败",
		"调用失败",
		"panic",
		"panic:",
		"panic：",
	}
	for _, prefix := range strongPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func hasStrongSuccessPrefix(lower string) bool {
	if lower == "" {
		return false
	}

	strongPrefixes := []string{
		"success:",
		"success：",
		"成功:",
		"成功：",
		"执行成功",
		"调用成功",
		"处理完成",
		"已完成",
		"完成：",
		"完成:",
	}
	for _, prefix := range strongPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func toString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func hasSkillFailurePrefix(lower string) bool {
	if lower == "" {
		return false
	}

	return strings.HasPrefix(lower, "启动 skill 失败") ||
		strings.HasPrefix(lower, "failed to launch skill")
}

func getAgentFriendlyName(name string) string {
	// 主智能体的特殊处理
	if name == "orchestrator" {
		return "主智能体"
	}
	// 其他智能体直接返回名称（由配置文件中的 display_name 决定）
	return name
}

func (s *DeepServer) BroadcastTaskListUpdate(todos interface{}, sessionID ...string) error {
	taskSessionID := ""
	if len(sessionID) > 0 {
		taskSessionID = strings.TrimSpace(sessionID[0])
	}

	// 构建任务列表更新消息
	msg := Message{
		Type:      "task_list_update",
		SessionID: taskSessionID,
		Content:   "任务列表已更新",
		Data: map[string]interface{}{
			"todos": todos,
		},
	}
	if taskSessionID != "" {
		return s.sendSessionMessage(nil, msg)
	}

	// 序列化消息
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("序列化任务列表消息失败: %w", err)
	}

	s.mu.Lock()
	clients := make([]*websocket.Conn, 0, len(s.clients))
	for conn := range s.clients {
		clients = append(clients, conn)
	}
	s.mu.Unlock()

	// 广播到所有客户端
	for _, conn := range clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			logger.Error("发送任务列表到客户端失败: %v", err)
		}
	}

	return nil
}

func (s *DeepServer) sendToolStart(conn *websocket.Conn, toolName string) {
	s.sendSessionMessage(conn, Message{
		Type:    MsgTypeTool,
		Content: fmt.Sprintf("正在执行 %s...", toolName),
		Data: map[string]interface{}{
			"tool":   toolName,
			"status": "running",
		},
	})
}

func (s *DeepServer) sendToolEnd(conn *websocket.Conn, toolName string, success bool, result string) {
	status := "completed"
	content := fmt.Sprintf("工具 %s 执行完成", toolName)

	if !success {
		status = "failed"
		content = fmt.Sprintf("工具 %s 执行失败", toolName)
	}

	s.sendSessionMessage(conn, Message{
		Type:    MsgTypeTool,
		Content: content,
		Data: map[string]interface{}{
			"tool":   toolName,
			"status": status,
			"result": result,
		},
	})
}

func (s *DeepServer) sendThinking(conn *websocket.Conn, sessionID, content string) {
	// 获取当前智能体
	currentAgent := s.getSessionAgent(conn, sessionID)

	if currentAgent == "" {
		currentAgent = "orchestrator"
	}

	friendlyName := getAgentFriendlyName(currentAgent)

	// 发送 agent_workflow 类型的消息
	s.sendSessionMessage(conn, Message{
		Type:             "agent_workflow",
		SessionID:        sessionID,
		Agent:            currentAgent,
		AgentDisplayName: friendlyName,
		Phase:            "thinking",
		Content:          content,
	})
}

func (s *DeepServer) sendDecision(conn *websocket.Conn, content string) {
	s.sendSessionMessage(conn, Message{
		Type:    "decision",
		Content: content,
	})
}

func (s *DeepServer) parseTodosFromArguments(arguments string, sessionID string) {
	logger.Info("开始解析 write_todos 参数...")
	logger.Info("参数内容: %s", arguments)

	// 解析 JSON 参数
	var params struct {
		Todos []deep.TODO `json:"todos"`
	}

	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		logger.Error("解析 write_todos 参数失败: %v", err)
		logger.Error("原始参数: %s", arguments)
		return
	}

	logger.Info("成功解析参数，共 %d 个任务", len(params.Todos))

	if len(params.Todos) > 0 {
		// 打印每个任务的详细信息
		for i, todo := range params.Todos {
			logger.Info("任务 %d: Content=%s, Status=%s", i+1, todo.Content, todo.Status)
		}

		// 广播任务列表
		logger.Info("准备广播任务列表...")
		if err := s.BroadcastTaskListUpdate(params.Todos, sessionID); err != nil {
			logger.Error("广播任务列表失败: %v", err)
		} else {
			logger.Info("任务列表广播成功")
		}
	} else {
		logger.Info("解析到的任务列表为空，不广播")
	}
}
