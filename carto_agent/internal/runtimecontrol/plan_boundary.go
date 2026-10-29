package runtimecontrol

import (
	"context"
	"encoding/json"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// WithPlanSubmissionBoundary ends the orchestrator's model loop after an accepted
// plan. Invalid proposals remain ordinary tool results which the model can repair.
// A subsequent user message starts a new turn, so persisted receipts cannot stop it.
func WithPlanSubmissionBoundary(inner model.ToolCallingChatModel) model.ToolCallingChatModel {
	return &planBoundaryModel{inner: inner}
}

type planBoundaryModel struct{ inner model.ToolCallingChatModel }

func (m *planBoundaryModel) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	bound, err := m.inner.WithTools(infos)
	if err != nil {
		return nil, err
	}
	return WithPlanSubmissionBoundary(bound), nil
}
func acceptedPlanAtEnd(messages []*schema.Message) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message == nil {
			continue
		}
		if message.Role != schema.Tool {
			return false
		}
		if message.ToolName != ToolPlanPropose && message.ToolName != ToolPlanRevise {
			continue
		}
		var receipt struct {
			Accepted bool `json:"accepted"`
		}
		if json.Unmarshal([]byte(message.Content), &receipt) == nil && receipt.Accepted {
			return true
		}
	}
	return false
}
func (m *planBoundaryModel) Generate(ctx context.Context, msg []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if acceptedPlanAtEnd(msg) {
		return schema.AssistantMessage("", nil), nil
	}
	return m.inner.Generate(ctx, msg, opts...)
}
func (m *planBoundaryModel) Stream(ctx context.Context, msg []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if acceptedPlanAtEnd(msg) {
		return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("", nil)}), nil
	}
	return m.inner.Stream(ctx, msg, opts...)
}
