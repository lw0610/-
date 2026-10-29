package websocket

import (
	"context"
	"encoding/json"

	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/logger"
)

type messageHandler func(*DeepServer, *websocket.Conn, Message) bool

var messageRouter = map[string]messageHandler{
	MsgTypeConfigGet:      (*DeepServer).handleConfigMessage,
	MsgTypeConfigSet:      (*DeepServer).handleConfigMessage,
	MsgTypeConfigGetAll:   (*DeepServer).handleConfigMessage,
	MsgTypeConfigBatchSet: (*DeepServer).handleConfigMessage,

	MsgTypeUserList:   (*DeepServer).handleUserMessage,
	MsgTypeUserCreate: (*DeepServer).handleUserMessage,
	MsgTypeUserSwitch: (*DeepServer).handleUserMessage,
	MsgTypeUserDelete: (*DeepServer).handleUserMessage,

	MsgTypeAgentConfigList:   (*DeepServer).handleAgentConfigMessage,
	MsgTypeAgentConfigSave:   (*DeepServer).handleAgentConfigMessage,
	MsgTypeAgentConfigSwitch: (*DeepServer).handleAgentConfigMessage,
	MsgTypeAgentConfigDelete: (*DeepServer).handleAgentConfigMessage,
	MsgTypeToolCatalogGet:    (*DeepServer).handleAgentConfigMessage,

	MsgTypeLLMConfigSave:    (*DeepServer).handleLLMConfigMessage,
	MsgTypeModelQuickSwitch: (*DeepServer).handleLLMConfigMessage,
	MsgTypeModeSwitch:       (*DeepServer).handleLLMConfigMessage,
	MsgTypeAgentQuickSwitch: (*DeepServer).handleAgentConfigMessage,

	MsgTypeSessionSave:    (*DeepServer).handleSessionMessage,
	MsgTypeSessionLoad:    (*DeepServer).handleSessionMessage,
	MsgTypeSessionList:    (*DeepServer).handleSessionMessage,
	MsgTypeSessionDelete:  (*DeepServer).handleSessionMessage,
	MsgTypeSessionDirInit: (*DeepServer).handleSessionMessage,
	MsgTypeSessionDirInfo: (*DeepServer).handleSessionMessage,
	MsgTypeSessionAttach:  (*DeepServer).handleSessionMessage,
	MsgTypeSessionDetach:  (*DeepServer).handleSessionMessage,

	MsgTypeContextCompress: (*DeepServer).handleContextMessage,
	MsgTypeContextStats:    (*DeepServer).handleContextMessage,

	MsgTypeSkillList:   (*DeepServer).handleSkillMessage,
	MsgTypeSkillGet:    (*DeepServer).handleSkillMessage,
	MsgTypeSkillSave:   (*DeepServer).handleSkillMessage,
	MsgTypeSkillDelete: (*DeepServer).handleSkillMessage,
	MsgTypeSkillImport: (*DeepServer).handleSkillMessage,

	MsgTypeInstanceList:   (*DeepServer).handleInstanceMessage,
	MsgTypeInstanceSelect: (*DeepServer).handleInstanceMessage,
}

// handleMessage 处理消息路由
func (s *DeepServer) handleMessage(conn *websocket.Conn, message []byte) {
	var msg Message
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Error("解析消息失败: %v", err)
		s.sendError(conn, "消息格式错误")
		return
	}

	// 处理取消请求
	if msg.Type == MsgTypeTaskCancel {
		ack, proceed := s.enforceTaskRequestContract(conn, msg)
		if !proceed {
			return
		}
		sessionID := ack.SessionID
		if sessionID == "" {
			sessionID = normalizeSessionID(conn, msg.SessionID)
		}
		if sessionID != "" {
			s.cancelSessionTask(conn, sessionID)
		} else {
			s.cancelAllTasks(conn)
		}
		s.sendMessage(conn, Message{
			Type:      MsgTypeCancelled,
			SessionID: sessionID,
			Content:   "任务已取消",
		})
		return
	}

	// 处理配置重载请求
	if msg.Type == "reload_config" {
		s.sendMessage(conn, Message{
			Type:    "reload_required",
			Content: "配置已更新，需要重启智能体服务以应用新配置",
			Data: map[string]any{
				"action": "restart_required",
			},
		})
		return
	}

	if s.routeMessage(conn, msg) {
		return
	}

	// 处理 HITL 中断响应
	if msg.Type == MsgTypeInterruptResponse {
		s.handleInterruptResponse(conn, msg)
		return
	}

	if msg.Type == MsgTypeTaskRetry {
		ack, proceed := s.enforceTaskRequestContract(conn, msg)
		if !proceed {
			return
		}
		msg.Type = MsgTypeTaskCreate
		msg.SessionID = ack.SessionID
	}

	sessionID := normalizeSessionID(conn, msg.SessionID)
	if msg.Type == MsgTypeTaskCreate {
		ack, proceed := s.enforceTaskRequestContract(conn, msg)
		if !proceed {
			return
		}
		msg.SessionID = ack.SessionID
		sessionID = ack.SessionID
	}
	if sessionID != "" {
		msg.SessionID = sessionID
		if s.answerPendingClarification(conn, msg) {
			return
		}
		if !s.hasPlanAwaitingRevision(sessionID) {
			s.cancelSessionTask(conn, sessionID)
		}
	}

	// 创建可取消的 context
	ctx, cancel := context.WithCancel(context.Background())

	// 异步处理
	if !s.startSessionTask(ctx, conn, sessionID, cancel, func() { s.processTask(ctx, conn, msg) }) {
		s.sendSessionError(conn, sessionID, "会话已删除，不能继续执行")
	}
}

func (s *DeepServer) routeMessage(conn *websocket.Conn, msg Message) bool {
	handler, ok := messageRouter[msg.Type]
	if !ok {
		return false
	}
	return handler(s, conn, msg)
}
