// Package xlsx 读取受限的 Office Open XML 工作簿，不计算公式、不访问外部关系、不解压到磁盘。
package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	MaxWorkbookBytes = 100 << 20
	MaxXMLBytes      = 16 << 20
	MaxZipMembers    = 10000
	MaxSheetRows     = 10000
	MaxSheetCells    = 100000
)

var ErrInvalid = errors.New("Excel 文件无效")

type SheetInfo struct{ Name, Path string }
type Workbook struct {
	files  map[string]*zip.File
	cache  map[string][]byte
	read   uint64
	Sheets []SheetInfo
	shared []string
}
type Relationship struct {
	ID         string `xml:"Id,attr"`
	Type       string `xml:"Type,attr"`
	Target     string `xml:"Target,attr"`
	TargetMode string `xml:"TargetMode,attr"`
}

type CellText struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func (t CellText) value() string {
	var b strings.Builder
	b.WriteString(t.Text)
	for _, run := range t.Runs {
		b.WriteString(run.Text)
	}
	return b.String()
}

type Cell struct {
	Reference string   `xml:"r,attr"`
	Type      string   `xml:"t,attr"`
	Value     string   `xml:"v"`
	Inline    CellText `xml:"is"`
}

type Sheet struct {
	XMLName xml.Name `xml:"worksheet"`
	Rows    []struct {
		Number int    `xml:"r,attr"`
		Cells  []Cell `xml:"c"`
	} `xml:"sheetData>row"`
	Drawings []struct {
		ID string `xml:"id,attr"`
	} `xml:"drawing"`
}

