package llm

import (
	"errors"
	"fmt"
	"testing"
)

func TestNonRecoverableModelServiceErrors(t *testing.T) {
	for _, message := range []string{
		"[NodeRunError] error, status code: 402, status: 402 Payment Required, message: Insufficient Balance",
		"status code: 401, invalid API key",
		"HTTP 403 Forbidden",
		"status code: 429, code: insufficient_quota",
		"authentication_error: credit balance is too low",
	} {
		err := fmt.Errorf("run model: %w", errors.New(message))
		if !IsNonRecoverableServiceError(err) || shouldRetryLLMError(err) {
			t.Errorf("account failure was retryable: %s", message)
		}
	}
	for _, message := range []string{
		"status code: 429, rate limit exceeded",
		"status code: 503, temporarily unavailable",
		"timeout",
		"图层产出必须是数组",
		"column 402 is missing",
	} {
		if IsNonRecoverableServiceError(errors.New(message)) {
			t.Errorf("recoverable/task error treated as account failure: %s", message)
		}
	}
	if IsNonRecoverableServiceError(nil) {
		t.Fatal("nil error classified as failure")
	}
}
