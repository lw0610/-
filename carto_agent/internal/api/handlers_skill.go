package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"github.com/autocarto/carto_agent/internal/settings"
)

type scopedAssetCatalog interface {
	ListSkillPackagesByScope(scope, configName string) ([]settings.SkillPackageSummary, error)
	GetSkillPackageByScope(name, scope, configName string) (settings.SkillPackageDetail, error)
}

type scopedSettingsMutationService interface {
	ToggleSkillPackageWithScope(name, scope, configName string, enabled bool) (settings.SettingsMutationResult, error)
	SaveSkillWithScope(name, scope, configName, description, content string) (settings.SettingsMutationResult, error)
	DeleteSkillWithScope(name, scope, configName string) (settings.SettingsMutationResult, error)
	ImportSkillWithScope(sourcePath string, overwrite bool, scope, configName string) (map[string]any, error)
	MigrateExclusiveSkillPackages() (map[string]any, error)
}

func (a *API) listSkillPackagesWithScope(scope, configName string) ([]settings.SkillPackageSummary, error) {
	if strings.TrimSpace(scope) == "" && strings.TrimSpace(configName) == "" {
		return a.assetCatalogService.ListSkillPackages()
	}
	if scopedCatalog, ok := a.assetCatalogService.(scopedAssetCatalog); ok {
		return scopedCatalog.ListSkillPackagesByScope(scope, configName)
	}
	return a.assetCatalogService.ListSkillPackages()
}

func (a *API) getSkillPackageWithScope(name, scope, configName string) (settings.SkillPackageDetail, error) {
	if strings.TrimSpace(scope) == "" && strings.TrimSpace(configName) == "" {
		return a.assetCatalogService.GetSkillPackage(name)
	}
	if scopedCatalog, ok := a.assetCatalogService.(scopedAssetCatalog); ok {
		return scopedCatalog.GetSkillPackageByScope(name, scope, configName)
	}
	return a.assetCatalogService.GetSkillPackage(name)
}

func (a *API) toggleSkillPackageWithScope(name, scope, configName string, enabled bool) (settings.SettingsMutationResult, error) {
	if scopedMutation, ok := a.settingsMutationService.(scopedSettingsMutationService); ok {
		return scopedMutation.ToggleSkillPackageWithScope(name, scope, configName, enabled)
	}
	return a.settingsMutationService.ToggleSkillPackage(name, enabled)
}

func (a *API) saveSkillWithScope(name, scope, configName, description, content string) (settings.SettingsMutationResult, error) {
	if scopedMutation, ok := a.settingsMutationService.(scopedSettingsMutationService); ok {
		return scopedMutation.SaveSkillWithScope(name, scope, configName, description, content)
	}
	return a.settingsMutationService.SaveSkill(name, description, content)
}

func (a *API) deleteSkillWithScope(name, scope, configName string) (settings.SettingsMutationResult, error) {
	if scopedMutation, ok := a.settingsMutationService.(scopedSettingsMutationService); ok {
		return scopedMutation.DeleteSkillWithScope(name, scope, configName)
	}
	return a.settingsMutationService.DeleteSkill(name)
}

func (a *API) importSkillWithScope(sourcePath string, overwrite bool, scope, configName string) (map[string]any, error) {
	if scopedMutation, ok := a.settingsMutationService.(scopedSettingsMutationService); ok {
		return scopedMutation.ImportSkillWithScope(sourcePath, overwrite, scope, configName)
	}
	return a.settingsMutationService.ImportSkill(sourcePath, overwrite)
}

// handleListSkills GET /api/skills
func (a *API) handleListSkills(w http.ResponseWriter, r *http.Request) {
	if a.assetCatalogService == nil {
		Error(w, http.StatusServiceUnavailable, "资产目录服务未初始化")
		return
	}
	skills, err := a.assetCatalogService.ListSkills()
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if skills == nil {
		skills = []map[string]any{}
	}
	Success(w, map[string]any{"skills": skills})
}

