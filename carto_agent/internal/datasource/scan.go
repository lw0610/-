package datasource

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/resource"
)

type LocalDraft struct {
	Source   Source       `json:"source"`
	Entries  []DraftEntry `json:"entries"`
	Messages []string     `json:"messages"`
}
type DraftEntry struct {
	Name     string             `json:"name"`
	Path     string             `json:"path"`
	Layer    string             `json:"layer,omitempty"`
	Kind     string             `json:"kind"`
	Exists   bool               `json:"exists"`
	Category string             `json:"category"`
	Fields   map[string]Strings `json:"fields"`
	Params   map[string]string  `json:"params,omitempty"`
}

func NewUserID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return "user." + hex.EncodeToString(b[:])
}

// ScanLocalDraft reuses the authoritative folder/CSV/XLSX resource resolver. It
// suggests catalog aliases only; the caller must confirm every selected category.
func (c *Catalog) ScanLocalDraft(ctx context.Context, path string) (LocalDraft, error) {
	result := LocalDraft{Entries: []DraftEntry{}, Messages: []string{}}
	if !filepath.IsAbs(path) {
		return result, fmt.Errorf("请选择本机文件夹或数据清单的绝对路径")
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return result, err
	}
	path = resolvedPath
	info, err := os.Stat(path)
	if err != nil {
		return result, err
	}
	kind, base := "data_folder", path
	if !info.IsDir() {
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".csv" && ext != ".xlsx" {
			return result, fmt.Errorf("请选择数据文件夹或 CSV/XLSX 数据清单")
		}
		kind, base = "data_list", filepath.Dir(path)
	}
	resolved := resource.InspectLocalData(ctx, path, kind, base)
	for _, d := range resolved.Problems {
		if d.Level == "error" && d.Code != "FLOW_DATA_FILE_MISSING" && d.Code != "FLOW_DATA_SIDECAR_MISSING" {
			return result, fmt.Errorf("%s", d.Message)
		}
		if d.Level == "error" {
			result.Messages = append(result.Messages, d.Message)
		}
	}
	result.Source = Source{APIVersion: "data_source/v1", ID: NewUserID(), Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), Kind: "local_folder", Root: base, Compliance: "unknown", Provides: []Provide{}}
	for _, entry := range resolved.Entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		rel, err := filepath.Rel(base, resource.DataSourcePath(entry.Path))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			result.Messages = append(result.Messages, "清单中的 "+entry.Name+" 位于根目录之外，未加入草稿")
			continue
		}
		layer := ""
		for _, option := range strings.Split(entry.Path, "|")[1:] {
			if name, ok := strings.CutPrefix(option, "layername="); ok {
				layer = name
			} else if strings.TrimSpace(option) != "" {
				return result, fmt.Errorf("数据 %s 包含尚不支持的连接选项 %s，请先导出为独立文件", entry.Name, option)
			}
		}
		draft := DraftEntry{Name: entry.Name, Path: filepath.ToSlash(rel), Layer: layer, Kind: entry.Kind, Exists: entry.Exists, Fields: map[string]Strings{}}
		candidate, _ := entry.Fields["category"].(string)
		if candidate == "" {
			candidate = strings.TrimSuffix(entry.Name, filepath.Ext(entry.Name))
		}
		if category, params, e := c.ResolveCategory(candidate, nil); category != nil {
			draft.Category = category.ID
			draft.Params = params
			if e != nil {
				result.Messages = append(result.Messages, e.Error())
			}
		}
		if raw, ok := entry.Fields["fields"].(string); ok {
			for _, field := range strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == '；' || r == '、' || r == ',' }) {
				parts := strings.SplitN(field, "=", 2)
				actual := strings.TrimSpace(parts[0])
				standard := strings.ToLower(actual)
				if len(parts) == 2 {
					standard = strings.ToLower(strings.TrimSpace(parts[1]))
				}
				if actual != "" && standard != "" {
					draft.Fields[standard] = append(draft.Fields[standard], actual)
				}
			}
		}
		result.Entries = append(result.Entries, draft)
	}
	if len(result.Entries) == 0 {
		return result, fmt.Errorf("没有找到支持的矢量或栅格数据，请检查目录或清单内容")
	}
	return result, nil
}

// Availability reports offline configuration readiness, not remote service health.
// Extent, category compliance and credentials/login validity are checked at use time.
func (e PlanningEnvironment) Availability(s Source) (bool, []string) {
	reasons := e.sourceReasons(s, nil, AcquisitionItem{})
	for _, r := range s.Requires {
		if r == "login:user" {
			reasons = append(reasons, "需要使用时完成登录授权；设置页未联网验证")
		}
	}
	if len(s.Provides) == 0 {
		reasons = append(reasons, "没有有效的数据类别")
	}
	return len(reasons) == 0, reasons
}
