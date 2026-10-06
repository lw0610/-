package llm

import (
	"context"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type retryingModelWrapper struct {
	underlying model.ToolCallingChatModel
}

var _ model.ToolCallingChatModel = (*retryingModelWrapper)(nil)

func NewRetryingModelWrapper(underlying model.ToolCallingChatModel) *retryingModelWrapper {
	return &retryingModelWrapper{underlying: underlying}
}

func (w *retryingModelWrapper) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	var lastErr error
	for attempt := 0; attempt <= 2; attempt++ {
		resp, err := w.underlying.Generate(ctx, input, opts...)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt == 2 || !shouldRetryLLMError(err) {
			return nil, err
		}
		if err := waitRetryBackoff(ctx, attempt); err != nil {
			return nil, lastErr
		}
	}
	return nil, lastErr
}

func (w *retryingModelWrapper) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	var lastErr error
	for attempt := 0; attempt <= 2; attempt++ {
		resp, err := w.underlying.Stream(ctx, input, opts...)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt == 2 || !shouldRetryLLMError(err) {
			return nil, err
		}
		if err := waitRetryBackoff(ctx, attempt); err != nil {
			return nil, lastErr
		}
	}
	return nil, lastErr
}

func (w *retryingModelWrapper) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	newUnderlying, err := w.underlying.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &retryingModelWrapper{underlying: newUnderlying}, nil
}

func shouldRetryLLMError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	if text == "" {
		return false
	}
	for _, marker := range []string{
		"connection reset by peer",
		"internal_error",
		"stream error: stream id",
		"unexpected eof",
		"eof",
		"timeout",
		"temporarily unavailable",
		"status code: 429",
		"status code: 502",
		"status code: 503",
		"status code: 504",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func waitRetryBackoff(ctx context.Context, attempt int) error {
	delay := time.Duration(attempt+1) * 300 * time.Millisecond
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
