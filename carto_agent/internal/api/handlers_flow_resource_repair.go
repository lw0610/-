package api

import (
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/gorilla/mux"
	"net/http"
)

func (a *API) handleRepairFlowResource(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "flow_resource_repair") {
		return
	}
	var request struct {
		Action      string `json:"action"`
		ResourceKey string `json:"resource_key"`
		ResourceID  string `json:"resource_id"`
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
	var item *flow.StoredFlow
	if request.Action == "convert_to_input" {
		var memory *flow.InputMemory
		memory, err = flow.DefaultInputMemory()
		if err == nil {
			item, err = store.ConvertResourceToInput(mux.Vars(r)["id"], request.ResourceKey, library, memory)
		}
	} else {
		item, err = store.RepairResource(r.Context(), mux.Vars(r)["id"], request.Action, request.ResourceKey, request.ResourceID, library)
	}
	if err != nil {
		flowError(w, err)
		return
	}
	if err = a.relinkFlow(r.Context(), item, r.URL.Query().Get("scheme")); err != nil {
		flowError(w, err)
		return
	}
	Success(w, item)
}
