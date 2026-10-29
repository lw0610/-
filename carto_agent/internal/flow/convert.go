package flow

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/shared/xlsx"
)

// ConvertLegacy is a migration-only reader. Its output is ordinary Markdown;
// runtime consumers never interpret old tables or declarations.
func ConvertLegacy(raw string) (string, GuideConversionReport, error) {
	v5 := isGuide(raw)
	legacy := false
	for _, section := range headings(raw, 2) {
		if sectionAliases[normalizeName(section.title)] == "steps" && len(headings(section.content, 3)) > 0 && legacyStepField.MatchString(section.content) {
			legacy = true
		}
	}
	if !v5 && !legacy {
		return raw, GuideConversionReport{}, nil
	}
	meta, body, _, _ := parseFrontmatter(normalizeMarkdown(raw))
	report := GuideConversionReport{Converted: true, Message: "已自动转换，请检查", Items: []string{"请核对图层需求、默认值、资源名称和成果要求；原文已完整备份。"}}
	bumpGuideVersion(&meta)
	var b strings.Builder
	b.WriteString(metadataYAML(meta))
	b.WriteString("\n# " + meta.Name + "\n")
	if v5 {
		sections := map[string]string{}
		for _, s := range headings(body, 2) {
			sections[s.title] = s.content
		}
		writeConvertedSection(&b, "什么时候用", sections["适用任务"])
		var needs strings.Builder
		for _, row := range guideTable(sections["数据"], 0) {
			content := row.values["内容"]
			if strings.HasPrefix(content, "清单「") {
				content = "根据所需图层逐项准备数据"
			}
			fmt.Fprintf(&needs, "- %s：%s", row.values["数据"], strings.TrimPrefix(content, "类别："))
			for _, key := range []string{"范围", "字段", "说明"} {
				value := row.values[key]
				if value != "" && value != "—" && value != "-" && value != "见清单" {
					fmt.Fprintf(&needs, "；%s：%s", key, value)
				}
			}
			needs.WriteString("。\n")
		}
		for _, row := range guideTable(sections["图层处理"], 0) {
			fmt.Fprintf(&needs, "- %s：%s", row.values["图层"], row.values["处理"])
			for _, key := range []string{"数据", "样式", "标注", "顺序", "图例"} {
				if value := row.values[key]; value != "" && value != "—" {
					fmt.Fprintf(&needs, "；%s：%s", key, value)
				}
			}
			needs.WriteString("。\n")
		}
		writeConvertedSection(&b, "需要的图层与处理", needs.String())
		writeConvertedSection(&b, "默认值与范围说明", naturalInputs(sections["需要明确的信息"]))
		writeConvertedSection(&b, "做法", naturalMethod(sections["制作要点"]))
		writeConvertedSection(&b, "版面", sections["版面要求"])
		writeConvertedSection(&b, "成果", naturalOutputs(sections["产出与检查"]))
		writeConvertedSection(&b, "注意事项", sections["注意事项"])
		known := map[string]bool{"适用任务": true, "数据": true, "图层处理": true, "需要明确的信息": true, "制作要点": true, "版面要求": true, "产出与检查": true, "使用的资源": true, "注意事项": true, "参与角色": true}
		for _, s := range headings(body, 2) {
			if !known[s.title] {
				writeConvertedSection(&b, s.title, s.content)
			}
		}
	} else {
		old := Parse(raw)
		writeConvertedSection(&b, "什么时候用", old.Applicability)
		var needs strings.Builder
		for _, r := range old.Requirements {
			fmt.Fprintf(&needs, "- %s：%s；范围：%s", r.Name, r.Category, r.Extent)
			if r.Level != "" {
				fmt.Fprintf(&needs, "；行政层级：%s", r.Level)
			}
			if len(r.Fields) > 0 {
				fmt.Fprintf(&needs, "；字段要求：%s", joinGuideFields(r.Fields))
			}
			needs.WriteString("。\n")
		}
		for _, i := range old.Inputs {
			fmt.Fprintf(&needs, "- %s：%s", i.Label, i.Description)
			if i.Default != "" {
				fmt.Fprintf(&needs, "；默认 %s", i.Default)
			}
			if len(i.Options) > 0 {
				fmt.Fprintf(&needs, "；可选 %s", strings.Join(i.Options, "、"))
			}
			needs.WriteString("。\n")
		}
		writeConvertedSection(&b, "需要的图层与处理", needs.String())
		b.WriteString("\n## 做法\n")
		for _, step := range old.Steps {
			b.WriteString("\n### " + step.Title + "\n")
			if step.Condition != nil {
				b.WriteString("适用情况：" + step.Condition.Text + "。\n")
				report.Items = append(report.Items, "请核对“"+step.Title+"”的条件含义："+step.Condition.Text)
			}
			b.WriteString(step.Instructions + "\n")
			if step.Completion != "" {
				b.WriteString("完成后应核对：" + step.Completion + "\n")
			}
			if len(step.Capabilities) > 0 {
				b.WriteString("可使用" + strings.Join(step.Capabilities, "、") + "能力完成。\n")
			}
			if step.OnFailure == "stop" {
				b.WriteString("本项失败时停止，并保留已经完成的成果。\n")
			}
		}
		writeConvertedSection(&b, "成果", old.Deliverables)
		if len(old.Rules) > 0 {
			writeConvertedSection(&b, "注意事项", "- "+strings.Join(old.Rules, "\n- "))
		}
		for _, note := range old.Notes {
			writeConvertedSection(&b, note.Title, note.Content)
		}
	}
	for _, r := range legacyResources(raw) {
		if r.Type == "style" || r.Type == "layout" || r.Type == "style_set" {
			kind := "样式"
			if r.Type == "layout" {
				kind = "版面"
			}
			fmt.Fprintf(&b, "\n使用%s「%s」。\n", kind, r.Label)
		}
	}
	return b.String(), report, nil
}
func bumpGuideVersion(meta *FlowMetadata) {
	major, _ := strconv.Atoi(strings.Split(meta.Version, ".")[0])
	meta.Version = fmt.Sprintf("%d.0.0", major+1)
}
func writeConvertedSection(b *strings.Builder, title, content string) {
	if strings.TrimSpace(content) != "" {
		fmt.Fprintf(b, "\n## %s\n%s\n", title, strings.TrimSpace(content))
	}
}
func naturalInputs(raw string) string {
	tmp := &FlowDoc{}
	parseInputs(tmp, strings.ReplaceAll(raw, "必须", "必填"), 0)
	var b strings.Builder
	for _, i := range tmp.Inputs {
		fmt.Fprintf(&b, "- %s", i.Label)
		if i.Default != "" {
			fmt.Fprintf(&b, "默认 %s", i.Default)
		}
		if i.Description != "" {
			fmt.Fprintf(&b, "：%s", i.Description)
		}
		if len(i.Options) > 0 {
			fmt.Fprintf(&b, "；可选 %s", strings.Join(i.Options, "、"))
		}
		b.WriteString("。\n")
	}
	return b.String()
}
func naturalMethod(raw string) string {
	out := []string{}
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "需要的能力：") {
			out = append(out, "可使用"+strings.TrimPrefix(trimmed, "需要的能力：")+"相关能力。")
			continue
		}
		if strings.HasPrefix(trimmed, "本阶段产出：") {
			line = strings.Replace(line, "本阶段产出：", "完成后应得到：", 1)
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
func naturalOutputs(raw string) string {
	return strings.ReplaceAll(strings.ReplaceAll(raw, "交付说明：", "交付时说明："), "检查：", "应核对：")
}
func joinGuideFields(m map[string]string) string {
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []string{}
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return strings.Join(out, "；")
}
func legacyResources(raw string) []ResourceDoc {
	out := []ResourceDoc{}
	types := map[string]string{"样式": "style", "版式": "layout", "版面": "layout", "样式集": "style_set", "底图数据": "data_list", "数据清单": "data_list", "数据文件夹": "data_folder", "路线样式": "route_style", "符号": "symbols", "参考文档": "doc", "文档": "doc"}
	for _, s := range headings(raw, 2) {
		if sectionAliases[normalizeName(s.title)] != "resources" {
			continue
		}
		for _, item := range listItems(s.content) {
			match := resourcePattern.FindStringSubmatch(item.content)
			if match == nil {
				continue
			}
			location := folderInputPattern.ReplaceAllString(match[3], "")
			out = append(out, ResourceDoc{Label: strings.TrimSpace(match[2]), Type: types[strings.TrimSpace(match[1])], Location: strings.TrimSpace(location)})
		}
	}
	return out
}
func convertPackage(dir string, libraries ...*resource.Library) (*GuideConversionReport, error) {
	path := packageMainPath(dir)
	raw, err := readBounded(path, MaxFileBytes)
	if err != nil {
		return nil, err
	}
	existingLock, err := ReadLock(dir)
	if err != nil {
		return nil, err
	}
	if existingLock.DocumentFormat == documentFormatV6 {
		return nil, nil
	}
	// Standalone Markdown and unmarked folders are not evidence of an old
	// executable format. Their prose may legitimately use the same headings.
	_, lockErr := os.Stat(filepath.Join(dir, "flow.lock"))
	_, resourcesErr := os.Stat(filepath.Join(dir, "resources"))
	legacyPackage := filepath.Base(path) == "GUIDE.md" || lockErr == nil || resourcesErr == nil
	if !legacyPackage {
		_, candidate, e := ConvertLegacy(string(raw))
		if e != nil {
			return nil, e
		}
		if !candidate.Converted {
			return nil, nil
		}
		report := GuideConversionReport{Converted: false, Message: "已保留原文，请检查", Items: []string{"正文使用了旧格式常见标题或字段，但没有旧版包标识。无法可靠区分新自由文档与旧流程，已原样保留；请核对资源名称与制图要求。"}}
		encoded, _ := json.MarshalIndent(report, "", "  ")
		if e = writeGuideFile(filepath.Join(dir, "conversion-report.json"), encoded); e != nil {
			return nil, e
		}
		return &report, nil
	}
	converted, report, err := ConvertLegacy(string(raw))
	if err != nil {
		return nil, err
	}
	if !report.Converted && filepath.Base(path) == "FLOW.md" {
		return nil, nil
	}
	if !report.Converted {
		meta, body, _, _ := parseFrontmatter(string(raw))
		bumpGuideVersion(&meta)
		converted = metadataYAML(meta) + body
		report = GuideConversionReport{Converted: true, Message: "已自动转换，请检查", Items: []string{"旧 GUIDE.md 已改为 FLOW.md，原文件已备份。"}}
	}
	backup := path + ".bak"
	for n := 1; ; n++ {
		old, e := os.ReadFile(backup)
		if os.IsNotExist(e) {
			if err = os.WriteFile(backup, raw, 0600); err != nil {
				return nil, err
			}
			break
		}
		if e != nil {
			return nil, e
		}
		if string(old) == string(raw) {
			break
		}
		backup = fmt.Sprintf("%s.bak.%d", path, n)
	}
	text, notes, err := legacyResourceText(dir, string(raw), firstLibrary(libraries))
	if err != nil {
		return nil, err
	}
	var migrated []resource.MigrationItem
	if len(libraries) > 0 && libraries[0] != nil {
		migrated, err = libraries[0].LegacyMigrationItems()
		if err != nil {
			return nil, err
		}
		local, migrationErr := libraries[0].MigrateFlowResources(dir)
		err = migrationErr
		migrated = append(migrated, local...)
		if err != nil {
			return nil, err
		}
	}
	converted += text
	report.Items = append(report.Items, notes...)
	converted = rewriteMigratedNames(converted, migrated)
	for _, item := range migrated {
		if item.To != "" {
			report.Items = append(report.Items, item.PreviousName+" → "+item.Name+"（"+item.Type+"）")
		}
	}
	for _, r := range legacyResources(string(raw)) {
		if r.Type == "route_style" || r.Type == "symbols" {
			converted = strings.ReplaceAll(converted, "「"+r.Label+"」", "“"+strings.TrimPrefix(r.Label, "路线样式-")+"”")
		}
	}
	// Preserve old bundled visual dependencies in the new package-only location.
	lock, err := ReadLock(dir)
	if err != nil {
		return nil, err
	}
	for i := range lock.Resources {
		e := &lock.Resources[i]
		if (e.Type == "style" || e.Type == "layout") && strings.HasPrefix(e.Bundled, "resources/") {
			target := "deps/" + e.ID + "/"
			if err = copyTree(filepath.Join(dir, filepath.FromSlash(e.Bundled)), filepath.Join(dir, filepath.FromSlash(target)), false); err != nil {
				return nil, err
			}
			e.Bundled = target
		}
	}
	if err = writeLock(dir, retainedLock(lock, resourceDocumentWithLock(converted, firstLibrary(libraries), lock))); err != nil {
		return nil, err
	}
	report.Items = uniqueStrings(report.Items)
	encoded, _ := json.MarshalIndent(report, "", "  ")
	if err = writeGuideFile(filepath.Join(dir, "conversion-report.json"), encoded); err != nil {
		return nil, err
	}
	if err = writeGuideFile(filepath.Join(dir, "FLOW.md"), []byte(converted)); err != nil {
		return nil, err
	}
	if filepath.Base(path) == "GUIDE.md" {
		if err = os.Remove(path); err != nil {
			return nil, err
		}
	}
	if _, e := os.Stat(filepath.Join(dir, "resources")); e == nil {
		backupDir := filepath.Join(dir, ".resources-v5-backup")
		if _, e = os.Stat(backupDir); os.IsNotExist(e) {
			if err = copyTree(filepath.Join(dir, "resources"), backupDir, false); err != nil {
				return nil, err
			}
		}
		if err = os.RemoveAll(filepath.Join(dir, "resources")); err != nil {
			return nil, err
		}
	}
	return &report, nil
}
func firstLibrary(libraries []*resource.Library) *resource.Library {
	if len(libraries) > 0 {
		return libraries[0]
	}
	return nil
}
func rewriteMigratedNames(text string, items []resource.MigrationItem) string {
	grouped := map[string][]resource.MigrationItem{}
	for _, item := range items {
		grouped[item.PreviousName] = append(grouped[item.PreviousName], item)
	}
	for old, rows := range grouped {
		if old == "" {
			continue
		}
		names := []string{}
		for _, r := range rows {
			if r.Type == "style" || r.Type == "layout" {
				names = append(names, "「"+r.Name+"」")
			} else if r.Type == "symbols" || r.Type == "route_style" {
				names = append(names, "“"+r.Name+"”")
			}
		}
		if len(names) > 0 {
			text = strings.ReplaceAll(text, "「"+old+"」", strings.Join(uniqueStrings(names), "、"))
		}
	}
	return text
}
func (s *GuideStore) MigrateLegacy() ([]GuideConversionReport, error) {
	s.Packages.mu.Lock()
	defer s.Packages.mu.Unlock()
	reports := []GuideConversionReport{}
	entries, err := os.ReadDir(s.Packages.UserDir)
	if os.IsNotExist(err) {
		return reports, nil
	}
	if err != nil {
		return nil, err
	}
	library := s.Packages.Library
	if library == nil {
		library = resource.NewLibrary("", filepath.Join(filepath.Dir(s.Packages.UserDir), "resources"), s.Packages.Bindings)
		s.Packages.Library = library
	}
	for _, entry := range entries {
		if !entry.IsDir() || ValidateID(entry.Name()) != nil {
			continue
		}
		dir := filepath.Join(s.Packages.UserDir, entry.Name())
		path := packageMainPath(dir)
		raw, e := readBounded(path, MaxFileBytes)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return reports, e
		}
		existingLock, e := ReadLock(dir)
		if e != nil {
			return reports, e
		}
		if existingLock.DocumentFormat == documentFormatV6 {
			continue
		}
		_, report, e := ConvertLegacy(string(raw))
		if e != nil {
			return reports, e
		}
		if !report.Converted && filepath.Base(path) == "FLOW.md" {
			continue
		}
		// A persistent, backed-up migration record prevents duplicate resource imports
		// if publication of the converted document is interrupted.
		if _, e = library.MigrateFlowResources(dir); e != nil {
			return reports, e
		}
		stage, e := os.MkdirTemp("", "autocarto-flow-convert-")
		if e != nil {
			return reports, e
		}
		e = copyTree(dir, stage, false)
		var converted *GuideConversionReport
		if e == nil {
			converted, e = convertPackage(stage, library)
		}
		if e == nil {
			var body []byte
			body, e = readBounded(filepath.Join(stage, "FLOW.md"), MaxFileBytes)
			if e == nil {
				_, e = s.Packages.write(string(body), entry.Name(), stage, true)
			}
		}
		os.RemoveAll(stage)
		if e != nil {
			return reports, fmt.Errorf("转换 %s：%w", entry.Name(), e)
		}
		if converted != nil {
			reports = append(reports, *converted)
		}
	}
	return reports, nil
}
func (s *GuideStore) Import(source string, options ImportOptions, libraries ...*resource.Library) (*StoredGuide, error) {
	if err := rejectSymlinks(source); err != nil {
		return nil, err
	}
	info, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp("", "autocarto-flow-import-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	root := stage
	if info.IsDir() {
		err = copyTree(source, stage, false)
	} else {
		switch strings.ToLower(filepath.Ext(source)) {
		case ".zip":
			err = extractZIP(source, stage)
			if err == nil {
				root, err = packageRoot(stage)
			}
		case ".md":
			var raw []byte
			raw, err = readBounded(source, MaxFileBytes)
			if err == nil {
				name := "FLOW.md"
				if strings.EqualFold(filepath.Base(source), "GUIDE.md") {
					name = "GUIDE.md"
				}
				err = os.WriteFile(filepath.Join(stage, name), raw, 0600)
			}
		default:
			return nil, fmt.Errorf("只接受目录、ZIP 或 Markdown 流程")
		}
	}
	if err != nil {
		return nil, err
	}
	library := firstLibrary(libraries)
	if library == nil {
		library = s.Packages.ResourceLibrary()
	}
	if library == nil {
		library = resource.NewLibrary("", filepath.Join(filepath.Dir(s.Packages.UserDir), "resources"), s.Packages.Bindings)
	}
	s.Packages.SetLibrary(library)
	if _, err = convertPackage(root, library); err != nil {
		return nil, err
	}
	raw, err := readBounded(packageMainPath(root), MaxFileBytes)
	if err != nil {
		return nil, err
	}
	if options.NewID == "" {
		directory := filepath.Base(source)
		if strings.EqualFold(filepath.Ext(source), ".zip") && root != stage {
			directory = filepath.Base(root)
		} else if !info.IsDir() {
			directory = strings.TrimSuffix(directory, filepath.Ext(directory))
		}
		options.NewID, err = PackageID(string(raw), directory, "")
		if err != nil {
			return nil, err
		}
	}
	item, err := s.Packages.Import(root, options, library)
	if err != nil {
		return nil, err
	}
	return s.decorate(item)
}

func legacyResourceText(dir, raw string, library *resource.Library) (string, []string, error) {
	var b strings.Builder
	notes := []string{}
	for _, r := range legacyResources(raw) {
		if r.Type != "data_list" && r.Type != "doc" {
			continue
		}
		path := "legacy.csv"
		var data []byte
		if strings.HasPrefix(r.Location, "公共资源库") || r.Location == "" {
			if library == nil {
				notes = append(notes, "请核对旧公共资源“"+r.Label+"”；迁移资源库未配置，原内容保留在备份。")
				continue
			}
			id := ""
			if strings.HasPrefix(r.Location, "公共资源库/") {
				id = strings.TrimPrefix(r.Location, "公共资源库/")
			}
			lookupName := r.Label
			if id != "" {
				lookupName = ""
			} // Old explicit IDs remain authoritative even when the prose used an alias.
			text, readErr := library.ReadLegacyContent(r.Type, lookupName, id)
			if readErr != nil {
				notes = append(notes, "旧公共资源“"+r.Label+"”未自动转写："+readErr.Error()+"。原内容保留在资源备份，请检查。")
				continue
			}
			data = []byte(text)
		} else {
			relative, err := safeRelative(r.Location)
			if err != nil {
				notes = append(notes, "旧资源“"+r.Label+"”位于流程包外，未访问外部文件；请核对原文备份并在对话中提供数据。")
				continue
			}
			path = filepath.Join(dir, relative)
			var readErr error
			data, readErr = readBounded(path, MaxFileBytes)
			if os.IsNotExist(readErr) {
				notes = append(notes, "旧资源“"+r.Label+"”文件不存在，请检查原备份。")
				continue
			}
			if readErr != nil {
				return "", nil, readErr
			}
		}
		if r.Type == "doc" {
			writeConvertedSection(&b, "补充说明："+r.Label, string(data))
			continue
		}
		records, err := legacyListRows(path, data)
		if err != nil {
			return "", nil, err
		}
		if len(records) == 0 {
			continue
		}
		lines := []string{}
		omitted := map[string]bool{}
		for _, row := range records {
			label := ""
			for _, key := range []string{"图层", "名称", "name", "layer"} {
				if row[key] != "" {
					label = row[key]
					break
				}
			}
			if label == "" {
				continue
			}
			details := []string{}
			used := map[string]bool{"图层": true, "名称": true, "name": true, "layer": true}
			for _, column := range []struct {
				label string
				keys  []string
			}{
				{"类别", []string{"类别", "category"}}, {"类型", []string{"类型", "type"}},
				{"几何", []string{"几何", "geometry"}}, {"字段", []string{"字段", "fields"}},
				{"标注字段", []string{"标注字段", "label_field"}}, {"必需", []string{"必需", "required"}},
				{"推荐来源", []string{"推荐来源", "source_hint"}}, {"说明", []string{"说明", "描述", "description"}},
			} {
				for _, key := range column.keys {
					used[key] = true
				}
				for _, key := range column.keys {
					if value := row[key]; value != "" {
						details = append(details, column.label+"："+value)
						break
					}
				}
			}
			extra := []string{}
			for key, value := range row {
				if strings.TrimSpace(key) == "" || value == "" || used[key] {
					continue
				}
				if legacyLocationColumn(key) {
					omitted[key] = true
					continue
				}
				extra = append(extra, key)
			}
			sort.Strings(extra)
			for _, key := range extra {
				details = append(details, key+"："+row[key])
			}
			lines = append(lines, "- "+label+"："+strings.Join(details, "；")+"。")
		}
		writeConvertedSection(&b, "补充图层需求："+r.Label, strings.Join(lines, "\n"))
		notes = append(notes, "已将“"+r.Label+"”中的图层名称、字段和业务说明写入正文，原件保留在备份。")
		if len(omitted) > 0 {
			columns := []string{}
			for column := range omitted {
				columns = append(columns, column)
			}
			sort.Strings(columns)
			notes = append(notes, "未保留旧执行定位列："+strings.Join(columns, "、")+"；数据路径在本次对话中提供。")
		}
	}
	return b.String(), notes, nil
}

// Only old execution-location columns are omitted. Unrecognized columns may
// carry domain knowledge (year, accuracy, use) and must survive conversion.
func legacyLocationColumn(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "路径", "文件", "文件名", "文件夹", "目录", "绑定", "绑定目录", "path", "file", "filename", "folder", "directory", "base_dir", "binding", "binding_key", "location", "layer_id", "图层id":
		return true
	}
	return strings.HasSuffix(key, "路径") || strings.HasSuffix(key, "_path")
}

