package datasource

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var sourceID = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,127}$`)

// Load uses explicit roots; omit userRoot for builtins-only validation.
func Load(assetsRoot, userRoot string) (*Catalog, error) {
	c := &Catalog{Categories: []Category{}, Sources: []Source{}, Diagnostics: []Diagnostic{}}
	regions, regionErr := loadRegions(assetsRoot)
	if regionErr != nil {
		return nil, regionErr
	}
	c.Regions = regions
	data, err := os.ReadFile(filepath.Join(assetsRoot, "categories.yaml"))
	if err != nil {
		return nil, err
	}
	var vocabulary struct {
		APIVersion string     `yaml:"apiVersion"`
		Categories []Category `yaml:"categories"`
	}
	if err = strictYAML(data, &vocabulary); err != nil || vocabulary.APIVersion != "data_category/v1" {
		return nil, fmt.Errorf("数据类别词表格式不正确：%v", err)
	}
	seen, names := map[string]bool{}, map[string]string{}
	for _, category := range vocabulary.Categories {
		if category.ID == "" || category.Name == "" || seen[category.ID] || !contains([]string{"point", "line", "polygon", "raster", "table", "tiles"}, category.Geometry) {
			return nil, fmt.Errorf("数据类别无效或重复：%s", category.ID)
		}
		seen[category.ID] = true
		for _, a := range append([]Alias{{Name: category.ID}, {Name: category.Name}}, category.Aliases...) {
			key := strings.ToLower(a.Name)
			if old := names[key]; old != "" && old != category.ID {
				return nil, fmt.Errorf("数据类别别名冲突：%s", a.Name)
			}
			names[key] = category.ID
		}
		c.Categories = append(c.Categories, category)
	}
	for _, category := range c.Categories {
		for _, derive := range category.Derive {
			if !seen[derive.From] {
				return nil, fmt.Errorf("派生类别不存在：%s", derive.From)
			}
		}
	}
	for _, root := range []struct {
		path    string
		builtin bool
	}{{filepath.Join(assetsRoot, "sources"), true}, {userRoot, false}} {
		if root.path == "" {
			continue
		}
		entries, err := os.ReadDir(root.path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || (filepath.Ext(entry.Name()) != ".yaml" && filepath.Ext(entry.Name()) != ".yml") {
				continue
			}
			file := filepath.Join(root.path, entry.Name())
			raw, err := os.ReadFile(file)
			if err != nil {
				c.Diagnostics = append(c.Diagnostics, Diagnostic{Code: "DS_SCHEMA_INVALID", Level: "error", Message: "读取数据源失败", File: file})
				continue
			}
			source, diagnostics := c.ParseSource(raw, root.builtin)
			source.File = file
			for i := range diagnostics {
				diagnostics[i].File = file
			}
			c.Diagnostics = append(c.Diagnostics, diagnostics...)
			if hasError(diagnostics) {
				continue
			}
			if c.Source(source.ID) != nil {
				c.Diagnostics = append(c.Diagnostics, Diagnostic{Code: "DS_ID_CONFLICT", Level: "error", Message: "数据源编号重复，不能覆盖已有来源", SourceID: source.ID, File: file})
				continue
			}
			c.Sources = append(c.Sources, source)
		}
	}
	sort.Slice(c.Sources, func(i, j int) bool { return c.Sources[i].ID < c.Sources[j].ID })
	c.Rehash()
	return c, nil
}
func (c *Catalog) Rehash() {
	encoded, _ := json.Marshal(struct {
		Categories  []Category
		Sources     []Source
		Diagnostics []Diagnostic
		Regions     []Region
	}{c.Categories, c.Sources, c.Diagnostics, c.Regions})
	digest := sha256.Sum256(encoded)
	c.Revision = hex.EncodeToString(digest[:])
}
func strictYAML(data []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("仅允许一个 YAML 文档")
	}
	return nil
}
func hasError(ds []Diagnostic) bool {
	for _, d := range ds {
		if d.Level == "error" {
			return true
		}
	}
	return false
}

func (c *Catalog) ParseSource(raw []byte, builtin bool) (Source, []Diagnostic) {
	s := Source{Builtin: builtin}
	ds := []Diagnostic{}
	add := func(code, level, message string) {
		ds = append(ds, Diagnostic{Code: code, Level: level, Message: message, SourceID: s.ID})
	}
	var node yaml.Node
	if yaml.Unmarshal(raw, &node) != nil {
		add("DS_SCHEMA_INVALID", "error", "数据源 YAML 无效")
		return s, ds
	}
	if containsSecret(&node) {
		add("DS_SECRET_INLINE", "error", "数据源只能引用密钥名称，不能包含密钥值")
		return s, ds
	}
	if err := strictYAML(raw, &s); err != nil {
		add("DS_SCHEMA_INVALID", "error", "数据源结构包含不支持的字段或类型")
		return s, ds
	}
	s.Builtin = builtin
	if strings.TrimSpace(s.Handbook.GoodFor) == "" {
		add("DS_HANDBOOK_MISSING", "warning", "数据源缺少适用范围手册")
	}
	if s.APIVersion != "data_source/v1" || !sourceID.MatchString(s.ID) || s.Name == "" || !contains([]string{"local_folder", "offline_asset", "http_file", "overpass", "ogc_wfs", "arcgis_rest", "tianditu", "gee", "portal", "web_search"}, s.Kind) {
		add("DS_SCHEMA_INVALID", "error", "数据源版本、编号、名称或种类无效")
	}
	if !builtin && !strings.HasPrefix(s.ID, "user.") {
		add("DS_ID_CONFLICT", "error", "用户数据源编号必须以 user. 开头，不能覆盖内置来源")
	}
	if s.Compliance == "" {
		s.Compliance = "unknown"
	}
	if !contains([]string{"unknown", "open", "cn_official", "cn_reference"}, s.Compliance) {
		add("DS_SCHEMA_INVALID", "error", "来源合规标签无效")
	}
	if s.Status != "" && !contains([]string{"active", "experimental", "disabled"}, s.Status) {
		add("DS_SCHEMA_INVALID", "error", "来源状态无效")
	}
	if s.Kind != "offline_asset" && s.Kind != "local_folder" && len(s.Hosts) == 0 {
		add("DS_HOST_MISSING", "error", "联网数据源必须声明允许访问的主机")
	}
	for _, host := range s.Hosts {
		if strings.ContainsAny(host, "/?#@ ") || strings.TrimSpace(host) == "" {
			add("DS_SCHEMA_INVALID", "error", "主机白名单只允许完整主机名（可带端口）")
		}
	}
	for _, require := range s.Requires {
		if !strings.HasPrefix(require, "integration:") && !strings.HasPrefix(require, "secret:") && require != "login:user" {
			add("DS_SCHEMA_INVALID", "error", "不支持的数据源前置条件")
		}
	}
	if s.Kind == "local_folder" {
		if s.Root == "" {
			add("DS_SCHEMA_INVALID", "error", "本机数据源必须指定根目录")
		} else if info, err := os.Stat(s.Root); err != nil || !info.IsDir() {
			add("DS_LOCAL_ROOT_MISSING", "warning", "本机数据源目录不存在")
		}
	}
	valid := []Provide{}
	for _, p := range s.Provides {
		if c.Category(p.Category) == nil {
			add("DS_CATEGORY_UNKNOWN", "warning", "忽略未知类别："+p.Category)
			continue
		}
		if p.URL != "" {
			u, err := url.Parse(p.URL)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || !HostAllowed(u, s.Hosts) {
				add("DS_SCHEMA_INVALID", "error", "下载地址必须属于来源的主机白名单")
			}
		}
		if p.Path != "" && (filepath.IsAbs(p.Path) || strings.HasPrefix(filepath.Clean(p.Path), "..") || strings.Contains(p.Path, `\`)) {
			add("DS_SCHEMA_INVALID", "error", "来源文件必须使用根目录内的相对路径")
		}
		valid = append(valid, p)
	}
	s.Provides = valid
	if len(s.Coverage.BBox) > 0 && len(s.Coverage.BBox) != 4 {
		add("DS_SCHEMA_INVALID", "error", "覆盖范围须含四个坐标")
	}
	if s.RefreshDays < 0 || s.SizeMB < 0 || s.Limits.MaxMB < 0 {
		add("DS_SCHEMA_INVALID", "error", "数据源限制不能为负数")
	}
	return s, ds
}
func HostAllowed(u *url.URL, hosts []string) bool {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	for _, host := range hosts {
		if strings.EqualFold(u.Host, host) {
			return true
		}
	}
	return false
}
func secretKey(key string) bool {
	switch strings.ToLower(strings.ReplaceAll(key, "-", "_")) {
	case "key", "tk", "token", "access_token", "api_key", "apikey", "password", "secret", "authorization", "client_secret":
		return true
	}
	return false
}
func containsSecret(n *yaml.Node) bool {
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			if secretKey(key.Value) && value.Value != "" {
				return true
			}
			if containsSecret(value) {
				return true
			}
		}
		return false
	}
	if n.Kind == yaml.ScalarNode {
		if u, err := url.Parse(n.Value); err == nil && u.Host != "" {
			if u.User != nil {
				return true
			}
			for k, values := range u.Query() {
				if secretKey(k) && len(values) > 0 && values[0] != "" {
					return true
				}
			}
		}
	}
	for _, child := range n.Content {
		if containsSecret(child) {
			return true
		}
	}
	return false
}
