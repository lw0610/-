package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/gorilla/mux"
)

func (a *API) SetResourceLibrary(library *resource.Library) {
	a.resourceLibraryMu.Lock()
	defer a.resourceLibraryMu.Unlock()
	a.resourceLibrary = library
}

func (a *API) resourceStore() (*resource.Library, error) {
	a.resourceLibraryMu.Lock()
	defer a.resourceLibraryMu.Unlock()
	if a.resourceLibrary != nil {
		return a.resourceLibrary, nil
	}
	library, err := resource.NewDefaultLibrary()
	if err == nil {
		a.resourceLibrary = library
	}
	return library, err
}

func (a *API) setupResourceRoutes(router *mux.Router) {
	router.HandleFunc("/resources", a.handleResources).Methods("GET", "OPTIONS")
	router.HandleFunc("/resources/types", a.handleResourceTypes).Methods("GET", "OPTIONS")
	router.HandleFunc("/resources/capture", a.handleCaptureResource).Methods("POST", "OPTIONS")
	router.HandleFunc("/resources/import", a.handleImportResource).Methods("POST", "OPTIONS")
	router.HandleFunc("/resources/bindings", a.handleResourceBinding).Methods("PUT", "OPTIONS")
	router.HandleFunc("/resources/by-id/{id}", a.handleResourceByID).Methods("GET", "OPTIONS")
	router.HandleFunc("/resources/by-id/{id}/preview", a.handleResourcePreview).Methods("GET", "OPTIONS")
	router.HandleFunc("/resources/{id}/copy", a.handleCopyResource).Methods("POST", "OPTIONS")
	router.HandleFunc("/resources/{id}/content", a.handleReplaceResource).Methods("PUT", "OPTIONS")
	router.HandleFunc("/resources/{id}/references", a.handleResourceReferences).Methods("GET", "OPTIONS")
	router.HandleFunc("/resources/{id}/upgrade", a.handleUpgradeResource).Methods("POST", "OPTIONS")
	router.HandleFunc("/resources/{id}", a.handleDeleteResourceByID).Methods("DELETE", "OPTIONS")
	router.HandleFunc("/resources/{type}/{name}/preview", a.handleResourcePreview).Methods("GET", "OPTIONS")
	router.HandleFunc("/resources/{type}/{name}", a.handleResource).Methods("GET", "DELETE", "OPTIONS")
}

func resourceError(w http.ResponseWriter, err error) {
	status, code := http.StatusBadRequest, "RESOURCE_REQUEST_INVALID"
	switch {
	case errors.Is(err, resource.ErrNotFound):
		status, code = http.StatusNotFound, "RESOURCE_NOT_FOUND"
	case errors.Is(err, resource.ErrReadOnly):
		status, code = http.StatusForbidden, "RESOURCE_READ_ONLY"
	case errors.Is(err, resource.ErrConflict):
		status, code = http.StatusConflict, "RESOURCE_CONFLICT"
	}
	var conflict *resource.ConflictError
	if errors.As(err, &conflict) {
		ErrorWithCodeAndDetails(w, status, code, err.Error(), map[string]any{"existing_id": conflict.ExistingID})
		return
	}
	ErrorWithCode(w, status, code, err.Error())
}

func decodeResourceRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		ErrorWithCode(w, http.StatusBadRequest, "RESOURCE_REQUEST_INVALID", "请求内容不是合法 JSON 对象")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		ErrorWithCode(w, http.StatusBadRequest, "RESOURCE_REQUEST_INVALID", "请求只允许一个 JSON 对象")
		return false
	}
	return true
}

func (a *API) handleResources(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "resources") {
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		resourceError(w, err)
		return
	}
	items, err := library.List(r.URL.Query().Get("scope"))
	if err != nil {
		resourceError(w, err)
		return
	}
	if r.URL.Query().Get("scope") == "retired" {
		store, err := a.flowStore()
		if err != nil {
			flowError(w, err)
			return
		}
		rows := []map[string]any{}
		for _, item := range items {
			raw, _ := json.Marshal(item)
			row := map[string]any{}
			_ = json.Unmarshal(raw, &row)
			refs, err := store.ResourceReferences(library, item.ID)
			if err != nil {
				flowError(w, err)
				return
			}
			row["references"] = refs
			rows = append(rows, row)
		}
		Success(w, rows)
		return
	}
	Success(w, items)
}

func (a *API) handleResource(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "resource") {
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		resourceError(w, err)
		return
	}
	vars := mux.Vars(r)
	if r.Method == http.MethodDelete {
		items, err := library.List("user")
		if err != nil {
			resourceError(w, err)
			return
		}
		id := ""
		for _, item := range items {
			if item.Type == vars["type"] && item.Name == vars["name"] {
				if id != "" {
					resourceError(w, resource.ErrConflict)
					return
				}
				id = item.ID
			}
		}
		if id == "" {
			if _, err := library.Get(vars["type"], vars["name"]); err != nil {
				resourceError(w, err)
			} else {
				resourceError(w, resource.ErrReadOnly)
			}
			return
		}
		a.deleteManagedResource(w, r, library, id)
		return
	}
	item, err := library.Get(vars["type"], vars["name"])
	if err != nil {
		resourceError(w, err)
		return
	}
	Success(w, item)
}

