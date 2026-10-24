package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"github.com/gorilla/mux"
)

// Preview only serves a resolved library document or a numbered symbol entry.
// It never accepts a caller-provided filesystem path.
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
	var resolved *resource.ResolvedResource
	if vars["id"] != "" {
		resolved, err = library.GetByID(vars["id"])
	} else {
		resolved, err = library.Get(vars["type"], vars["name"])
	}
	if err != nil {
		resourceError(w, err)
		return
	}
	if resource.HasErrors(resolved.Problems) {
		Error(w, 400, "资源校验未通过，无法预览")
		return
	}
	path := resolved.Entry
	if resolved.Type == "symbols" {
		index, parseErr := strconv.Atoi(r.URL.Query().Get("entry"))
		if parseErr != nil || index < 0 || index >= len(resolved.Entries) {
			Error(w, 400, "符号条目不存在")
			return
		}
		path = resolved.Entries[index].Path
		root := resolved.Root
		if info, statErr := os.Stat(root); statErr != nil {
			resourceError(w, statErr)
			return
		} else if !info.IsDir() {
			root = filepath.Dir(root)
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			Error(w, 400, "符号路径越界")
			return
		}
	} else if resolved.Type != "doc" {
		Error(w, 400, "此类资源仅提供文件信息预览")
		return
	}
	if err = bindings.RejectSymlinks(path); err != nil {
		resourceError(w, err)
		return
	}
	types := map[string]string{".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif", ".md": "text/plain; charset=utf-8", ".txt": "text/plain; charset=utf-8", ".pdf": "application/pdf"}
	contentType := types[strings.ToLower(filepath.Ext(path))]
	if contentType == "" || (resolved.Type == "symbols" && !strings.HasPrefix(contentType, "image/")) {
		Error(w, 400, "不支持预览该文件类型")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		resourceError(w, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 10<<20 {
		Error(w, 400, "预览只接受不超过10MB的普通文件")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", "preview"+filepath.Ext(path)))
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), file)
}
