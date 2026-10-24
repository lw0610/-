package knowledge

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"github.com/autocarto/carto_agent/internal/shared/xlsx"
	"image"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

const maxCalloutWorkbookBytes = xlsx.MaxWorkbookBytes

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

type calloutXLSXColumns struct {
	place, body, caption, imagePath int
	headerRow                       int
}

func readCalloutWorkbook(sourcePath string) ([]calloutImportRow, []string, error) {
	a, err := xlsx.Open(sourcePath)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrReviewInvalid, err)
	}
	sheets := append([]xlsx.SheetInfo(nil), a.Sheets...)
	sort.SliceStable(sheets, func(i, j int) bool { return sheets[i].Name == "图文标注" && sheets[j].Name != "图文标注" })
	var warnings []string
	for _, sheet := range sheets {
		var data xlsx.Sheet
		if err = a.XML(sheet.Path, &data); err != nil {
			warnings = append(warnings, fmt.Sprintf("工作表 %s 无法读取：%v", sheet.Name, err))
			continue
		}
		cells, err := a.Cells(data)
		if err != nil {
			return nil, warnings, fmt.Errorf("%w: 工作表 %s：%v", ErrReviewInvalid, sheet.Name, err)
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
		warnings = append(warnings, attachCalloutImages(a, sheet.Path, data, rows)...)
		return rows, warnings, nil
	}
	return nil, warnings, fmt.Errorf("%w: 未找到包含“地点”和“正文/文本”表头及数据的工作表", ErrReviewInvalid)
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

func attachCalloutImages(a *xlsx.Workbook, sheetPath string, sheet xlsx.Sheet, rows []calloutImportRow) []string {
	if len(sheet.Drawings) == 0 {
		return nil
	}
	var warnings []string
	rels, err := a.Relationships(sheetPath)
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
		drawingRels, relErr := a.Relationships(rel.Target)
		raw, readErr := a.Bytes(rel.Target, xlsx.MaxXMLBytes)
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
				imageBytes, imageErr := a.Bytes(imageRel.Target, MaxReviewImageBytes)
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