// handleGetSkill GET /api/skills/{name}
func (a *API) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	if a.assetCatalogService == nil {
		Error(w, http.StatusServiceUnavailable, "资产目录服务未初始化")
		return
	}
	name := mux.Vars(r)["name"]
	content, err := a.assetCatalogService.GetSkillContent(name)
	if err != nil {
		Error(w, http.StatusNotFound, err.Error())
		return
	}
	Success(w, content)
}

func (a *API) handleListSkillPackages(w http.ResponseWriter, r *http.Request) {
	if a.assetCatalogService == nil {
		Error(w, http.StatusServiceUnavailable, "资产目录服务未初始化")
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	configName := strings.TrimSpace(r.URL.Query().Get("config_name"))
	packages, err := a.listSkillPackagesWithScope(scope, configName)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if packages == nil {
		packages = []settings.SkillPackageSummary{}
	}
	Success(w, map[string]any{"packages": packages})
}

func (a *API) handleGetSkillPackage(w http.ResponseWriter, r *http.Request) {
	if a.assetCatalogService == nil {
		Error(w, http.StatusServiceUnavailable, "资产目录服务未初始化")
		return
	}
	name := mux.Vars(r)["name"]
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	configName := strings.TrimSpace(r.URL.Query().Get("config_name"))
	pkg, err := a.getSkillPackageWithScope(name, scope, configName)
	if err != nil {
		if writeSkillPackageError(w, err) {
			return
		}
		Error(w, http.StatusNotFound, err.Error())
		return
	}
	Success(w, pkg)
}

// handleSaveSkill PUT /api/skills/{name}
func (a *API) handleSaveSkill(w http.ResponseWriter, r *http.Request) {
	if a.settingsMutationService == nil {
		Error(w, http.StatusServiceUnavailable, "设置服务未初始化")
		return
	}
	name := mux.Vars(r)["name"]
	var body struct {
		Description string `json:"description"`
		Content     string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "无效的请求体")
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	configName := strings.TrimSpace(r.URL.Query().Get("config_name"))
	result, err := a.saveSkillWithScope(name, scope, configName, body.Description, body.Content)
	if err != nil {
		if writeSettingsMutationError(w, r, err) {
			return
		}
		if writeSkillPackageError(w, err) {
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.InvalidateFlowEnvironment()
	a.realtimeHub.BroadcastSkillCatalogChanged("refresh", "api", strings.TrimSpace(name))
	Success(w, result)
}

// handleDeleteSkill DELETE /api/skills/{name}
func (a *API) handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	if a.settingsMutationService == nil {
		Error(w, http.StatusServiceUnavailable, "设置服务未初始化")
		return
	}
	name := mux.Vars(r)["name"]
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	configName := strings.TrimSpace(r.URL.Query().Get("config_name"))
	result, err := a.deleteSkillWithScope(name, scope, configName)
	if err != nil {
		if writeSettingsMutationError(w, r, err) {
			return
		}
		var referencedErr *settings.SkillReferencedError
		if errors.As(err, &referencedErr) {
			ErrorWithCode(w, http.StatusConflict, "SKILL_REFERENCED", referencedErr.Error())
			return
		}
		if writeSkillPackageError(w, err) {
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.InvalidateFlowEnvironment()
	a.realtimeHub.BroadcastSkillCatalogChanged("refresh", "api", strings.TrimSpace(name))
	Success(w, result)
}

// handleGetSkillReferences GET /api/skills/{name}/references
func (a *API) handleGetSkillReferences(w http.ResponseWriter, r *http.Request) {
	if a.assetCatalogService == nil {
		Success(w, map[string]any{"references": []any{}})
		return
	}
	name := mux.Vars(r)["name"]
	refs := a.assetCatalogService.GetSkillReferences(name)
	Success(w, map[string]any{"references": refs})
}

func (a *API) handleGetSkillPackageReferences(w http.ResponseWriter, r *http.Request) {
	if a.assetCatalogService == nil {
		Success(w, map[string]any{"references": []any{}})
		return
	}
	name := mux.Vars(r)["name"]
	refs := a.assetCatalogService.GetSkillPackageReferences(name)
	Success(w, map[string]any{"package_id": strings.TrimSpace(name), "references": refs})
}

func (a *API) handleToggleSkillPackage(w http.ResponseWriter, r *http.Request) {
	if a.settingsMutationService == nil || a.assetCatalogService == nil {
		Error(w, http.StatusServiceUnavailable, "设置服务未初始化")
		return
	}
	name := mux.Vars(r)["name"]
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			Error(w, http.StatusBadRequest, "无效的请求体")
			return
		}
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	configName := strings.TrimSpace(r.URL.Query().Get("config_name"))
	var err error
	enabled := false
	if body.Enabled != nil {
		enabled = *body.Enabled
	} else {
		pkg, fetchErr := a.getSkillPackageWithScope(name, scope, configName)
		if fetchErr != nil {
			if writeSkillPackageError(w, fetchErr) {
				return
			}
			Error(w, http.StatusNotFound, fetchErr.Error())
			return
		}
		enabled = !pkg.Enabled
	}
	result, err := a.toggleSkillPackageWithScope(name, scope, configName, enabled)
	if err != nil {
		if writeSettingsMutationError(w, r, err) {
			return
		}
		if writeSkillPackageError(w, err) {
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.InvalidateFlowEnvironment()
	a.realtimeHub.BroadcastSkillCatalogChanged("refresh", "api", strings.TrimSpace(name))
	pkg, pkgErr := a.getSkillPackageWithScope(name, scope, configName)
	if pkgErr != nil {
		Success(w, map[string]any{"package_id": strings.TrimSpace(name), "enabled": enabled, "mutation": result})
		return
	}
	Success(w, map[string]any{"package": pkg, "mutation": result})
}

func (a *API) handleMigrateSkillPackages(w http.ResponseWriter, r *http.Request) {
	if a.settingsMutationService == nil {
		Error(w, http.StatusServiceUnavailable, "设置服务未初始化")
		return
	}
	scopedMutation, ok := a.settingsMutationService.(scopedSettingsMutationService)
	if !ok {
		Error(w, http.StatusNotImplemented, "当前设置服务不支持技能包迁移")
		return
	}
	result, err := scopedMutation.MigrateExclusiveSkillPackages()
	if err != nil {
		if writeSettingsMutationError(w, r, err) {
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.InvalidateFlowEnvironment()
	a.realtimeHub.BroadcastSkillCatalogChanged("refresh", "api", "")
	Success(w, result)
}

// handleImportSkill POST /api/skills/import
func (a *API) handleImportSkill(w http.ResponseWriter, r *http.Request) {
	if a.settingsMutationService == nil {
		Error(w, http.StatusServiceUnavailable, "设置服务未初始化")
		return
	}
	var body struct {
		SourcePath string `json:"source_path"`
		Overwrite  bool   `json:"overwrite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "无效的请求体")
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	configName := strings.TrimSpace(r.URL.Query().Get("config_name"))
	result, err := a.importSkillWithScope(body.SourcePath, body.Overwrite, scope, configName)
	if err != nil {
		if writeSettingsMutationError(w, r, err) {
			return
		}
		if writeSkillPackageError(w, err) {
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	importedCount := 0
	if importedRaw, ok := result["imported"].([]map[string]any); ok {
		importedCount = len(importedRaw)
	}
	if importedCount > 0 {
		a.InvalidateFlowEnvironment()
		a.realtimeHub.BroadcastSkillCatalogChanged("refresh", "api", "")
	}
	Success(w, result)
}

func writeSkillPackageError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var notFoundErr *settings.SkillPackageNotFoundError
	if errors.As(err, &notFoundErr) {
		Error(w, http.StatusNotFound, err.Error())
		return true
	}
	var unsupportedErr *settings.SkillMutationUnsupportedError
	if errors.As(err, &unsupportedErr) {
		ErrorWithCode(w, http.StatusConflict, "SKILL_PACKAGE_OPERATION_UNSUPPORTED", unsupportedErr.Error())
		return true
	}
	return false
}
