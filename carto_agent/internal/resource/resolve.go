package resource

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"gopkg.in/yaml.v3"
)

func newResolved(request Request) *ResolvedResource {
	return &ResolvedResource{Key: request.Key, Label: request.Label, Type: request.Type, Location: request.Location, Entries: []ResourceEntry{}, Problems: []Diagnostic{}}
}

func (r *ResolvedResource) problem(level, code string, line int, message, action string) {
	d := Diagnostic{Level: level, Code: code, Line: line, Message: message}
	if action != "" {
		d.Fix = map[string]any{"action": action, "resource_key": r.Key, "binding_key": r.BindingKey}
	}
	r.Problems = append(r.Problems, d)
}

func (l *Library) resolvePath(request Request, path, origin string) *ResolvedResource {
	r := newResolved(request)
	r.ResolvedPath = path
	r.Entry = request.Entry
	r.Origin = origin
	if typeDirectories[request.Type] == "" {
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, "资源类型无法识别。", "")
		return r
	}
	if err := bindings.RejectSymlinks(path); err != nil {
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, err.Error(), "")
		return r
	}
	info, err := os.Stat(path)
	if err != nil {
		r.problem("error", "FLOW_RESOURCE_NOT_FOUND", 0, "无法读取资源“"+request.Label+"”："+err.Error(), "import_resource")
		return r
	}
	if !info.IsDir() && (!info.Mode().IsRegular() || info.Size() > MaxFileBytes) {
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, "资源不是普通文件或超过 200 MiB。", "")
		return r
	}
	if request.Type == "style" && info.IsDir() || request.Type == "style_set" && !info.IsDir() && strings.EqualFold(filepath.Ext(path), ".qml") {
		if info.IsDir() {
			r.Type = "style_set"
		} else {
			r.Type = "style"
		}
		r.problem("warning", "FLOW_RESOURCE_TYPE_ADJUSTED", 0, "资源声明的样式类型与实际文件不一致，已按实际类型解析。", "rewrite_type")
	}
	switch r.Type {
	case "data_list":
		l.resolveDataList(r, request)
	case "data_folder":
		resolveDataFolder(r)
	case "style":
		if !validEntryExtension("style", path) {
			err = fmt.Errorf("样式文件必须是 .qml")
		} else {
			spec, _ := TypeInfo("style")
			err = validateXML(path, spec.XMLRoot, false)
		}
	case "style_set":
		resolveStyleSet(r)
	case "route_style":
		resolveRouteStyle(r)
	case "layout":
		err = validateLayout(path)
	case "symbols":
		resolveSymbols(r)
	case "doc":
		err = validateDocument(path)
	}
	if err != nil {
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, err.Error(), "")
	}
	return r
}

