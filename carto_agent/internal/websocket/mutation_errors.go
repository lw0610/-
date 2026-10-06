package websocket

import (
	"errors"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/settings"
)

func buildMutationErrorData(err error) map[string]any {
	if err == nil {
		return nil
	}
	var blockedErr *control.ActiveRunBlockedError
	if !errors.As(err, &blockedErr) {
		return nil
	}
	return map[string]any{
		"code":     settings.MutationRequiredActionStopActiveRuns,
		"mutation": blockedErr.Mutation,
	}
}

func buildConfigMigrationEnvelope(err error, operation string, extra map[string]any) *control.ErrorEnvelope {
	if err == nil {
		return nil
	}
	migrationErr, ok := config.AsConfigMigrationError(err)
	if !ok {
		return nil
	}
	debugContext := map[string]any{
		"failure_kind": "schema_migration",
		"operation":    operation,
		"cause":        migrationErr.Error(),
	}
	if migrationErr.Operation != "" {
		debugContext["migration_operation"] = migrationErr.Operation
	}
	for key, value := range extra {
		debugContext[key] = value
	}
	envelope := control.NewErrorEnvelope("CONFIG_MIGRATION_FAILED", "配置迁移或解析失败", "config", "config_migration")
	envelope.UserAction = "请修复配置文件格式或迁移失败原因后重试。"
	return envelope.WithDebugContext(debugContext)
}
