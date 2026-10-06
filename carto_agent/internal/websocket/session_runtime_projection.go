package websocket

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/runtimepersist"
)

type workflowToolCard struct {
	ID                  string         `json:"id"`
	Tool                string         `json:"tool"`
	Status              string         `json:"status"`
	Args                map[string]any `json:"args,omitempty"`
	Result              string         `json:"result,omitempty"`
	StatusSource        string         `json:"statusSource,omitempty"`
	Success             *bool          `json:"success,omitempty"`
	ErrorCode           string         `json:"errorCode,omitempty"`
	Retryable           *bool          `json:"retryable,omitempty"`
	ProviderDiagnostics map[string]any `json:"providerDiagnostics,omitempty"`
	StartedAt           int64          `json:"startedAt"`
	FinishedAt          *int64         `json:"finishedAt,omitempty"`
}

type workflowEntry struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Content   string            `json:"content"`
	Phase     string            `json:"phase,omitempty"`
	Timestamp int64             `json:"timestamp"`
	Data      map[string]any    `json:"data,omitempty"`
	ToolCard  *workflowToolCard `json:"toolCard,omitempty"`
}

type workflowBlock struct {
	ID               string          `json:"id"`
	Agent            string          `json:"agent"`
	AgentDisplayName string          `json:"agentDisplayName"`
	Summary          string          `json:"summary"`
	Status           string          `json:"status"`
	StartedAt        int64           `json:"startedAt"`
	UpdatedAt        int64           `json:"updatedAt"`
	FinishedAt       *int64          `json:"finishedAt,omitempty"`
	Entries          []workflowEntry `json:"entries"`
}

type sessionRuntimeProjection struct {
	RuntimeState          SessionRuntimeState
	RuntimeSummary        string
	WorkflowBlocks        []workflowBlock
	ActiveWorkflowBlockID string
	Tasks                 []map[string]any
}

func cloneSessionRuntimeProjection(src *sessionRuntimeProjection) sessionRuntimeProjection {
	if src == nil {
		return sessionRuntimeProjection{}
	}
	data, err := json.Marshal(src)
	if err != nil {
		return sessionRuntimeProjection{
			RuntimeState:          src.RuntimeState,
			RuntimeSummary:        src.RuntimeSummary,
			ActiveWorkflowBlockID: src.ActiveWorkflowBlockID,
			WorkflowBlocks:        append([]workflowBlock(nil), src.WorkflowBlocks...),
			Tasks:                 append([]map[string]any(nil), src.Tasks...),
		}
	}
	var cloned sessionRuntimeProjection
	if err := json.Unmarshal(data, &cloned); err != nil {
		return sessionRuntimeProjection{
			RuntimeState:          src.RuntimeState,
			RuntimeSummary:        src.RuntimeSummary,
			ActiveWorkflowBlockID: src.ActiveWorkflowBlockID,
			WorkflowBlocks:        append([]workflowBlock(nil), src.WorkflowBlocks...),
			Tasks:                 append([]map[string]any(nil), src.Tasks...),
		}
	}
	return cloned
}

func stringValue(raw any) string {
	text, _ := raw.(string)
	return strings.TrimSpace(text)
}

func boolPointer(raw any) *bool {
	value, ok := raw.(bool)
	if !ok {
		return nil
	}
	copyValue := value
	return &copyValue
}

func workflowBlocksFromAny(raw any) []workflowBlock {
	if raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var blocks []workflowBlock
	if err := json.Unmarshal(data, &blocks); err != nil {
		return nil
	}
	return blocks
}

func workflowBlocksToAny(blocks []workflowBlock) []any {
	if len(blocks) == 0 {
		return []any{}
	}
	data, err := json.Marshal(blocks)
	if err != nil {
		return []any{}
	}
	var encoded []any
	if err := json.Unmarshal(data, &encoded); err != nil {
		return []any{}
	}
	return encoded
}