func (l *Library) resolveDataList(r *ResolvedResource, request Request) {
	r.BaseDir = filepath.Dir(r.ResolvedPath)
	r.BaseDirFromInput = request.BaseDirFromInput
	if request.BaseDirOverride != "" {
		request.BaseDir = request.BaseDirOverride
	}
	deferred := request.BaseDirFromInput != "" && request.BaseDir == ""
	r.BindingKey = ""
	if strings.HasPrefix(r.Origin, "library_") {
		r.BindingKey = "library:" + request.ResourceID
	}
	if request.FlowID != "" {
		r.BindingKey = "flow:" + request.FlowID + "/" + request.Key
	}
	if request.BaseDirFromInput != "" {
		r.BindingKey = ""
	}
	bound := false
	if request.BaseDir != "" {
		base, err := config.ResolveProjectPath(request.BaseDir)
		if err == nil {
			err = rejectResourcePath(base)
		}
		info, statErr := os.Stat(base)
		if err != nil || statErr != nil || !info.IsDir() {
			r.problem("error", "FLOW_RESOURCE_INVALID", 0, "数据基准目录覆盖值不可读取或不是文件夹。", "")
			return
		}
		r.BaseDir = base
		bound = true
	}
	if request.BaseDirFromInput == "" && request.BaseDir == "" && l.Bindings != nil && r.BindingKey != "" {
		base, found, err := l.Bindings.Get(r.BindingKey)
		if err != nil {
			r.problem("error", "FLOW_RESOURCE_INVALID", 0, "资源绑定无法读取："+err.Error(), "")
			return
		}
		if !found && strings.HasPrefix(r.Origin, "library_") && request.FlowID != "" {
			base, found, err = l.Bindings.Get("library:" + request.ResourceID)
		}
		if err != nil {
			r.problem("error", "FLOW_RESOURCE_INVALID", 0, "资源绑定无法读取："+err.Error(), "")
			return
		}
		if found {
			r.BaseDir = base
			bound = true
			r.problem("warning", "FLOW_DATA_BINDING_LEGACY", 0, "此数据清单仍在使用旧绑定，请转换为文件夹输入。", "convert_to_input")
		}
	}
	if !validEntryExtension("data_list", r.ResolvedPath) {
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, "数据清单必须是 .csv 或 .xlsx。", "")
		return
	}
	rows, err := readTable(r.ResolvedPath, "data_list")
	if err != nil {
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, err.Error(), "")
		return
	}
	if deferred {
		r.BaseDir = ""
		r.BindingKey = ""
		r.problem("info", "FLOW_DATA_FROM_INPUT", 0, "文件位置在运行时由「"+request.BaseDirFromInput+"」决定。", "")
	}
	unbound := false
	for _, row := range rows {
		entry := ResourceEntry{Name: row.values["name"], Kind: normalizeDataKind(row.values["type"], row.values["path"]), Extra: row.values, Fields: row.fields}
		required, _ := row.fields["required"].(bool)
		if entry.Kind == "" {
			r.problem("error", "FLOW_RESOURCE_INVALID", row.line, "数据类型无法识别，请填写矢量/vector 或栅格/raster。", "")
		}
		rawPath := filepath.FromSlash(row.values["path"])
		entry.Path = rawPath
		if filepath.IsAbs(rawPath) {
			r.problem("warning", "FLOW_RESOURCE_PATH_EXTERNAL", row.line, "数据清单包含外部绝对路径："+entry.Name, "")
		}
		if deferred && !filepath.IsAbs(rawPath) {
			entry.ExistsUnknown = true
			r.Entries = append(r.Entries, entry)
			continue
		}
		if !filepath.IsAbs(rawPath) {
			entry.Path = filepath.Join(r.BaseDir, rawPath)
		}
		entry.Exists = regularFileExists(DataSourcePath(entry.Path))
		if !entry.Exists && !filepath.IsAbs(rawPath) && bound && request.BaseDirFromInput == "" {
			fallback := filepath.Join(filepath.Dir(r.ResolvedPath), rawPath)
			if regularFileExists(DataSourcePath(fallback)) {
				entry.Path, entry.Exists = fallback, true
			}
		}
		if !entry.Exists {
			level := "error"
			if !required {
				level = "warning"
			}
			r.problem(level, "FLOW_DATA_FILE_MISSING", row.line, "数据“"+entry.Name+"”的文件不存在或不可读取："+entry.Path, "choose_input_folder")
			if required && !bound && !filepath.IsAbs(rawPath) {
				unbound = true
			}
		}
		checkShapefileSidecars(r, entry, required, row.line)
		r.Entries = append(r.Entries, entry)
	}
	if unbound && request.BaseDirFromInput == "" {
		r.problem("error", "FLOW_RESOURCE_UNBOUND", 0, "清单中的相对路径未找到文件，请选择数据基准文件夹。", "choose_input_folder")
	}
}

func regularFileExists(path string) bool {
	if bindings.RejectSymlinks(path) != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func normalizeDataKind(kind, path string) string {
	if strings.TrimSpace(kind) != "" {
		column := columnInfo("data_list", "type")
		value, ok := valueFromMap(kind, column.Values)
		if ok {
			return value
		}
		return ""
	}
	return DataKindForFile(path, false)
}

func resolveDataFolder(r *ResolvedResource) {
	info, err := os.Stat(r.ResolvedPath)
	if err != nil || !info.IsDir() {
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, "数据文件夹不是文件夹。", "")
		return
	}
	err = filepath.WalkDir(r.ResolvedPath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path != r.ResolvedPath && (formatSet.FolderScan.SkipHidden && strings.HasPrefix(entry.Name(), ".") || entry.IsDir() && containsString(formatSet.FolderScan.SkipDirs, entry.Name())) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if err := bindings.RejectSymlinks(path); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		kind := DataKindForFile(path, true)
		if kind != "" {
			item := ResourceEntry{Name: strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())), Path: path, Kind: kind, Exists: regularFileExists(path), Extra: map[string]string{}, Fields: map[string]any{}}
			checkShapefileSidecars(r, item, true, 0)
			r.Entries = append(r.Entries, item)
		}
		return nil
	})
	if err != nil {
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, err.Error(), "")
	}
	if len(r.Entries) == 0 {
		r.problem("warning", "FLOW_RESOURCE_INVALID", 0, "数据文件夹中没有支持的矢量或栅格文件。", "")
	}
	counts := map[string]int{}
	for _, entry := range r.Entries {
		counts[entry.Name]++
	}
	for i := range r.Entries {
		entry := &r.Entries[i]
		if counts[entry.Name] > 1 {
			r.problem("warning", "FLOW_RESOURCE_ENTRY_NAME_CONFLICT", 0, "数据文件夹存在同名条目“"+entry.Name+"”，已使用相对路径区分。", "")
			relative, _ := filepath.Rel(r.ResolvedPath, entry.Path)
			entry.Name = filepath.ToSlash(relative)
		}
	}

}

