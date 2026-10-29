package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/gorilla/mux"
	"io"
	"net/http"
)

func (a *API) SetGuideStore(store *flow.GuideStore) {
	a.resourceLibraryMu.Lock()
	defer a.resourceLibraryMu.Unlock()
	a.guidePackages = store
	if store != nil {
		a.flowPackages = store.Packages
	}
}
func (a *API) GuideStore() (*flow.GuideStore, error) {
	a.resourceLibraryMu.Lock()
	store := a.guidePackages
	a.resourceLibraryMu.Unlock()
	if store != nil {
		library, err := a.resourceStore()
		if err != nil {
			return nil, err
		}
		store.Packages.SetLibrary(library)
		return store, nil
	}
	library, err := a.resourceStore()
	if err != nil {
		return nil, err
	}
	candidate, err := flow.NewDefaultGuideStore(library)
	if err != nil {
		return nil, err
	}
	a.resourceLibraryMu.Lock()
	defer a.resourceLibraryMu.Unlock()
	if a.guidePackages != nil {
		return a.guidePackages, nil
	}
	if a.flowPackages != nil {
		candidate.Packages = a.flowPackages
	}
	a.guidePackages = candidate
	a.flowPackages = candidate.Packages
	return candidate, nil
}
func (a *API) setupGuideRoutes(r *mux.Router) {
	r.HandleFunc("/guides", a.handleGuides).Methods("GET", "POST", "OPTIONS")
	r.HandleFunc("/guides/import", a.handleImportGuide).Methods("POST", "OPTIONS")
	r.HandleFunc("/guides/{id}/context", a.handleGuideContext).Methods("GET", "OPTIONS")
	r.HandleFunc("/guides/{id}/enabled", a.handleGuideEnabled).Methods("PUT", "OPTIONS")
	r.HandleFunc("/guides/{id}/copy", a.handleCopyGuide).Methods("POST", "OPTIONS")
	r.HandleFunc("/guides/{id}/export", a.handleExportGuide).Methods("POST", "OPTIONS")
	r.HandleFunc("/guides/{id}/resources/repair", a.handleGuideResourceRepair).Methods("POST", "OPTIONS")
	r.HandleFunc("/guides/{id}", a.handleGuide).Methods("GET", "PUT", "DELETE", "OPTIONS")
}
func decodeGuideRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, flow.MaxFileBytes+(1<<20)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		guideError(w, fmt.Errorf("请求格式错误：%w", err))
		return false
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		guideError(w, fmt.Errorf("请求只能包含一个 JSON 对象"))
		return false
	}
	return true
}
func guideError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	code := "GUIDE_INVALID"
	switch {
	case errors.Is(err, flow.ErrNotFound):
		status = http.StatusNotFound
		code = "GUIDE_NOT_FOUND"
	case errors.Is(err, flow.ErrReadOnly):
		status = http.StatusForbidden
		code = "GUIDE_READ_ONLY"
	case errors.Is(err, flow.ErrConflict):
		status = http.StatusConflict
		code = "GUIDE_DUPLICATE_ID"
	}
	ErrorWithCode(w, status, code, err.Error())
}
func (a *API) guideDetail(w http.ResponseWriter, r *http.Request, id string) {
	store, err := a.GuideStore()
	if err != nil {
		guideError(w, err)
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		guideError(w, err)
		return
	}
	snapshot, err := store.Resolve(r.Context(), id, library)
	if err != nil {
		guideError(w, err)
		return
	}
	Success(w, snapshot.Package)
}
func (a *API) handleGuides(w http.ResponseWriter, r *http.Request) {
	s, err := a.GuideStore()
	if err != nil {
		guideError(w, err)
		return
	}
	if r.Method == "GET" {
		items, err := s.List()
		if err != nil {
			guideError(w, err)
			return
		}
		Success(w, items)
		return
	}
	if !ensureHTTPControlContractVersion(w, r, "guide_create") {
		return
	}
	var req struct {
		GuideMD string `json:"guide_md"`
		ID      string `json:"id"`
	}
	if !decodeGuideRequest(w, r, &req) {
		return
	}
	item, err := s.Create(req.GuideMD, req.ID)
	if err != nil {
		guideError(w, err)
		return
	}
	a.guideDetail(w, r, item.Meta.ID)
}
func (a *API) handleGuide(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if r.Method == "GET" {
		a.guideDetail(w, r, id)
		return
	}
	if !ensureHTTPControlContractVersion(w, r, "guide_mutation") {
		return
	}
	s, err := a.GuideStore()
	if err != nil {
		guideError(w, err)
		return
	}
	if r.Method == "DELETE" {
		if err = s.Delete(id); err != nil {
			guideError(w, err)
			return
		}
		Success(w, map[string]bool{"deleted": true})
		return
	}
	var req struct {
		GuideMD string `json:"guide_md"`
	}
	if !decodeGuideRequest(w, r, &req) {
		return
	}
	if _, err = s.Save(id, req.GuideMD); err != nil {
		guideError(w, err)
		return
	}
	a.guideDetail(w, r, id)
}
func (a *API) handleGuideEnabled(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "guide_enabled") {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeGuideRequest(w, r, &req) {
		return
	}
	s, err := a.GuideStore()
	if err != nil {
		guideError(w, err)
		return
	}
	id := mux.Vars(r)["id"]
	if _, err = s.SetEnabled(id, req.Enabled); err != nil {
		guideError(w, err)
		return
	}
	a.guideDetail(w, r, id)
}
func (a *API) handleCopyGuide(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "guide_copy") {
		return
	}
	var req struct {
		ID   string `json:"new_id"`
		Name string `json:"new_name"`
	}
	if !decodeGuideRequest(w, r, &req) {
		return
	}
	s, err := a.GuideStore()
	if err != nil {
		guideError(w, err)
		return
	}
	item, err := s.Copy(mux.Vars(r)["id"], req.ID, req.Name)
	if err != nil {
		guideError(w, err)
		return
	}
	a.guideDetail(w, r, item.Meta.ID)
}
func (a *API) handleImportGuide(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "guide_import") {
		return
	}
	var req struct {
		Source    string `json:"source_path"`
		Overwrite bool   `json:"overwrite"`
		NewID     string `json:"new_id"`
	}
	if !decodeGuideRequest(w, r, &req) {
		return
	}
	s, err := a.GuideStore()
	if err != nil {
		guideError(w, err)
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		guideError(w, err)
		return
	}
	item, err := s.Import(req.Source, flow.ImportOptions{Overwrite: req.Overwrite, NewID: req.NewID}, library)
	if err != nil {
		guideError(w, err)
		return
	}
	snapshot, err := s.Resolve(r.Context(), item.Meta.ID, library)
	if err != nil {
		guideError(w, err)
		return
	}
	Success(w, map[string]any{"guide": snapshot.Package, "report": item.ConversionReport})
}
func (a *API) handleExportGuide(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "guide_export") {
		return
	}
	var req struct {
		Target string `json:"target_path"`
		Data   bool   `json:"include_data_files"`
		Deps   *bool  `json:"include_library_deps"`
	}
	if !decodeGuideRequest(w, r, &req) {
		return
	}
	s, err := a.GuideStore()
	if err != nil {
		guideError(w, err)
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		guideError(w, err)
		return
	}
	id := mux.Vars(r)["id"]
	if _, _, _, _, err = s.Packages.ResolveResources(r.Context(), id, library); err != nil {
		guideError(w, err)
		return
	}
	path, err := s.Packages.ExportWithOptions(id, req.Target, flow.ExportOptions{IncludeDataFiles: req.Data, IncludeLibraryDeps: req.Deps == nil || *req.Deps, Library: library})
	if err != nil {
		guideError(w, err)
		return
	}
	Success(w, map[string]string{"zip_path": path})
}
func (a *API) handleGuideResourceRepair(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "guide_resource_repair") {
		return
	}
	var req struct {
		Action string `json:"action"`
		Key    string `json:"resource_key"`
		ID     string `json:"resource_id"`
	}
	if !decodeGuideRequest(w, r, &req) {
		return
	}
	s, err := a.GuideStore()
	if err != nil {
		guideError(w, err)
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		guideError(w, err)
		return
	}
	id := mux.Vars(r)["id"]
	if _, err = s.Packages.RepairResource(r.Context(), id, req.Action, req.Key, req.ID, library); err != nil {
		guideError(w, err)
		return
	}
	a.guideDetail(w, r, id)
}
func (a *API) handleGuideContext(w http.ResponseWriter, r *http.Request) {
	s, err := a.GuideStore()
	if err != nil {
		guideError(w, err)
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		guideError(w, err)
		return
	}
	content, err := s.ReadContext(r.Context(), mux.Vars(r)["id"], library)
	if err != nil {
		guideError(w, err)
		return
	}
	Success(w, map[string]string{"content": content})
}
