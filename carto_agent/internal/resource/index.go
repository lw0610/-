package resource

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

func cloneValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return value
		}
		result := reflect.New(value.Type()).Elem()
		result.Set(cloneValue(value.Elem()))
		return result
	case reflect.Pointer:
		if value.IsNil() {
			return value
		}
		result := reflect.New(value.Type().Elem())
		result.Elem().Set(cloneValue(value.Elem()))
		return result
	case reflect.Map:
		if value.IsNil() {
			return value
		}
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			result.SetMapIndex(iter.Key(), cloneValue(iter.Value()))
		}
		return result
	case reflect.Slice:
		if value.IsNil() {
			return value
		}
		result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			result.Index(i).Set(cloneValue(value.Index(i)))
		}
		return result
	case reflect.Struct:
		result := reflect.New(value.Type()).Elem()
		for i := 0; i < value.NumField(); i++ {
			result.Field(i).Set(cloneValue(value.Field(i)))
		}
		return result
	default:
		return value
	}
}
func cloneItem(item LibraryItem) LibraryItem {
	return cloneValue(reflect.ValueOf(item)).Interface().(LibraryItem)
}
func cloneResolved(item *ResolvedResource) *ResolvedResource {
	return cloneValue(reflect.ValueOf(item)).Interface().(*ResolvedResource)
}

// Clone keeps normalized field types while isolating linked execution snapshots.
func (r *ResolvedResource) Clone() *ResolvedResource { return cloneResolved(r) }
func (l *Library) indexStamp() (string, error) {
	var stamp strings.Builder
	for _, root := range append([]string{l.BuiltinDir, l.UserDir}, l.ToolDirs...) {
		if root == "" {
			continue
		}
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if os.IsNotExist(err) {
				fmt.Fprintf(&stamp, "missing:%s\n", path)
				return nil
			}
			if err != nil {
				return err
			}
			if path != root && strings.HasPrefix(entry.Name(), ".") {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			fmt.Fprintf(&stamp, "%s:%d:%d:%s\n", path, info.Size(), info.ModTime().UnixNano(), info.Mode())
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	return stamp.String(), nil
}
func applyIdentityRules(items []LibraryItem) {
	names, ids := map[string][]int{}, map[string][]int{}
	for i, item := range items {
		names[item.Owner+"/"+filepath.Dir(item.Path)+"/"+resourceBaseName(item.Path, item.Type)] = append(names[item.Owner+"/"+filepath.Dir(item.Path)+"/"+resourceBaseName(item.Path, item.Type)], i)
		if item.ID != "" {
			key := item.Owner + "/" + item.ID
			ids[key] = append(ids[key], i)
		}
	}
	add := func(indices []int, code, message string) {
		for _, i := range indices {
			already := false
			for _, problem := range items[i].Problems {
				if problem.Code == code {
					already = true
					break
				}
			}
			if already {
				continue
			}
			items[i].Problems = append(items[i].Problems, Diagnostic{Level: "error", Code: code, Message: message})
		}
	}
	for _, matches := range names {
		if len(matches) > 1 {
			add(matches, "RESOURCE_NAME_CONFLICT", "同一目录存在多个同名主文件，请保留一个或另命名。")
		}
	}
	for _, matches := range ids {
		if len(matches) > 1 {
			add(matches, "FLOW_RESOURCE_INVALID", "同一资源库中的编号重复。")
		}
	}
	for i, item := range items {
		if item.Owner == "library_user" {
			for _, tool := range items {
				if tool.Owner == "tool" && tool.ID == item.ID {
					add([]int{i}, "RESOURCE_OVERRIDE_CONFLICT", "用户资源不能使用工具资产的编号。")
				}
			}
		}
	}
	overrides := map[string][]int{}
	for i, item := range items {
		if item.Overrides == "" || HasErrors(item.Problems) {
			continue
		}
		valid := false
		toolConflict := false
		for _, base := range items {
			if base.Owner == "tool" && base.ID == item.Overrides {
				toolConflict = true
			}
			if base.Owner == "library_builtin" && base.ID == item.Overrides && base.Type == item.Type && !HasErrors(base.Problems) {
				valid = true
			}
		}
		if toolConflict {
			add([]int{i}, "RESOURCE_OVERRIDE_CONFLICT", "工具资产不能被用户资源覆盖。")
			continue
		}
		if !valid {
			add([]int{i}, "FLOW_RESOURCE_INVALID", "overrides 必须指向同类型的现有内置资源。")
			continue
		}
		overrides[item.Overrides] = append(overrides[item.Overrides], i)
	}
	for target, matches := range overrides {
		if len(matches) > 1 {
			add(matches, "RESOURCE_OVERRIDE_CONFLICT", "多个用户资源覆盖同一个内置资源，已回落内置资源。")
			continue
		}
		for i := range items {
			if items[i].Owner == "library_builtin" && items[i].ID == target {
				items[i].OverriddenBy = items[matches[0]].ID
			}
		}
	}
}
