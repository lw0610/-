package api

import (
	"github.com/gorilla/mux"
	"net/http"
)

func (a *API) handleAdoptFlowResource(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "flow_resource_adopt") {
		return
	}
	var request struct {
		ResourceID string `json:"resource_id"`
		Label      string `json:"label"`
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
		resourceError(w, err)
		return
	}
	item, err := store.AdoptResource(r.Context(), mux.Vars(r)["id"], request.ResourceID, request.Label, library)
	if err != nil {
		flowError(w, err)
		return
	}
	if err = a.decorateFlow(r.Context(), item, r.URL.Query().Get("scheme"), "saved"); err != nil {
		flowError(w, err)
		return
	}
	Success(w, item)
}
