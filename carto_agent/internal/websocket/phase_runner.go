package websocket

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/deepagent"
	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/autocarto/carto_agent/internal/session"
)

func (s *DeepServer) runAgentPhase(
	ctx context.Context,
	conn *websocket.Conn,
	sessionID string,
	projectPath string,
	boundInstanceID string,
	sess *session.Session,
	agent adk.Agent,
	input []*schema.Message,
	options consumeEventsOptions,
) consumeEventsResult {
	if agent == nil {
		err := fmt.Errorf("智能体未初始化")
		s.sendSessionErrorWithCode(conn, sessionID, "当前模式智能体未就绪，请检查智能体配置与工具可用性后重试。", ErrorCodeAgentUnavailable)
		return consumeEventsResult{Err: err}
	}

	checkpointID := fmt.Sprintf("task-%s-%d", sessionID, time.Now().UnixNano())
	if strings.TrimSpace(options.runID) != "" {
		checkpointID = strings.TrimSpace(options.runID)
	}
	taskCtx := withToolSessionContext(ctx, sessionID, projectPath, boundInstanceID)
	taskCtx = s.withFlowService(taskCtx, sessionID)
	taskCtx = s.withGEEProgress(taskCtx, conn, sessionID, options.modePhase)
	taskCtx = s.withOSMProgress(taskCtx, conn, sessionID, options.modePhase)
	taskCtx = deepagent.WithKnowledgeReview(taskCtx, s.KnowledgeReviewStore())
	// Preserve source before model compression/delegation without enabling ADK
	// SessionValues' automatic FString formatting of JSON-bearing agent prompts.
	if options.originalSource != nil {
		taskCtx = knowledge.WithOriginalSource(taskCtx, *options.originalSource)
	} else {
		for i := len(input) - 1; i >= 0; i-- {
			if input[i] != nil && input[i].Role == schema.User {
				taskCtx = knowledge.WithOriginalSource(taskCtx, input[i].Content)
				break
			}
		}
	}
	s.markSessionRuntimeRunning(sessionID, checkpointID, options.executionMode, options.modePhase, projectPath, boundInstanceID)
	s.persistManagedSessionState(sessionID, projectPath, options.executionMode)
	runner := adk.NewRunner(taskCtx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: false,
		CheckPointStore: s.checkpointStore,
	})

	iter := runner.Run(taskCtx, input, adk.WithCheckPointID(checkpointID))
	return s.consumeEvents(taskCtx, conn, iter, runner, checkpointID, sessionID, projectPath, sess, options)
}
