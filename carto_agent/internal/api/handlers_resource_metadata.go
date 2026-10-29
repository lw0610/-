package api

import (
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/gorilla/mux"
	"net/http"
)

func (a *API) handleResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "resource_metadata") {
		return
	}
	var request resource.MetadataUpdate
	if !decodeResourceRequest(w, r, &request) {
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		resourceError(w, err)
		return
	}
	id := mux.Vars(r)["id"]
	old, err := library.GetByID(id)
	if err != nil {
		resourceError(w, err)
		return
	}
	result, err := library.UpdateMetadata(id, request)
	if err != nil {
		resourceError(w, err)
		return
	}
	if old.Name != result.Name {
		result.Problems = append(result.Problems, resource.Diagnostic{Level: "warning", Code: "RESOURCE_RENAMED", Message: "资源编号保持不变；已锁定的流程继续使用该资源，请将正文中的旧名称更新为新名称。"})
	}
	a.queueResourcePreviews(library)
	Success(w, result)
}
func (a *API) handleResourceExport(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "resource_export") {
		return
	}
	var request struct {
		Target string `json:"target_path"`
	}
	if !decodeResourceRequest(w, r, &request) {
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		resourceError(w, err)
		return
	}
	path, err := library.Export(r.Context(), mux.Vars(r)["id"], request.Target)
	if err != nil {
		resourceError(w, err)
		return
	}
	Success(w, map[string]string{"path": path})
}
