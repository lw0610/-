package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/layers"
	bridgeprovider "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/gorilla/mux"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (a *API) layerStore() (*layers.Library, error) {
	if a.layerLibrary != nil {
		return a.layerLibrary, nil
	}
	return layers.NewDefaultLibrary()
}
func (a *API) SetLayerLibrary(l *layers.Library) { a.layerLibrary = l }
func (a *API) setupLayerRoutes(r *mux.Router) {
	r.HandleFunc("/layers", a.handleLayers).Methods("GET", "OPTIONS")
	r.HandleFunc("/layers/import/scan", a.handleLayerScan).Methods("POST", "OPTIONS")
	r.HandleFunc("/layers/import/confirm", a.handleLayerConfirm).Methods("POST", "OPTIONS")
	r.HandleFunc("/layers/{id}/preview", a.handleLayerPreview).Methods("GET", "POST", "OPTIONS")
	r.HandleFunc("/layers/{id}", a.handleLayer).Methods("GET", "PATCH", "DELETE", "OPTIONS")
}
func layerError(w http.ResponseWriter, e error) {
	status := 400
	if errors.Is(e, layers.ErrNotFound) {
		status = 404
	}
	if errors.Is(e, layers.ErrReadOnly) {
		status = 403
	}
	ErrorWithCode(w, status, "LAYER_REQUEST_INVALID", e.Error())
}
func (a *API) handleLayers(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "layers") {
		return
	}
	l, e := a.layerStore()
	if e != nil {
		layerError(w, e)
		return
	}
	items, e := l.List()
	if e != nil {
		layerError(w, e)
		return
	}
	home, e := config.ResolveAutoCartoHome()
	if e != nil {
		layerError(w, e)
		return
	}
	suggestions, e := l.MigrationSuggestions(home)
	if e != nil {
		layerError(w, e)
		return
	}
	env, _, e := a.dataSourceEnvironment()
	if e != nil {
		layerError(w, e)
		return
	}
	Success(w, map[string]any{"items": items, "categories": env.Catalog.Categories, "migration_suggestions": suggestions})
}

type layerProbe struct {
	DeclaredCRS       string          `json:"declared_crs"`
	CRS               string          `json:"crs"`
	SuggestedCRS      string          `json:"suggested_crs"`
	NeedsConfirmation bool            `json:"needs_confirmation"`
	Messages          []string        `json:"messages"`
	Geometry          string          `json:"geometry"`
	FeatureCount      int64           `json:"feature_count"`
	AvailableFields   []string        `json:"available_fields"`
	Coverage          layers.Coverage `json:"coverage"`
	Scale             layers.Scale    `json:"scale"`
}
type layerDraftEntry struct {
	Manifest          layers.Manifest `json:"manifest"`
	AvailableFields   []string        `json:"available_fields"`
	SuggestedCRS      string          `json:"suggested_crs"`
	NeedsConfirmation bool            `json:"needs_confirmation"`
	Messages          []string        `json:"messages"`
}

