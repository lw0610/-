package deepagent

import (
	"context"

	"github.com/autocarto/carto_agent/internal/taskmaterials"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// withTaskMaterials installs the same read-only material context on root and
// child agents. It does not copy the parent's conversation or change its task.
func withTaskMaterials(handlers []adk.ChatModelAgentMiddleware) []adk.ChatModelAgentMiddleware {
	result := make([]adk.ChatModelAgentMiddleware, 0, len(handlers)+1)
	installed := false
	for _, handler := range handlers {
		if _, ok := handler.(*taskMaterialsMiddleware); ok {
			if installed {
				continue
			}
			installed = true
		}
		result = append(result, handler)
	}
	if !installed {
		result = append(result, &taskMaterialsMiddleware{})
	}
	return result
}

type taskMaterialsMiddleware struct {
	adk.BaseChatModelAgentMiddleware
}

func (*taskMaterialsMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	prepared, err := taskmaterials.Prepare(ctx)
	return prepared, runCtx, err
}

func (*taskMaterialsMiddleware) WrapModel(_ context.Context, inner model.BaseChatModel, _ *adk.ModelContext) (model.BaseChatModel, error) {
	return &taskMaterialsModel{BaseChatModel: inner}, nil
}

type taskMaterialsModel struct{ model.BaseChatModel }

func (m *taskMaterialsModel) Generate(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	input, err := materialModelInput(ctx, messages)
	if err != nil {
		return nil, err
	}
	return m.BaseChatModel.Generate(ctx, input, opts...)
}

func (m *taskMaterialsModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	input, err := materialModelInput(ctx, messages)
	if err != nil {
		return nil, err
	}
	return m.BaseChatModel.Stream(ctx, input, opts...)
}

func materialModelInput(ctx context.Context, messages []*schema.Message) ([]*schema.Message, error) {
	material, err := taskmaterials.ContextMessage(ctx)
	if err != nil {
		return nil, err
	}
	// Work on the request slice only. Persisted history and OriginalSource must
	// retain the real last user message, and compression may run independently.
	input := make([]*schema.Message, 0, len(messages)+1)
	inserted := false
	for _, message := range messages {
		if message != nil && message.Extra[taskmaterials.MessageMarker] == true {
			continue
		}
		if !inserted && material != nil && message != nil && message.Role == schema.User {
			input = append(input, material)
			inserted = true
		}
		input = append(input, message)
	}
	if material != nil && !inserted {
		input = append(input, material)
	}
	return input, nil
}
