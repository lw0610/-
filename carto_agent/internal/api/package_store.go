package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/autocarto/carto_agent/internal/flow"
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
