package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/gorilla/mux"
)

// PlanService is implemented by the session runtime. Read and edit operations
// are deterministic; execution is only scheduled by an explicit start action.
type PlanService interface {
	ListPlans(context.Context, string, string) ([]*plan.Snapshot, error)
	EditPlan(context.Context, string, string, string, plan.EditRequest) (*plan.Snapshot, error)
	ActOnPlan(context.Context, string, string, string, plan.ActionRequest) (*plan.Snapshot, error)
}

func (a *API) SetPlanService(service PlanService) { a.planService = service }
func (a *API) setupPlanRoutes(r *mux.Router) {
	r.HandleFunc("/plans/config", a.handlePlanConfig).Methods("GET", "PUT", "OPTIONS")
	r.HandleFunc("/chat/attachments/check", a.handleCheckAttachments).Methods("POST", "OPTIONS")
	r.HandleFunc("/sessions/{id}/plans", a.handleSessionPlans).Methods("GET", "OPTIONS")
	r.HandleFunc("/sessions/{id}/plans/{plan_id}/{action:edits|start|cancel|failure}", a.handleSessionPlans).Methods("POST", "OPTIONS")
}
func (a *API) handleSessionPlans(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "cartographic_plan") {
		return
	}
	if a.planService == nil {
		ErrorWithCode(w, 503, "PLAN_SERVICE_UNAVAILABLE", "制图计划服务未就绪")
		return
	}
	vars := mux.Vars(r)
	sid, id, project := vars["id"], vars["plan_id"], r.URL.Query().Get("project_path")
	if r.Method == http.MethodGet {
		list, err := a.planService.ListPlans(r.Context(), sid, project)
		if err != nil {
			planAPIError(w, err)
			return
		}
		Success(w, map[string]any{"plans": list})
		return
	}
	if vars["action"] == "edits" {
		var body plan.EditRequest
		if !decodeFlowRequest(w, r, &body) {
			return
		}
		value, err := a.planService.EditPlan(r.Context(), sid, project, id, body)
		if err != nil {
			planAPIError(w, err)
			return
		}
		Success(w, value)
		return
	}
	var body plan.ActionRequest
	if !decodeFlowRequest(w, r, &body) {
		return
	}
	if vars["action"] != "failure" {
		body.Action = vars["action"]
	}
	value, err := a.planService.ActOnPlan(r.Context(), sid, project, id, body)
	if err != nil {
		planAPIError(w, err)
		return
	}
	Success(w, value)
}
func planAPIError(w http.ResponseWriter, err error) {
	var changed *flowrun.ResourceChangeError
	if errors.As(err, &changed) {
		ErrorWithCodeAndDetails(w, http.StatusConflict, "PLAN_RESOURCE_CHANGED", err.Error(), map[string]any{"resource_change": changed.Change})
		return
	}
	status := http.StatusBadRequest
	code := "PLAN_INVALID"
	if errors.Is(err, plan.ErrVersionConflict) {
		status = http.StatusConflict
		code = "PLAN_VERSION_CONFLICT"
	}
	if os.IsNotExist(err) {
		status = http.StatusNotFound
		code = "PLAN_NOT_FOUND"
	}
	var validation *plan.ValidationError
	if errors.As(err, &validation) {
		ErrorWithCodeAndDetails(w, status, code, "计划校验未通过", validation.Diagnostics)
		return
	}
	ErrorWithCode(w, status, code, err.Error())
}
func (a *API) handlePlanConfig(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "plan_config") {
		return
	}
	if a.configService == nil || a.settingsMutationService == nil {
		ErrorWithCode(w, 503, "CONFIG_SERVICE_UNAVAILABLE", "设置服务未就绪")
		return
	}
	if r.Method == http.MethodGet {
		Success(w, a.configService.GetCartographicPlanConfig())
		return
	}
	var body config.CartographicPlanConfig
	if !decodeFlowRequest(w, r, &body) {
		return
	}
	if err := body.Validate(); err != nil {
		Error(w, 400, err.Error())
		return
	}
	if _, err := a.settingsMutationService.SaveConfig("plan.confirm", body.Confirm, ""); err != nil {
		if !writeSettingsMutationError(w, r, err) {
			Error(w, 500, err.Error())
		}
		return
	}
	Success(w, a.configService.GetCartographicPlanConfig())
}
func (a *API) handleCheckAttachments(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "chat_attachments") {
		return
	}
	var body struct {
		Paths []string `json:"paths"`
	}
	if !decodeFlowRequest(w, r, &body) {
		return
	}
	if len(body.Paths) > 100 {
		Error(w, 400, "一次最多添加100个附件")
		return
	}
	root, err := config.ResolveProjectRoot()
	if err != nil {
		Error(w, 500, err.Error())
		return
	}
	out := []plan.Attachment{}
	for _, p := range body.Paths {
		if strings.TrimSpace(p) == "" {
			Error(w, 400, "附件路径不能为空")
			return
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		p = filepath.Clean(p)
		info, err := os.Stat(p)
		if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
			ErrorWithCode(w, 400, "ATTACHMENT_MISSING", "附件不存在或不是普通文件/文件夹："+p)
			return
		}
		kind := "file"
		if info.IsDir() {
			kind = "folder"
		}
		out = append(out, plan.Attachment{Path: p, Kind: kind})
	}
	Success(w, map[string]any{"attachments": out})
}
