package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/gorilla/mux"
)

func (a *API) SetFlowStore(store *flow.Store) {
	a.resourceLibraryMu.Lock()
	defer a.resourceLibraryMu.Unlock()
	a.flowPackages = store
}

func (a *API) flowStore() (*flow.Store, error) {
	a.resourceLibraryMu.Lock()
	store := a.flowPackages
	a.resourceLibraryMu.Unlock()
	if store != nil {
		return store, nil
	}
	library, err := a.resourceStore()
	if err != nil {
		return nil, err
	}
	a.resourceLibraryMu.Lock()
	defer a.resourceLibraryMu.Unlock()
	if a.flowPackages != nil {
		return a.flowPackages, nil
	}
	store, err = flow.NewDefaultStore(library)
	if err == nil {
		a.flowPackages = store
	}
	return store, err
}

func (a *API) setupFlowRoutes(r *mux.Router) {
	r.HandleFunc("/flows", a.handleFlows).Methods("GET", "POST", "OPTIONS")
	r.HandleFunc("/flows/bindings/status", a.handleFlowBindingStatus).Methods("GET", "OPTIONS")
	r.HandleFunc("/flows/import", a.handleImportFlow).Methods("POST", "OPTIONS")
	r.HandleFunc("/flows/{id}/roles", a.handleFlowRoles).Methods("GET", "PUT", "OPTIONS")
	r.HandleFunc("/flows/{id}/export", a.handleExportFlow).Methods("POST", "OPTIONS")
	r.HandleFunc("/flows/{id}/copy", a.handleCopyFlow).Methods("POST", "OPTIONS")
	r.HandleFunc("/flows/{id}/resources/repair", a.handleRepairFlowResource).Methods("POST", "OPTIONS")
	r.HandleFunc("/flows/{id}/resources/import", a.handleImportFlowResource).Methods("POST", "OPTIONS")
	r.HandleFunc("/flows/{id}/resources", a.handleDeleteFlowResource).Methods("DELETE", "OPTIONS")
	r.HandleFunc("/flows/{id}/inputs/check", a.handleCheckFlowInputs).Methods("POST", "OPTIONS")
	r.HandleFunc("/flows/{id}/inputs/memory", a.handleFlowInputMemory).Methods("GET", "OPTIONS")
	r.HandleFunc("/flows/{id}/inputs/memory/{label}", a.handleFlowInputMemory).Methods("PUT", "DELETE", "OPTIONS")
	r.HandleFunc("/flows/{id}/bindings", a.handleFlowBinding).Methods("PUT", "OPTIONS")
	r.HandleFunc("/flows/{id}", a.handleFlow).Methods("GET", "PUT", "DELETE", "OPTIONS")
}

func decodeFlowRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, flow.MaxFileBytes+(1<<20)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		ErrorWithCode(w, http.StatusBadRequest, "FLOW_REQUEST_INVALID", "请求内容不是合法 JSON 对象")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		ErrorWithCode(w, http.StatusBadRequest, "FLOW_REQUEST_INVALID", "请求只允许一个 JSON 对象")
		return false
	}
	return true
}

func flowError(w http.ResponseWriter, err error) {
	status, code := http.StatusBadRequest, "FLOW_REQUEST_INVALID"
	switch {
	case errors.Is(err, flow.ErrNotFound):
		status, code = http.StatusNotFound, "FLOW_NOT_FOUND"
	case errors.Is(err, flow.ErrReadOnly):
		status, code = http.StatusForbidden, "FLOW_READ_ONLY"
	case errors.Is(err, flow.ErrConflict):
		status, code = http.StatusConflict, "FLOW_DUPLICATE_ID"
	}
	ErrorWithCode(w, status, code, err.Error())
}

