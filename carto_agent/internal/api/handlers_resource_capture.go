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
	return a.resourceInstanceReadyFor(w, r, instanceID, toolName, "resource_capture")
}

func (a *API) resourceInstanceReadyFor(w http.ResponseWriter, r *http.Request, instanceID, toolName, endpoint string) bool {
	if instanceID == "" {
		ErrorWithCode(w, 400, errCodeInstanceToolInvokeInvalid, "请选择 QGIS 工程")
		return false
	}
	if a.localBridgeProvider == nil || !a.localBridgeProvider.HasInstanceConnected(instanceID) {
		ErrorWithCode(w, 503, errCodeInstanceToolInvokerMissing, "QGIS 工程尚未连接")
		return false
	}
	if err := a.authorizeInstanceToolInvoke(r.Context(), toolName, endpoint, instanceID); err != nil {
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
	toolName := map[string]string{"layout": "layout_save_template", "style": "layer_save_style"}[request.Kind]
	if toolName == "" {
		resourceError(w, fmt.Errorf("只能保存样式或版面"))
		return
	}
	if request.Kind == "style" && len(request.Layers) == 0 {
		resourceError(w, fmt.Errorf("请选择至少一个图层"))
		return
	}
	if request.Kind == "layout" && request.Layout == "" {
		resourceError(w, fmt.Errorf("请选择版面"))
		return
	}
	if request.ReplaceID != "" && request.Kind == "style" && len(request.Layers) != 1 {
		resourceError(w, fmt.Errorf("替换样式时请选择一个图层"))
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
			resourceError(w, fmt.Errorf("替换资源类型不一致"))
			return
		}
	}
	stage, cleanup, err := library.NewStaging()
	if err != nil {
		resourceError(w, err)
		return
	}
	defer cleanup()
	selected := request.Layers
	if request.Kind == "layout" {
		selected = []string{request.Layout}
	}
	result := &resource.ImportResult{Items: []*resource.ResolvedResource{}, Warnings: []string{}}
	for i, selectedName := range selected {
		params := map[string]any{"instance_id": request.InstanceID}
		source := filepath.Join(stage, fmt.Sprintf("capture-%d.qml", i))
		name := selectedName
		if len(selected) == 1 && strings.TrimSpace(request.Name) != "" {
			name = request.Name
		}
		if request.Kind == "layout" {
			source = filepath.Join(stage, "layout.qpt")
			params["layout"] = selectedName
			if request.MainMapItem != "" {
				params["main_map_item"] = request.MainMapItem
			}
		} else {
			params["layer"] = selectedName
		}
		params["path"] = source
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		response, invokeErr := bridgeprovider.NewComputeToolInvoker(a.localBridgeProvider).InvokeTool(ctx, toolName, params)
		cancel()
		if invokeErr != nil {
			ErrorWithCodeAndDetails(w, 502, errCodeInstanceToolInvokeFailed, "从工程保存失败："+invokeErr.Error(), bridgeErrorDetailsFromError(invokeErr))
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
		if request.Kind == "style" && (len(selected) > 1 || strings.TrimSpace(request.Name) == "") {
			if output, ok := response.Output.(map[string]any); ok {
				if data, ok := output["data"].(map[string]any); ok {
					if layerName, ok := data["layer_name"].(string); ok && strings.TrimSpace(layerName) != "" {
						name = layerName
					}
				}
			}
		}
		var item *resource.ResolvedResource
		if request.ReplaceID != "" {
			item, err = library.Replace(request.ReplaceID, source)
		} else {
			original := name
			for n := 1; n < 10000; n++ {
				item, err = library.ImportNamed(source, request.Kind, name)
				if !errors.Is(err, resource.ErrConflict) || len(selected) == 1 {
					break
				}
				name = fmt.Sprintf("%s（%d）", original, n+1)
			}
			if name != original {
				result.Warnings = append(result.Warnings, original+" 已存在，已保存为 "+name)
			}
		}
		if err != nil {
			resourceError(w, err)
			return
		}
		result.Items = append(result.Items, item)
	}
	a.queueResourcePreviews(library)
	if len(result.Items) == 1 {
		Success(w, result.Items[0])
	} else {
		Success(w, result)
	}
}
