package api

import (
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/gorilla/mux"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (a *API) dataSourceEnvironment() (datasource.PlanningEnvironment, string, error) {
	env := datasource.PlanningEnvironment{Integrations: map[string]bool{}, Secrets: map[string]bool{}, AutoAcquire: "allow"}
	home, err := config.ResolveAutoCartoHome()
	if err != nil {
		return env, "", err
	}
	assets, err := config.ResolveAssetsRoot()
	if err != nil {
		return env, "", err
	}
	user := filepath.Join(home, "sources")
	env.Catalog, err = datasource.Load(filepath.Join(assets, "tools", "system", "data-acquisition", "assets"), user)
	if err != nil {
		return env, user, err
	}
	if a.configService != nil {
		services, e := a.configService.GetIntegrationsConfig()
		if e != nil {
			return env, user, e
		}
		raw, _ := json.Marshal(services)
		var flags map[string]struct {
			Enabled bool `json:"enabled"`
		}
		_ = json.Unmarshal(raw, &flags)
		for name, flag := range flags {
			env.Integrations[name] = flag.Enabled
		}
		for _, name := range a.configService.ConfiguredSecretNames() {
			env.Secrets[name] = true
		}
		cfg := a.configService.GetAcquisitionConfig()
		env.WebSearch, env.MaxDownloadMB = cfg.WebSearchEnabled, cfg.MaxDownloadMB
	}
	return env, user, nil
}
func (a *API) handleDataSources(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "data_sources") {
		return
	}
	env, root, err := a.dataSourceEnvironment()
	if err != nil {
		flowError(w, err)
		return
	}
	if r.Method == http.MethodGet {
		type view struct {
			datasource.Source
			Available bool     `json:"available"`
			Reasons   []string `json:"reasons"`
		}
		sources := []view{}
		for _, source := range env.Catalog.Sources {
			ready, reasons := env.Availability(source)
			sources = append(sources, view{source, ready, reasons})
		}
		Success(w, map[string]any{"sources": sources, "categories": env.Catalog.Categories, "diagnostics": env.Catalog.Diagnostics})
		return
	}
	id := mux.Vars(r)["id"]
	if r.Method == http.MethodDelete {
		source := env.Catalog.Source(id)
		if source == nil {
			ErrorWithCode(w, 404, "DATA_SOURCE_NOT_FOUND", "数据源不存在")
			return
		}
		if source.Builtin {
			ErrorWithCode(w, 403, "DATA_SOURCE_READ_ONLY", "内置数据源只读")
			return
		}
		if r.URL.Query().Get("confirm") != "true" {
			ErrorWithCode(w, 400, "CONFIRM_REQUIRED", "请先确认删除本机数据源；原始数据文件不会删除")
			return
		}
		if err := datasource.DeleteUser(root, id); err != nil {
			flowError(w, err)
			return
		}
		a.InvalidateFlowEnvironment()
		Success(w, map[string]bool{"deleted": true})
		return
	}
	var source datasource.Source
	if !decodeFlowRequest(w, r, &source) {
		return
	}
	if source.Kind != "local_folder" {
		ErrorWithCode(w, 400, "DATA_SOURCE_INVALID", "用户数据源只支持本机数据文件夹")
		return
	}
	if r.Method == http.MethodPost {
		source.ID = datasource.NewUserID()
		if source.ID == "" {
			flowError(w, fmt.Errorf("无法生成数据源编号"))
			return
		}
	} else {
		existing := env.Catalog.Source(id)
		if existing == nil {
			ErrorWithCode(w, 404, "DATA_SOURCE_NOT_FOUND", "数据源不存在")
			return
		}
		if existing.Builtin {
			ErrorWithCode(w, 403, "DATA_SOURCE_READ_ONLY", "内置数据源只读")
			return
		}
		source.ID = id
	}
	source.APIVersion = "data_source/v1"
	source.Builtin = false
	if err := validateLocalSource(env.Catalog, source); err != nil {
		ErrorWithCode(w, 400, "DATA_SOURCE_INVALID", err.Error())
		return
	}
	diagnostics, err := env.Catalog.SaveUser(root, source)
	if err != nil {
		ErrorWithCode(w, 400, "DATA_SOURCE_INVALID", err.Error())
		return
	}
	a.InvalidateFlowEnvironment()
	Success(w, map[string]any{"source": source, "diagnostics": diagnostics})
}
func validateLocalSource(c *datasource.Catalog, s datasource.Source) error {
	if !filepath.IsAbs(s.Root) {
		return fmt.Errorf("本机数据根目录须为绝对路径")
	}
	if info, err := os.Stat(s.Root); err != nil || !info.IsDir() {
		return fmt.Errorf("本机数据根目录不存在")
	}
	realRoot, err := filepath.EvalSymlinks(s.Root)
	if err != nil {
		return err
	}
	if len(s.Provides) == 0 {
		return fmt.Errorf("请至少选择一条数据并确认类别")
	}
	for _, p := range s.Provides {
		category := c.Category(p.Category)
		if category == nil {
			return fmt.Errorf("请为每条数据确认有效类别")
		}
		if _, _, err := c.ResolveCategory(p.Category, p.Params); err != nil {
			return err
		}
		if p.Path == "" || filepath.IsAbs(p.Path) {
			return fmt.Errorf("数据路径须为根目录内的相对路径")
		}
		path := filepath.Join(s.Root, filepath.FromSlash(p.Path))
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("数据文件不存在：%s", p.Path)
		}
		rel, err := filepath.Rel(realRoot, real)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("数据文件不能越出根目录")
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("数据文件无效：%s", p.Path)
		}
		for field, actual := range p.Fields {
			if _, ok := category.Fields[field]; !ok {
				return fmt.Errorf("类别 %s 没有标准字段 %s", category.Name, field)
			}
			for _, name := range actual {
				if strings.TrimSpace(name) == "" {
					return fmt.Errorf("字段对应不能为空")
				}
			}
		}
	}
	return nil
}
func (a *API) handleScanDataSource(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "data_source_scan") {
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if !decodeFlowRequest(w, r, &body) {
		return
	}
	env, _, err := a.dataSourceEnvironment()
	if err != nil {
		flowError(w, err)
		return
	}
	draft, err := env.Catalog.ScanLocalDraft(r.Context(), body.Path)
	if err != nil {
		ErrorWithCode(w, 400, "DATA_SOURCE_SCAN_FAILED", err.Error())
		return
	}
	Success(w, draft)
}
func (a *API) handleAcquisitionConfig(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "acquisition_config") {
		return
	}
	if a.configService == nil || a.settingsMutationService == nil {
		ErrorWithCode(w, 503, "CONFIG_SERVICE_UNAVAILABLE", "设置服务未初始化")
		return
	}
	if r.Method == http.MethodGet {
		Success(w, a.configService.GetAcquisitionConfig())
		return
	}
	var body config.AcquisitionConfig
	if !decodeFlowRequest(w, r, &body) {
		return
	}
	if err := body.Validate(); err != nil {
		ErrorWithCode(w, 400, "ACQUISITION_CONFIG_INVALID", err.Error())
		return
	}
	result, err := a.settingsMutationService.SaveConfig("acquisition", body, "")
	if err != nil {
		if !writeSettingsMutationError(w, r, err) {
			flowError(w, err)
		}
		return
	}
	a.InvalidateFlowEnvironment()
	Success(w, result)
}