func (a *API) handleFlows(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "flows") {
		return
	}
	store, err := a.flowStore()
	if err != nil {
		flowError(w, err)
		return
	}
	if r.Method == http.MethodGet {
		items, err := store.List()
		if err != nil {
			flowError(w, err)
			return
		}
		if err = a.decorateFlowSummaries(r.Context(), items, r.URL.Query().Get("scheme")); err != nil {
			flowError(w, err)
			return
		}
		Success(w, items)
		return
	}
	var request struct {
		FlowMD string `json:"flow_md"`
		ID     string `json:"id"`
	}
	if !decodeFlowRequest(w, r, &request) {
		return
	}
	item, err := store.Create(request.FlowMD, request.ID)
	if err != nil {
		flowError(w, err)
		return
	}
	if err = a.decorateFlow(r.Context(), item, r.URL.Query().Get("scheme"), "created"); err != nil {
		flowError(w, err)
		return
	}
	Success(w, item)
}

func (a *API) handleFlow(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "flow") {
		return
	}
	store, err := a.flowStore()
	if err != nil {
		flowError(w, err)
		return
	}
	id := mux.Vars(r)["id"]
	if r.Method == http.MethodDelete {
		item, err := store.Get(id)
		if err != nil {
			flowError(w, err)
			return
		}
		if item.Source == "builtin" {
			flowError(w, flow.ErrReadOnly)
			return
		}
		memory, err := flow.DefaultInputMemory()
		if err == nil {
			err = memory.DeleteFlow(id)
		}
		if err != nil {
			flowError(w, err)
			return
		}
		if err = store.Delete(id); err != nil {
			flowError(w, err)
			return
		}
		Success(w, map[string]bool{"deleted": true})
		return
	}
	var item *flow.StoredFlow
	if r.Method == http.MethodGet {
		item, err = store.Get(id)
	} else {
		var request struct {
			FlowMD string `json:"flow_md"`
		}
		if !decodeFlowRequest(w, r, &request) {
			return
		}
		item, err = store.Save(id, request.FlowMD)
	}
	if err != nil {
		flowError(w, err)
		return
	}
	reason := ""
	if r.Method != http.MethodGet {
		reason = "saved"
	}
	if err = a.decorateFlow(r.Context(), item, r.URL.Query().Get("scheme"), reason); err != nil {
		flowError(w, err)
		return
	}
	Success(w, item)
}

func (a *API) handleImportFlow(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "flow_import") {
		return
	}
	var request struct {
		Source    string `json:"source_path"`
		Overwrite bool   `json:"overwrite"`
		NewID     string `json:"new_id"`
	}
	if !decodeFlowRequest(w, r, &request) {
		return
	}
	store, err := a.flowStore()
	if err != nil {
		flowError(w, err)
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		flowError(w, err)
		return
	}
	item, err := store.Import(request.Source, flow.ImportOptions{Overwrite: request.Overwrite, NewID: request.NewID}, library)
	if err != nil {
		flowError(w, err)
		return
	}
	if err = a.decorateFlow(r.Context(), item, r.URL.Query().Get("scheme"), "imported"); err != nil {
		flowError(w, err)
		return
	}
	Success(w, map[string]any{"flow": item})
}

func (a *API) handleExportFlow(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "flow_export") {
		return
	}
	var request struct {
		Target             string `json:"target_path"`
		IncludeData        bool   `json:"include_data_files"`
		IncludeLibraryDeps *bool  `json:"include_library_deps"`
	}
	if !decodeFlowRequest(w, r, &request) {
		return
	}
	store, err := a.flowStore()
	if err != nil {
		flowError(w, err)
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		flowError(w, err)
		return
	}
	id := mux.Vars(r)["id"]
	if _, _, _, _, err = store.ResolveResources(r.Context(), id, library); err != nil {
		flowError(w, err)
		return
	}
	include := request.IncludeLibraryDeps == nil || *request.IncludeLibraryDeps
	path, err := store.ExportWithOptions(id, request.Target, flow.ExportOptions{IncludeDataFiles: request.IncludeData, IncludeLibraryDeps: include, Library: library})
	if err != nil {
		flowError(w, err)
		return
	}
	Success(w, map[string]string{"zip_path": path})
}

