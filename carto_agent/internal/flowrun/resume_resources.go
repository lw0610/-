package flowrun

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/flow"
)

const ResourceChangeMessage = "资源在暂停期间发生变化。继续将使用本次运行保存的资源引用；已修改的文件内容不会恢复，文件缺失或内容变化仍可能导致执行失败。"

type ResourceChange struct {
	Fingerprint string `json:"fingerprint"`
	Message     string `json:"message"`
}
type ResourceChangeError struct{ Change ResourceChange }

func (e *ResourceChangeError) Error() string { return e.Change.Message }
func compileKey(p *flow.FlowPlan) string {
	if p == nil {
		return ""
	}
	return p.CompileKey
}
func checkResumeSource(state *RunState, source string) error {
	if state.Plan == nil {
		return fmt.Errorf("计划运行记录缺少执行快照")
	}
	for _, input := range state.Plan.Inputs {
		if input.Type != "conversation_text" || !provided(state.Inputs[input.Key]) {
			continue
		}
		old, _ := json.Marshal(state.Inputs[input.Key])
		current, _ := json.Marshal(ConversationReference(source))
		if string(old) != string(current) {
			return fmt.Errorf("恢复所需原文与运行记录不一致")
		}
	}
	return nil
}

// CheckResume validates the persisted plan before any resumed tool can execute.
// Historical flow-run records remain readable, but are never recompiled or run.
func (c *Controller) CheckResume(ctx context.Context, request ResumeRequest) (*RunState, error) {
	state, err := c.options.Store.Load(ctx, request.SessionDir, request.FlowRunID)
	if err != nil {
		return nil, err
	}
	if state.SessionID != request.SessionID {
		return nil, fmt.Errorf("计划与会话不匹配")
	}
	if state.CartoPlan == nil {
		return nil, fmt.Errorf("旧流程运行记录只读，请从指南生成新计划")
	}
	return c.checkCartographicResume(ctx, state, request)
}
