package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/autocarto/carto_agent/internal/logger"
	bridgeprovider "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"github.com/gorilla/mux"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func (a *API) setupSessionFileRoutes(r *mux.Router) {
	r.HandleFunc("/sessions/{id}/files", a.handleListSessionFiles).Methods("GET", "OPTIONS")
	r.HandleFunc("/sessions/{id}/files/{file_id}/content", a.handleSessionFileContent).Methods("GET", "OPTIONS")
	for _, action := range []string{"inspect", "thumbnail", "load"} {
		r.HandleFunc("/sessions/{id}/files/{file_id}/"+action, a.handleSessionFileAction).Methods("POST", "OPTIONS")
	}
}
func sessionFileHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("Cache-Control", "private")
	w.Header().Set("Access-Control-Expose-Headers", "X-Preview-Truncated, Content-Disposition")
}
func (a *API) sessionFileStore(w http.ResponseWriter, r *http.Request) (*sessionfiles.Store, map[string]any) {
	sessionFileHeaders(w)
	if a.sessionService == nil {
		Error(w, 503, "会话服务未初始化")
		return nil, nil
	}
	id, project := mux.Vars(r)["id"], r.URL.Query().Get("project_path")
	// Validate identity before opening the existing session; no arbitrary path endpoint.
	store, err := sessionfiles.New(project, id)
	if err != nil {
		Error(w, 400, err.Error())
		return nil, nil
	}
	session, err := a.sessionService.LoadSession(id, project)
	if err != nil {
		Error(w, 404, "会话不存在")
		return nil, nil
	}
	return store, session
}
func (a *API) handleListSessionFiles(w http.ResponseWriter, r *http.Request) {
	store, session := a.sessionFileStore(w, r)
	if store == nil {
		return
	}
	warnings := []string{}
	for _, err := range store.RecoverToolArtifacts(session) {
		logger.Warn("会话产物补登记失败: session_id=%s %v", store.SessionID, err)
		warnings = append(warnings, err.Error())
	}
	items, err := store.List(r.URL.Query().Get("all") == "1")
	if err != nil {
		Error(w, 500, "读取会话文件失败："+err.Error())
		return
	}
	Success(w, map[string]any{"items": items, "session_dir": store.Dir, "warnings": warnings})
}
func indexedSessionFile(w http.ResponseWriter, r *http.Request, store *sessionfiles.Store) (sessionfiles.Item, bool) {
	item, err := store.Get(mux.Vars(r)["file_id"])
	if err != nil {
		Error(w, 404, "文件不存在或已不在会话允许目录中")
		return item, false
	}
	return item, true
}
func utf8Text(data []byte) []byte {
	if utf8.Valid(data) {
		return bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	}
	decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(data)
	if err == nil {
		return decoded
	}
	return []byte(strings.ToValidUTF8(string(data), "�"))
}
func (a *API) handleSessionFileContent(w http.ResponseWriter, r *http.Request) {
	store, _ := a.sessionFileStore(w, r)
	if store == nil {
		return
	}
	item, ok := indexedSessionFile(w, r, store)
	if !ok {
		return
	}
	ext := strings.ToLower(filepath.Ext(item.Path))
	contentType := ""
	limit := int64(50 << 20)
	text := false
	switch ext {
	case ".png":
		contentType = "image/png"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".webp":
		contentType = "image/webp"
	case ".gif":
		contentType = "image/gif"
	case ".svg":
		contentType = "image/svg+xml"
	case ".pdf":
		contentType = "application/pdf"
	case ".csv", ".tsv":
		contentType = "text/csv; charset=utf-8"
		limit = 5 << 20
	case ".txt", ".md", ".json", ".geojson", ".yaml", ".yml", ".xml", ".qml", ".qpt", ".log":
		contentType = "text/plain; charset=utf-8"
		limit = 1 << 20
		text = true
	case ".zip", ".xlsx":
		contentType = "application/json"
	default:
		Error(w, 415, "此文件类型不支持内容预览")
		return
	}
	if item.Kind == "folder" {
		Error(w, 415, "目录不能作为文件预览")
		return
	}
	if item.Size > limit && !text {
		Error(w, 413, "文件超过预览大小限制，请使用系统程序打开")
		return
	}
	if r.URL.Query().Get("download") == "1" {
		if item.Size > 50<<20 {
			Error(w, 413, "文件超过下载大小限制")
			return
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": item.Name}))
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeFile(w, r, item.Path)
		return
	}
	if ext == ".zip" || ext == ".xlsx" {
		archivePreview(w, item.Path, ext == ".xlsx")
		return
	}
	f, err := os.Open(item.Path)
	if err != nil {
		Error(w, 404, "文件无法读取")
		return
	}
	defer f.Close()
	if text || ext == ".csv" || ext == ".tsv" {
		data, e := io.ReadAll(io.LimitReader(f, limit))
		if e != nil {
			Error(w, 500, "文件读取失败")
			return
		}
		data = utf8Text(data)
		if item.Size > limit {
			w.Header().Set("X-Preview-Truncated", "1")
		}
		if ext == ".csv" || ext == ".tsv" {
			reader := csv.NewReader(bytes.NewReader(data))
			reader.FieldsPerRecord = -1
			if ext == ".tsv" {
				reader.Comma = '\t'
			}
			rows := [][]string{}
			truncated := false
			for i := 0; i < 202; i++ {
				row, e := reader.Read()
				if e == io.EOF {
					break
				}
				if e != nil {
					Error(w, 422, "表格格式不正确："+e.Error())
					return
				}
				if i == 201 {
					truncated = true
					break
				}
				rows = append(rows, row)
			}
			Success(w, map[string]any{"rows": rows, "truncated": truncated})
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(data)
		return
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, item.Name, item.ModifiedAt, f)
}
func archivePreview(w http.ResponseWriter, path string, xlsx bool) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		Error(w, 422, "压缩文件无法读取")
		return
	}
	defer archive.Close()
	entries := []map[string]any{}
	sheets := []string{}
	for i, file := range archive.File {
		if i < 1000 {
			entries = append(entries, map[string]any{"name": file.Name, "size": file.UncompressedSize64})
		}
		if xlsx && file.Name == "xl/workbook.xml" && file.UncompressedSize64 <= 1<<20 {
			f, e := file.Open()
			if e != nil {
				continue
			}
			raw, e := io.ReadAll(io.LimitReader(f, 1<<20))
			f.Close()
			if e != nil {
				continue
			}
			var book struct {
				Sheets []struct {
					Name string `xml:"name,attr"`
				} `xml:"sheets>sheet"`
			}
			if xml.Unmarshal(raw, &book) == nil {
				for _, sheet := range book.Sheets {
					sheets = append(sheets, sheet.Name)
				}
			}
		}
	}
	Success(w, map[string]any{"entries": entries, "sheets": sheets, "truncated": len(archive.File) > 1000})
}

