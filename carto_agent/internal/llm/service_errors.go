package llm

import (
	"regexp"
	"strings"
)

var nonRecoverableHTTPStatus = regexp.MustCompile(`(?i)(?:status(?:[ _]code)?\s*[:=]?\s*|http\s+)(?:401|402|403)\b|\b(?:401 unauthorized|402 payment required|403 forbidden)\b`)

// IsNonRecoverableServiceError identifies model account/configuration failures.
// Retrying a step or asking the same service to replan cannot fix these. Keep
// transient rate limits, transport failures and ordinary tool errors retryable.
func IsNonRecoverableServiceError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	if nonRecoverableHTTPStatus.MatchString(message) {
		return true
	}
	for _, marker := range []string{
		"insufficient_balance", "insufficient balance", "insufficient_quota",
		"billing_hard_limit_reached", "credit balance is too low",
		"invalid_api_key", "invalid api key", "incorrect api key",
		"authentication_error", "authentication failed",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