func workflowBlocksToProjectionSnapshot(blocks []workflowBlock) []map[string]any {
	if len(blocks) == 0 {
		return nil
	}
	data, err := json.Marshal(blocks)
	if err != nil {
		return nil
	}
	var encoded []map[string]any
	if err := json.Unmarshal(data, &encoded); err != nil {
		return nil
	}
	return encoded
}

func workflowBlocksFromProjectionSnapshot(blocks []map[string]any) []workflowBlock {
	if len(blocks) == 0 {
		return nil
	}
	data, err := json.Marshal(blocks)
	if err != nil {
		return nil
	}
	var decoded []workflowBlock
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil
	}
	return decoded
}

func todoItemsFromAny(raw any) []map[string]any {
	if raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		return nil
	}
	return items
}

func deriveActiveWorkflowBlockID(blocks []workflowBlock) string {
	for idx := len(blocks) - 1; idx >= 0; idx-- {
		if strings.EqualFold(strings.TrimSpace(blocks[idx].Status), "running") {
			return strings.TrimSpace(blocks[idx].ID)
		}
	}
	return ""
}

func runtimeStateFromSessionData(sessionID string, sessionData map[string]any) SessionRuntimeState {
	if sessionData == nil {
		return SessionRuntimeState{SessionID: strings.TrimSpace(sessionID)}
	}
	state := SessionRuntimeState{
		SessionID:         strings.TrimSpace(sessionID),
		RunID:             stringValue(sessionData["run_id"]),
		Status:            stringValue(sessionData["runtime_status"]),
		ExecutionMode:     stringValue(sessionData["execution_mode"]),
		Phase:             stringValue(sessionData["runtime_phase"]),
		BoundInstanceID:   stringValue(sessionData["bound_instance_id"]),
		BoundProjectPath:  stringValue(sessionData["bound_project_path"]),
		CheckpointID:      stringValue(sessionData["checkpoint_id"]),
		RuntimeRevision:   stringValue(sessionData["runtime_revision"]),
		ActiveAgentConfig: stringValue(sessionData["active_agent_config"]),
		LastError:         stringValue(sessionData["last_error"]),
		FailureCode:       stringValue(sessionData["runtime_failure_code"]),
		FailureReason:     stringValue(sessionData["runtime_failure_reason"]),
		UpdatedAt:         stringValue(sessionData["updated_at"]),
	}
	return state
}

func projectionFromSessionData(sessionID string, sessionData map[string]any) sessionRuntimeProjection {
	blocks := workflowBlocksFromAny(sessionData["workflow_blocks"])
	projection := sessionRuntimeProjection{
		RuntimeState:          runtimeStateFromSessionData(sessionID, sessionData),
		RuntimeSummary:        stringValue(sessionData["runtime_summary"]),
		WorkflowBlocks:        blocks,
		ActiveWorkflowBlockID: deriveActiveWorkflowBlockID(blocks),
		Tasks:                 todoItemsFromAny(firstNonNil(sessionData["tasks"], sessionData["todos"])),
	}
	return projection
}

