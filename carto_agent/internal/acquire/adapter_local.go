package acquire

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type LocalAdapter struct{}

func (LocalAdapter) Kind() string { return "local_folder" }
func (LocalAdapter) Fetch(ctx context.Context, r FetchRequest) (FetchResult, error) {
	if err := ctx.Err(); err != nil {
		return FetchResult{}, err
	}
	if r.Source == nil || r.Provide == nil {
		return FetchResult{}, fmt.Errorf("本机来源缺少访问声明")
	}
	root, err := filepath.EvalSymlinks(r.Source.Root)
	if err != nil {
		return FetchResult{}, err
	}
	path, err := safeStorePath(root, r.Provide.Path)
	if err != nil {
		return FetchResult{}, err
	}
	files := []string{path}
	if strings.EqualFold(filepath.Ext(path), ".shp") {
		stem := strings.TrimSuffix(path, filepath.Ext(path))
		for _, ext := range []string{".shx", ".dbf", ".prj", ".cpg"} {
			side := stem + ext
			if info, err := os.Stat(side); err == nil && info.Mode().IsRegular() {
				files = append(files, side)
			} else if ext == ".shx" || ext == ".dbf" {
				return FetchResult{}, fmt.Errorf("Shapefile 缺少 %s", ext)
			}
		}
	}
	result := FetchResult{Files: files, Format: fileFormat(path), RetrievedAt: time.Now().UTC(), SHA256: map[string]string{}, Notes: []string{"只读使用用户登记的本机数据"}}
	for _, path := range files {
		digest, size, err := fileDigest(path)
		if err != nil {
			return FetchResult{}, err
		}
		result.SHA256[path] = digest
		result.Bytes += size
	}
	return result, nil
}
func fileFormat(path string) string {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".osm.pbf") {
		return "osm_pbf"
	}
	if strings.HasSuffix(lower, ".tar.gz") {
		return "tar.gz"
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".shp":
		return "shapefile"
	case ".tif", ".tiff":
		return "geotiff"
	case ".osm":
		return "osm_xml"
	default:
		return strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	}
}
