package control

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/settings"
)

const mutationRequiredActionStopActiveRuns = "stop_active_runs"

// ActiveRunBlockedError 表示当前 mutation 会影响活跃运行，已被硬阻止。
type ActiveRunBlockedError struct {
	Mutation settings.SettingsMutationResult
	message  string
}

func (e *ActiveRunBlockedError) Error() string {
	if e == nil {
		return "当前存在活跃运行，无法执行该设置变更"
	}
	if strings.TrimSpace(e.message) != "" {
		return e.message
	}
	if next := strings.TrimSpace(e.Mutation.NextStep); next != "" {
		return next
	}
	return "当前存在活跃运行，无法执行该设置变更"
}

func (e *ActiveRunBlockedError) ControlErrorEnvelope() *ErrorEnvelope {
	envelope := NewErrorEnvelope("ACTIVE_RUN_BLOCKED", e.Error(), string(DomainConfig), "settings_mutation")
	envelope.Retryable = true
	envelope.Degraded = false
	envelope.UserAction = strings.TrimSpace(e.Mutation.NextStep)
	if envelope.UserAction == "" {
		envelope.UserAction = "请先停止受影响的运行中会话，再重新保存该设置。"
	}
	envelope.DebugContext["scope"] = strings.TrimSpace(e.Mutation.Scope)
	envelope.DebugContext["required_action"] = strings.TrimSpace(e.Mutation.RequiredAction)
	envelope.DebugContext["blocking_runs"] = append([]string(nil), e.Mutation.BlockingRuns...)
	envelope.DebugContext["blocking_instances"] = append([]string(nil), e.Mutation.BlockingInstances...)
	return envelope
}

func (e *ActiveRunBlockedError) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.ControlErrorEnvelope())
}

func newActiveRunBlockedError(
	diagnostics settings.RuntimeDiagnosticsProvider,
	scope string,
	affectedPaths []string,
	runs []ActiveRunInfo,
	message string,
) *ActiveRunBlockedError {
	builder := settings.NewMutationBuilder(diagnostics)
	mutation := builder.Attach(settings.SettingsMutationResult{
		Scope:          strings.TrimSpace(scope),
		AffectedPaths:  normalizePaths(affectedPaths),
		RequiredAction: mutationRequiredActionStopActiveRuns,
		NextStep:       "请先停止受影响的运行中会话，再重新保存该设置。",
	})
	mutation.BlockingRuns = collectBlockingRunIDs(runs)
	mutation.BlockingInstances = collectBlockingInstanceIDs(runs)
	mutation = settings.FinalizeMutationResult(mutation)
	if strings.TrimSpace(message) == "" {
		message = buildActiveRunBlockedMessage(mutation)
	}
	return &ActiveRunBlockedError{
		Mutation: mutation,
		message:  strings.TrimSpace(message),
	}
}

func normalizePaths(items []string) []string {
	result := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func collectBlockingRunIDs(runs []ActiveRunInfo) []string {
	result := make([]string, 0, len(runs))
	seen := make(map[string]struct{}, len(runs))
	for _, run := range runs {
		sessionID := strings.TrimSpace(run.SessionID)
		if sessionID == "" {
			continue
		}
		if _, ok := seen[sessionID]; ok {
			continue
		}
		seen[sessionID] = struct{}{}
		result = append(result, sessionID)
	}
	return result
}

func collectBlockingInstanceIDs(runs []ActiveRunInfo) []string {
	result := make([]string, 0, len(runs))
	seen := make(map[string]struct{}, len(runs))
	for _, run := range runs {
		instanceID := strings.TrimSpace(run.BoundInstanceID)
		if instanceID == "" {
			continue
		}
		if _, ok := seen[instanceID]; ok {
			continue
		}
		seen[instanceID] = struct{}{}
		result = append(result, instanceID)
	}
	return result
}

func buildActiveRunBlockedMessage(mutation settings.SettingsMutationResult) string {
	parts := []string{"当前存在会影响运行态的活跃会话，已阻止本次设置保存"}
	if len(mutation.BlockingRuns) > 0 {
		parts = append(parts, fmt.Sprintf("阻塞会话：%s", strings.Join(mutation.BlockingRuns, ", ")))
	}
	if len(mutation.BlockingInstances) > 0 {
		parts = append(parts, fmt.Sprintf("关联实例：%s", strings.Join(mutation.BlockingInstances, ", ")))
	}
	if next := strings.TrimSpace(mutation.NextStep); next != "" {
		parts = append(parts, next)
	}
	return strings.Join(parts, "；")
}
