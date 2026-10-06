package knowledge

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	maxCalloutWorkbookBytes = 100 << 20
	maxCalloutXMLBytes      = 16 << 20
	maxCalloutZipMembers    = 10000
	maxCalloutSheetRows     = 10000
	maxCalloutSheetCells    = 100000
)

// calloutImportRow retains source text and the original Excel row number. Image
// paths are deliberately left unresolved for the caller's asset import checks.
type calloutImportRow struct {
	Sheet     string
	Row       int
	Place     string
	Body      string
	Caption   string
	Image     []byte
	ImagePath string
}

type calloutXLSXArchive struct {
	files map[string]*zip.File
	cache map[string][]byte
	read  uint64
}

type calloutXLSXRelationship struct {
	ID         string `xml:"Id,attr"`
	Type       string `xml:"Type,attr"`
	Target     string `xml:"Target,attr"`
	TargetMode string `xml:"TargetMode,attr"`
}

type calloutXLSXText struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func (t calloutXLSXText) value() string {
	var b strings.Builder
	b.WriteString(t.Text)
	for _, run := range t.Runs {
		b.WriteString(run.Text)
	}
	return b.String()
}

type calloutXLSXCell struct {
	Reference string          `xml:"r,attr"`
	Type      string          `xml:"t,attr"`
	Value     string          `xml:"v"`
	Inline    calloutXLSXText `xml:"is"`
}

type calloutXLSXSheet struct {
	XMLName xml.Name `xml:"worksheet"`
	Rows    []struct {
		Number int               `xml:"r,attr"`
		Cells  []calloutXLSXCell `xml:"c"`
	} `xml:"sheetData>row"`
	Drawings []struct {
		ID string `xml:"id,attr"`
	} `xml:"drawing"`
}

type calloutXLSXColumns struct {
	place, body, caption, imagePath int
	headerRow                       int
}

