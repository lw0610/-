package api

import (
	"errors"
	"net/http"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/settings"
)

func writeSettingsMutationError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	var blockedErr *control.ActiveRunBlockedError
	if !errors.As(err, &blockedErr) {
		return false
	}
	writeControlPlaneError(w, r, http.StatusConflict, blockedErr, newRequestEnvelope(
		"ACTIVE_RUN_BLOCKED",
		blockedErr.Error(),
		"config",
		"settings_mutation",
		"请先停止受影响的运行中会话，再重新保存该设置。",
		map[string]any{"required_action": settings.MutationRequiredActionStopActiveRuns},
	), blockedErr.Mutation)
	return true
}

func writeConfigMigrationError(w http.ResponseWriter, r *http.Request, err error, operation string, extra map[string]any) bool {
	if err == nil {
		return false
	}
	migrationErr, ok := config.AsConfigMigrationError(err)
	if !ok {
		return false
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
	writeControlPlaneError(w, r, http.StatusInternalServerError, migrationErr, newRequestEnvelope(
		"CONFIG_MIGRATION_FAILED",
		"配置迁移或解析失败",
		"config",
		"config_migration",
		"请修复配置文件格式或迁移失败原因后重试。",
		debugContext,
	), nil)
	return true
}