func projectionFromSnapshot(sessionID string, projection *runtimepersist.SessionRuntimeProjection) sessionRuntimeProjection {
	if projection == nil {
		return sessionRuntimeProjection{RuntimeState: SessionRuntimeState{SessionID: strings.TrimSpace(sessionID)}}
	}
	state := SessionRuntimeState{
		SessionID:         strings.TrimSpace(sessionID),
		RunID:             strings.TrimSpace(projection.RunID),
		Status:            strings.TrimSpace(projection.RuntimeStatus),
		ExecutionMode:     strings.TrimSpace(projection.ExecutionMode),
		Phase:             strings.TrimSpace(projection.Phase),
		BoundInstanceID:   strings.TrimSpace(projection.BoundInstanceID),
		BoundProjectPath:  strings.TrimSpace(projection.BoundProjectPath),
		CheckpointID:      strings.TrimSpace(projection.CheckpointID),
		RuntimeRevision:   strings.TrimSpace(projection.RuntimeRevision),
		ActiveAgentConfig: strings.TrimSpace(projection.ActiveAgentConfig),
		LastError:         strings.TrimSpace(projection.LastError),
		FailureCode:       strings.TrimSpace(projection.FailureCode),
		FailureReason:     strings.TrimSpace(projection.FailureReason),
		UpdatedAt:         strings.TrimSpace(projection.UpdatedAt),
	}
	blocks := workflowBlocksFromProjectionSnapshot(projection.WorkflowBlocks)
	return sessionRuntimeProjection{
		RuntimeState:          state,
		RuntimeSummary:        strings.TrimSpace(projection.RuntimeSummary),
		WorkflowBlocks:        blocks,
		ActiveWorkflowBlockID: deriveActiveWorkflowBlockID(blocks),
		Tasks:                 append([]map[string]any(nil), projection.Tasks...),
	}
}