func mappedPath(root, raw string) (string, error) {
	if filepath.IsAbs(raw) {
		return "", fmt.Errorf("对照表中的文件必须使用资源内相对路径")
	}
	path := filepath.Join(root, filepath.FromSlash(raw))
	if !inside(path, root) {
		return "", fmt.Errorf("对照表中的文件越出资源文件夹")
	}
	if err := bindings.RejectSymlinks(path); err != nil {
		return "", err
	}
	return path, nil
}

func resolveStyleSet(r *ResolvedResource) {
	file, err := r.Entry, error(nil)
	if file == "" || file == r.ResolvedPath {
		file, err = MappingFile(r.ResolvedPath)
	}
	if err != nil {
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, err.Error(), "")
		return
	}
	rows, err := readTable(file, "style_set")
	if err != nil {
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, err.Error(), "")
		return
	}
	for _, row := range rows {
		path, err := mappedPath(r.ResolvedPath, row.values["style_file"])
		if err == nil && !containsString(columnInfo("style_set", "style_file").Extensions, strings.ToLower(filepath.Ext(path))) {
			err = fmt.Errorf("样式文件必须是 .qml")
		}
		if err == nil {
			spec, _ := TypeInfo("style")
			err = validateXML(path, spec.XMLRoot, false)
		}
		entry := ResourceEntry{Name: row.values["layer"], Path: path, Kind: "style", Exists: regularFileExists(path), Extra: row.values, Fields: row.fields}
		if err != nil {
			r.problem("error", "FLOW_RESOURCE_INVALID", row.line, "图层“"+entry.Name+"”的样式无效："+err.Error(), "")
		}
		r.Entries = append(r.Entries, entry)
	}
}

func resolveRouteStyle(r *ResolvedResource) {
	rows, err := readTable(r.ResolvedPath, "route_style")
	if err != nil {
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, err.Error(), "")
		return
	}
	for _, row := range rows {
		value := row.values
		name := value["topology"] + "/" + value["side"]
		r.Entries = append(r.Entries, ResourceEntry{Name: name, Path: r.ResolvedPath, Kind: "route_style", Exists: true, Extra: value, Fields: row.fields})
	}
}

func resolveSymbols(r *ResolvedResource) {
	root := r.ResolvedPath
	info, err := os.Stat(root)
	if err != nil {
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, err.Error(), "")
		return
	}
	file := root
	if info.IsDir() {
		file = r.Entry
		if file == "" || file == root {
			file, err = MappingFile(root)
		}
	} else {
		if !validEntryExtension("data_list", file) {
			err = fmt.Errorf("符号对照表必须是 .csv 或 .xlsx")
		}
		root = filepath.Dir(file)
	}
	if err != nil {
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, err.Error(), "")
		return
	}
	rows, err := readTable(file, "symbols")
	if err != nil {
		r.problem("error", "FLOW_RESOURCE_INVALID", 0, err.Error(), "")
		return
	}
	for _, row := range rows {
		path, err := mappedPath(root, row.values["svg_path"])
		if err == nil && !containsString(columnInfo("symbols", "svg_path").Extensions, strings.ToLower(filepath.Ext(path))) {
			err = fmt.Errorf("符号文件扩展名无法识别")
		}
		if err == nil {
			switch strings.ToLower(filepath.Ext(path)) {
			case ".svg":
				err = validateXML(path, "svg", false)
			case ".png":
				err = validatePNG(path)
			default:
				err = fmt.Errorf("符号文件必须是 .svg 或 .png")
			}
		}
		if err != nil {
			r.problem("error", "FLOW_RESOURCE_INVALID", row.line, "符号“"+row.values["symbol_type"]+"”无效："+err.Error(), "")
		}
		r.Entries = append(r.Entries, ResourceEntry{Name: row.values["symbol_type"], Path: path, Kind: "symbol", Exists: regularFileExists(path), Extra: row.values, Fields: row.fields})
	}
}

func boundedFile(path string, limit int64) ([]byte, error) {
	if err := bindings.RejectSymlinks(path); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("资源不是普通文件或超过读取上限")
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if len(raw) > int(limit) {
		return nil, fmt.Errorf("资源超过读取上限")
	}
	return raw, err
}

