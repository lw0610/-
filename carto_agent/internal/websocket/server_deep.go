package websocket

import (
	"context"
	"fmt"
	"time"

	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/logger"
)

func (s *DeepServer) handleContextMessage(conn *websocket.Conn, msg Message) bool {
	requestID, _ := msg.Data["request_id"].(string)

	switch msg.Type {
	case MsgTypeContextCompress:
		go s.processContextCompress(conn, msg.SessionID, requestID, msg.Data)
		return true

	case MsgTypeContextStats:
		s.processContextStats(conn, msg.SessionID, requestID, msg.Data)
		return true
	}

	return false
}

// processContextCompress 处理上下文压缩请求。
func (s *DeepServer) processContextCompress(conn *websocket.Conn, sessionID, requestID string, data map[string]any) {
	logger.Info("开始处理上下文压缩请求...")

	messagesRaw, ok := data["messages"].([]any)
	if !ok || len(messagesRaw) == 0 {
		s.sendContextResponse(conn, sessionID, requestID, false, nil, "消息列表为空或格式无效")
		return
	}

	chatModel, err := s.resolveContextCompressionModel()
	if err != nil {
		s.sendContextResponse(conn, sessionID, requestID, false, nil, err.Error())
		return
	}
	providerCfg := s.resolveContextCompressionProviderConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := compressContextMessages(ctx, chatModel, messagesRaw, providerCfg)
	if err != nil {
		logger.Error("上下文压缩失败: %v", err)
		s.sendContextResponse(conn, sessionID, requestID, false, nil, fmt.Sprintf("上下文压缩失败: %v", err))
		return
	}

	logger.Info("上下文压缩完成，原始消息数: %d, 压缩后消息数: %d", result.OriginalCount, result.CompressedCount)
	compressedMessages := decodeSessionMessages(compressedContextToAny(result.CompressedContext), -1)
	stats := buildContextStatsSnapshot(compressedMessages, result.CompressedCount)
	s.sendContextResponse(conn, sessionID, requestID, true, map[string]any{
		"summary":            result.Summary,
		"compressed_context": result.CompressedContext,
		"original_count":     result.OriginalCount,
		"compressed_count":   result.CompressedCount,
		"message_count":      stats.MessageCount,
		"total_chars":        stats.TotalChars,
		"estimated_tokens":   stats.EstimatedTokens,
		"total_tokens":       stats.EstimatedTokens,
	}, "")
}

// processContextStats 处理上下文统计请求。
func (s *DeepServer) processContextStats(conn *websocket.Conn, sessionID, requestID string, data map[string]any) {
	messagesRaw, _ := data["messages"].([]any)

	totalChars := 0
	estimatedTokens := 0
	for _, msg := range messagesRaw {
		if msgMap, ok := msg.(map[string]any); ok {
			role, _ := msgMap["role"].(string)
			if content, ok := msgMap["content"].(string); ok {
				totalChars += len(content)
				estimatedTokens += estimateTextTokens(role + "\n" + content)
			}
		}
	}

	s.sendContextResponse(conn, sessionID, requestID, true, map[string]any{
		"message_count":    len(messagesRaw),
		"total_chars":      totalChars,
		"estimated_tokens": estimatedTokens,
	}, "")
}

// sendContextResponse 发送上下文操作响应。
func (s *DeepServer) sendContextResponse(conn *websocket.Conn, sessionID, requestID string, success bool, data map[string]any, errMsg string) {
	respData := map[string]any{
		"request_id": requestID,
		"success":    success,
		"error":      errMsg,
	}
	if data != nil {
		for k, v := range data {
			respData[k] = v
		}
	}
	s.sendMessage(conn, Message{
		Type:      MsgTypeContextResponse,
		SessionID: sessionID,
		Data:      respData,
	})
}