// readCalloutWorkbook supports .xlsx shared/inline strings and standard
// oneCellAnchor/twoCellAnchor drawings. It never evaluates formulas, follows
// external relationships, extracts files, or changes the supplied workbook.
func readCalloutWorkbook(sourcePath string) ([]calloutImportRow, []string, error) {
	if !filepath.IsAbs(sourcePath) || !strings.EqualFold(filepath.Ext(sourcePath), ".xlsx") {
		return nil, nil, fmt.Errorf("%w: 请选择绝对路径的 .xlsx 文件（不支持 .xls）", ErrReviewInvalid)
	}
	resolved, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: 无法读取 Excel：%v", ErrReviewInvalid, err)
	}
	if resolved != filepath.Clean(sourcePath) {
		return nil, nil, fmt.Errorf("%w: Excel 路径不能包含符号链接", ErrReviewInvalid)
	}
	raw, err := readReviewFile(sourcePath, maxCalloutWorkbookBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: 无法读取 Excel：%v", ErrReviewInvalid, err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil || len(zr.File) > maxCalloutZipMembers {
		return nil, nil, fmt.Errorf("%w: Excel 不是有效的 .xlsx 或包含过多文件", ErrReviewInvalid)
	}
	a := &calloutXLSXArchive{files: make(map[string]*zip.File), cache: make(map[string][]byte)}
	var total uint64
	for _, file := range zr.File {
		name := strings.TrimSuffix(file.Name, "/")
		if name == "" || strings.ContainsAny(name, "\\\x00") || path.IsAbs(name) || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || file.Mode()&os.ModeSymlink != 0 {
			return nil, nil, fmt.Errorf("%w: Excel 包含非法文件路径", ErrReviewInvalid)
		}
		if _, duplicate := a.files[name]; duplicate {
			return nil, nil, fmt.Errorf("%w: Excel 包含重复文件路径", ErrReviewInvalid)
		}
		if file.UncompressedSize64 > maxCalloutWorkbookBytes-total {
			return nil, nil, fmt.Errorf("%w: Excel 解压后超过 100 MiB", ErrReviewInvalid)
		}
		total += file.UncompressedSize64
		a.files[name] = file
	}
	// Validate every relationship, including unused sheets, before touching any
	// image. Valid package-relative ../ references stay inside the ZIP root.
	for _, file := range zr.File {
		if strings.HasSuffix(file.Name, ".rels") {
			if _, err = a.relationshipsForFile(file.Name); err != nil {
				return nil, nil, fmt.Errorf("%w: Excel 关系文件无效：%v", ErrReviewInvalid, err)
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
	if err = a.xml("xl/workbook.xml", &workbook); err != nil {
		return nil, nil, fmt.Errorf("%w: 无法读取 Excel 工作簿：%v", ErrReviewInvalid, err)
	}
	rels, err := a.relationships("xl/workbook.xml")
	if err != nil {
		return nil, nil, fmt.Errorf("%w: 无法读取工作表关系：%v", ErrReviewInvalid, err)
	}
	var shared []string
	for _, rel := range rels {
		if strings.HasSuffix(rel.Type, "/sharedStrings") {
			var sst struct {
				XMLName xml.Name          `xml:"sst"`
				Strings []calloutXLSXText `xml:"si"`
			}
			if err = a.xml(rel.Target, &sst); err != nil {
				return nil, nil, fmt.Errorf("%w: 无法读取共享文本：%v", ErrReviewInvalid, err)
			}
			for _, item := range sst.Strings {
				shared = append(shared, item.value())
			}
			break
		}
	}
	sort.SliceStable(workbook.Sheets, func(i, j int) bool {
		return workbook.Sheets[i].Name == "图文标注" && workbook.Sheets[j].Name != "图文标注"
	})
	var warnings []string
	for _, sheet := range workbook.Sheets {
		rel, ok := rels[sheet.ID]
		if !ok || !strings.HasSuffix(rel.Type, "/worksheet") {
			continue
		}
		var data calloutXLSXSheet
		if err = a.xml(rel.Target, &data); err != nil {
			warnings = append(warnings, fmt.Sprintf("工作表 %s 无法读取：%v", sheet.Name, err))
			continue
		}
		cells, cellErr := calloutSheetCells(data, shared)
		if cellErr != nil {
			return nil, warnings, fmt.Errorf("%w: 工作表 %s：%v", ErrReviewInvalid, sheet.Name, cellErr)
		}
		columns, ok := calloutFindColumns(cells)
		if !ok {
			continue
		}
		var rows []calloutImportRow
		var numbers []int
		for row := range cells {
			if row > columns.headerRow {
				numbers = append(numbers, row)
			}
		}
		sort.Ints(numbers)
		for _, number := range numbers {
			cell := cells[number]
			row := calloutImportRow{Sheet: sheet.Name, Row: number, Place: strings.TrimSpace(cell[columns.place]), Body: strings.TrimSpace(cell[columns.body]), Caption: strings.TrimSpace(cell[columns.caption]), ImagePath: strings.TrimSpace(cell[columns.imagePath])}
			if row.ImagePath == "" && calloutLooksLikeImagePath(row.Caption) {
				row.ImagePath, row.Caption = row.Caption, ""
			}
			if row.Place != "" || row.Body != "" || row.Caption != "" || row.ImagePath != "" {
				rows = append(rows, row)
			}
		}
		if len(rows) == 0 {
			continue
		}
		warnings = append(warnings, a.attachImages(rel.Target, data, rows)...)
		return rows, warnings, nil
	}
	return nil, warnings, fmt.Errorf("%w: 未找到包含“地点”和“正文/文本”表头及数据的工作表", ErrReviewInvalid)
}

func (a *calloutXLSXArchive) bytes(name string, limit uint64) ([]byte, error) {
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
	if file.UncompressedSize64 > limit || file.UncompressedSize64 > maxCalloutWorkbookBytes-a.read {
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

func (a *calloutXLSXArchive) xml(name string, out any) error {
	raw, err := a.bytes(name, maxCalloutXMLBytes)
	if err != nil {
		return err
	}
	return xml.Unmarshal(raw, out)
}

func (a *calloutXLSXArchive) relationships(owner string) (map[string]calloutXLSXRelationship, error) {
	return a.relationshipsForFile(path.Join(path.Dir(owner), "_rels", path.Base(owner)+".rels"))
}

func (a *calloutXLSXArchive) relationshipsForFile(name string) (map[string]calloutXLSXRelationship, error) {
	var document struct {
		XMLName       xml.Name                  `xml:"Relationships"`
		Relationships []calloutXLSXRelationship `xml:"Relationship"`
	}
	if err := a.xml(name, &document); err != nil {
		return nil, err
	}
	base := path.Dir(path.Dir(name))
	result := make(map[string]calloutXLSXRelationship)
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

func calloutSheetCells(sheet calloutXLSXSheet, shared []string) (map[int]map[int]string, error) {
	if len(sheet.Rows) > maxCalloutSheetRows {
		return nil, fmt.Errorf("行数超过 %d", maxCalloutSheetRows)
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
			if count > maxCalloutSheetCells {
				return nil, fmt.Errorf("单元格超过 %d", maxCalloutSheetCells)
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

func calloutFindColumns(rows map[int]map[int]string) (calloutXLSXColumns, bool) {
	for number := 1; number <= 50; number++ {
		columns := calloutXLSXColumns{headerRow: number}
		var ordered []int
		for column := range rows[number] {
			ordered = append(ordered, column)
		}
		sort.Ints(ordered)
		for _, column := range ordered {
			text := rows[number][column]
			switch strings.TrimSpace(text) {
			case "地点", "地名", "位置":
				if columns.place == 0 {
					columns.place = column
				}
			case "文本", "正文", "内容", "注记":
				if columns.body == 0 {
					columns.body = column
				}
			case "说明", "图片说明", "图片":
				columns.caption = column
			case "图片路径":
				columns.imagePath = column
			}
		}
		if columns.place > 0 && columns.body > 0 {
			return columns, true
		}
	}
	return calloutXLSXColumns{}, false
}

func calloutLooksLikeImagePath(value string) bool {
	if strings.ContainsAny(value, "\r\n") {
		return false
	}
	switch strings.ToLower(filepath.Ext(value)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff":
		return true
	}
	return false
}

func (a *calloutXLSXArchive) attachImages(sheetPath string, sheet calloutXLSXSheet, rows []calloutImportRow) []string {
	if len(sheet.Drawings) == 0 {
		return nil
	}
	var warnings []string
	rels, err := a.relationships(sheetPath)
	if err != nil {
		return []string{fmt.Sprintf("工作表 %s 的图片关系无法读取，已保留文字：%v", rows[0].Sheet, err)}
	}
	byNumber := make(map[int]*calloutImportRow)
	for i := range rows {
		byNumber[rows[i].Row] = &rows[i]
	}
	seen := make(map[int]bool)
	for _, drawing := range sheet.Drawings {
		rel, ok := rels[drawing.ID]
		if !ok || !strings.HasSuffix(rel.Type, "/drawing") {
			warnings = append(warnings, fmt.Sprintf("工作表 %s 缺少图片关系 %s，已保留文字", rows[0].Sheet, drawing.ID))
			continue
		}
		drawingRels, relErr := a.relationships(rel.Target)
		raw, readErr := a.bytes(rel.Target, maxCalloutXMLBytes)
		if relErr != nil || readErr != nil {
			warnings = append(warnings, fmt.Sprintf("工作表 %s 的图片信息无法读取，已保留文字", rows[0].Sheet))
			continue
		}
		decoder := xml.NewDecoder(bytes.NewReader(raw))
		for {
			token, tokenErr := decoder.Token()
			if tokenErr == io.EOF {
				break
			}
			if tokenErr != nil {
				warnings = append(warnings, fmt.Sprintf("工作表 %s 的图片 XML 损坏，已保留文字", rows[0].Sheet))
				break
			}
			start, ok := token.(xml.StartElement)
			if !ok || start.Name.Local != "oneCellAnchor" && start.Name.Local != "twoCellAnchor" {
				continue
			}
			var anchor struct {
				From struct {
					Row *int `xml:"row"`
				} `xml:"from"`
				To struct {
					Row *int `xml:"row"`
				} `xml:"to"`
				Pictures []struct {
					Blip struct {
						Embed string `xml:"embed,attr"`
						Link  string `xml:"link,attr"`
					} `xml:"blipFill>blip"`
				} `xml:"pic"`
			}
			if err = decoder.DecodeElement(&anchor, &start); err != nil {
				warnings = append(warnings, fmt.Sprintf("工作表 %s 的图片锚点损坏，已保留文字", rows[0].Sheet))
				break
			}
			if anchor.From.Row == nil || *anchor.From.Row < 0 || *anchor.From.Row >= 1048576 {
				warnings = append(warnings, fmt.Sprintf("工作表 %s 存在无有效行号的图片，已跳过", rows[0].Sheet))
				continue
			}
			number := *anchor.From.Row + 1
			row := byNumber[number]
			if row == nil {
				warnings = append(warnings, fmt.Sprintf("工作表 %s 第 %d 行图片没有对应文字行，已跳过", rows[0].Sheet, number))
				continue
			}
			if anchor.To.Row != nil && *anchor.To.Row > *anchor.From.Row+1 {
				warnings = append(warnings, fmt.Sprintf("工作表 %s 第 %d 行图片跨行，按左上角所在行导入", row.Sheet, number))
			}
			for _, picture := range anchor.Pictures {
				if seen[number] {
					warnings = append(warnings, fmt.Sprintf("工作表 %s 第 %d 行有多张图片，仅使用第一张", row.Sheet, number))
					continue
				}
				seen[number] = true
				imageRel, exists := drawingRels[picture.Blip.Embed]
				if !exists || picture.Blip.Link != "" || !strings.HasSuffix(imageRel.Type, "/image") {
					warnings = append(warnings, fmt.Sprintf("工作表 %s 第 %d 行图片引用缺失或不支持，已保留文字", row.Sheet, number))
					continue
				}
				imageBytes, imageErr := a.bytes(imageRel.Target, MaxReviewImageBytes)
				if imageErr == nil {
					cfg, format, decodeErr := image.DecodeConfig(bytes.NewReader(imageBytes))
					if decodeErr != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 40000000 || format != "png" && format != "jpeg" && format != "gif" {
						imageErr = fmt.Errorf("不支持或损坏的图片")
					}
				}
				if imageErr != nil {
					warnings = append(warnings, fmt.Sprintf("工作表 %s 第 %d 行图片读取失败，已保留文字：%v", row.Sheet, number, imageErr))
					continue
				}
				row.Image = imageBytes
			}
		}
	}
	return warnings
}