func projectionToSnapshot(projection sessionRuntimeProjection) *runtimepersist.SessionRuntimeProjection {
	if strings.TrimSpace(projection.RuntimeState.Status) == "" && strings.TrimSpace(projection.RuntimeSummary) == "" && len(projection.WorkflowBlocks) == 0 {
		return nil
	}
	state := projection.RuntimeState
	return &runtimepersist.SessionRuntimeProjection{
		RuntimeStatus:     strings.TrimSpace(state.Status),
		RunID:             strings.TrimSpace(state.RunID),
		ExecutionMode:     strings.TrimSpace(state.ExecutionMode),
		Phase:             strings.TrimSpace(state.Phase),
		BoundInstanceID:   strings.TrimSpace(state.BoundInstanceID),
		BoundProjectPath:  strings.TrimSpace(state.BoundProjectPath),
		CheckpointID:      strings.TrimSpace(state.CheckpointID),
		RuntimeRevision:   strings.TrimSpace(state.RuntimeRevision),
		ActiveAgentConfig: strings.TrimSpace(state.ActiveAgentConfig),
		LastError:         strings.TrimSpace(state.LastError),
		FailureCode:       strings.TrimSpace(state.FailureCode),
		FailureReason:     strings.TrimSpace(state.FailureReason),
		UpdatedAt:         strings.TrimSpace(state.UpdatedAt),
		RuntimeSummary:    strings.TrimSpace(projection.RuntimeSummary),
		WorkflowBlocks:    workflowBlocksToProjectionSnapshot(projection.WorkflowBlocks),
		Tasks:             append([]map[string]any(nil), projection.Tasks...),
	}
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func (s *DeepServer) ensureSessionRuntimeProjection(sessionID string) *sessionRuntimeProjection {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionRuntimeProjections == nil {
		s.sessionRuntimeProjections = make(map[string]*sessionRuntimeProjection)
	}
	projection := s.sessionRuntimeProjections[sid]
	if projection == nil {
		projection = &sessionRuntimeProjection{RuntimeState: SessionRuntimeState{SessionID: sid}}
		s.sessionRuntimeProjections[sid] = projection
	}
	return projection
}

func (s *DeepServer) sessionRuntimeProjectionSnapshot(sessionID string, fallback map[string]any) sessionRuntimeProjection {
	sid := strings.TrimSpace(sessionID)
	s.mu.Lock()
	if s.sessionRuntimeProjections != nil {
		if projection := s.sessionRuntimeProjections[sid]; projection != nil {
			cloned := cloneSessionRuntimeProjection(projection)
			s.mu.Unlock()
			return cloned
		}
	}
	s.mu.Unlock()
	return projectionFromSessionData(sid, fallback)
}

func (s *DeepServer) updateSessionRuntimeProjectionState(state SessionRuntimeState) {
	sid := strings.TrimSpace(state.SessionID)
	if sid == "" {
		return
	}
	projection := s.ensureSessionRuntimeProjection(sid)
	if projection == nil {
		return
	}
	projection.RuntimeState = state
	if strings.TrimSpace(projection.RuntimeState.Status) == sessionRuntimeStatusFailed && strings.TrimSpace(projection.RuntimeState.FailureReason) != "" {
		projection.RuntimeSummary = strings.TrimSpace(projection.RuntimeState.FailureReason)
	}
	if projection.RuntimeState.Status == sessionRuntimeStatusCompleted || projection.RuntimeState.Status == sessionRuntimeStatusCancelled {
		projection.completeActiveBlock("completed", time.Now().UnixMilli())
	}
	if projection.RuntimeState.Status == sessionRuntimeStatusFailed {
		projection.completeActiveBlock("error", time.Now().UnixMilli())
	}
}

func (p *sessionRuntimeProjection) completeActiveBlock(status string, timestamp int64) {
	blockID := strings.TrimSpace(p.ActiveWorkflowBlockID)
	if blockID == "" {
		return
	}
	for idx := range p.WorkflowBlocks {
		if strings.TrimSpace(p.WorkflowBlocks[idx].ID) != blockID {
			continue
		}
		if strings.TrimSpace(p.WorkflowBlocks[idx].Status) != "running" {
			break
		}
		p.WorkflowBlocks[idx].Status = strings.TrimSpace(status)
		p.WorkflowBlocks[idx].UpdatedAt = timestamp
		p.WorkflowBlocks[idx].FinishedAt = &timestamp
		break
	}
	p.ActiveWorkflowBlockID = ""
}

func (s *DeepServer) applySessionWorkflowMessage(msg Message) bool {
	sid := strings.TrimSpace(msg.SessionID)
	if sid == "" {
		return false
	}
	msgType := strings.TrimSpace(msg.Type)
	if msgType == "" || msgType == MsgTypeRunEvent {
		return false
	}
	switch msgType {
	case MsgTypeThinking, MsgTypeAgentWorkflow, "decision":
		// continue
	case MsgTypePhaseTransition, MsgTypeError, MsgTypeCancelled, MsgTypeTaskListUpdate:
		// continue
	default:
		return false
	}
	projection := s.ensureSessionRuntimeProjection(sid)
	if projection == nil {
		return false
	}
	timestamp := time.Now().UnixMilli()
	switch msgType {
	case MsgTypeThinking:
		projection.appendWorkflowEvent(Message{
			Type:             MsgTypeAgentWorkflow,
			SessionID:        sid,
			Content:          msg.Content,
			Data:             msg.Data,
			Agent:            msg.Agent,
			AgentDisplayName: msg.AgentDisplayName,
			Phase:            "thinking",
		}, timestamp)
	case MsgTypeAgentWorkflow:
		projection.appendWorkflowEvent(msg, timestamp)
	case "decision":
		projection.appendWorkflowEvent(Message{
			Type:             MsgTypeAgentWorkflow,
			SessionID:        sid,
			Content:          msg.Content,
			Data:             msg.Data,
			Agent:            msg.Agent,
			AgentDisplayName: msg.AgentDisplayName,
			Phase:            "decision",
		}, timestamp)
	case MsgTypePhaseTransition:
		projection.completeActiveBlock("completed", timestamp)
		if planSummary := strings.TrimSpace(stringValue(msg.Data["plan_summary"])); planSummary != "" {
			projection.RuntimeSummary = planSummary
		} else if content := strings.TrimSpace(msg.Content); content != "" {
			projection.RuntimeSummary = content
		}
	case MsgTypeError:
		projection.completeActiveBlock("error", timestamp)
		if content := strings.TrimSpace(msg.Content); content != "" {
			projection.RuntimeSummary = content
		}
	case MsgTypeCancelled:
		projection.completeActiveBlock("completed", timestamp)
		if content := strings.TrimSpace(msg.Content); content != "" {
			projection.RuntimeSummary = content
		}
	case MsgTypeTaskListUpdate:
		projection.Tasks = todoItemsFromAny(firstNonNil(msg.Data["todos"], msg.Data["tasks"], msg.Data["items"]))
		projection.reconcileTaskListUpdate(timestamp)
	}
	return true
}

func (p *sessionRuntimeProjection) appendWorkflowEvent(msg Message, timestamp int64) {
	phase := strings.ToLower(strings.TrimSpace(msg.Phase))
	if phase == "" {
		phase = "thinking"
	}
	incomingAgent := strings.TrimSpace(msg.Agent)
	if incomingAgent == "" {
		incomingAgent = "orchestrator"
	}
	displayName := strings.TrimSpace(msg.AgentDisplayName)
	if displayName == "" {
		displayName = incomingAgent
	}
	activeIdx := -1
	for idx := range p.WorkflowBlocks {
		if strings.TrimSpace(p.WorkflowBlocks[idx].ID) == strings.TrimSpace(p.ActiveWorkflowBlockID) {
			activeIdx = idx
			break
		}
	}
	var active *workflowBlock
	if activeIdx >= 0 {
		active = &p.WorkflowBlocks[activeIdx]
	}
	if active == nil || strings.TrimSpace(active.Status) != "running" || strings.TrimSpace(active.Agent) != incomingAgent {
		p.completeActiveBlock("completed", timestamp)
		next := workflowBlock{
			ID:               buildWorkflowBlockID(timestamp),
			Agent:            incomingAgent,
			AgentDisplayName: displayName,
			Summary:          buildRuntimeWorkflowSummary(strings.TrimSpace(msg.Content), phase),
			Status:           "running",
			StartedAt:        timestamp,
			UpdatedAt:        timestamp,
			Entries:          []workflowEntry{},
		}
		p.WorkflowBlocks = append(p.WorkflowBlocks, next)
		p.ActiveWorkflowBlockID = next.ID
		activeIdx = len(p.WorkflowBlocks) - 1
		active = &p.WorkflowBlocks[activeIdx]
	}
	if active == nil {
		return
	}
	if phase == "thinking" || phase == "decision" || phase == "tool_call" || phase == "progress" {
		active.Summary = buildRuntimeWorkflowSummary(strings.TrimSpace(msg.Content), phase)
		p.RuntimeSummary = active.Summary
	}
	entry := buildWorkflowEntry(msg, timestamp)
	if entry != nil {
		updated := appendOrMergeProjectionWorkflowEntry(*active, *entry, timestamp)
		p.WorkflowBlocks[activeIdx] = updated
		active = &p.WorkflowBlocks[activeIdx]
	} else {
		active.UpdatedAt = timestamp
	}
	if strings.TrimSpace(active.Summary) != "" {
		p.RuntimeSummary = strings.TrimSpace(active.Summary)
	}
}

func buildWorkflowBlockID(timestamp int64) string {
	return "wf-" + strings.TrimSpace(time.UnixMilli(timestamp).UTC().Format("20060102150405.000"))
}

func buildRuntimeWorkflowSummary(content, phase string) string {
	if strings.TrimSpace(content) != "" {
		return strings.TrimSpace(content)
	}
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "tool_call":
		return "正在调用工具处理任务"
	case "tool_result":
		return "正在整理工具返回结果"
	case "decision":
		return "正在形成执行决策"
	default:
		return "正在处理任务"
	}
}