func validateXML(path, root string, requireMap bool) error {
	raw, err := boundedFile(path, maxTableBytes)
	if err != nil {
		return err
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	seenRoot, mainMap := false, false
	depth := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("XML 无法解析：%v", err)
		}
		if start, ok := token.(xml.StartElement); ok {
			if depth == 0 && seenRoot {
				return fmt.Errorf("XML 只能包含一个根元素")
			}
			depth++
			if !seenRoot {
				if start.Name.Local != root {
					return fmt.Errorf("XML 根元素须为 %s", root)
				}
				seenRoot = true
			}
			for _, attr := range start.Attr {
				if attr.Name.Local == "id" && attr.Value == "main_map" {
					mainMap = true
				}
			}
		}
		if _, ok := token.(xml.EndElement); ok {
			depth--
		}
	}
	if !seenRoot {
		return fmt.Errorf("XML 文件为空")
	}
	if requireMap && !mainMap {
		return fmt.Errorf("版面模板缺少 ID 为 main_map 的主地图项；请在 QGIS 中把主地图项 ID 设为 main_map，或使用“从当前工程保存”")
	}
	return nil
}

func validatePNG(path string) error {
	raw, err := boundedFile(path, maxTableBytes)
	if err != nil {
		return err
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || format != "png" || config.Width <= 0 || config.Height <= 0 {
		return fmt.Errorf("PNG 符号无法读取")
	}
	return nil
}

func validateLayout(path string) error {
	spec, _ := TypeInfo("layout")
	var selected FileFormat
	format := ""
	for name, item := range spec.Formats {
		if strings.HasSuffix(strings.ToLower(path), item.Suffix) {
			format = name
			selected = item
			break
		}
	}
	if format == "" {
		return fmt.Errorf("版面扩展名无法识别")
	}
	if selected.XMLRoot != "" {
		return validateXML(path, selected.XMLRoot, selected.RequiredItemID != "")
	}
	raw, err := boundedFile(path, maxTableBytes)
	if err != nil {
		return err
	}
	var layout map[string]any
	if err = yaml.Unmarshal(raw, &layout); err != nil {
		return fmt.Errorf("版面 YAML 无法解析：%v", err)
	}
	for _, key := range selected.RequiredKeys {
		value, ok := layout[key]
		if !ok || value == nil {
			return fmt.Errorf("版面缺少 %s", key)
		}
	}
	elements, ok := layout["elements"].(map[string]any)
	if !ok {
		return fmt.Errorf("版面 elements 必须是对象")
	}
	for _, key := range selected.RequiredElements {
		if elements[key] == nil {
			return fmt.Errorf("版面缺少元素 %s", key)
		}
	}
	for key := range elements {
		if !containsString(selected.AllowedElements, key) {
			return fmt.Errorf("版面元素无法识别：%s", key)
		}
	}
	return nil
}

func validateDocument(path string) error {
	if !validEntryExtension("doc", path) {
		return fmt.Errorf("文档扩展名无法识别")
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".txt":
		raw, err := boundedFile(path, maxTableBytes)
		if err != nil {
			return err
		}
		if !utf8.Valid(raw) {
			return fmt.Errorf("文档必须使用 UTF-8 编码")
		}
		return nil
	case ".pdf":
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		header := make([]byte, 5)
		_, err = io.ReadFull(file, header)
		if err != nil || string(header) != "%PDF-" {
			return fmt.Errorf("PDF 文件头无法识别")
		}
		return nil
	}
	return fmt.Errorf("文档必须是 .md、.txt 或 .pdf")
}

// ShapefileSidecarPaths includes missing paths so creation/removal also changes fingerprints.
func ShapefileSidecarPaths(path string) []string {
	path = DataSourcePath(path)
	if !strings.EqualFold(filepath.Ext(path), ".shp") {
		return nil
	}
	base := strings.TrimSuffix(path, filepath.Ext(path))
	result := []string{}
	rules := SidecarRules()
	for _, ext := range append(append([]string{}, rules.Required...), rules.Recommended...) {
		result = append(result, base+ext)
	}
	return result
}
func checkShapefileSidecars(r *ResolvedResource, entry ResourceEntry, required bool, line int) {
	missing := []string{}
	defer func() {
		if entry.Fields != nil {
			entry.Fields["sidecars_missing"] = missing
		}
	}()
	for _, path := range ShapefileSidecarPaths(entry.Path) {
		if regularFileExists(path) {
			continue
		}
		missing = append(missing, filepath.Ext(path))
		level := "warning"
		if required && containsString(SidecarRules().Required, filepath.Ext(path)) {
			level = "error"
		}
		r.problem(level, "FLOW_DATA_SIDECAR_MISSING", line, "数据“"+entry.Name+"”缺少附属文件："+path, "choose_input_folder")
	}
}
