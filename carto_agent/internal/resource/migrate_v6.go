package resource

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/autocarto/carto_agent/internal/shared/xlsx"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// MigrationItem records every original unit, including material handed back to
// the flow converter. Originals are backed up before any replacement.
type MigrationItem struct {
	From         string `json:"from"`
	To           string `json:"to,omitempty"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	PreviousName string `json:"previous_name,omitempty"`
	Note         string `json:"note,omitempty"`
}

// ReadLegacyContent is a migration-only reader, not a public resource lookup.
// Callers supply identity, never a path. Even a forged report cannot redirect
// reads outside this library's complete, hidden migration originals.
func (l *Library) ReadLegacyContent(kind, name, id string) (string, error) {
	if kind != "data_list" && kind != "doc" {
		return "", fmt.Errorf("只支持迁移旧数据清单与参考文本")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	report := filepath.Join(l.UserDir, ".migration-v6-report.json")
	if err := rejectResourcePath(report); err != nil {
		return "", err
	}
	raw, err := boundedFile(report, maxTableBytes)
	if err != nil {
		return "", err
	}
	var rows []MigrationItem
	if err = json.Unmarshal(raw, &rows); err != nil {
		return "", err
	}
	paths := []string{}
	for _, row := range rows {
		if row.Type != kind {
			continue
		}
		if name != "" && row.PreviousName != name && row.Name != name {
			continue
		}
		if !inside(row.From, l.UserDir) {
			return "", fmt.Errorf("迁移报告源位置越出用户资源库")
		}
		rel, e := filepath.Rel(l.UserDir, row.From)
		if e != nil {
			return "", e
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 2 || parts[0] != typeDirectories[kind] || !validName(parts[1]) {
			return "", fmt.Errorf("迁移报告源位置无效")
		}
		path := filepath.Join(l.UserDir, ".migration-v6", filepath.FromSlash(rel))
		if err = rejectResourcePath(path); err != nil {
			return "", err
		}
		archived := path
		info, statErr := os.Stat(archived)
		if statErr != nil {
			return "", statErr
		}
		if info.IsDir() {
			manifestPath := filepath.Join(archived, "resource.yaml")
			if err = rejectResourcePath(manifestPath); err != nil {
				return "", err
			}
			manifestRaw, e := boundedFile(manifestPath, maxTableBytes)
			if e != nil {
				return "", e
			}
			var m Manifest
			if e = yaml.Unmarshal(manifestRaw, &m); e != nil {
				return "", e
			}
			if e = ValidateManifest(&m, false); e != nil {
				return "", e
			}
			if m.Type != kind || m.Entry == "" {
				return "", fmt.Errorf("归档资源包类型或入口无效")
			}
			if id != "" && m.ID != id && !(m.ID == "" && legacyID(kind, row.From, l.UserDir) == id) {
				continue
			}
			path = filepath.Join(archived, filepath.FromSlash(m.Entry))
			if !inside(path, archived) {
				return "", fmt.Errorf("归档资源包入口越界")
			}
			if err = rejectResourcePath(path); err != nil {
				return "", err
			}
		}
		if id != "" && !info.IsDir() {
			item := inspectItem(path, kind, "migration", filepath.Dir(path))
			if item.ID != id && !(item.Legacy && legacyID(kind, row.From, l.UserDir) == id) {
				continue
			}
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return "", ErrNotFound
	}
	if len(paths) > 1 {
		return "", ErrConflict
	}
	if kind == "data_list" && strings.EqualFold(filepath.Ext(paths[0]), ".xlsx") {
		workbook, e := xlsx.Open(paths[0])
		if e != nil {
			return "", e
		}
		if len(workbook.Sheets) == 0 {
			return "", fmt.Errorf("旧工作簿没有工作表")
		}
		_, cells, e := workbook.ReadSheet(workbook.Sheets[0])
		if e != nil {
			return "", e
		}
		numbers := []int{}
		width := 0
		for row, values := range cells {
			numbers = append(numbers, row)
			for column := range values {
				if column > width {
					width = column
				}
			}
		}
		sort.Ints(numbers)
		var buffer bytes.Buffer
		writer := csv.NewWriter(&buffer)
		for _, number := range numbers {
			record := make([]string, width)
			for column, value := range cells[number] {
				record[column-1] = value
			}
			_ = writer.Write(record)
		}
		writer.Flush()
		return buffer.String(), writer.Error()
	}
	ext := strings.ToLower(filepath.Ext(paths[0]))
	if ext != ".md" && ext != ".txt" && !(kind == "data_list" && ext == ".csv") {
		return "", fmt.Errorf("旧参考文件 %s 不能直接转换为正文，原件已保留在备份中", ext)
	}
	content, err := boundedFile(paths[0], maxTableBytes)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(content) {
		return "", fmt.Errorf("旧参考文本不是 UTF-8，原件已保留在备份中")
	}
	return string(content), nil
}

func writeMigrationReport(path string, rows []MigrationItem) error {
	raw, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".migration-report-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func backupMigrationResources(source, target string) error {
	if _, err := os.Stat(target); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	temp, err := os.MkdirTemp(filepath.Dir(target), ".resource-backup-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	if err = copyResource(source, temp); err != nil {
		return err
	}
	return os.Rename(temp, target)
}

// A migrated preset uses canonical CSV columns even if its original was XLSX,
// had translated headers, or selected a custom mapping filename.
func normalizedPresetTable(path, kind string) ([]byte, error) {
	rows, err := readTable(path, kind)
	if err != nil {
		return nil, err
	}
	keys := []string{}
	for _, column := range Columns(kind) {
		keys = append(keys, column.Key)
	}
	var buffer bytes.Buffer
	w := csv.NewWriter(&buffer)
	_ = w.Write(keys)
	for _, row := range rows {
		values := []string{}
		for _, key := range keys {
			value := row.fields[key]
			text := ""
			if items, ok := value.([]string); ok {
				text = strings.Join(items, "、")
			} else if value != nil {
				text = fmt.Sprint(value)
			}
			values = append(values, text)
		}
		_ = w.Write(values)
	}
	w.Flush()
	return buffer.Bytes(), w.Error()
}

// LegacyMigrationItems is data driven: the flow converter does not know any
// built-in set or preset name. User migrations take precedence over the shipped
// mapping because a conflicting import may have received a unique display name.
func (l *Library) LegacyMigrationItems() ([]MigrationItem, error) {
	result := []MigrationItem{}
	for _, path := range []string{filepath.Join(filepath.Dir(l.BuiltinDir), "migrations", "v6.json"), filepath.Join(l.UserDir, ".migration-v6-report.json")} {
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var rows []MigrationItem
		if err = json.Unmarshal(raw, &rows); err != nil {
			return nil, fmt.Errorf("读取资源迁移映射 %s: %w", path, err)
		}
		result = append(result, rows...)
	}
	return result, nil
}

func (l *Library) nextName(kind, name string) (string, error) {
	for n := 1; n < 10000; n++ {
		candidate := name
		if n > 1 {
			candidate = fmt.Sprintf("%s（%d）", name, n)
		}
		if err := l.checkName(kind, candidate, ""); err == nil {
			return candidate, nil
		} else if !errors.Is(err, ErrConflict) {
			return "", err
		}
	}
	return "", fmt.Errorf("无法分配唯一资源名称")
}
func (l *Library) migrateUnit(path, kind, oldName string) ([]MigrationItem, error) {
	rows := []MigrationItem{}
	created := []string{}
	complete := false
	defer func() {
		if !complete {
			for _, path := range created {
				_ = os.RemoveAll(path)
			}
			l.invalidate()
		}
	}()
	switch kind {
	case "style_set":
		resolved := l.resolveItem(inspectItem(path, kind, "migration", filepath.Dir(path)), Ref{Type: kind}, Scope{})
		if HasErrors(resolved.Problems) {
			return nil, fmt.Errorf("无法迁移样式集 %s：%v", path, resolved.Problems)
		}
		for _, entry := range resolved.Entries {
			name, err := l.nextName("style", oldName+"-"+entry.Name)
			if err != nil {
				return nil, err
			}
			source := inspectItem(entry.Path, "style", "migration", filepath.Dir(entry.Path))
			m := manifestForItem(source)
			m.LabelField = entry.Extra["label_field"]
			m.Tags = splitList(entry.Extra["aliases"])
			m.Description = entry.Extra["description"]
			source.Manifest = &m
			imported, err := l.importItem(source, name, "")
			if err != nil {
				return nil, err
			}
			created = append(created, imported.Root)
			rows = append(rows, MigrationItem{From: entry.Path, To: imported.Root, Type: "style", Name: imported.Name, PreviousName: oldName})
		}
	case "style", "layout":
		source := inspectItem(path, kind, "migration", filepath.Dir(path))
		name, err := l.nextName(kind, oldName)
		if err != nil {
			return nil, err
		}
		imported, err := l.importItem(source, name, "")
		if err != nil {
			return nil, err
		}
		created = append(created, imported.Root)
		rows = append(rows, MigrationItem{From: path, To: imported.Root, Type: kind, Name: name, PreviousName: oldName})
	case "route_style", "symbols":
		group := "styles"
		if kind == "symbols" {
			group = "symbols"
		}
		name := strings.TrimPrefix(oldName, "路线样式-")
		roots := []string{filepath.Join(filepath.Dir(l.UserDir), "presets", "route-cartography", group)}
		for _, d := range l.ToolDirs {
			if strings.Contains(d, "route-cartography") {
				roots = append(roots, filepath.Join(d, group))
			}
		}
		ext := ""
		if info, _ := os.Stat(path); info != nil && !info.IsDir() {
			ext = ".csv"
		}
		candidate := name
		for n := 1; ; n++ {
			found := false
			for _, root := range roots {
				for _, base := range []string{candidate + ext, "路线样式-" + candidate + ext} {
					if _, err := os.Stat(filepath.Join(root, base)); err == nil {
						found = true
					}
				}
			}
			if !found {
				break
			}
			candidate = fmt.Sprintf("%s（%d）", name, n+1)
		}
		target := filepath.Join(roots[0], candidate+ext)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return nil, err
		}
		created = append(created, target)
		item := inspectItem(path, kind, "migration", filepath.Dir(path))
		if HasErrors(item.Problems) {
			return nil, fmt.Errorf("预设清单无效：%v", item.Problems)
		}
		table := item.Entry
		if kind == "symbols" {
			if err := copyResource(path, target); err != nil {
				return nil, err
			}
		}
		normalized, err := normalizedPresetTable(table, kind)
		if err != nil {
			return nil, err
		}
		output := target
		if kind == "symbols" {
			output = filepath.Join(target, "对照表.csv")
		}
		if err := os.WriteFile(output, normalized, 0600); err != nil {
			return nil, err
		}
		rows = append(rows, MigrationItem{From: path, To: target, Type: kind, Name: candidate, PreviousName: oldName})
	default:
		rows = append(rows, MigrationItem{From: path, Type: kind, Name: oldName, PreviousName: oldName, Note: "由流程转换器将说明内容写入正文；原文件保留于完整备份"})
	}
	complete = true
	return rows, nil
}

// MigrateFlowResources is local and idempotent. The flow converter owns the
// complete flow backup and deletes the old resources only after writing its new body.
func (l *Library) MigrateFlowResources(flowDir string) ([]MigrationItem, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	reportPath := filepath.Join(flowDir, ".resource-migration-v6.json")
	if raw, err := os.ReadFile(reportPath); err == nil {
		var rows []MigrationItem
		if json.Unmarshal(raw, &rows) == nil {
			return rows, nil
		}
	}
	root := filepath.Join(flowDir, "resources")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return []MigrationItem{}, nil
	}
	backup := filepath.Join(flowDir, ".resources-v5-backup")
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		if err = backupMigrationResources(root, backup); err != nil {
			return nil, err
		}
	}
	units, err := migrationUnits(root)
	if err != nil {
		return nil, err
	}
	result := []MigrationItem{}
	complete := false
	defer func() {
		if !complete {
			for _, row := range result {
				if row.To != "" {
					_ = os.RemoveAll(row.To)
				}
			}
			l.invalidate()
		}
	}()
	for _, unit := range units {
		rows, err := l.migrateUnit(unit.From, unit.Type, unit.Name)
		if err != nil {
			return nil, err
		}
		result = append(result, rows...)
	}
	if err = writeMigrationReport(reportPath, result); err != nil {
		return nil, err
	}
	complete = true
	return result, nil
}

// MigrateV6 backs up the entire user library before moving legacy units out of
// its public index. Failed units remain in place and are retried on next launch.
func (l *Library) MigrateV6() ([]MigrationItem, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := os.Stat(l.UserDir); os.IsNotExist(err) {
		return []MigrationItem{}, nil
	}
	units, err := migrationUnits(l.UserDir)
	if err != nil {
		return nil, err
	}
	legacy := []MigrationItem{}
	for _, unit := range units {
		info, _ := os.Stat(unit.From)
		if !IsPublicType(unit.Type) || info != nil && !info.IsDir() {
			legacy = append(legacy, unit)
		}
	}
	if len(legacy) == 0 {
		return []MigrationItem{}, nil
	}
	backup := filepath.Join(filepath.Dir(l.UserDir), ".backups", "resources-v5")
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		if err = backupMigrationResources(l.UserDir, backup); err != nil {
			return nil, err
		}
	}
	result := []MigrationItem{}
	reportPath := filepath.Join(l.UserDir, ".migration-v6-report.json")
	if raw, e := os.ReadFile(reportPath); e == nil {
		if e = json.Unmarshal(raw, &result); e != nil {
			return nil, e
		}
	}
	for _, unit := range legacy {
		hidden := filepath.Join(l.UserDir, ".migration-v6", filepath.Base(filepath.Dir(unit.From)), filepath.Base(unit.From))
		if err = os.MkdirAll(filepath.Dir(hidden), 0700); err != nil {
			return nil, err
		}
		if err = os.Rename(unit.From, hidden); err != nil {
			return nil, err
		}
		manifest := ManifestPath(unit.From, unit.Type)
		if manifest != filepath.Join(unit.From, "resource.yaml") {
			if _, e := os.Stat(manifest); e == nil {
				_ = os.Rename(manifest, ManifestPath(hidden, unit.Type))
			}
		}
		l.invalidate()
		rows, e := l.migrateUnit(hidden, unit.Type, unit.Name)
		if e != nil {
			_ = os.Rename(hidden, unit.From)
			if manifest != filepath.Join(unit.From, "resource.yaml") {
				_ = os.Rename(ManifestPath(hidden, unit.Type), manifest)
			}
			l.invalidate()
			return result, e
		}
		for i := range rows {
			rows[i].From = unit.From
		}
		previous := len(result)
		result = append(result, rows...)
		if err = writeMigrationReport(reportPath, result); err != nil {
			for _, row := range rows {
				if row.To != "" {
					_ = os.RemoveAll(row.To)
				}
			}
			_ = os.Rename(hidden, unit.From)
			if manifest != filepath.Join(unit.From, "resource.yaml") {
				_ = os.Rename(ManifestPath(hidden, unit.Type), manifest)
			}
			l.invalidate()
			return result[:previous], err
		}
	}
	return result, nil
}
func migrationUnits(root string) ([]MigrationItem, error) {
	result := []MigrationItem{}
	groups := []string{"styles", "layouts", "symbols", "data", "docs"}
	for _, group := range groups {
		entries, err := os.ReadDir(filepath.Join(root, group))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") || strings.HasSuffix(e.Name(), ".resource.yaml") {
				continue
			}
			path := filepath.Join(root, group, e.Name())
			if err := rejectResourcePath(path); err != nil {
				return nil, err
			}
			kind, name := "", ""
			manifest := filepath.Join(path, "resource.yaml")
			if !e.IsDir() {
				manifest = ManifestPath(path, "")
			}
			var m Manifest
			if raw, err := os.ReadFile(manifest); err == nil && yaml.Unmarshal(raw, &m) == nil {
				kind, name = m.Type, m.Name
			}
			if kind == "" {
				for _, typ := range legacyFormatSet.Types {
					if typ.Group != group || typ.Folder != e.IsDir() {
						continue
					}
					if e.IsDir() {
						kind = typ.Type
						break
					}
					for _, ext := range typ.EntryExtensions {
						if strings.HasSuffix(strings.ToLower(e.Name()), ext) {
							kind = typ.Type
							break
						}
					}
					if kind != "" {
						break
					}
				}
			}
			if kind == "" {
				continue
			}
			if name == "" {
				name = resourceBaseName(path, kind)
			}
			result = append(result, MigrationItem{From: path, Type: kind, Name: name})
		}
	}
	return result, nil
}