func buildWorkflowEntry(msg Message, timestamp int64) *workflowEntry {
	phase := strings.ToLower(strings.TrimSpace(msg.Phase))
	if phase == "" {
		phase = "thinking"
	}
	content := strings.TrimSpace(msg.Content)
	data := cloneMap(msg.Data)
	if phase == "tool_call" || phase == "tool_result" {
		toolName := stringValue(data["tool"])
		if toolName == "" {
			toolName = "未知工具"
		}
		toolStatus := normalizeProjectionToolStatus(data["status"], data["success"])
		if phase == "tool_call" {
			toolStatus = "running"
		}
		var success *bool
		if value, ok := data["success"].(bool); ok {
			success = &value
		}
		toolCard := &workflowToolCard{
			ID:                  "tool-" + time.UnixMilli(timestamp).UTC().Format("20060102150405.000"),
			Tool:                toolName,
			Status:              toolStatus,
			Args:                asMap(data["tool_args"]),
			Result:              stringValue(data["result"]),
			StatusSource:        stringValue(data["status_source"]),
			Success:             success,
			ErrorCode:           stringValue(firstNonNil(data["error_code"], data["code"])),
			Retryable:           boolPointer(data["retryable"]),
			ProviderDiagnostics: asMap(data["provider_diagnostics"]),
			StartedAt:           timestamp,
		}
		if phase == "tool_result" {
			finishedAt := timestamp
			toolCard.FinishedAt = &finishedAt
			if toolCard.Result == "" {
				toolCard.Result = content
			}
		}
		entryContent := content
		if entryContent == "" {
			if phase == "tool_call" {
				entryContent = "调用工具: " + toolName
			} else {
				entryContent = "工具返回: " + toolName
			}
		}
		return &workflowEntry{
			ID:        "entry-" + time.UnixMilli(timestamp).UTC().Format("20060102150405.000"),
			Type:      "tool",
			Content:   entryContent,
			Phase:     phase,
			Timestamp: timestamp,
			Data:      data,
			ToolCard:  toolCard,
		}
	}
	if content == "" {
		return nil
	}
	entryType := "status"
	switch phase {
	case "decision":
		entryType = "decision"
	case "thinking":
		entryType = "thinking"
	}
	return &workflowEntry{
		ID:        "entry-" + time.UnixMilli(timestamp).UTC().Format("20060102150405.000"),
		Type:      entryType,
		Content:   content,
		Phase:     phase,
		Timestamp: timestamp,
		Data:      data,
	}
}

