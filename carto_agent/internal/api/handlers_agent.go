package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/settings"
)

func writeAgentConfigError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	if writeSettingsMutationError(w, r, err) {
		return
	}
	message := err.Error()
	status := http.StatusInternalServerError

	switch {
	case strings.Contains(message, "不存在"):
		status = http.StatusNotFound
	case strings.Contains(message, "不能为空"),
		strings.Contains(message, "已存在"),
		strings.Contains(message, "冲突"),
		strings.Contains(message, "不能"),
		strings.Contains(message, "无效"):
		status = http.StatusBadRequest
	}

	Error(w, status, message)
}

// handleListAgentConfigs GET /api/agent-configs
func (a *API) handleListAgentConfigs(w http.ResponseWriter, r *http.Request) {
	if a.assetCatalogService == nil {
		Error(w, http.StatusServiceUnavailable, "资产目录服务未初始化")
		return
	}
	userID := r.URL.Query().Get("user_id")
	configs, err := a.assetCatalogService.ListAgentConfigs(userID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	Success(w, map[string]any{"configs": configs})
}

// handleGetAgentConfig GET /api/agent-configs/{name}
func (a *API) handleGetAgentConfig(w http.ResponseWriter, r *http.Request) {
	if a.assetCatalogService == nil {
		Error(w, http.StatusServiceUnavailable, "资产目录服务未初始化")
		return
	}
	userID := r.URL.Query().Get("user_id")
	name := mux.Vars(r)["name"]
	cfg, err := a.assetCatalogService.GetAgentConfig(userID, name)
	if err != nil {
		Error(w, http.StatusNotFound, err.Error())
		return
	}
	Success(w, cfg)
}

// handleGetAgentConfigData GET /api/agent-configs/{name}/data
func (a *API) handleGetAgentConfigData(w http.ResponseWriter, r *http.Request) {
	if a.assetCatalogService == nil {
		Error(w, http.StatusServiceUnavailable, "资产目录服务未初始化")
		return
	}
	name := mux.Vars(r)["name"]
	profile, err := a.assetCatalogService.GetAgentProfile(name)
	if err != nil {
		Error(w, http.StatusNotFound, err.Error())
		return
	}
	Success(w, profile)
}

// handleCreateAgentConfig POST /api/agent-configs
func (a *API) handleCreateAgentConfig(w http.ResponseWriter, r *http.Request) {
	if a.settingsMutationService == nil {
		Error(w, http.StatusServiceUnavailable, "设置服务未初始化")
		return
	}
	var body struct {
		Name      string `json:"name"`
		CloneFrom string `json:"clone_from"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "无效的请求体")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		Error(w, http.StatusBadRequest, "name 不能为空")
		return
	}
	cloneFrom := strings.TrimSpace(body.CloneFrom)
	payload, err := a.settingsMutationService.CreateAgentConfig(r.URL.Query().Get("user_id"), name, cloneFrom)
	if err != nil {
		writeAgentConfigError(w, r, err)
		return
	}
	a.InvalidateFlowEnvironment()
	Success(w, payload)
}

// handleCloneAgentConfig POST /api/agent-configs/{name}/clone
func (a *API) handleCloneAgentConfig(w http.ResponseWriter, r *http.Request) {
	if a.settingsMutationService == nil {
		Error(w, http.StatusServiceUnavailable, "设置服务未初始化")
		return
	}
	sourceName := strings.TrimSpace(mux.Vars(r)["name"])
	if sourceName == "" {
		Error(w, http.StatusBadRequest, "源配置名不能为空")
		return
	}
	var body struct {
		TargetName string `json:"target_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "无效的请求体")
		return
	}
	targetName := strings.TrimSpace(body.TargetName)
	if targetName == "" {
		Error(w, http.StatusBadRequest, "target_name 不能为空")
		return
	}
	payload, err := a.settingsMutationService.CloneAgentConfig(r.URL.Query().Get("user_id"), sourceName, targetName)
	if err != nil {
		writeAgentConfigError(w, r, err)
		return
	}
	a.InvalidateFlowEnvironment()
	Success(w, payload)
}

// handleSaveAgentConfig PUT /api/agent-configs/{name}
func (a *API) handleSaveAgentConfig(w http.ResponseWriter, r *http.Request) {
	if a.settingsMutationService == nil {
		Error(w, http.StatusServiceUnavailable, "设置服务未初始化")
		return
	}
	userID := r.URL.Query().Get("user_id")
	name := mux.Vars(r)["name"]
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		Error(w, http.StatusBadRequest, "无效的请求体")
		return
	}
	var shape map[string]json.RawMessage
	if err := json.Unmarshal(raw, &shape); err != nil {
		Error(w, http.StatusBadRequest, "无效的配置对象")
		return
	}
	var result settings.SettingsMutationResult
	var err error
	if _, direct := shape["runtime"]; direct {
		var data config.AgentConfigData
		if err = json.Unmarshal(raw, &data); err != nil {
			Error(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err = a.settingsMutationService.SaveAgentConfigData(userID, name, &data)
	} else {
		var profile settings.AgentProfileView
		if err = json.Unmarshal(raw, &profile); err != nil {
			Error(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err = a.settingsMutationService.SaveAgentConfig(userID, name, profile)
	}
	if err != nil {
		writeAgentConfigError(w, r, err)
		return
	}
	a.InvalidateFlowEnvironment()
	Success(w, result)
}

func (a *API) handleMigrateAgentConfig(w http.ResponseWriter, r *http.Request) {
	catalog, ok := a.assetCatalogService.(interface {
		PreviewAgentConfigMigration(string) (*config.AgentConfigMigrationResult, error)
	})
	if !ok {
		Error(w, http.StatusServiceUnavailable, "方案迁移服务未初始化")
		return
	}
	var body struct {
		DryRun bool `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
		Error(w, http.StatusBadRequest, "无效的请求体")
		return
	}
	if r.URL.Query().Get("dry_run") == "true" {
		body.DryRun = true
	}
	name := mux.Vars(r)["name"]
	preview, err := catalog.PreviewAgentConfigMigration(name)
	if err != nil {
		writeAgentConfigError(w, r, err)
		return
	}
	preview.DryRun = body.DryRun
	if !body.DryRun {
		if a.settingsMutationService == nil {
			Error(w, http.StatusServiceUnavailable, "设置服务未初始化")
			return
		}
		mutation, err := a.settingsMutationService.SaveAgentConfigData(r.URL.Query().Get("user_id"), name, preview.Config)
		if err != nil {
			writeAgentConfigError(w, r, err)
			return
		}
		a.InvalidateFlowEnvironment()
		Success(w, map[string]any{"config": preview.Config, "warnings": preview.Warnings, "changed": preview.Changed, "dry_run": false, "mutation": mutation})
		return
	}
	Success(w, preview)
}

// handleDeleteAgentConfig DELETE /api/agent-configs/{name}
func (a *API) handleDeleteAgentConfig(w http.ResponseWriter, r *http.Request) {
	if a.settingsMutationService == nil {
		Error(w, http.StatusServiceUnavailable, "设置服务未初始化")
		return
	}
	userID := r.URL.Query().Get("user_id")
	name := mux.Vars(r)["name"]
	payload, err := a.settingsMutationService.DeleteAgentConfig(userID, name)
	if err != nil {
		writeAgentConfigError(w, r, err)
		return
	}
	a.InvalidateFlowEnvironment()
	Success(w, payload)
}

// handleActivateAgentConfig POST /api/agent-configs/{name}/activate
func (a *API) handleActivateAgentConfig(w http.ResponseWriter, r *http.Request) {
	if a.settingsMutationService == nil {
		Error(w, http.StatusServiceUnavailable, "设置服务未初始化")
		return
	}
	userID := r.URL.Query().Get("user_id")
	name := mux.Vars(r)["name"]
	result, err := a.settingsMutationService.ActivateAgentConfig(userID, name)
	if err != nil {
		writeAgentConfigError(w, r, err)
		return
	}
	a.InvalidateFlowEnvironment()
	Success(w, result)
}