func legacyListRows(path string, raw []byte) ([]map[string]string, error) {
	var records [][]string
	if strings.EqualFold(filepath.Ext(path), ".xlsx") {
		workbook, err := xlsx.Open(path)
		if err != nil {
			return nil, err
		}
		if len(workbook.Sheets) == 0 {
			return nil, nil
		}
		_, cells, err := workbook.ReadSheet(workbook.Sheets[0])
		if err != nil {
			return nil, err
		}
		rows := []int{}
		for n := range cells {
			rows = append(rows, n)
		}
		sort.Ints(rows)
		for _, n := range rows {
			cols := cells[n]
			max := 0
			for c := range cols {
				if c > max {
					max = c
				}
			}
			row := make([]string, max+1)
			for c, value := range cols {
				row[c] = value
			}
			records = append(records, row)
		}
	} else {
		reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(raw), "\ufeff")))
		reader.FieldsPerRecord = -1
		var err error
		records, err = reader.ReadAll()
		if err != nil {
			return nil, err
		}
	}
	out := []map[string]string{}
	if len(records) < 2 {
		return out, nil
	}
	for _, row := range records[1:] {
		entry := map[string]string{}
		for i, value := range row {
			if i < len(records[0]) {
				entry[strings.TrimSpace(records[0][i])] = strings.TrimSpace(value)
			}
		}
		out = append(out, entry)
	}
	return out, nil
}