func (a *API) handleImportResource(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "resource_import") {
		return
	}
	var request struct {
		Source string `json:"source_path"`
		Type   string `json:"type"`
		Name   string `json:"name"`
	}
	if !decodeResourceRequest(w, r, &request) {
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		resourceError(w, err)
		return
	}
	item, err := library.ImportNamed(request.Source, request.Type, request.Name)
	if err != nil {
		resourceError(w, err)
		return
	}
	Success(w, item)
}

func (a *API) handleResourceBinding(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "resource_binding") {
		return
	}
	ErrorWithCode(w, http.StatusGone, "RESOURCE_BINDING_RETIRED", "公共数据绑定已退出，请在流程输入中选择文件夹；旧资源可先移入流程。")
}

func (a *API) handleResourceTypes(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "resource_types") {
		return
	}
	Success(w, resource.TypeInfos())
}

func (a *API) handleResourceByID(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "resource_by_id") {
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		resourceError(w, err)
		return
	}
	item, err := library.GetByID(mux.Vars(r)["id"])
	if err != nil {
		resourceError(w, err)
		return
	}
	Success(w, item)
}

func (a *API) handleCopyResource(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "resource_copy") {
		return
	}
	var request struct {
		Override bool   `json:"override"`
		Name     string `json:"name"`
	}
	if !decodeResourceRequest(w, r, &request) {
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		resourceError(w, err)
		return
	}
	result, err := library.Copy(mux.Vars(r)["id"], request.Override, request.Name)
	if err != nil {
		resourceError(w, err)
		return
	}
	Success(w, result)
}
func (a *API) handleReplaceResource(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "resource_replace") {
		return
	}
	var request struct {
		Source       string            `json:"source_path"`
		EntryAliases map[string]string `json:"entry_aliases"`
	}
	if !decodeResourceRequest(w, r, &request) {
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		resourceError(w, err)
		return
	}
	var result *resource.ResolvedResource
	if request.EntryAliases != nil {
		if request.Source != "" {
			ErrorWithCode(w, 400, "RESOURCE_REQUEST_INVALID", "source_path 与 entry_aliases 只能提供一个")
			return
		}
		result, err = library.ReplaceAliases(mux.Vars(r)["id"], request.EntryAliases)
	} else {
		result, err = library.Replace(mux.Vars(r)["id"], request.Source)
	}
	if err != nil {
		resourceError(w, err)
		return
	}
	Success(w, result)
}
func (a *API) handleResourceReferences(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "resource_references") {
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		resourceError(w, err)
		return
	}
	store, err := a.flowStore()
	if err != nil {
		flowError(w, err)
		return
	}
	refs, err := store.ResourceReferences(library, mux.Vars(r)["id"])
	if err != nil {
		resourceError(w, err)
		return
	}
	Success(w, refs)
}
func (a *API) handleDeleteResourceByID(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "resource_delete") {
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		resourceError(w, err)
		return
	}
	a.deleteManagedResource(w, r, library, mux.Vars(r)["id"])
}
func (a *API) deleteManagedResource(w http.ResponseWriter, r *http.Request, library *resource.Library, id string) {
	item, err := library.GetByID(id)
	if err != nil {
		resourceError(w, err)
		return
	}
	if item.Owner != "library_user" {
		resourceError(w, resource.ErrReadOnly)
		return
	}
	force := false
	if raw := r.URL.Query().Get("force"); raw != "" {
		force, err = strconv.ParseBool(raw)
		if err != nil {
			ErrorWithCode(w, 400, "RESOURCE_REQUEST_INVALID", "force 必须是 true 或 false")
			return
		}
	}
	store, err := a.flowStore()
	if err != nil {
		flowError(w, err)
		return
	}
	refs, err := store.ResourceReferences(library, id)
	if err != nil {
		resourceError(w, err)
		return
	}
	if len(refs) > 0 && !force {
		ErrorWithCodeAndDetails(w, 409, "RESOURCE_IN_USE", "资源仍被流程引用，请确认影响后删除", map[string]any{"references": refs})
		return
	}
	if err = library.DeleteID(id); err != nil {
		resourceError(w, err)
		return
	}
	Success(w, map[string]bool{"deleted": true})
}
func (a *API) handleUpgradeResource(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "resource_upgrade") {
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		resourceError(w, err)
		return
	}
	oldID := mux.Vars(r)["id"]
	result, err := library.Upgrade(oldID)
	if err != nil {
		resourceError(w, err)
		return
	}
	store, err := a.flowStore()
	if err != nil {
		resourceError(w, err)
		return
	}
	if err = store.MigrateResourceIdentity(oldID, result); err != nil {
		resourceError(w, err)
		return
	}
	Success(w, struct {
		*resource.ResolvedResource
		OldID string `json:"old_id"`
	}{result, oldID})
}
