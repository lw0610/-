package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/gorilla/mux"
)

// inputPlan uses existing enrichment while deliberately avoiding saved-source
// writes: input checks may not create or modify flow.lock/cache files.
func (a *API) inputPlan(ctx context.Context, id, scheme string) (*flow.FlowPlan, error) {
	compiler, err := a.FlowCompiler()
	if err != nil {
		return nil, err
	}
	return compiler.PlanReadOnly(ctx, id, scheme)
}
func (a *API) handleCheckFlowInputs(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "flow_inputs_check") {
		return
	}
	var request struct {
		Scheme string         `json:"scheme"`
		Values map[string]any `json:"values"`
	}
	if !decodeFlowRequest(w, r, &request) {
		return
	}
	plan, err := a.inputPlan(r.Context(), mux.Vars(r)["id"], request.Scheme)
	if err != nil {
		flowError(w, err)
		return
	}
	root, err := config.ResolveProjectRoot()
	if err != nil {
		flowError(w, err)
		return
	}
	Success(w, flowrun.ValidateInputs(plan, request.Values, root))
}
func (a *API) handleFlowInputMemory(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "flow_input_memory") {
		return
	}
	id, label := mux.Vars(r)["id"], mux.Vars(r)["label"]
	store, err := a.flowStore()
	if err != nil {
		flowError(w, err)
		return
	}
	item, err := store.Get(id)
	if err != nil {
		flowError(w, err)
		return
	}
	memory, err := flow.DefaultInputMemory()
	if err != nil {
		flowError(w, err)
		return
	}
	if r.Method == http.MethodGet {
		values, err := memory.Get(id)
		if err != nil {
			flowError(w, err)
			return
		}
		Success(w, values)
		return
	}
	if r.Method == http.MethodDelete {
		if err = memory.Set(id, label, ""); err != nil {
			flowError(w, err)
			return
		}
		Success(w, map[string]bool{"deleted": true})
		return
	}
	var request struct {
		Value string `json:"value"`
	}
	if !decodeFlowRequest(w, r, &request) {
		return
	}
	if err = setInputMemoryValue(memory, item, label, request.Value); err != nil {
		flowError(w, err)
		return
	}
	Success(w, map[string]bool{"saved": true})
}
func setInputMemoryValue(memory *flow.InputMemory, item *flow.StoredFlow, label, value string) error {
	doc := flow.Parse(item.FlowMD)
	var typ string
	for _, input := range flow.DeclaredInputs(doc.Inputs) {
		if input.Label == label {
			typ = input.Type
			if !input.Remember && strings.TrimSpace(value) != "" {
				return fmt.Errorf("该输入不允许记住选择")
			}
			break
		}
	}
	if typ != "file" && typ != "folder" {
		return fmt.Errorf("只能记住已声明的文件或文件夹输入")
	}
	path, err := config.ResolveProjectPath(value)
	if err != nil {
		return err
	}
	if strings.TrimSpace(value) == "" {
		return memory.Set(item.Meta.ID, label, "")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("输入路径不存在")
	}
	if typ == "folder" && !info.IsDir() || typ == "file" && !info.Mode().IsRegular() {
		return fmt.Errorf("输入路径类型与声明不一致")
	}
	return memory.Set(item.Meta.ID, label, filepath.Clean(path))
}
