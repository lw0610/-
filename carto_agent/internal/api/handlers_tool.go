package api

import (
	"errors"
	"net/http"

	"github.com/autocarto/carto_agent/internal/control"
)

// handleGetToolCatalog GET /api/tools/catalog
func (a *API) handleGetToolCatalog(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "tool_catalog") {
		return
	}
	if a.assetCatalogService == nil {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope(
			"ASSET_CATALOG_UNAVAILABLE",
			"资产目录服务未初始化",
			"catalog",
			"catalog_api",
			map[string]any{"dependency": "asset_catalog_service", "operation": "get_tool_catalog"},
		), nil)
		return
	}
	scope := r.URL.Query().Get("scope")
	view, err := a.loadToolCatalogView(r.Context(), scope)
	if err != nil {
		if errors.Is(err, errAssetCatalogReadTimeout) {
			projection := control.ToolCatalogView{
				Scope:   scope,
				Catalog: degradedPublicCatalogContractForStartup(),
			}.ResponseProjection()
			Success(w, projection.Payload)
			return
		}
		writeControlPlaneError(w, r, http.StatusInternalServerError, err, newRequestEnvelope(
			"TOOL_CATALOG_READ_FAILED",
			"读取工具目录失败",
			"catalog",
			"catalog_api",
			"请检查目录构建状态后重试。",
			map[string]any{"operation": "get_tool_catalog", "scope": scope},
		), nil)
		return
	}
	projection := view.ResponseProjection()
	switch projection.FailureMode {
	case control.ToolCatalogFailureModeUnsupportedRuntime:
		fallback := newAvailabilityEnvelope(
			"TOOL_CATALOG_UNSUPPORTED_RUNTIME",
			projection.SharedError.Message,
			"catalog",
			"catalog_api",
			map[string]any{"scope": projection.Scope, "catalog_id": projection.CatalogID},
		)
		fallback.UserAction = projection.SharedError.UserAction
		fallback.Retryable = projection.SharedError.Retryable
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, fallback, projection.Payload)
		return
	case control.ToolCatalogFailureModeAbnormalEmpty:
		fallback := newRequestEnvelope(
			"TOOL_CATALOG_ABNORMAL_EMPTY",
			projection.SharedError.Message,
			"catalog",
			"catalog_api",
			projection.SharedError.UserAction,
			map[string]any{"scope": projection.Scope, "catalog_id": projection.CatalogID},
		)
		fallback.Retryable = projection.SharedError.Retryable
		writeControlPlaneError(w, r, http.StatusInternalServerError, nil, fallback, projection.Payload)
		return
	default:
		Success(w, projection.Payload)
	}
}
