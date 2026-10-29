package api

import (
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"strings"

	"github.com/autocarto/carto_agent/internal/compute"
	bridgeprovider "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/gorilla/mux"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var resourcePreviewWorkers sync.Map

func (a *API) renderResourcePreview(ctx context.Context, r *resource.ResolvedResource, target string) error {
	if a.localBridgeProvider == nil {
		return fmt.Errorf("QGIS 尚未连接")
	}
	ids := a.localBridgeProvider.ConnectedInstanceIDs()
	if len(ids) == 0 {
		return fmt.Errorf("QGIS 尚未连接")
	}
	params := map[string]any{"instance_id": ids[0], "resource_path": r.Entry, "resource_type": r.Type, "output_path": target, "label_field": r.LabelField, "geometry": r.Geometry, "name": r.Name}
	response, err := bridgeprovider.NewComputeToolInvoker(a.localBridgeProvider).InvokeTool(ctx, "resource_preview", params)
	if err != nil {
		return err
	}
	return resourcePreviewResultError(response)
}

func resourcePreviewResultError(response *compute.InvokeResponse) error {
	if response == nil {
		return fmt.Errorf("QGIS 未返回资源预览结果")
	}
	var payload map[string]any
	if raw, ok := response.Output.(string); ok {
		_ = json.Unmarshal([]byte(raw), &payload)
	} else if raw, err := json.Marshal(response.Output); err == nil {
		_ = json.Unmarshal(raw, &payload)
	}
	innerSuccess, hasInnerSuccess := payload["success"].(bool)
	if response.Success && (!hasInnerSuccess || innerSuccess) {
		return nil
	}
	for _, value := range []any{response.Error, payload["info"], payload["error"]} {
		if message, ok := value.(string); ok && strings.TrimSpace(message) != "" {
			return fmt.Errorf("资源预览失败：%s", message)
		}
	}
	return fmt.Errorf("QGIS 未能生成资源预览")
}

// The client selects a registered resource and a layer ID only. Paths and
// the hidden renderer are chosen here, and the image is never saved as the
// resource's default preview or applied to the user's layer.
func (a *API) handleResourceLayerPreview(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "resource_layer_preview") {
		return
	}
	var request struct {
		InstanceID string `json:"instance_id"`
		LayerID    string `json:"layer_id"`
	}
	if !decodeResourceRequest(w, r, &request) {
		return
	}
	request.InstanceID = strings.TrimSpace(request.InstanceID)
	request.LayerID = strings.TrimSpace(request.LayerID)
	if request.InstanceID == "" || request.LayerID == "" {
		Error(w, http.StatusBadRequest, "请选择已连接的 QGIS 工程和用于预览的图层")
		return
	}
	// Reuse the public read-layer permission; resource_preview intentionally
	// remains hidden from general tool discovery and arbitrary invocation.
	if !a.resourceInstanceReadyFor(w, r, request.InstanceID, "get_layers_info", "resource_layer_preview") {
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		resourceError(w, err)
		return
	}
	item, err := library.GetByID(mux.Vars(r)["id"])
	if err != nil {
		resourceError(w, err)
		return
	}
	if item.Type != "style" || resource.HasErrors(item.Problems) {
		Error(w, http.StatusBadRequest, "请选择有效的图层样式")
		return
	}
	stage, cleanup, err := library.NewStaging()
	if err != nil {
		resourceError(w, err)
		return
	}
	defer cleanup()
	target := filepath.Join(stage, "layer-preview.png")
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	response, err := bridgeprovider.NewComputeToolInvoker(a.localBridgeProvider).InvokeTool(ctx, "resource_preview", map[string]any{
		"instance_id": request.InstanceID, "layer_id": request.LayerID,
		"resource_path": item.Entry, "resource_type": "style", "output_path": target,
		"label_field": item.LabelField, "geometry": item.Geometry, "name": item.Name,
	})
	if err == nil {
		err = resourcePreviewResultError(response)
	}
	if err != nil {
		ErrorWithCode(w, http.StatusBadGateway, "RESOURCE_LAYER_PREVIEW_FAILED", err.Error())
		return
	}
	info, err := os.Lstat(target)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 10<<20 {
		Error(w, http.StatusBadGateway, "QGIS 未返回有效的预览图片")
		return
	}
	file, err := os.Open(target)
	if err != nil {
		Error(w, http.StatusBadGateway, "预览图片读取失败")
		return
	}
	defer file.Close()
	if _, err = png.DecodeConfig(file); err != nil {
		Error(w, http.StatusBadGateway, "QGIS 返回的预览不是有效 PNG 图片")
		return
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		Error(w, http.StatusBadGateway, "预览图片读取失败")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, file)
}