func (a *API) handleCopyFlow(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "flow_copy") {
		return
	}
	var request struct {
		ID   string `json:"new_id"`
		Name string `json:"new_name"`
	}
	if !decodeFlowRequest(w, r, &request) {
		return
	}
	store, err := a.flowStore()
	if err != nil {
		flowError(w, err)
		return
	}
	item, err := store.Copy(mux.Vars(r)["id"], request.ID, request.Name)
	if err != nil {
		flowError(w, err)
		return
	}
	if err = a.decorateFlow(r.Context(), item, r.URL.Query().Get("scheme"), "copied"); err != nil {
		flowError(w, err)
		return
	}
	Success(w, item)
}

func (a *API) handleImportFlowResource(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "flow_resources_import") {
		return
	}
	var request struct {
		Source string `json:"source_path"`
		Subdir string `json:"target_subdir"`
	}
	if !decodeFlowRequest(w, r, &request) {
		return
	}
	store, err := a.flowStore()
	if err != nil {
		flowError(w, err)
		return
	}
	item, err := store.ImportResource(mux.Vars(r)["id"], request.Source, request.Subdir)
	if err != nil {
		flowError(w, err)
		return
	}
	Success(w, map[string]any{"resources": item.Resources})
}

func (a *API) handleDeleteFlowResource(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "flow_resources_delete") {
		return
	}
	store, err := a.flowStore()
	if err != nil {
		flowError(w, err)
		return
	}
	item, err := store.DeleteResource(mux.Vars(r)["id"], r.URL.Query().Get("path"))
	if err != nil {
		flowError(w, err)
		return
	}
	Success(w, map[string]any{"resources": item.Resources})
}

func (a *API) handleFlowBinding(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "flow_bindings") {
		return
	}
	var request struct {
		Key     string `json:"resource_key"`
		BaseDir string `json:"base_dir"`
	}
	if !decodeFlowRequest(w, r, &request) {
		return
	}
	store, err := a.flowStore()
	if err != nil {
		flowError(w, err)
		return
	}
	if _, err = store.Get(mux.Vars(r)["id"]); err != nil {
		flowError(w, err)
		return
	}
	if strings.TrimSpace(request.Key) == "" {
		Error(w, http.StatusBadRequest, "资源键不能为空")
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		flowError(w, err)
		return
	}
	itemForInput, doc, _, resolved, err := store.ResolveResourcesReadOnly(r.Context(), mux.Vars(r)["id"], library)
	if err != nil {
		flowError(w, err)
		return
	}
	label := ""
	for i, value := range resolved {
		if value.Key == request.Key || value.ID == request.Key {
			label = doc.Resources[i].FolderInput
			break
		}
	}
	if label == "" {
		ErrorWithCode(w, http.StatusGone, "FLOW_BINDING_RETIRED", "此清单尚未关联输入，请先使用 convert_to_input 转换为文件夹输入。")
		return
	}
	memory, err := flow.DefaultInputMemory()
	if err == nil {
		err = setInputMemoryValue(memory, itemForInput, label, request.BaseDir)
	}
	if err != nil {
		flowError(w, err)
		return
	}

	item, err := store.Get(mux.Vars(r)["id"])
	if err == nil {
		err = a.relinkFlow(r.Context(), item, r.URL.Query().Get("scheme"))
	}
	if err != nil {
		flowError(w, err)
		return
	}
	Success(w, map[string]any{"plan": item.Plan, "diagnostics": item.Diagnostics})
}

func (a *API) handleFlowBindingStatus(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "flow_bindings") {
		return
	}
	store, err := a.flowStore()
	if err != nil {
		flowError(w, err)
		return
	}
	count, err := store.OrphanedBindingCount()
	if err != nil {
		flowError(w, err)
		return
	}
	Success(w, map[string]int{"orphaned_count": count})
}
