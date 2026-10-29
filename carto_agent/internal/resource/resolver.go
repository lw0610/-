package resource

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"gopkg.in/yaml.v3"
)

type Ref struct {
	EntryLayerIDs            map[string]string
	EntryOverrides           map[string]string
	EntryOrigins             map[string]string
	Type, Name, ID, Location string
	Key                      string
	BaseDir                  string
	BaseDirFromInput         string
	BaseDirOverride          string
}
type Scope struct {
	FlowID, FlowDir string
	Lock            *Lock
	Linked          []*ResolvedResource
}
type Resolver interface {
	Resolve(context.Context, Ref, Scope) *ResolvedResource
}

var _ Resolver = (*Library)(nil)

func (l *Library) Resolve(ctx context.Context, ref Ref, scope Scope) *ResolvedResource {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.resolve(ctx, ref, scope)
}
func (l *Library) resolve(ctx context.Context, ref Ref, scope Scope) *ResolvedResource {
	ref.Name = strings.TrimSpace(strings.Trim(ref.Name, "「」"))
	ref.Location = strings.TrimSpace(ref.Location)
	expected, format := ref.Type, ""
	if parts := strings.SplitN(expected, ":", 2); len(parts) == 2 {
		expected, format = parts[0], parts[1]
	}
	ref.Type = expected
	empty := newResolved(Request{Key: ref.Key, Label: ref.Name, Type: expected, Location: ref.Location})
	if err := ctx.Err(); err != nil {
		empty.problem("error", "FLOW_RESOURCE_INVALID", 0, err.Error(), "")
		return empty
	}
	finish := func(r *ResolvedResource) *ResolvedResource {
		if format != "" && r.Format != format {
			r.problem("error", "FLOW_RESOURCE_TYPE_MISMATCH", 0, "资源格式与工具要求不符，需要 "+format+"。", "")
		}
		return r
	}
	// A linked snapshot is authoritative for this run; never re-scan shared files
	// to resolve a label that was already linked for this flow.
	for _, match := range []func(*ResolvedResource) bool{
		func(r *ResolvedResource) bool { return ref.Location == "" && ref.Name != "" && r.Label == ref.Name },
		func(r *ResolvedResource) bool {
			return ref.Location == "" && (ref.ID != "" && r.ID == ref.ID || ref.Name != "" && (r.ID == ref.Name || r.Key == ref.Name))
		},
	} {
		candidates := []*ResolvedResource{}
		for _, r := range scope.Linked {
			if r != nil && match(r) && (expected == "" || r.Type == expected) {
				candidates = append(candidates, r)
			}
		}
		if len(candidates) > 1 {
			empty.problem("error", "FLOW_RESOURCE_AMBIGUOUS", 0, "流程作用域中存在多个匹配资源。", "choose_resource")
			choices := []map[string]any{}
			for _, item := range candidates {
				choices = append(choices, map[string]any{"id": item.ID, "name": item.Name, "type": item.Type, "owner": item.Owner, "version": item.Version})
			}
			empty.Problems[len(empty.Problems)-1].Fix["candidates"] = choices
			return empty
		}
		if len(candidates) == 1 {
			linked := candidates[0]
			if (ref.BaseDir != "" || ref.BaseDirOverride != "" || len(ref.EntryOverrides) > 0 || len(ref.EntryLayerIDs) > 0) && linked.Type == "data_list" {
				if ref.BaseDirFromInput == "" {
					ref.BaseDirFromInput = linked.BaseDirFromInput
				}
				item := inspectItem(linked.Root, linked.Type, linked.Owner, scope.FlowDir)
				item.ID = linked.ID
				item.Name = linked.Name
				item.Entry = linked.Entry
				ref.Name = linked.Label
				ref.Location = linked.Location
				ref.Key = linked.Key
				return finish(l.resolveItem(item, ref, scope))
			}
			copy := cloneResolved(linked)
			return finish(copy)
		}
	}
	location := ref.Location
	public := location == "" || location == "公共资源库" || strings.HasPrefix(location, "公共资源库/")
	if public && expected != "" && !IsPublicType(expected) {
		empty.problem("error", "FLOW_RESOURCE_NOT_FOUND", 0, "资源库只提供样式和版面；旧资源仅供迁移读取。", "")
		return empty
	}
	// Legacy bare layout names are interpreted as library names only when present.
	if expected == "layout" && location != "公共资源库" && validName(location) && filepath.Ext(location) == "" {
		ref.Name = location
		public = true
	}
	if strings.HasPrefix(location, "公共资源库/") {
		ref.ID = strings.TrimPrefix(location, "公共资源库/")
	}
	var locked *LockEntry
	if public {
		locked = scope.Lock.Find(expected, ref.Name)
		if locked != nil {
			ref.ID = locked.ID
		}
	}
	if locked != nil && locked.Source == "bundled" && locked.Bundled != "" {
		if scope.FlowDir == "" {
			empty.problem("error", "FLOW_RESOURCE_LOCK_MISSING", 0, "随包资源缺少流程目录。", "")
			return empty
		}
		root := filepath.Join(scope.FlowDir, filepath.FromSlash(locked.Bundled))
		if !inside(root, filepath.Join(scope.FlowDir, "resources", "_deps")) && !inside(root, filepath.Join(scope.FlowDir, "deps")) {
			empty.problem("error", "FLOW_RESOURCE_INVALID", 0, "随包资源路径越界。", "")
			return empty
		}
		path, err := bundledResourcePath(root, expected)
		if err != nil {
			empty.problem("error", "FLOW_RESOURCE_LOCK_MISSING", 0, err.Error(), "choose_resource")
			return empty
		}
		item := inspectItem(path, expected, "bundled", root)
		if item.Legacy && strings.HasPrefix(locked.ID, "legacy."+item.Type+".") {
			item.ID = locked.ID
		}
		if item.ID != locked.ID {
			empty.problem("error", "FLOW_RESOURCE_INVALID", 0, "随包资源清单编号与锁定不符。", "")
			return empty
		}
		r := l.resolveItem(item, ref, scope)
		for i := range r.Problems {
			if r.Problems[i].Code == "FLOW_RESOURCE_MANIFEST_MISSING" {
				r.Problems[i].Fix["action"] = "install_resource"
			}
		}
		r.problem("info", "FLOW_RESOURCE_BUNDLED_DIFFERS", 0, "使用流程包附带的资源副本，可安装为公共资源。", "install_resource")
		r.Problems[len(r.Problems)-1].Fix["resource_id"] = locked.ID
		if local, err := l.itemByID(locked.ID); err == nil && !HasErrors(local.Problems) {
			r.Problems[len(r.Problems)-1].Fix["actions"] = []string{"install_resource", "use_library"}
		}
		if drift := locked.Drift(r); drift != nil {
			r.Problems = append(r.Problems, *drift)
		}
		return finish(r)
	}
	if public {
		items, err := l.list("all")
		if err != nil {
			empty.problem("error", "FLOW_RESOURCE_INVALID", 0, err.Error(), "")
			return empty
		}
		var candidates []LibraryItem
		if ref.ID == "" && ValidID(ref.Name) {
			ref.ID = ref.Name
		}
		if ref.ID != "" {
			for _, item := range items {
				if item.ID == ref.ID && !HasErrors(item.Problems) && (IsPublicType(item.Type) || item.Owner == "library_user" || item.Owner == "tool") {
					candidates = append(candidates, item)
				}
			}
			if len(candidates) == 1 && candidates[0].OverriddenBy != "" {
				id := candidates[0].OverriddenBy
				for _, item := range items {
					if item.ID == id && !HasErrors(item.Problems) {
						candidates = []LibraryItem{item}
						break
					}
				}
			}
		} else {
			for _, item := range items {
				if item.Owner == "tool" || !IsPublicType(item.Type) && item.Owner != "library_user" || HasErrors(item.Problems) || expected != "" && item.Type != expected {
					continue
				}
				matched := item.Name == ref.Name
				if item.Overrides != "" {
					for _, base := range items {
						if base.ID == item.Overrides && base.Name == ref.Name && base.Type == item.Type {
							matched = true
						}
					}
				}
				if matched && item.OverriddenBy == "" {
					candidates = append(candidates, item)
				}
			}
			if len(candidates) == 0 && (expected == "style" || expected == "style_set") {
				for _, item := range items {
					if !HasErrors(item.Problems) && item.Name == ref.Name && item.OverriddenBy == "" && (item.Type == "style" || item.Type == "style_set") {
						candidates = append(candidates, item)
					}
				}
			}
		}
		if len(candidates) == 0 {
			code := "FLOW_RESOURCE_NOT_FOUND"
			if locked != nil {
				code = "FLOW_RESOURCE_LOCK_MISSING"
			}
			empty.problem("error", code, 0, "模板与样式库中未找到可用资源“"+ref.Name+ref.ID+"”。数据在任务运行时选择；路线与符号使用工具预设名称。", "choose_resource")
			choices := []map[string]any{}
			for _, item := range items {
				if item.Type == expected && !HasErrors(item.Problems) && item.OverriddenBy == "" {
					choices = append(choices, map[string]any{"id": item.ID, "name": item.Name, "type": item.Type, "owner": item.Owner, "version": item.Version})
				}
			}
			empty.Problems[len(empty.Problems)-1].Fix["candidates"] = choices
			return empty
		}
		if len(candidates) > 1 {
			empty.problem("error", "FLOW_RESOURCE_AMBIGUOUS", 0, "同名资源存在多个候选，请选择资源或明确覆盖关系。", "choose_resource")
			choices := []map[string]any{}
			for _, item := range candidates {
				choices = append(choices, map[string]any{"id": item.ID, "name": item.Name, "type": item.Type, "owner": item.Owner, "version": item.Version})
			}
			empty.Problems[len(empty.Problems)-1].Fix["candidates"] = choices
			return empty
		}
		resolved := l.resolveItem(candidates[0], ref, scope)
		if !IsPublicType(resolved.Type) && resolved.Owner == "library_user" {
			resolved.problem("warning", "FLOW_RESOURCE_LIBRARY_TYPE_RETIRED", 0, "这类旧资源已退出模板与样式库，请移入流程 resources/。", "adopt_into_flow")
			resolved.Problems[len(resolved.Problems)-1].Fix["resource_id"] = resolved.ID
			resolved.Problems[len(resolved.Problems)-1].Fix["label"] = resolved.Label
		}
		if drift := locked.Drift(resolved); drift != nil {
			resolved.Problems = append(resolved.Problems, *drift)
		}
		return finish(resolved)
	}
	var path string
	var err error
	if filepath.IsAbs(location) {
		path = filepath.Clean(location)
	} else if scope.FlowDir != "" {
		path = filepath.Join(scope.FlowDir, filepath.FromSlash(location))
		if !inside(path, scope.FlowDir) {
			err = fmt.Errorf("流程资源相对路径不能越出流程包")
		}
	} else {
		path, err = config.ResolveProjectPath(location)
	}
	if err != nil {
		empty.problem("error", "FLOW_RESOURCE_INVALID", 0, err.Error(), "")
		return empty
	}
	if err = rejectResourcePath(path); err != nil {
		empty.problem("error", "FLOW_RESOURCE_INVALID", 0, err.Error(), "")
		return empty
	}
	kind := expected
	if info, e := os.Stat(path); e == nil {
		if kind == "style" && info.IsDir() {
			kind = "style_set"
		} else if kind == "style_set" && !info.IsDir() && validEntryExtension("style", path) {
			kind = "style"
		}
		if kind == "" {
			kind = inferPathType(path, info.IsDir())
		}
	}
	if kind == "" {
		empty.problem("error", "FLOW_RESOURCE_INVALID", 0, "无法识别资源类型，请指定类型。", "")
		return empty
	}
	owner, root := "external", filepath.Dir(path)
	if scope.FlowDir != "" && inside(path, scope.FlowDir) {
		owner, root = "flow", scope.FlowDir
	}
	item := inspectItem(path, kind, owner, root)
	r := l.resolveItem(item, ref, scope)
	if owner == "external" {
		r.problem("warning", "FLOW_RESOURCE_PATH_EXTERNAL", 0, "资源位于流程包外，分享前请将它导入流程。", "import_resource")
	}
	return finish(r)
}
func inferPathType(path string, folder bool) string {
	if folder {
		if raw, err := boundedFile(filepath.Join(path, "resource.yaml"), maxTableBytes); err == nil {
			var m Manifest
			if yaml.Unmarshal(raw, &m) == nil {
				return m.Type
			}
		}
		return "data_folder"
	}
	for _, typ := range TypeInfos() {
		if !typ.Folder && validEntryExtension(typ.Type, path) {
			return typ.Type
		}
	}
	return ""
}
func bundledResourcePath(root, kind string) (string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return root, nil
	}
	if regularFileExists(filepath.Join(root, "resource.yaml")) {
		return root, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	primaries := []string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".resource.yaml") {
			continue
		}
		raw, err := boundedFile(filepath.Join(root, entry.Name()), maxTableBytes)
		if err != nil {
			return "", err
		}
		var m Manifest
		if err = yaml.Unmarshal(raw, &m); err != nil {
			return "", err
		}
		if err = ValidateManifest(&m, false); err != nil {
			return "", err
		}
		spec, _ := TypeInfo(m.Type)
		if spec.Folder {
			return "", fmt.Errorf("文件夹资源清单须命名 resource.yaml")
		}
		primaries = append(primaries, filepath.Join(root, filepath.FromSlash(m.Entry)))
	}
	if len(primaries) > 1 {
		return "", fmt.Errorf("随包资源包含多个主文件清单")
	}
	if len(primaries) == 1 {
		return primaries[0], nil
	}
	if spec, ok := TypeInfo(kind); ok && spec.Folder {
		return root, nil
	}
	for _, entry := range entries {
		if !entry.IsDir() && validEntryExtension(kind, entry.Name()) {
			primaries = append(primaries, filepath.Join(root, entry.Name()))
		}
	}
	if len(primaries) != 1 {
		return "", fmt.Errorf("随包资源须包含一个主文件")
	}
	return primaries[0], nil
}