func appendOrMergeProjectionWorkflowEntry(block workflowBlock, entry workflowEntry, timestamp int64) workflowBlock {
	// Heartbeats replace the status for the same request instead of appending
	// an unbounded history. Completed/failed attempts retain their own record.
	if entry.Phase == "progress" {
		key := strings.TrimSpace(stringValue(entry.Data["request_key"]))
		if key != "" {
			for idx := len(block.Entries) - 1; idx >= 0; idx-- {
				old := block.Entries[idx]
				if old.Phase == "progress" && stringValue(old.Data["request_key"]) == key {
					entry.ID = old.ID
					block.Entries[idx], block.UpdatedAt = entry, timestamp
					return block
				}
			}
		}
	}
	if entry.ToolCard == nil {
		block.Entries = append(block.Entries, entry)
		block.UpdatedAt = timestamp
		return block
	}
	if strings.TrimSpace(entry.Phase) == "tool_result" {
		for idx := len(block.Entries) - 1; idx >= 0; idx-- {
			item := block.Entries[idx]
			if item.ToolCard == nil {
				continue
			}
			if strings.TrimSpace(item.ToolCard.Tool) != strings.TrimSpace(entry.ToolCard.Tool) {
				continue
			}
			preserveTaskListCompletion := shouldPreserveProjectionTaskListCompletion(item.ToolCard, entry.ToolCard)
			if strings.TrimSpace(item.ToolCard.Status) != "running" && !preserveTaskListCompletion {
				continue
			}
			merged := item
			finishedAt := timestamp
			if preserveTaskListCompletion {
				merged.ToolCard.Status = "completed"
				if strings.TrimSpace(merged.ToolCard.Result) == "" {
					merged.ToolCard.Result = entry.ToolCard.Result
				}
				if strings.TrimSpace(merged.ToolCard.StatusSource) == "" {
					merged.ToolCard.StatusSource = "task_list_update"
				}
				success := true
				merged.ToolCard.Success = &success
				if merged.ToolCard.FinishedAt == nil {
					merged.ToolCard.FinishedAt = &finishedAt
				}
			} else {
				merged.ToolCard.Status = entry.ToolCard.Status
				merged.ToolCard.Result = entry.ToolCard.Result
				merged.ToolCard.StatusSource = entry.ToolCard.StatusSource
				merged.ToolCard.Success = entry.ToolCard.Success
				merged.ToolCard.FinishedAt = &finishedAt
				merged.Content = entry.Content
				merged.Timestamp = timestamp
			}
			merged.Data = entry.Data
			merged.Phase = entry.Phase
			block.Entries[idx] = merged
			block.UpdatedAt = timestamp
			return block
		}
	}
	block.Entries = append(block.Entries, entry)
	block.UpdatedAt = timestamp
	return block
}