func Open(sourcePath string) (*Workbook, error) {
	if !filepath.IsAbs(sourcePath) || !strings.EqualFold(filepath.Ext(sourcePath), ".xlsx") {
		return nil, fmt.Errorf("%w: 请选择绝对路径的 .xlsx 文件（不支持 .xls）", ErrInvalid)
	}
	resolved, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("%w: 无法读取 Excel：%v", ErrInvalid, err)
	}
	if resolved != filepath.Clean(sourcePath) {
		return nil, fmt.Errorf("%w: Excel 路径不能包含符号链接", ErrInvalid)
	}
	raw, err := readWorkbookFile(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("%w: 无法读取 Excel：%v", ErrInvalid, err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil || len(zr.File) > MaxZipMembers {
		return nil, fmt.Errorf("%w: Excel 不是有效的 .xlsx 或包含过多文件", ErrInvalid)
	}
	a := &Workbook{files: make(map[string]*zip.File), cache: make(map[string][]byte)}
	var total uint64
	for _, file := range zr.File {
		name := strings.TrimSuffix(file.Name, "/")
		if name == "" || strings.ContainsAny(name, "\\\x00") || path.IsAbs(name) || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || file.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: Excel 包含非法文件路径", ErrInvalid)
		}
		if _, duplicate := a.files[name]; duplicate {
			return nil, fmt.Errorf("%w: Excel 包含重复文件路径", ErrInvalid)
		}
		if file.UncompressedSize64 > MaxWorkbookBytes-total {
			return nil, fmt.Errorf("%w: Excel 解压后超过 100 MiB", ErrInvalid)
		}
		total += file.UncompressedSize64
		a.files[name] = file
	}
	// Validate every relationship, including unused sheets, before touching any
	// image. Valid package-relative ../ references stay inside the ZIP root.
	for _, file := range zr.File {
		if strings.HasSuffix(file.Name, ".rels") {
			if _, err = a.relationshipsForFile(file.Name); err != nil {
				return nil, fmt.Errorf("%w: Excel 关系文件无效：%v", ErrInvalid, err)
			}
		}
	}
	var workbook struct {
		XMLName xml.Name `xml:"workbook"`
		Sheets  []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err = a.XML("xl/workbook.xml", &workbook); err != nil {
		return nil, fmt.Errorf("%w: 无法读取 Excel 工作簿：%v", ErrInvalid, err)
	}
	rels, err := a.Relationships("xl/workbook.xml")
	if err != nil {
		return nil, fmt.Errorf("%w: 无法读取工作表关系：%v", ErrInvalid, err)
	}
	var shared []string
	for _, rel := range rels {
		if strings.HasSuffix(rel.Type, "/sharedStrings") {
			var sst struct {
				XMLName xml.Name   `xml:"sst"`
				Strings []CellText `xml:"si"`
			}
			if err = a.XML(rel.Target, &sst); err != nil {
				return nil, fmt.Errorf("%w: 无法读取共享文本：%v", ErrInvalid, err)
			}
			for _, item := range sst.Strings {
				shared = append(shared, item.value())
			}
			break
		}
	}

	a.shared = shared
	for _, sheet := range workbook.Sheets {
		rel, ok := rels[sheet.ID]
		if ok && strings.HasSuffix(rel.Type, "/worksheet") {
			a.Sheets = append(a.Sheets, SheetInfo{Name: sheet.Name, Path: rel.Target})
		}
	}
	return a, nil
}

func readWorkbookFile(source string) ([]byte, error) {
	file, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxWorkbookBytes {
		return nil, fmt.Errorf("Excel 文件超过 100 MiB 或不是普通文件")
	}
	raw, err := io.ReadAll(io.LimitReader(file, MaxWorkbookBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxWorkbookBytes {
		return nil, fmt.Errorf("Excel 文件超过 100 MiB")
	}
	return raw, nil
}

func (a *Workbook) ReadSheet(sheet SheetInfo) (Sheet, map[int]map[int]string, error) {
	var data Sheet
	if err := a.XML(sheet.Path, &data); err != nil {
		return data, nil, err
	}
	cells, err := SheetCells(data, a.shared)
	return data, cells, err
}

func (a *Workbook) Cells(sheet Sheet) (map[int]map[int]string, error) {
	return SheetCells(sheet, a.shared)
}
func (a *Workbook) Bytes(name string, limit uint64) ([]byte, error) {
	if raw, ok := a.cache[name]; ok {
		if uint64(len(raw)) > limit {
			return nil, fmt.Errorf("文件 %s 过大", name)
		}
		return raw, nil
	}
	file, ok := a.files[name]
	if !ok || file.FileInfo().IsDir() {
		return nil, fmt.Errorf("文件 %s 不存在", name)
	}
	if file.UncompressedSize64 > limit || file.UncompressedSize64 > MaxWorkbookBytes-a.read {
		return nil, fmt.Errorf("文件 %s 过大", name)
	}
	r, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	raw, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(raw)) > limit || uint64(len(raw)) != file.UncompressedSize64 {
		return nil, fmt.Errorf("文件 %s 长度异常", name)
	}
	a.read += uint64(len(raw))
	a.cache[name] = raw
	return raw, nil
}

func (a *Workbook) XML(name string, out any) error {
	raw, err := a.Bytes(name, MaxXMLBytes)
	if err != nil {
		return err
	}
	return xml.Unmarshal(raw, out)
}

func (a *Workbook) Relationships(owner string) (map[string]Relationship, error) {
	return a.relationshipsForFile(path.Join(path.Dir(owner), "_rels", path.Base(owner)+".rels"))
}

func (a *Workbook) relationshipsForFile(name string) (map[string]Relationship, error) {
	var document struct {
		XMLName       xml.Name       `xml:"Relationships"`
		Relationships []Relationship `xml:"Relationship"`
	}
	if err := a.XML(name, &document); err != nil {
		return nil, err
	}
	base := path.Dir(path.Dir(name))
	result := make(map[string]Relationship)
	for _, rel := range document.Relationships {
		u, err := url.Parse(rel.Target)
		if err != nil || rel.TargetMode != "" && !strings.EqualFold(rel.TargetMode, "Internal") || u == nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" || u.Path == "" || strings.ContainsAny(u.Path, "\\\x00") || strings.HasPrefix(u.Path, "//") {
			return nil, fmt.Errorf("不支持外部或非法引用 %s", rel.ID)
		}
		if path.IsAbs(u.Path) {
			rel.Target = path.Clean(strings.TrimPrefix(u.Path, "/"))
		} else {
			rel.Target = path.Clean(path.Join(base, u.Path))
		}
		if rel.Target == "." || rel.Target == ".." || strings.HasPrefix(rel.Target, "../") || rel.ID == "" {
			return nil, fmt.Errorf("引用 %s 越出工作簿", rel.ID)
		}
		if _, duplicate := result[rel.ID]; duplicate {
			return nil, fmt.Errorf("重复引用 %s", rel.ID)
		}
		result[rel.ID] = rel
	}
	return result, nil
}

func SheetCells(sheet Sheet, shared []string) (map[int]map[int]string, error) {
	if len(sheet.Rows) > MaxSheetRows {
		return nil, fmt.Errorf("行数超过 %d", MaxSheetRows)
	}
	result := make(map[int]map[int]string)
	previousRow, count := 0, 0
	for _, row := range sheet.Rows {
		number := row.Number
		if number == 0 {
			number = previousRow + 1
		}
		if number <= previousRow || number > 1048576 {
			return nil, fmt.Errorf("行号无效或重复")
		}
		previousRow = number
		cells, previousColumn := make(map[int]string), 0
		for _, cell := range row.Cells {
			count++
			if count > MaxSheetCells {
				return nil, fmt.Errorf("单元格超过 %d", MaxSheetCells)
			}
			column := previousColumn + 1
			if cell.Reference != "" {
				column = 0
				i := 0
				for i < len(cell.Reference) && cell.Reference[i] >= 'A' && cell.Reference[i] <= 'Z' {
					column = column*26 + int(cell.Reference[i]-'A'+1)
					if column > 16384 {
						return nil, fmt.Errorf("列号越界")
					}
					i++
				}
				cellRow, err := strconv.Atoi(cell.Reference[i:])
				if err != nil || column == 0 || cellRow != number {
					return nil, fmt.Errorf("单元格地址 %s 无效", cell.Reference)
				}
			}
			if column <= previousColumn || column > 16384 {
				return nil, fmt.Errorf("列号无效或重复")
			}
			previousColumn = column
			value := cell.Value
			switch cell.Type {
			case "s":
				index, err := strconv.Atoi(strings.TrimSpace(value))
				if err != nil || index < 0 || index >= len(shared) {
					return nil, fmt.Errorf("第 %d 行共享文本索引无效", number)
				}
				value = shared[index]
			case "inlineStr":
				value = cell.Inline.value()
			}
			cells[column] = value
		}
		result[number] = cells
	}
	return result, nil
}
