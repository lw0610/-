package api

import "net/http"

func (a *API) handleSettingsWorkspace(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "settings_workspace") {
		return
	}
	if a.assetCatalogService == nil {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope(
			"SETTINGS_WORKSPACE_UNAVAILABLE",
			"设置工作区未初始化",
			"config",
			"settings_workspace",
			map[string]any{"dependency": "asset_catalog_service", "operation": "settings_workspace"},
		), nil)
		return
	}
	Success(w, a.assetCatalogService.SettingsWorkspace())
}