func shouldPreserveProjectionTaskListCompletion(existing *workflowToolCard, incoming *workflowToolCard) bool {
	if existing == nil || incoming == nil {
		return false
	}
	if strings.TrimSpace(existing.Tool) != "write_todos" || strings.TrimSpace(incoming.Tool) != "write_todos" {
		return false
	}
	return strings.TrimSpace(existing.Status) == "completed" && strings.TrimSpace(existing.StatusSource) == "task_list_update" && strings.TrimSpace(incoming.Status) == "running"
}

func normalizeProjectionToolStatus(raw any, success any) string {
	value := strings.ToLower(strings.TrimSpace(stringValue(raw)))
	switch value {
	case "completed", "success", "done":
		return "completed"
	case "failed", "error", "blocked":
		return "failed"
	case "running", "active", "in_progress", "processing":
		return "running"
	default:
		if successValue, ok := success.(bool); ok {
			if successValue {
				return "completed"
			}
			return "failed"
		}
		return "running"
	}
}

func (p *sessionRuntimeProjection) reconcileTaskListUpdate(timestamp int64) {
	for blockIndex := len(p.WorkflowBlocks) - 1; blockIndex >= 0; blockIndex-- {
		block := p.WorkflowBlocks[blockIndex]
		for entryIndex := len(block.Entries) - 1; entryIndex >= 0; entryIndex-- {
			entry := block.Entries[entryIndex]
			if entry.ToolCard == nil {
				continue
			}
			if strings.TrimSpace(entry.ToolCard.Tool) != "write_todos" || strings.TrimSpace(entry.ToolCard.Status) != "running" {
				continue
			}
			finishedAt := timestamp
			block.Entries[entryIndex].Phase = "tool_result"
			block.Entries[entryIndex].Timestamp = timestamp
			block.Entries[entryIndex].ToolCard.Status = "completed"
			block.Entries[entryIndex].ToolCard.StatusSource = "task_list_update"
			success := true
			block.Entries[entryIndex].ToolCard.Success = &success
			block.Entries[entryIndex].ToolCard.FinishedAt = &finishedAt
			block.UpdatedAt = timestamp
			p.WorkflowBlocks[blockIndex] = block
			return
		}
	}
}

