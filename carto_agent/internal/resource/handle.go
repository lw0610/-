package resource

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
)

func (l *Library) resolveItem(item LibraryItem, ref Ref, scope Scope) *ResolvedResource {
	label := ref.Name
	if label == "" {
		label = item.Name
	}
	key := item.ID
	if strings.HasPrefix(item.Owner, "library_") {
		key = "library:" + item.ID
	} else if item.Legacy && scope.FlowDir != "" {
		base := filepath.Join(scope.FlowDir, "resources")
		relative, err := filepath.Rel(base, item.Path)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			key = "path:" + filepath.ToSlash(relative)
		}
	}
	path := item.Path
	if IsPublicType(item.Type) && item.Entry != "" {
		path = item.Entry
	}
	r := l.resolvePath(Request{Key: key, ResourceID: item.ID, Label: label, Type: item.Type, Location: ref.Location, FlowID: scope.FlowID, FlowDir: scope.FlowDir, Entry: item.Entry, BaseDir: ref.BaseDir, BaseDirFromInput: ref.BaseDirFromInput, BaseDirOverride: ref.BaseDirOverride, EntryOverrides: ref.EntryOverrides, EntryOrigins: ref.EntryOrigins, EntryLayerIDs: ref.EntryLayerIDs}, path, item.Owner)
	r.Key = key
	r.ID = item.ID
	r.Label = label
	r.Name = item.Name
	r.Version = item.Version
	r.Owner = item.Owner
	r.Origin = item.Owner
	r.Root = item.Root
	r.Entry = item.Entry
	r.Digest = item.Digest
	r.Overrides = item.Overrides
	r.OverriddenBy = item.OverriddenBy
	r.Description = item.Description
	r.Source = item.Source
	r.Geometry, r.LabelField, r.Paper, r.Orientation = item.Geometry, item.LabelField, item.Paper, item.Orientation
	r.Elements, r.Tags, r.PreviewURL, r.PreviewStatus = item.Elements, item.Tags, item.PreviewURL, item.PreviewStatus
	r.Legacy = item.Legacy
	r.Problems = append(item.Problems, r.Problems...)
	r.ResolvedPath = r.Entry
	if r.Type == "data_folder" {
		r.Entry = ""
		r.ResolvedPath = r.Root
	}
	if ref.Type != "" && ref.Type != r.Type {
		if ref.Type == "style" && r.Type == "style_set" || ref.Type == "style_set" && r.Type == "style" {
			r.problem("warning", "FLOW_RESOURCE_TYPE_ADJUSTED", 0, "资源类型已按实际文件调整。", "rewrite_type")
		} else {
			r.problem("error", "FLOW_RESOURCE_TYPE_MISMATCH", 0, "声明的资源类型与资源清单不符。", "")
		}
	}
	if item.Overrides != "" && !HasErrors(item.Problems) {
		r.problem("info", "FLOW_RESOURCE_OVERRIDDEN", 0, "使用了显式覆盖内置资源的用户版本。", "")
	}
	switch r.Type {
	case "style":
		r.Format = "qml"
		if r.Geometry == "" {
			r.Geometry = qmlGeometry(r.Entry)
		}
	case "layout":
		spec, _ := TypeInfo("layout")
		for format, detail := range spec.Formats {
			if strings.HasSuffix(strings.ToLower(r.Entry), detail.Suffix) {
				r.Format = format
			}
		}
	case "doc":
		r.Format = strings.TrimPrefix(strings.ToLower(filepath.Ext(r.Entry)), ".")
	}
	for i := range r.Entries {
		entry := &r.Entries[i]
		if entry.Fields == nil {
			entry.Fields = map[string]any{}
		}
		switch r.Type {
		case "style_set":
			entry.Fields["style_path"] = entry.Path
			entry.Fields["geometry"] = qmlGeometry(entry.Path)
			labelField, _ := entry.Fields["label_field"].(string)
			mode := "field"
			if labelField == "" {
				mode = "keep"
			} else if labelField == "无" {
				mode = "off"
				labelField = ""
			}
			entry.Fields["label_mode"] = mode
			entry.Fields["label_field"] = labelField
			delete(entry.Fields, "style_file")
		case "route_style":
			entry.Path = ""
			if sides, ok := entry.Fields["default_sides"].([]string); ok && len(sides) == 0 {
				entry.Fields["default_sides"] = []string{entry.Extra["side"]}
			}
			if entry.Fields["preset_ref"] == "" {
				entry.Fields["preset_ref"] = entry.Extra["topology"] + "_" + entry.Extra["side"]
			}
		case "symbols":
			entry.Fields["format"] = strings.TrimPrefix(strings.ToLower(filepath.Ext(entry.Path)), ".")
			delete(entry.Fields, "svg_path")
		case "data_list":
			delete(entry.Fields, "name")
			delete(entry.Fields, "path")
			delete(entry.Fields, "type")
		}
	}
	if len(r.Entries) > 10000 {
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, "资源超过 10000 条，请使用较小的数据清单。", "")
	}
	for i := range r.Problems {
		if r.Problems[i].Fix != nil {
			r.Problems[i].Fix["resource_key"] = r.Key
			r.Problems[i].Fix["resource_id"] = r.ID
			if r.Problems[i].Code == "FLOW_RESOURCE_TYPE_ADJUSTED" {
				r.Problems[i].Fix["actual_type"] = r.Type
			}
		}
	}
	if IsPublicType(r.Type) {
		r.PreviewStatus = "pending"
		if previewCurrent(l.previewRoot(r), r.Digest) {
			r.PreviewStatus = "ready"
		}
	}
	return r
}
func qmlGeometry(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "unknown"
	}
	var data struct {
		Geometry *int `xml:"layerGeometryType"`
	}
	if xml.Unmarshal(raw, &data) != nil || data.Geometry == nil {
		return "unknown"
	}
	switch *data.Geometry {
	case 0:
		return "point"
	case 1:
		return "line"
	case 2:
		return "polygon"
	}
	return "unknown"
}
func (r *ResolvedResource) Handle() map[string]any {
	if r == nil || HasErrors(r.Problems) {
		return nil
	}
	entries := []map[string]any{}
	for _, entry := range r.Entries {
		value := map[string]any{}
		for k, v := range entry.Fields {
			value[k] = v
		}
		value["name"] = entry.Name
		value["path"] = entry.Path
		value["kind"] = entry.Kind
		value["exists"] = entry.Exists
		if entry.LayerID != "" {
			value["layer_id"] = entry.LayerID
		}
		if entry.ExistsUnknown {
			value["exists"] = nil
		}
		entries = append(entries, value)
	}
	warnings := []map[string]string{}
	for _, p := range r.Problems {
		warnings = append(warnings, map[string]string{"code": p.Code, "message": p.Message})
	}
	result := map[string]any{"api_version": "resource-handle/v1", "id": r.ID, "type": r.Type, "name": r.Name, "version": r.Version, "owner": r.Owner, "root": r.Root, "entry": r.Entry, "format": r.Format, "base_dir": r.BaseDir, "digest": r.Digest, "entries": entries, "total_entries": len(entries), "truncated": false, "warnings": warnings}
	if r.Geometry != "" {
		result["geometry"] = r.Geometry
		result["label_field"] = r.LabelField
	}
	return result
}
