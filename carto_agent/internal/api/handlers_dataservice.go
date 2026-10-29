package api

import (
	"errors"
	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/gorilla/mux"
	"net/http"
	"path/filepath"
)

func (a *API) setupDataServiceRoutes(r *mux.Router) {
	r.HandleFunc("/data/find", a.handleDataFind).Methods("POST", "OPTIONS")
	r.HandleFunc("/sessions/{id}/data/find", a.handleDataFind).Methods("POST", "OPTIONS")
	r.HandleFunc("/sessions/{id}/data/find/{find_id}", a.handleDataFind).Methods("GET", "OPTIONS")
	r.HandleFunc("/data-memory", a.handleDataMemory).Methods("GET", "DELETE", "OPTIONS")
}
func dataServiceError(w http.ResponseWriter, err error) {
	var issue *dataservice.Error
	if errors.As(err, &issue) {
		ErrorWithCode(w, http.StatusBadRequest, issue.Code, issue.Message)
		return
	}
	flowError(w, err)
}
func (a *API) handleDataFind(w http.ResponseWriter, r *http.Request) {
	env, _, err := a.dataSourceEnvironment()
	if err != nil {
		dataServiceError(w, err)
		return
	}
	home, err := config.ResolveAutoCartoHome()
	if err != nil {
		dataServiceError(w, err)
		return
	}
	memory, err := flow.DefaultDataMemory()
	if err != nil {
		dataServiceError(w, err)
		return
	}
	cfg := config.AcquisitionConfig{}.WithDefaults()
	if a.configService != nil {
		cfg = a.configService.GetAcquisitionConfig()
	}
	options := dataservice.Options{Environment: env, Memory: memory, Config: cfg, Engine: &acquire.Engine{Catalog: env.Catalog, Store: acquire.NewStore(filepath.Join(home, "data", "acquired"), 0)}}
	id := mux.Vars(r)["id"]
	if id != "" {
		store, _ := a.sessionFileStore(w, r)
		if store == nil {
			return
		}
		options.SessionID = id
		options.SessionDir = store.Dir
		options.UserData, _ = dataservice.LoadBindings(store.Dir)
	}
	service := dataservice.New(options)
	if r.Method == http.MethodGet {
		result, err := service.FindResult(r.Context(), mux.Vars(r)["find_id"])
		if err != nil {
			dataServiceError(w, err)
			return
		}
		Success(w, result)
		return
	}
	var request dataservice.FindRequest
	if !decodeFlowRequest(w, r, &request) {
		return
	}
	result, err := service.Find(r.Context(), request)
	if err != nil {
		dataServiceError(w, err)
		return
	}
	Success(w, result)
}
func (a *API) handleDataMemory(w http.ResponseWriter, r *http.Request) {
	memory, err := flow.DefaultDataMemory()
	if err != nil {
		dataServiceError(w, err)
		return
	}
	if r.Method == http.MethodDelete {
		q := r.URL.Query()
		if q.Get("name") == "" || (q.Get("guide") == "" && q.Get("category") == "") {
			ErrorWithCode(w, 400, "DATA_MEMORY_KEY_INVALID", "请指定要清除的数据记忆")
			return
		}
		err = memory.Forget(flow.DataMemoryEntry{Guide: q.Get("guide"), Name: q.Get("name"), Category: q.Get("category"), Extent: q.Get("extent")})
		if err != nil {
			dataServiceError(w, err)
			return
		}
		Success(w, map[string]bool{"deleted": true})
		return
	}
	entries, err := memory.List()
	if err != nil {
		dataServiceError(w, err)
		return
	}
	Success(w, map[string]any{"entries": entries})
}