func cloneMap(input map[string]interface{}) map[string]any {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func asMap(raw any) map[string]any {
	if raw == nil {
		return nil
	}
	switch value := raw.(type) {
	case map[string]any:
		return value
	default:
		return nil
	}
}

func applyProjectionToSessionData(sessionData map[string]any, projection sessionRuntimeProjection) {
	if sessionData == nil {
		return
	}
	state := projection.RuntimeState
	if status := strings.TrimSpace(state.Status); status != "" {
		sessionData["runtime_status"] = status
	} else {
		delete(sessionData, "runtime_status")
	}
	if runID := strings.TrimSpace(state.RunID); runID != "" {
		sessionData["run_id"] = runID
	} else {
		delete(sessionData, "run_id")
	}
	if phase := strings.TrimSpace(state.Phase); phase != "" {
		sessionData["runtime_phase"] = phase
	} else {
		delete(sessionData, "runtime_phase")
	}
	if boundInstanceID := strings.TrimSpace(state.BoundInstanceID); boundInstanceID != "" {
		sessionData["bound_instance_id"] = boundInstanceID
	} else {
		delete(sessionData, "bound_instance_id")
	}
	if boundProjectPath := strings.TrimSpace(state.BoundProjectPath); boundProjectPath != "" {
		sessionData["bound_project_path"] = boundProjectPath
	} else {
		delete(sessionData, "bound_project_path")
	}
	if checkpointID := strings.TrimSpace(state.CheckpointID); checkpointID != "" {
		sessionData["checkpoint_id"] = checkpointID
	} else {
		delete(sessionData, "checkpoint_id")
	}
	if runtimeRevision := strings.TrimSpace(state.RuntimeRevision); runtimeRevision != "" {
		sessionData["runtime_revision"] = runtimeRevision
	} else {
		delete(sessionData, "runtime_revision")
	}
	if activeAgentConfig := strings.TrimSpace(state.ActiveAgentConfig); activeAgentConfig != "" {
		sessionData["active_agent_config"] = activeAgentConfig
	} else {
		delete(sessionData, "active_agent_config")
	}
	if lastError := strings.TrimSpace(state.LastError); lastError != "" {
		sessionData["last_error"] = lastError
	} else {
		delete(sessionData, "last_error")
	}
	if failureCode := strings.TrimSpace(state.FailureCode); failureCode != "" {
		sessionData["runtime_failure_code"] = failureCode
	} else {
		delete(sessionData, "runtime_failure_code")
	}
	if failureReason := strings.TrimSpace(state.FailureReason); failureReason != "" {
		sessionData["runtime_failure_reason"] = failureReason
	} else {
		delete(sessionData, "runtime_failure_reason")
	}
	if summary := strings.TrimSpace(projection.RuntimeSummary); summary != "" {
		sessionData["runtime_summary"] = summary
	} else {
		delete(sessionData, "runtime_summary")
	}
	if len(projection.WorkflowBlocks) > 0 {
		sessionData["workflow_blocks"] = workflowBlocksToAny(projection.WorkflowBlocks)
	} else {
		delete(sessionData, "workflow_blocks")
	}
	if len(projection.Tasks) > 0 {
		sessionData["tasks"] = append([]map[string]any(nil), projection.Tasks...)
	} else {
		delete(sessionData, "tasks")
	}
	if execMode := strings.TrimSpace(state.ExecutionMode); execMode != "" {
		sessionData["execution_mode"] = execMode
		sessionData["run_intent"] = map[executionMode]string{
			executionModePlan:  "plan_only",
			executionModeBuild: "execute",
		}[parseExecutionModeOrDefault(execMode)]
	}
}

func buildAttachedSessionStatePayload(sessionData map[string]any) map[string]any {
	if sessionData == nil {
		return nil
	}
	payload := make(map[string]any, len(sessionData)+4)
	for key, value := range sessionData {
		payload[key] = value
	}
	if status := stringValue(sessionData["runtime_status"]); status != "" {
		payload["status"] = status
	}
	if phase := stringValue(sessionData["runtime_phase"]); phase != "" {
		payload["phase"] = phase
	}
	if failureCode := stringValue(sessionData["runtime_failure_code"]); failureCode != "" {
		payload["failure_code"] = failureCode
	}
	if failureReason := stringValue(sessionData["runtime_failure_reason"]); failureReason != "" {
		payload["failure_reason"] = failureReason
	}
	return payload
}

func (s *DeepServer) persistSessionRuntimeProjection(sessionID string) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}
	projection := s.sessionRuntimeProjectionSnapshot(sid, nil)
	projectPath := strings.TrimSpace(projection.RuntimeState.BoundProjectPath)
	mode := parseExecutionModeOrDefault(projection.RuntimeState.ExecutionMode)
	s.persistManagedSessionState(sid, projectPath, mode)
}