func (a *API) invokeLayerTool(ctx context.Context, name string, params map[string]any, dst any) error {
	if a.localBridgeProvider == nil || len(a.localBridgeProvider.ConnectedInstanceIDs()) == 0 {
		return fmt.Errorf("请先连接 QGIS，以读取图层坐标系并生成预览")
	}
	params["instance_id"] = a.localBridgeProvider.ConnectedInstanceIDs()[0]
	response, e := bridgeprovider.NewComputeToolInvoker(a.localBridgeProvider).InvokeTool(ctx, name, params)
	if e != nil {
		return e
	}
	if e = resourcePreviewResultError(response); e != nil {
		return e
	}
	if dst == nil {
		return nil
	}
	var raw []byte
	if s, ok := response.Output.(string); ok {
		raw = []byte(s)
	} else {
		raw, e = json.Marshal(response.Output)
		if e != nil {
			return e
		}
	}
	var result struct {
		Data json.RawMessage `json:"data"`
	}
	if e = json.Unmarshal(raw, &result); e != nil {
		return e
	}
	return json.Unmarshal(result.Data, dst)
}
func (a *API) probeLayer(ctx context.Context, m *layers.Manifest) (layerProbe, error) {
	var p layerProbe
	e := a.invokeLayerTool(ctx, "layer_inspect", map[string]any{"path": m.File, "sublayer": m.Sublayer, "crs": m.CRS}, &p)
	if e != nil {
		return p, e
	}
	m.DeclaredCRS = p.DeclaredCRS
	m.CRS = p.CRS
	m.Geometry = p.Geometry
	m.FeatureCount = p.FeatureCount
	m.Coverage = p.Coverage
	if m.Scale.BestFrom == 0 && m.Scale.BestTo == 0 {
		m.Scale = p.Scale
	}
	return p, nil
}
func (a *API) handleLayerScan(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "layers") {
		return
	}
	var req struct {
		Path string `json:"path"`
		Pack string `json:"pack"`
	}
	if !decodeResourceRequest(w, r, &req) {
		return
	}
	env, _, e := a.dataSourceEnvironment()
	if e != nil {
		layerError(w, e)
		return
	}
	draft, e := env.Catalog.ScanLocalDraft(r.Context(), req.Path)
	if e != nil {
		layerError(w, e)
		return
	}
	l, e := a.layerStore()
	if e != nil {
		layerError(w, e)
		return
	}
	if req.Pack == "" {
		req.Pack = filepath.Base(req.Path)
	}
	entries := []layerDraftEntry{}
	existing, e := l.List()
	if e != nil {
		layerError(w, e)
		return
	}
	names := map[string]bool{}
	for _, i := range existing {
		names[i.Name] = true
	}
	for _, d := range draft.Entries {
		if !d.Exists {
			continue
		}
		path := d.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(draft.Source.Root, path)
		}
		inspectPath := path
		if d.Layer != "" {
			inspectPath += "|layername=" + d.Layer
		}
		headers, e := dataservice.InspectProvided(r.Context(), inspectPath)
		if e != nil {
			draft.Messages = append(draft.Messages, d.Name+"："+e.Error())
			continue
		}
		for _, header := range headers {
			if header.Geometry == "table" {
				continue
			}
			m := layers.Manifest{APIVersion: "layer/v1", ID: layers.NewID(), Name: header.Name, Pack: req.Pack, File: header.Path, Sublayer: header.Sublayer, Geometry: header.Geometry, Category: d.Category, Source: layers.Source{Title: req.Pack, License: "用户提供"}}
			if header.Sublayer != "" {
				m.Name = header.Sublayer
			}
			entry := layerDraftEntry{Manifest: m, AvailableFields: header.Fields, Messages: []string{}}
			probe, e := a.probeLayer(r.Context(), &entry.Manifest)
			if e != nil {
				entry.NeedsConfirmation = true
				entry.Messages = append(entry.Messages, e.Error())
			} else {
				entry.AvailableFields = probe.AvailableFields
				entry.SuggestedCRS = probe.SuggestedCRS
				entry.NeedsConfirmation = probe.NeedsConfirmation
				entry.Messages = append(entry.Messages, probe.Messages...)
			}
			if e = l.Suggest(&entry.Manifest, entry.AvailableFields); e != nil {
				layerError(w, e)
				return
			}
			if names[entry.Manifest.Name] {
				entry.Manifest.Name += "（" + req.Pack + "）"
			}
			names[entry.Manifest.Name] = true
			entries = append(entries, entry)
		}
	}
	Success(w, map[string]any{"path": req.Path, "pack": req.Pack, "entries": entries, "messages": draft.Messages})
}
func (a *API) validateLayerInput(ctx context.Context, l *layers.Library, m *layers.Manifest) error {
	if !filepath.IsAbs(m.File) {
		return fmt.Errorf("图层原文件必须是绝对路径")
	}
	if m.CRS == "" {
		return fmt.Errorf("请确认 %s 的实际坐标系", m.Name)
	}
	p, e := a.probeLayer(ctx, m)
	if e != nil {
		return e
	}
	for _, name := range append([]string{m.LabelField}, mapValues(m.Fields)...) {
		if name == "" || name == "无" {
			continue
		}
		found := false
		for _, f := range p.AvailableFields {
			if name == f {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("%s：字段不存在：%s", m.Name, name)
		}
	}
	return nil
}
func mapValues(values map[string]string) []string {
	out := []string{}
	for _, v := range values {
		out = append(out, v)
	}
	return out
}
func (a *API) renderLayer(ctx context.Context, item layers.Item) error {
	params := map[string]any{"resource_path": item.Path(), "resource_type": "layer", "output_path": item.PreviewPath(), "name": item.Name, "sublayer": item.Sublayer, "crs": item.CRS, "label_field": item.LabelField}
	if item.DefaultStyle != "" {
		library, err := a.resourceStore()
		if err != nil {
			return err
		}
		resolved := library.Resolve(ctx, resource.Ref{Type: "style", Name: item.DefaultStyle}, resource.Scope{})
		handle := resolved.Handle()
		if handle == nil {
			return fmt.Errorf("图层默认样式无法使用：%s：%v", item.DefaultStyle, resolved.Problems)
		}
		params["style"] = handle
	}
	return a.invokeLayerTool(ctx, "resource_preview", params, nil)
}
func (a *API) handleLayerConfirm(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "layers") {
		return
	}
	var req struct {
		Pack    string            `json:"pack"`
		Entries []layers.Manifest `json:"entries"`
	}
	if !decodeResourceRequest(w, r, &req) {
		return
	}
	if len(req.Entries) == 0 {
		layerError(w, fmt.Errorf("请至少选择一个图层"))
		return
	}
	l, e := a.layerStore()
	if e != nil {
		layerError(w, e)
		return
	}
	for n := range req.Entries {
		m := &req.Entries[n]
		if req.Pack != "" {
			m.Pack = req.Pack
		}
		if e = a.validateLayerInput(r.Context(), l, m); e != nil {
			layerError(w, e)
			return
		}
	}
	items, e := l.SaveBatch(req.Entries)
	if e != nil {
		layerError(w, e)
		return
	}
	for n := range items {
		if e = a.renderLayer(r.Context(), items[n]); e != nil {
			items[n].Issues = append(items[n].Issues, "登记已保存；预览未生成："+e.Error())
		} else {
			items[n].PreviewURL = "/api/layers/" + items[n].ID + "/preview"
		}
	}
	a.InvalidateFlowEnvironment()
	Success(w, map[string]any{"items": items})
}
func (a *API) handleLayer(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "layers") {
		return
	}
	l, e := a.layerStore()
	if e != nil {
		layerError(w, e)
		return
	}
	item, e := l.Get(mux.Vars(r)["id"])
	if e != nil {
		layerError(w, e)
		return
	}
	if r.Method == "GET" {
		Success(w, item)
		return
	}
	if r.Method == "DELETE" {
		if e = l.Delete(item.ID); e != nil {
			layerError(w, e)
			return
		}
		a.InvalidateFlowEnvironment()
		Success(w, map[string]bool{"deleted": true})
		return
	}
	if item.Scope == "system" {
		layerError(w, layers.ErrReadOnly)
		return
	}
	var patch map[string]json.RawMessage
	if !decodeResourceRequest(w, r, &patch) {
		return
	}
	allowed := map[string]bool{"name": true, "aliases": true, "label_field": true, "crs": true, "scale": true, "fields": true, "default_style": true, "category": true, "params": true, "source": true, "compliance": true}
	raw, _ := json.Marshal(item.Manifest)
	var merged map[string]json.RawMessage
	_ = json.Unmarshal(raw, &merged)
	for k, v := range patch {
		if !allowed[k] {
			layerError(w, fmt.Errorf("字段不可修改：%s", k))
			return
		}
		merged[k] = v
	}
	raw, _ = json.Marshal(merged)
	var m layers.Manifest
	if e = json.Unmarshal(raw, &m); e != nil {
		layerError(w, e)
		return
	}
	if e = a.validateLayerInput(r.Context(), l, &m); e != nil {
		layerError(w, e)
		return
	}
	item, e = l.Save(m)
	if e != nil {
		layerError(w, e)
		return
	}
	if e = a.renderLayer(r.Context(), item); e != nil {
		item.Issues = append(item.Issues, e.Error())
	} else {
		item.PreviewURL = "/api/layers/" + item.ID + "/preview"
	}
	a.InvalidateFlowEnvironment()
	Success(w, item)
}
func (a *API) handleLayerPreview(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "layers") {
		return
	}
	l, e := a.layerStore()
	if e != nil {
		layerError(w, e)
		return
	}
	item, e := l.Get(mux.Vars(r)["id"])
	if e != nil {
		layerError(w, e)
		return
	}
	if r.Method == "POST" {
		if e = a.renderLayer(r.Context(), item); e != nil {
			layerError(w, e)
			return
		}
		item.PreviewURL = "/api/layers/" + item.ID + "/preview"
		Success(w, item)
		return
	}
	if !strings.HasSuffix(item.PreviewPath(), ".png") {
		http.NotFound(w, r)
		return
	}
	if _, e = os.Stat(item.PreviewPath()); e != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, item.PreviewPath())
}
