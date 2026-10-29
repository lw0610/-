package resource

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

type ResourceSource struct {
	Author   string `json:"author,omitempty" yaml:"author,omitempty"`
	License  string `json:"license,omitempty" yaml:"license,omitempty"`
	Upstream string `json:"upstream,omitempty" yaml:"upstream,omitempty"`
}
type Manifest struct {
	Geometry    string   `json:"geometry,omitempty" yaml:"geometry,omitempty"`
	LabelField  string   `json:"label_field,omitempty" yaml:"label_field,omitempty"`
	Paper       string   `json:"paper,omitempty" yaml:"paper,omitempty"`
	Orientation string   `json:"orientation,omitempty" yaml:"orientation,omitempty"`
	Elements    []string `json:"elements,omitempty" yaml:"elements,omitempty"`
	Tags        []string `json:"tags,omitempty" yaml:"tags,omitempty"`

	APIVersion  string         `json:"apiVersion" yaml:"apiVersion"`
	ID          string         `json:"id,omitempty" yaml:"id,omitempty"`
	Type        string         `json:"type" yaml:"type"`
	Name        string         `json:"name" yaml:"name"`
	Version     string         `json:"version" yaml:"version"`
	Entry       string         `json:"entry,omitempty" yaml:"entry,omitempty"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
	Overrides   string         `json:"overrides,omitempty" yaml:"overrides,omitempty"`
	Source      ResourceSource `json:"source,omitempty" yaml:"source,omitempty"`
}

var resourceIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z0-9][a-z0-9.-]*$`)
var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func ValidID(id string) bool { return len(id) <= 64 && resourceIDPattern.MatchString(id) }
func ValidateManifest(m *Manifest, public bool) error {
	if m.APIVersion != "resource/v1" {
		return fmt.Errorf("资源清单 apiVersion 必须是 resource/v1")
	}
	typ, ok := TypeInfo(m.Type)
	if !ok {
		return fmt.Errorf("资源清单类型无效")
	}
	if m.ID == "" {
		if public {
			return fmt.Errorf("公共资源清单缺少编号")
		}
	} else if !ValidID(m.ID) || !strings.HasPrefix(m.ID, m.Type+".") {
		return fmt.Errorf("资源编号格式无效或与类型不符")
	}
	if !validDisplayName(m.Name) {
		return fmt.Errorf("资源名称须为 1–64 字符且不能含路径分隔符、冒号或引用括号")
	}
	if m.Version == "" {
		m.Version = "1.0.0"
	}
	if !versionPattern.MatchString(m.Version) {
		return fmt.Errorf("资源版本须为 MAJOR.MINOR.PATCH")
	}
	if utf8.RuneCountInString(m.Description) > 500 {
		return fmt.Errorf("资源说明不能超过 500 字符")
	}
	if !typ.Folder && m.Entry == "" {
		return fmt.Errorf("单文件资源清单缺少 entry")
	}
	if m.Entry != "" {
		path := strings.ReplaceAll(m.Entry, "\\", "/")
		if filepath.IsAbs(path) || strings.Contains(path, ":") {
			return fmt.Errorf("资源 entry 必须是资源内相对路径")
		}
		for _, part := range strings.Split(path, "/") {
			if part == ".." || part == "" || part == "." {
				return fmt.Errorf("资源 entry 不能含空段、. 或 ..")
			}
		}
		if !typ.Folder && !validEntryExtension(m.Type, m.Entry) {
			return fmt.Errorf("资源类型与主文件扩展名不符")
		}
	}
	return nil
}
func validDisplayName(name string) bool {
	if strings.TrimSpace(name) != name || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 64 || strings.ContainsAny(name, "/\\:「」") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func resourceBaseName(path, typ string) string {
	base := filepath.Base(path)
	info, _ := TypeInfo(typ)
	for _, ext := range info.EntryExtensions {
		if strings.HasSuffix(strings.ToLower(base), ext) {
			return base[:len(base)-len(ext)]
		}
	}
	// Legacy suffixes are read only for migrating old packages and snapshots.
	for _, other := range legacyFormatSet.Types {
		for _, ext := range other.EntryExtensions {
			if strings.HasSuffix(strings.ToLower(base), ext) {
				return base[:len(base)-len(ext)]
			}
		}
	}
	return base
}
func ManifestPath(path, typ string) string {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return filepath.Join(path, "resource.yaml")
	}
	return filepath.Join(filepath.Dir(path), resourceBaseName(path, typ)+".resource.yaml")
}
func legacyID(typ, path, root string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		relative = path
	}
	sum := sha256.Sum256([]byte(filepath.ToSlash(relative)))
	return "legacy." + typ + "." + hex.EncodeToString(sum[:])[:10]
}
func inspectItem(path, kind, owner, root string) LibraryItem {
	item := LibraryItem{Name: resourceBaseName(path, kind), Type: kind, Owner: owner, Origin: owner, Path: path, Root: path, Entry: path, Problems: []Diagnostic{}}
	info, err := os.Stat(path)
	if err != nil {
		item.Problems = append(item.Problems, Diagnostic{Level: "error", Code: "FLOW_RESOURCE_NOT_FOUND", Message: err.Error()})
		return item
	}
	item.Size = info.Size()
	if info.IsDir() {
		item.Name = filepath.Base(path)
	}
	item.ManifestPath = ManifestPath(path, kind)
	if !info.IsDir() && hasSiblingConflict(path, kind) {
		item.Problems = append(item.Problems, Diagnostic{Level: "error", Code: "RESOURCE_NAME_CONFLICT", Message: "同一目录存在多个同名主文件，请保留一个或另命名。"})
	}
	raw, err := boundedFile(item.ManifestPath, maxTableBytes)
	if os.IsNotExist(err) {
		item.Legacy = true
		item.ID = legacyID(kind, path, root)
		item.Version = "0.0.0"
		item.Problems = append(item.Problems, Diagnostic{Level: "info", Code: "FLOW_RESOURCE_MANIFEST_MISSING", Message: "资源没有清单，可升级以获得稳定编号。", Fix: map[string]any{"action": "upgrade_resource", "resource_id": item.ID}})
	} else if err != nil {
		item.Problems = append(item.Problems, Diagnostic{Level: "error", Code: "FLOW_RESOURCE_INVALID", Message: err.Error()})
	} else {
		var m Manifest
		err = yaml.Unmarshal(raw, &m)
		if err == nil {
			err = ValidateManifest(&m, strings.HasPrefix(owner, "library_"))
		}
		if err == nil {
			typ, _ := TypeInfo(m.Type)
			if typ.Folder != info.IsDir() && !(info.IsDir() && IsPublicType(m.Type) && m.Entry != "") {
				err = fmt.Errorf("资源清单类型与文件形态不符")
			}
			if !info.IsDir() && filepath.Clean(filepath.Join(filepath.Dir(item.ManifestPath), filepath.FromSlash(m.Entry))) != filepath.Clean(path) {
				err = fmt.Errorf("资源清单 entry 与主文件不符")
			}
			if m.Overrides != "" && owner != "library_user" && owner != "bundled" {
				err = fmt.Errorf("只有用户公共资源可以声明 overrides")
			}
		}
		if err != nil {
			item.Problems = append(item.Problems, Diagnostic{Level: "error", Code: "FLOW_RESOURCE_INVALID", Message: "资源清单无效：" + err.Error()})
		} else {
			item.Manifest = &m
			item.ID = m.ID
			item.Type = m.Type
			item.Name = m.Name
			item.Version = m.Version
			item.Overrides = m.Overrides
			item.Description = m.Description
			item.Source = m.Source
			item.Geometry, item.LabelField, item.Paper, item.Orientation = m.Geometry, m.LabelField, m.Paper, m.Orientation
			item.Elements, item.Tags = m.Elements, m.Tags
			if info.IsDir() && m.Entry != "" {
				item.Entry = filepath.Join(path, filepath.FromSlash(m.Entry))
				if err := rejectResourcePath(item.Entry); err != nil {
					item.Problems = append(item.Problems, Diagnostic{Level: "error", Code: "FLOW_RESOURCE_INVALID", Message: err.Error()})
				}
			}
		}
	}
	if info.IsDir() && item.Entry == path && !IsPublicType(item.Type) && item.Type != "data_folder" {
		if entry, err := MappingFile(path); err == nil {
			item.Entry = entry
		}
	}
	if !HasErrors(item.Problems) {
		item.Digest, err = resourceDigest(item)
		if err != nil {
			item.Problems = append(item.Problems, Diagnostic{Level: "error", Code: "FLOW_RESOURCE_INVALID", Message: err.Error()})
		}
	}
	enrichMetadata(&item)
	return item
}
func HasErrors(problems []Diagnostic) bool {
	for _, p := range problems {
		if p.Level == "error" {
			return true
		}
	}
	return false
}

func hasSiblingConflict(path, kind string) bool {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return false
	}
	base := resourceBaseName(path, kind)
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == filepath.Base(path) || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		other := filepath.Join(filepath.Dir(path), entry.Name())
		for _, typ := range TypeInfos() {
			if !typ.Folder && validEntryExtension(typ.Type, other) && resourceBaseName(other, typ.Type) == base {
				return true
			}
		}
	}
	return false
}
