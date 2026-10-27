package resource

import (
	"context"
	"path/filepath"
)

// InspectLocalData parses the explicitly selected local input. Unlike a library
// lookup, a.csv and a.xlsx are separate files and do not compete for an identity.
// The shared folder/table parsers and file guards remain authoritative.
func InspectLocalData(ctx context.Context, path, kind, base string) *ResolvedResource {
	request := Request{Type: kind, Label: filepath.Base(path), Location: path, BaseDirOverride: base}
	if err := ctx.Err(); err != nil {
		r := newResolved(request)
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, err.Error(), "")
		return r
	}
	if !filepath.IsAbs(path) || (kind != "data_list" && kind != "data_folder") {
		r := newResolved(request)
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, "请选择本机数据文件夹或清单的完整路径。", "")
		return r
	}
	r := (&Library{}).resolvePath(request, path, "external")
	if err := ctx.Err(); err != nil {
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, err.Error(), "")
	}
	return r
}