// One worker per library waits for a QGIS connection. Rendering runs outside
// the HTTP request and never mutates the user's active project.
func (a *API) queueResourcePreviews(library *resource.Library) {
	if a.localBridgeProvider == nil {
		return
	}
	if _, loaded := resourcePreviewWorkers.LoadOrStore(library, true); loaded {
		return
	}
	go func() {
		defer resourcePreviewWorkers.Delete(library)
		failures := map[string]bool{}
		for {
			items, err := library.List("all")
			if err != nil {
				return
			}
			pending := []resource.LibraryItem{}
			for _, item := range items {
				_, ready, err := library.PreviewPath(item.ID)
				if err == nil && !ready && !failures[item.ID+item.Digest] {
					pending = append(pending, item)
				}
			}
			if len(pending) == 0 {
				return
			}
			if len(a.localBridgeProvider.ConnectedInstanceIDs()) == 0 {
				time.Sleep(5 * time.Second)
				continue
			}
			for _, item := range pending {
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				_, err := library.GeneratePreview(ctx, item.ID, a.renderResourcePreview, false)
				cancel()
				if err != nil {
					if len(a.localBridgeProvider.ConnectedInstanceIDs()) == 0 {
						break
					}
					failures[item.ID+item.Digest] = true
				}
			}
		}
	}()
}
func (a *API) handleResourcePreview(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "resource_preview") {
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		resourceError(w, err)
		return
	}
	vars := mux.Vars(r)
	id := vars["id"]
	if id == "" {
		resolved, err := library.Get(vars["type"], vars["name"])
		if err != nil {
			resourceError(w, err)
			return
		}
		id = resolved.ID
	}
	path, ready, err := library.PreviewPath(id)
	if err != nil {
		resourceError(w, err)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	if !ready {
		a.queueResourcePreviews(library)
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("X-Preview-Status", "pending")
		fmt.Fprint(w, `<svg xmlns="http://www.w3.org/2000/svg" width="480" height="270" viewBox="0 0 480 270"><rect width="480" height="270" fill="#f1f5f9"/><path d="M170 165l48-54 34 32 35-50 30 72z" fill="#cbd5e1"/><text x="240" y="210" text-anchor="middle" fill="#64748b" font-size="16">连接 QGIS 后生成预览</text></svg>`)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		resourceError(w, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() > 10<<20 {
		Error(w, 400, "预览文件无效")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Preview-Status", "ready")
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), file)
}
func (a *API) handleRegenerateResourcePreview(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "resource_preview_regenerate") {
		return
	}
	library, err := a.resourceStore()
	if err != nil {
		resourceError(w, err)
		return
	}
	id := mux.Vars(r)["id"]
	if a.localBridgeProvider == nil || len(a.localBridgeProvider.ConnectedInstanceIDs()) == 0 {
		item, err := library.QueuePreview(id)
		if err != nil {
			resourceError(w, err)
			return
		}
		item.PreviewStatus = "pending"
		a.queueResourcePreviews(library)
		Success(w, item)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	item, err := library.GeneratePreview(ctx, id, a.renderResourcePreview, true)
	if err != nil {
		resourceError(w, err)
		return
	}
	Success(w, item)
}
