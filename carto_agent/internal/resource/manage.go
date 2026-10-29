package resource

import (
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"gopkg.in/yaml.v3"
)

type ConflictError struct{ ExistingID string }

func (e *ConflictError) Error() string {
	return "资源名称已存在，请使用不同名称"
}
func (e *ConflictError) Unwrap() error { return ErrConflict }

func newResourceID(kind string) (string, error) {
	raw := make([]byte, 4)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return kind + ".u-" + hex.EncodeToString(raw), nil
}
func (l *Library) uniqueID(kind string) (string, error) {
	for i := 0; i < 10; i++ {
		id, err := newResourceID(kind)
		if err != nil {
			return "", err
		}
		if _, err = l.itemByID(id); errors.Is(err, ErrNotFound) {
			return id, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("无法分配唯一资源编号")
}
func writableItem(item LibraryItem) error {
	if item.Owner != "library_user" {
		return ErrReadOnly
	}
	return rejectResourcePath(item.Path)
}
func resourceName(name string) bool { return validName(name) && validDisplayName(name) }
func (l *Library) checkName(kind, name, exceptID string) error {
	items, err := l.list("all")
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ID != exceptID && IsPublicType(item.Type) && item.Name == name {
			return &ConflictError{ExistingID: item.ID}
		}
	}
	return nil
}
func manifestForItem(item LibraryItem) Manifest {
	if item.Manifest != nil {
		return *item.Manifest
	}
	return Manifest{APIVersion: "resource/v1", ID: item.ID, Type: item.Type, Name: item.Name, Version: "1.0.0", Description: item.Description, Source: item.Source, Geometry: item.Geometry, LabelField: item.LabelField, Paper: item.Paper, Orientation: item.Orientation, Elements: item.Elements, Tags: item.Tags}
}
func sourceResource(source, kind string) (LibraryItem, error) {
	path, err := config.ResolveProjectPath(source)
	if err != nil {
		return LibraryItem{}, err
	}
	if err = rejectResourcePath(path); err != nil {
		return LibraryItem{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return LibraryItem{}, err
	}
	spec, ok := TypeInfo(kind)
	if !ok || (spec.Folder != info.IsDir() && !(info.IsDir() && IsPublicType(kind))) || !info.IsDir() && !validEntryExtension(kind, path) {
		return LibraryItem{}, fmt.Errorf("文件或文件夹与资源类型不符")
	}
	item := inspectItem(path, kind, "external", filepath.Dir(path))
	for _, p := range item.Problems {
		if p.Level == "error" {
			return LibraryItem{}, fmt.Errorf("源资源无效：%s", p.Message)
		}
	}
	if item.Type != kind {
		return LibraryItem{}, fmt.Errorf("源资源清单类型与所选类型不符")
	}
	return item, nil
}
func writeResourceManifest(path string, m Manifest) error {
	if err := ValidateManifest(&m, true); err != nil {
		return err
	}
	raw, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	target := ManifestPath(path, m.Type)
	return os.WriteFile(target, raw, 0600)
}
func stageResource(stage string, source LibraryItem, filename string, m Manifest) (string, error) {
	target := filepath.Join(stage, filename)
	if IsPublicType(m.Type) {
		if err := os.MkdirAll(target, 0700); err != nil {
			return "", err
		}
		entry := source.Entry
		if entry == "" {
			entry = source.Path
		}
		m.Entry = filepath.Base(entry)
		if err := copyResource(entry, filepath.Join(target, m.Entry)); err != nil {
			return "", err
		}
		// QML may reference relative images or SVG files: keep the complete managed package.
		if info, _ := os.Stat(source.Path); info != nil && info.IsDir() {
			_ = os.RemoveAll(target)
			if err := copyResource(source.Path, target); err != nil {
				return "", err
			}
			if source.Manifest != nil {
				m.Entry = source.Manifest.Entry
			}
		}
		if err := writeResourceManifest(target, m); err != nil {
			return "", err
		}
		return target, nil
	}
	if err := copyResource(source.Path, target); err != nil {
		return "", err
	}
	if info, _ := os.Stat(target); info.IsDir() {
		if source.Manifest != nil {
			m.Entry = source.Manifest.Entry
		}
	} else {
		m.Entry = filename
	}
	if err := writeResourceManifest(target, m); err != nil {
		return "", err
	}
	return target, nil
}

func (l *Library) validateStaged(path, kind string, allowUnbound bool) error {
	item := inspectItem(path, kind, "library_user", filepath.Dir(path))
	result := l.resolveItem(item, Ref{Type: kind}, Scope{})
	for _, p := range result.Problems {
		if p.Level != "error" {
			continue
		}
		if allowUnbound && kind == "data_list" && (p.Code == "FLOW_DATA_FILE_MISSING" || p.Code == "FLOW_RESOURCE_UNBOUND" || p.Code == "FLOW_DATA_SIDECAR_MISSING") {
			continue
		}
		return fmt.Errorf("资源校验失败：%s", p.Message)
	}
	return nil
}
func extensionFor(path, kind string) string {
	return filepath.Base(path)[len(resourceBaseName(path, kind)):]
}
func packagePaths(path, kind string) []string {
	result := []string{path}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		manifest := ManifestPath(path, kind)
		if _, err = os.Stat(manifest); err == nil {
			result = append(result, manifest)
		}
	}
	return result
}
func publishResource(staged, target, kind string) error {
	paths := packagePaths(staged, kind)
	published := []string{}
	for _, path := range paths {
		dest := target
		if path != staged {
			dest = ManifestPath(target, kind)
		}
		if _, err := os.Lstat(dest); err == nil {
			for _, p := range published {
				_ = os.RemoveAll(p)
			}
			return ErrConflict
		} else if !os.IsNotExist(err) {
			for _, p := range published {
				_ = os.RemoveAll(p)
			}
			return err
		}
		if err := os.Rename(path, dest); err != nil {
			for _, p := range published {
				_ = os.RemoveAll(p)
			}
			return err
		}
		published = append(published, dest)
	}
	return nil
}
func (l *Library) importItem(source LibraryItem, name, overrides string) (*ResolvedResource, error) {
	if !IsPublicType(source.Type) {
		return nil, fmt.Errorf("%s", publicImportHelp)
	}
	if name == "" {
		name = source.Name
	}
	if !resourceName(name) {
		return nil, fmt.Errorf("资源名称无效")
	}
	if err := l.checkName(source.Type, name, ""); err != nil {
		return nil, err
	}
	id, err := l.uniqueID(source.Type)
	if err != nil {
		return nil, err
	}
	parent := filepath.Join(l.UserDir, typeDirectories[source.Type])
	if err = rejectResourcePath(parent); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(parent, 0700); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(parent, ".resource-import-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	m := manifestForItem(source)
	m.ID = id
	m.Name = name
	m.Version = "1.0.0"
	m.Overrides = overrides
	filename := name
	if !IsPublicType(source.Type) {
		if info, _ := os.Stat(source.Path); !info.IsDir() {
			filename += extensionFor(source.Path, source.Type)
		}
	}
	staged, err := stageResource(stage, source, filename, m)
	if err != nil {
		return nil, err
	}
	generated := false
	if source.Type == "style_set" && (source.Manifest == nil || source.Manifest.Entry == "") {
		generated, err = generateStyleMapping(staged)
		if err != nil {
			return nil, err
		}
	}
	if err = l.validateStaged(staged, source.Type, true); err != nil {
		return nil, err
	}
	target := filepath.Join(parent, filename)
	if hasSiblingConflict(target, source.Type) {
		return nil, ErrConflict
	}
	if err = publishResource(staged, target, source.Type); err != nil {
		return nil, err
	}
	l.invalidate()
	item, err := l.itemByID(id)
	if err != nil {
		return nil, err
	}
	result := l.resolveItem(item, Ref{Type: item.Type, Name: item.Name}, Scope{})
	if generated {
		result.problem("info", "RESOURCE_STYLE_MAPPING_GENERATED", 0, "已按文件名生成对照表，可在详情中调整别名。", "")
	}
	return result, nil
}
func (l *Library) ImportNamed(source, kind, name string) (*ResolvedResource, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	detected, err := DetectPublicType(source)
	if err != nil {
		return nil, err
	}
	if kind != "" && kind != detected {
		return nil, fmt.Errorf("传入类型与自动识别结果不一致：%s", detected)
	}
	kind = detected
	item, err := sourceResource(source, kind)
	if err != nil {
		return nil, err
	}
	if inside(item.Path, l.UserDir) && !inside(item.Path, filepath.Join(l.UserDir, ".staging")) {
		return nil, fmt.Errorf("请从用户资源库以外导入资源")
	}
	return l.importItem(item, name, "")
}
func (l *Library) Copy(id string, override bool, name string) (*ResolvedResource, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	item, err := l.itemByID(id)
	if err != nil {
		return nil, err
	}
	if HasErrors(item.Problems) {
		return nil, fmt.Errorf("原资源校验未通过，无法复制")
	}
	target := ""
	if override {
		if item.Owner != "library_builtin" {
			return nil, fmt.Errorf("只能显式覆盖内置公共资源")
		}
		if item.OverriddenBy != "" {
			return nil, &ConflictError{ExistingID: item.OverriddenBy}
		}
		target = item.ID
	}
	if name == "" {
		name, err = l.nextName(item.Type, item.Name+"（我的）")
		if err != nil {
			return nil, err
		}
	}
	return l.importItem(item, name, target)
}
func (l *Library) Replace(id, source string) (*ResolvedResource, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.replace(id, source)
}
func (l *Library) replace(id, source string) (*ResolvedResource, error) {
	item, err := l.itemByID(id)
	if err != nil {
		return nil, err
	}
	if err = writableItem(item); err != nil {
		return nil, err
	}
	if item.Legacy {
		return nil, fmt.Errorf("请先升级旧式资源再替换内容")
	}
	if !IsPublicType(item.Type) {
		return nil, fmt.Errorf("%s", publicImportHelp)
	}
	detected, err := DetectPublicType(source)
	if err != nil {
		return nil, err
	}
	if detected != item.Type {
		return nil, fmt.Errorf("替换资源类型与原资源不一致")
	}
	input, err := sourceResource(source, item.Type)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(item.Path)
	stage, err := os.MkdirTemp(parent, ".resource-replace-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	m := manifestForItem(item)
	parts := strings.Split(m.Version, ".")
	patch, _ := strconv.Atoi(parts[2])
	parts[2] = strconv.Itoa(patch + 1)
	m.Version = strings.Join(parts, ".")
	filename := resourceBaseName(item.Path, item.Type)
	if !IsPublicType(item.Type) {
		if info, _ := os.Stat(input.Path); !info.IsDir() {
			filename += extensionFor(input.Path, item.Type)
		}
	}
	staged, err := stageResource(stage, input, filename, m)
	if err != nil {
		return nil, err
	}
	if item.Type == "style_set" && (input.Manifest == nil || input.Manifest.Entry == "") {
		if _, err = generateStyleMapping(staged); err != nil {
			return nil, err
		}
	}
	if err = l.validateStaged(staged, item.Type, true); err != nil {
		return nil, err
	}
	backup := filepath.Join(l.UserDir, ".backup", id, time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err = rejectResourcePath(backup); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(backup, 0700); err != nil {
		return nil, err
	}
	original := packagePaths(item.Path, item.Type)
	moved := map[string]string{}
	rollback := func() {
		for from, to := range moved {
			_ = os.Rename(to, from)
		}
	}
	for _, path := range original {
		to := filepath.Join(backup, filepath.Base(path))
		if err = os.Rename(path, to); err != nil {
			rollback()
			return nil, err
		}
		moved[path] = to
	}
	target := filepath.Join(parent, filename)
	if err = publishResource(staged, target, item.Type); err != nil {
		rollback()
		return nil, err
	}
	l.invalidate()
	updated, err := l.itemByID(id)
	if err != nil {
		return nil, err
	}
	return l.resolveItem(updated, Ref{Type: updated.Type, Name: updated.Name}, Scope{}), nil
}
func (l *Library) DeleteID(id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	item, err := l.itemByID(id)
	if err != nil {
		return err
	}
	return l.deleteItem(item)
}
func (l *Library) deleteItem(item LibraryItem) error {
	if err := writableItem(item); err != nil {
		return err
	}
	// Move the complete unit out of the public index before removing it.
	stage, err := os.MkdirTemp(l.UserDir, ".resource-delete-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	moved := map[string]string{}
	for _, path := range packagePaths(item.Path, item.Type) {
		to := filepath.Join(stage, filepath.Base(path))
		if err = os.Rename(path, to); err != nil {
			for from, to := range moved {
				_ = os.Rename(to, from)
			}
			return err
		}
		moved[path] = to
	}
	l.invalidate()
	return nil
}
func (l *Library) Upgrade(id string) (*ResolvedResource, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	item, err := l.itemByID(id)
	if err != nil {
		return nil, err
	}
	if err = writableItem(item); err != nil {
		return nil, err
	}
	if !item.Legacy {
		return l.resolveItem(item, Ref{Type: item.Type, Name: item.Name}, Scope{}), nil
	}
	if err = l.validateStaged(item.Path, item.Type, true); err != nil {
		return nil, err
	}
	m := manifestForItem(item)
	m.ID, err = l.uniqueID(item.Type)
	if err != nil {
		return nil, err
	}
	m.Version = "1.0.0"
	if info, _ := os.Stat(item.Path); !info.IsDir() {
		m.Entry = filepath.Base(item.Path)
	}
	raw, err := yaml.Marshal(m)
	if err != nil {
		return nil, err
	}
	if err = ValidateManifest(&m, true); err != nil {
		return nil, err
	}
	path := ManifestPath(item.Path, item.Type)
	temp, err := os.CreateTemp(filepath.Dir(path), ".resource-upgrade-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(raw); err != nil {
		_ = temp.Close()
		return nil, err
	}
	if err = temp.Close(); err != nil {
		return nil, err
	}
	if err = os.Rename(temp.Name(), path); err != nil {
		return nil, err
	}
	l.invalidate()
	updated, err := l.itemByID(m.ID)
	if err != nil {
		return nil, err
	}
	return l.resolveItem(updated, Ref{Type: updated.Type, Name: updated.Name}, Scope{}), nil
}

const publicImportHelp = "资源库只接受样式（.qml）和版面（.qpt / .layout.yaml）；文件夹中的资源将分别导入"

func DetectPublicType(source string) (string, error) {
	path, err := config.ResolveProjectPath(source)
	if err != nil {
		return "", err
	}
	if err = rejectResourcePath(path); err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		raw, err := os.ReadFile(filepath.Join(path, "resource.yaml"))
		var m Manifest
		if err == nil && yaml.Unmarshal(raw, &m) == nil && IsPublicType(m.Type) && m.Entry != "" {
			return m.Type, nil
		}
	} else if info.Mode().IsRegular() {
		if validEntryExtension("layout", path) {
			return "layout", nil
		}
		if validEntryExtension("style", path) {
			return "style", nil
		}
	}
	return "", fmt.Errorf("%s", publicImportHelp)
}
func styleFiles(root string) ([]string, error) {
	files := []string{}
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = rejectResourcePath(path); err != nil {
			return err
		}
		if path != root && strings.HasPrefix(e.Name(), ".") {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !e.IsDir() && strings.EqualFold(filepath.Ext(path), ".qml") {
			rel, _ := filepath.Rel(root, path)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}
func generateStyleMapping(root string) (bool, error) {
	if _, err := MappingFile(root); err == nil {
		return false, nil
	}
	files, err := styleFiles(root)
	if err != nil {
		return false, err
	}
	if len(files) == 0 {
		return false, fmt.Errorf("样式集不含 .qml 文件")
	}
	f, err := os.OpenFile(filepath.Join(root, "对照表.csv"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return false, err
	}
	writer := csv.NewWriter(f)
	err = writer.Write([]string{"图层", "别名", "样式文件", "标注字段", "说明"})
	names := map[string]bool{}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		if names[name] {
			_ = f.Close()
			return false, fmt.Errorf("样式文件同名，请先分别命名：%s", name)
		}
		names[name] = true
		if err == nil {
			err = writer.Write([]string{name, "", file, "", ""})
		}
	}
	writer.Flush()
	if err == nil {
		err = writer.Error()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	return err == nil, err
}

// NewStaging creates an ignored, private workspace below the runtime library.
// The caller must invoke cleanup on every success and failure path.
func (l *Library) NewStaging() (string, func(), error) {
	parent := filepath.Join(l.UserDir, ".staging")
	if err := rejectResourcePath(parent); err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", nil, err
	}
	path, err := os.MkdirTemp(parent, "capture-")
	if err != nil {
		return "", nil, err
	}
	return path, func() { _ = os.RemoveAll(path) }, nil
}

// ReplaceAliases edits only normalized style-set aliases in a private copy. It
// never accepts a destination path and uses the normal versioned replacement.
func (l *Library) ReplaceAliases(id string, aliases map[string]string) (*ResolvedResource, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	item, err := l.itemByID(id)
	if err != nil {
		return nil, err
	}
	if err = writableItem(item); err != nil {
		return nil, err
	}
	if item.Type != "style_set" {
		return nil, fmt.Errorf("只有样式集可以编辑别名")
	}
	resolved := l.resolveItem(item, Ref{Type: item.Type}, Scope{})
	if HasErrors(resolved.Problems) {
		return nil, fmt.Errorf("请先修复样式集后编辑别名")
	}
	stage, cleanup, err := l.NewStaging()
	if err != nil {
		return nil, err
	}
	defer cleanup()
	source := filepath.Join(stage, "style-set")
	if err = copyResource(item.Path, source); err != nil {
		return nil, err
	}
	file, err := os.Create(filepath.Join(source, "对照表.csv"))
	if err != nil {
		return nil, err
	}
	writer := csv.NewWriter(file)
	_ = writer.Write([]string{"图层", "别名", "样式文件", "标注字段", "说明"})
	seen := map[string]bool{}
	for _, entry := range resolved.Entries {
		value := entry.Extra["aliases"]
		if v, ok := aliases[entry.Name]; ok {
			value = v
			seen[entry.Name] = true
		}
		stylePath, err := filepath.Rel(item.Path, entry.Path)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		_ = writer.Write([]string{entry.Name, value, filepath.ToSlash(stylePath), entry.Extra["label_field"], entry.Extra["description"]})
	}
	writer.Flush()
	err = writer.Error()
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	for label := range aliases {
		if !seen[label] {
			return nil, fmt.Errorf("样式集没有图层：%s", label)
		}
	}
	m := manifestForItem(item)
	m.Entry = "对照表.csv"
	if err = writeResourceManifest(source, m); err != nil {
		return nil, err
	}
	return l.replace(id, source)
}
