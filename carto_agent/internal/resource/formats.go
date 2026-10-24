package resource

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed formats/resource_formats.yaml
var resourceFormatsYAML []byte

type ColumnFormat struct {
	Key              string            `yaml:"key" json:"key"`
	Aliases          []string          `yaml:"aliases" json:"aliases"`
	Required         bool              `yaml:"required" json:"required"`
	Kind             string            `yaml:"kind" json:"kind"`
	Default          any               `yaml:"default" json:"default,omitempty"`
	Values           map[string]string `yaml:"values" json:"values,omitempty"`
	ItemValues       map[string]string `yaml:"item_values" json:"item_values,omitempty"`
	Extensions       []string          `yaml:"extensions" json:"extensions,omitempty"`
	MinimumExclusive *float64          `yaml:"minimum_exclusive" json:"minimum_exclusive,omitempty"`
}
type TableFormat struct {
	Columns []ColumnFormat `yaml:"columns"`
	Unique  []any          `yaml:"unique"`
	Rules   []string       `yaml:"rules"`
}
type FileFormat struct {
	Suffix           string   `yaml:"suffix"`
	XMLRoot          string   `yaml:"xml_root"`
	RequiredKeys     []string `yaml:"required_keys"`
	RequiredElements []string `yaml:"required_elements"`
	AllowedElements  []string `yaml:"allowed_elements"`
	RequiredItemID   string   `yaml:"required_item_id"`
}
type TypeFormat struct {
	Public          bool                  `yaml:"public" json:"public"`
	Type            string                `json:"type" yaml:"-"`
	Label           string                `yaml:"label" json:"label"`
	Words           []string              `yaml:"words" json:"words"`
	Group           string                `yaml:"group" json:"group"`
	Folder          bool                  `yaml:"folder" json:"folder"`
	EntryExtensions []string              `yaml:"entry_extensions" json:"entry_extensions"`
	XMLRoot         string                `yaml:"xml_root" json:"-"`
	Table           TableFormat           `yaml:"table" json:"-"`
	Formats         map[string]FileFormat `yaml:"formats" json:"-"`
}
type SidecarFormat struct {
	Required    []string `yaml:"required"`
	Recommended []string `yaml:"recommended"`
}
type FormatSet struct {
	APIVersion        string              `yaml:"apiVersion"`
	Booleans          map[string][]string `yaml:"booleans"`
	ListSeparators    []string            `yaml:"list_separators"`
	MappingFiles      []string            `yaml:"mapping_files"`
	DataExtensions    map[string][]string `yaml:"data_extensions"`
	ShapefileSidecars SidecarFormat       `yaml:"shapefile_sidecars"`
	FolderScan        struct {
		SkipHidden bool     `yaml:"skip_hidden"`
		SkipDirs   []string `yaml:"skip_dirs"`
	} `yaml:"folder_scan"`
	Types map[string]TypeFormat `yaml:"types"`
}

var formatSet = loadFormats()

func loadFormats() *FormatSet {
	var set FormatSet
	if err := yaml.Unmarshal(resourceFormatsYAML, &set); err != nil {
		panic(err)
	}
	if err := validateFormats(&set); err != nil {
		panic(err)
	}
	for name, typ := range set.Types {
		typ.Type = name
		if typ.EntryExtensions == nil {
			typ.EntryExtensions = []string{}
		}
		for _, format := range typ.Formats {
			typ.EntryExtensions = append(typ.EntryExtensions, format.Suffix)
		}
		sort.Strings(typ.EntryExtensions)
		set.Types[name] = typ
	}
	return &set
}
func validateFormats(set *FormatSet) error {
	if set.APIVersion != "resource-formats/v1" || len(set.Types) != 8 {
		return fmt.Errorf("资源格式规范版本或类型数量无效")
	}
	words := map[string]bool{}
	for name, typ := range set.Types {
		if typ.Label == "" || typ.Group == "" {
			return fmt.Errorf("资源类型 %s 缺少标签或分组", name)
		}
		for _, word := range typ.Words {
			if words[word] {
				return fmt.Errorf("资源类型词重复: %s", word)
			}
			words[word] = true
		}
		keys, aliases := map[string]bool{}, map[string]string{}
		for _, column := range typ.Table.Columns {
			if keys[column.Key] || column.Key == "" {
				return fmt.Errorf("资源列 key 重复或为空: %s", column.Key)
			}
			keys[column.Key] = true
			for _, alias := range append([]string{column.Key}, column.Aliases...) {
				canonical := canonicalText(alias)
				if prior, ok := aliases[canonical]; ok && prior != column.Key {
					return fmt.Errorf("资源列别名冲突: %s", alias)
				}
				aliases[canonical] = column.Key
			}
		}
	}
	return nil
}
func IsPublicType(typ string) bool           { return formatSet.Types[typ].Public }
func Formats() *FormatSet                    { return formatSet }
func TypeInfo(typ string) (TypeFormat, bool) { value, ok := formatSet.Types[typ]; return value, ok }
func TypeInfos() []TypeFormat {
	items := []TypeFormat{}
	for _, typ := range formatSet.Types {
		items = append(items, typ)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Type < items[j].Type })
	return items
}
func TypeForWord(word string) (string, bool) {
	for name, typ := range formatSet.Types {
		for _, value := range typ.Words {
			if word == value {
				return name, true
			}
		}
	}
	return "", false
}
func Columns(typ string) []ColumnFormat { return formatSet.Types[typ].Table.Columns }
func MappingFile(dir string) (string, error) {
	for _, name := range formatSet.MappingFiles {
		path := filepath.Join(dir, name)
		if regularFileExists(path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("资源文件夹缺少可读取的对照表")
}
func DataKindForFile(path string, inFolder bool) string {
	path = DataSourcePath(path)
	ext := strings.ToLower(filepath.Ext(path))
	for _, kind := range []string{"vector", "raster"} {
		for _, value := range formatSet.DataExtensions[kind] {
			if ext == value {
				return kind
			}
		}
	}
	return ""
}
func SidecarRules() SidecarFormat { return formatSet.ShapefileSidecars }
func canonicalText(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "\ufeff")))
}
func canonicalColumn(typ, value string) string {
	name := canonicalText(value)
	for _, column := range Columns(typ) {
		for _, alias := range append([]string{column.Key}, column.Aliases...) {
			if canonicalText(alias) == name {
				return column.Key
			}
		}
	}
	return name
}
func splitList(value string) []string {
	for _, sep := range formatSet.ListSeparators {
		value = strings.ReplaceAll(value, sep, "\n")
	}
	result := []string{}
	for _, part := range strings.Split(value, "\n") {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}
func valueFromMap(raw string, values map[string]string) (string, bool) {
	for name, value := range values {
		if canonicalText(name) == canonicalText(raw) {
			return value, true
		}
	}
	return "", false
}

func columnInfo(typ, key string) ColumnFormat {
	for _, column := range Columns(typ) {
		if column.Key == key {
			return column
		}
	}
	return ColumnFormat{}
}
func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
func validEntryExtension(typ, path string) bool {
	info, ok := TypeInfo(typ)
	if !ok {
		return false
	}
	for _, ext := range info.EntryExtensions {
		if strings.HasSuffix(strings.ToLower(path), ext) {
			return true
		}
	}
	return false
}

// DataSourcePath strips QGIS provider options only for file validation and fingerprinting.
func DataSourcePath(path string) string { return strings.SplitN(path, "|", 2)[0] }
