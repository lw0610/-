package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	bridgeprovider "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/gorilla/mux"
)

func (a *API) resourceInstanceReady(w http.ResponseWriter, r *http.Request, instanceID, toolName string) bool {
	if instanceID == "" {
		ErrorWithCode(w, 400, errCodeInstanceToolInvokeInvalid, "请选择 QGIS 工程")
		return false
	}
	if a.localBridgeProvider == nil || !a.localBridgeProvider.HasInstanceConnected(instanceID) {
		ErrorWithCode(w, 503, errCodeInstanceToolInvokerMissing, "QGIS 工程尚未连接")
		return false
	}
	if err := a.authorizeInstanceToolInvoke(r.Context(), toolName, "resource_capture", instanceID); err != nil {
		var denied *instanceToolInvokeDeniedError
		if errors.As(err, &denied) {
			ErrorWithCode(w, 403, errCodeInstanceToolInvokeForbidden, denied.Error())
		} else {
			ErrorWithCode(w, 503, errCodeInstanceToolInvokerMissing, "工具授权策略无法读取")
		}
		return false
	}
	return true
}
func (a *API) handleGetInstanceLayouts(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "instance_layouts") {
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])
	if !a.resourceInstanceReady(w, r, id, "layout_list") {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	response, err := bridgeprovider.NewComputeToolInvoker(a.localBridgeProvider).InvokeTool(ctx, "layout_list", map[string]any{"instance_id": id})
	if err != nil {
		ErrorWithCode(w, 502, errCodeInstanceToolInvokeFailed, "读取版面失败："+err.Error())
		return
	}
	if response == nil || !response.Success {
		ErrorWithCode(w, 502, errCodeInstanceToolInvokeFailed, "读取版面失败")
		return
	}
	result := response.Output
	if object, ok := result.(map[string]any); ok {
		if data, ok := object["data"]; ok {
			result = data
		}
	}
	Success(w, result)
}
func (a *API) handleCaptureResource(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "resource_capture") {
		return
	}
	var request struct {
		InstanceID  string   `json:"instance_id"`
		Kind        string   `json:"kind"`
		Name        string   `json:"name"`
		Layout      string   `json:"layout"`
		MainMapItem string   `json:"main_map_item"`
		Layers      []string `json:"layers"`
		ReplaceID   string   `json:"replace_id"`
	}
	if !decodeResourceRequest(w, r, &request) {
		return
	}
	tools := map[string]string{"layout": "layout_save_template", "style": "layer_save_style", "style_set": "style_set_export"}
	toolName := tools[request.Kind]
	if toolName == "" {
		resourceError(w, fmt.Errorf("只能保存版面、样式或样式集"))
		return
	}
	if strings.TrimSpace(request.Name) == "" && request.ReplaceID == "" {
		resourceError(w, fmt.Errorf("请填写保存名称"))
		return
	}
	if request.Kind == "layout" && request.Layout == "" {
		resourceError(w, fmt.Errorf("请选择版面"))
		return
	}
	if request.Kind == "style" && len(request.Layers) != 1 {
		resourceError(w, fmt.Errorf("单图层样式须选择一个图层"))
		return
	}
	if !a.resourceInstanceReady(w, r, strings.TrimSpace(request.InstanceID), toolName) {
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		resourceError(w, err)
		return
	}
	if request.ReplaceID != "" {
		existing, err := library.GetByID(request.ReplaceID)
		if err != nil {
			resourceError(w, err)
			return
		}
		if existing.Owner != "library_user" {
			resourceError(w, resource.ErrReadOnly)
			return
		}
		if existing.Type != request.Kind {
			resourceError(w, fmt.Errorf("替换资源类型与保存种类不一致"))
			return
		}
	}
	stage, cleanup, err := library.NewStaging()
	if err != nil {
		resourceError(w, err)
		return
	}
	defer cleanup()
	params := map[string]any{"instance_id": request.InstanceID}
	source := ""
	switch request.Kind {
	case "layout":
		source = filepath.Join(stage, "layout.qpt")
		params["layout"] = request.Layout
		params["path"] = source
		if request.MainMapItem != "" {
			params["main_map_item"] = request.MainMapItem
		}
	case "style":
		source = filepath.Join(stage, "layer.qml")
		params["layer"] = request.Layers[0]
		params["path"] = source
	case "style_set":
		source = filepath.Join(stage, "styles")
		params["directory"] = source
		if len(request.Layers) > 0 {
			params["layers"] = request.Layers
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	response, err := bridgeprovider.NewComputeToolInvoker(a.localBridgeProvider).InvokeTool(ctx, toolName, params)
	if err != nil {
		ErrorWithCodeAndDetails(w, 502, errCodeInstanceToolInvokeFailed, "从工程保存失败："+err.Error(), bridgeErrorDetailsFromError(err))
		return
	}
	if response == nil || !response.Success {
		message := "从工程保存失败"
		if response != nil && response.Error != "" {
			message += "：" + response.Error
		}
		ErrorWithCodeAndDetails(w, 502, errCodeInstanceToolInvokeFailed, message, bridgeErrorDetailsFromInvokeResponse(response))
		return
	}
	var result *resource.ResolvedResource
	if request.ReplaceID != "" {
		result, err = library.Replace(request.ReplaceID, source)
	} else {
		result, err = library.ImportNamed(source, request.Kind, request.Name)
	}
	if err != nil {
		resourceError(w, err)
		return
	}
	Success(w, result)
}