var sessionFileInspectionCache sync.Map

func sessionFileInstance(session map[string]any) string {
	id, _ := session["bound_instance_id"].(string)
	return strings.TrimSpace(id)
}
func (a *API) handleSessionFileAction(w http.ResponseWriter, r *http.Request) {
	store, session := a.sessionFileStore(w, r)
	if store == nil {
		return
	}
	item, ok := indexedSessionFile(w, r, store)
	if !ok {
		return
	}
	action := filepath.Base(r.URL.Path)
	if item.Kind != "vector" && item.Kind != "raster" && item.Kind != "table" {
		Error(w, 415, "此操作只支持地理数据文件")
		return
	}
	instance := sessionFileInstance(session)
	if instance == "" || a.localBridgeProvider == nil || !a.localBridgeProvider.HasInstanceConnected(instance) {
		Error(w, 503, "连接此会话绑定的 QGIS 后可查看数据信息或加载数据")
		return
	}
	toolName := map[string]string{"inspect": "geodata_inspect", "thumbnail": "geodata_thumbnail", "load": "data_load_vector"}[action]
	if action == "load" && item.Kind == "raster" {
		toolName = "data_load_raster"
	}
	if toolName == "" {
		Error(w, 400, "未知文件操作")
		return
	}
	if err := a.authorizeInstanceToolInvoke(r.Context(), toolName, "session_files", instance); err != nil {
		Error(w, 403, "当前方案不允许此文件操作："+err.Error())
		return
	}
	key := fmt.Sprintf("%s:%s:%s:%d:%d", instance, item.Path, action, item.ModifiedAt.UnixNano(), item.Size)
	params := map[string]any{"instance_id": instance, "path": item.Path, "reuse_existing": true, "sample_limit": 5}
	var request struct {
		LayerName string `json:"layer_name"`
	}
	if r.Body != nil {
		err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&request)
		if err != nil && err != io.EOF {
			Error(w, 400, "文件操作参数无效")
			return
		}
	}
	if request.LayerName != "" {
		params["layer_name"] = request.LayerName
		key += ":" + request.LayerName
	}
	if action == "inspect" {
		if cached, ok := sessionFileInspectionCache.Load(key); ok {
			Success(w, cached)
			return
		}
	}
	previewPath := ""
	if action == "thumbnail" {
		dir := filepath.Join(store.Dir, "temp", "previews")
		if err := os.MkdirAll(dir, 0700); err != nil {
			Error(w, 500, "缩略图目录不可用")
			return
		}
		if _, _, err := store.ValidatePath(dir); err != nil {
			Error(w, 403, "缩略图目录超出允许目录")
			return
		}
		// The renderer requires a new file; a cached registered thumbnail is reused.
		if cached, ok := sessionFileInspectionCache.Load(key); ok {
			if id, ok := cached.(string); ok {
				if preview, e := store.Get(id); e == nil {
					Success(w, sessionThumbnailResponse(r, preview))
					return
				}
			}
		}
		previewPath = filepath.Join(dir, fmt.Sprintf("%s-%d.png", item.ID, time.Now().UnixNano()))
		params["output_path"] = previewPath
		params["size"] = 512
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	response, err := bridgeprovider.NewComputeToolInvoker(a.localBridgeProvider).InvokeTool(ctx, toolName, params)
	if err != nil {
		Error(w, 502, "文件操作失败："+err.Error())
		return
	}
	if response == nil || !response.Success {
		message := "QGIS 文件操作失败"
		if response != nil && response.Error != "" {
			message += "：" + response.Error
		}
		Error(w, 502, message)
		return
	}
	result := response.Output
	if object, ok := result.(map[string]any); ok {
		if success, ok := object["success"].(bool); ok && !success {
			Error(w, 422, fmt.Sprint(object["error"]))
			return
		}
		if data, ok := object["data"]; ok {
			result = data
		}
	}
	if action == "thumbnail" {
		preview, err := store.Register(previewPath, sessionfiles.Origin{Type: "preview", Label: "数据缩略图"}, sessionfiles.WithGroup("other"))
		if err != nil {
			Error(w, 500, "缩略图未生成或路径无效")
			return
		}
		sessionFileInspectionCache.Store(key, preview.ID)
		Success(w, sessionThumbnailResponse(r, preview))
		return
	}
	if action == "inspect" {
		sessionFileInspectionCache.Store(key, result)
	}
	Success(w, result)
}
func sessionThumbnailResponse(r *http.Request, item sessionfiles.Item) map[string]any {
	return map[string]any{"file_id": item.ID, "url": "/api/sessions/" + url.PathEscape(mux.Vars(r)["id"]) + "/files/" + item.ID + "/content?project_path=" + url.QueryEscape(r.URL.Query().Get("project_path"))}
}
