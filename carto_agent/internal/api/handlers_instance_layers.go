package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	bridgeprovider "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/gorilla/mux"
)

type instanceLayerItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type,omitempty"`
	Visible *bool  `json:"visible,omitempty"`
}

// handleGetInstanceLayers exposes only the read-only get_layers_info projection.
func (a *API) handleGetInstanceLayers(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "instance_layers") {
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])
	if id == "" {
		ErrorWithCode(w, http.StatusBadRequest, errCodeInstanceToolInvokeInvalid, "实例 ID 不能为空")
		return
	}
	instance, err := a.loadInstance(r.Context(), id)
	if err != nil {
		if a.instanceRegistry == nil {
			ErrorWithCode(w, http.StatusServiceUnavailable, errCodeInstanceRegistryUnavailable, "实例注册表未初始化")
		} else {
			ErrorWithCode(w, http.StatusNotFound, errCodeInstanceNotFound, "实例不存在: "+id)
		}
		return
	}
	if instance.Status != "running" || a.localBridgeProvider == nil || !a.localBridgeProvider.HasInstanceConnected(id) {
		ErrorWithCode(w, http.StatusServiceUnavailable, errCodeInstanceToolInvokerMissing, "实例图层查询连接未就绪")
		return
	}
	if err := a.authorizeInstanceToolInvoke(r.Context(), "get_layers_info", "instance_layers", id); err != nil {
		var denied *instanceToolInvokeDeniedError
		if errors.As(err, &denied) {
			ErrorWithCode(w, http.StatusForbidden, errCodeInstanceToolInvokeForbidden, denied.Error())
		} else {
			ErrorWithCode(w, http.StatusServiceUnavailable, errCodeInstanceToolInvokerMissing, "实例图层查询策略未就绪")
		}
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	response, err := bridgeprovider.NewComputeToolInvoker(a.localBridgeProvider).InvokeTool(ctx, "get_layers_info", map[string]any{
		"instance_id": id, "include_invisible": true,
	})
	if err != nil {
		ErrorWithCodeAndDetails(w, http.StatusBadGateway, errCodeInstanceToolInvokeFailed, "读取实例图层失败: "+err.Error(), bridgeErrorDetailsFromError(err))
		return
	}
	if response == nil || !response.Success {
		message := "读取实例图层失败"
		if response != nil && strings.TrimSpace(response.Error) != "" {
			message += ": " + strings.TrimSpace(response.Error)
		}
		ErrorWithCodeAndDetails(w, http.StatusBadGateway, errCodeInstanceToolInvokeFailed, message, bridgeErrorDetailsFromInvokeResponse(response))
		return
	}
	items, err := extractInstanceLayerItems(response.Output)
	if err != nil {
		ErrorWithCode(w, http.StatusBadGateway, "INSTANCE_LAYER_RESULT_INVALID", "实例未返回有效图层目录: "+err.Error())
		return
	}
	Success(w, map[string]any{"items": items, "instance_id": id})
}

func extractInstanceLayerItems(value any) ([]instanceLayerItem, error) {
	var encoded []byte
	var err error
	if text, ok := value.(string); ok {
		encoded = []byte(text)
	} else {
		encoded, err = json.Marshal(value)
		if err != nil {
			return nil, err
		}
	}
	var normalized any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return nil, fmt.Errorf("图层结果不是 JSON 对象")
	}
	return layerItemsAtAllowedPath(normalized, 0)
}

// Only documented result envelopes are followed. In particular, fields.name,
// display text and arbitrary nested objects can never become layer records.
func layerItemsAtAllowedPath(value any, depth int) ([]instanceLayerItem, error) {
	if depth > 8 {
		return nil, fmt.Errorf("图层结果包裹层级过深")
	}
	if text, ok := value.(string); ok {
		var decoded any
		if json.Unmarshal([]byte(text), &decoded) != nil {
			return nil, fmt.Errorf("图层结果不是有效 JSON")
		}
		return layerItemsAtAllowedPath(decoded, depth+1)
	}
	if entries, ok := value.([]any); ok && depth > 0 {
		return projectLayerItems(entries)
	}
	payload, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("缺少图层目录")
	}
	if success, ok := payload["success"].(bool); ok && !success {
		return nil, fmt.Errorf("图层工具执行未成功")
	}
	if status, _ := payload["status"].(string); status == "error" || status == "failed" {
		return nil, fmt.Errorf("图层工具执行未成功")
	}
	if info, ok := payload["info"].(map[string]any); ok {
		if layers, exists := info["layers"]; exists {
			entries, ok := layers.([]any)
			if !ok {
				return nil, fmt.Errorf("info.layers 不是数组")
			}
			return projectLayerItems(entries)
		}
	}
	if layers, exists := payload["layers"]; exists {
		entries, ok := layers.([]any)
		if !ok {
			return nil, fmt.Errorf("layers 不是数组")
		}
		return projectLayerItems(entries)
	}
	for _, key := range []string{"data", "result"} {
		if nested, exists := payload[key]; exists {
			return layerItemsAtAllowedPath(nested, depth+1)
		}
	}
	return nil, fmt.Errorf("缺少 info.layers/data/result 图层目录")
}

func projectLayerItems(entries []any) ([]instanceLayerItem, error) {
	items := make([]instanceLayerItem, 0, len(entries))
	seen := map[string]bool{}
	for _, entry := range entries {
		layer, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("图层记录不是对象")
		}
		id, _ := layer["id"].(string)
		name, _ := layer["name"].(string)
		if strings.TrimSpace(id) == "" || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("图层记录缺少 ID 或名称")
		}
		if !seen[id] {
			item := instanceLayerItem{ID: id, Name: name}
			if typ, ok := layer["type"].(string); ok {
				item.Type = typ
			}
			if visible, ok := layer["visible"].(bool); ok {
				item.Visible = &visible
			}
			items = append(items, item)
			seen[id] = true
		}
	}
	return items, nil
}
