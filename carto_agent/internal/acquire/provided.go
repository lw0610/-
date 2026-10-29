package acquire

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/shared/xlsx"
)

// ValidateProvided verifies a read-only user file through the same quality gate.
// A temporary normalization proves it is readable, without renaming fields or
// overwriting the original. Only the original file is then passed to the flow.
func (e *Engine) ValidateProvided(ctx context.Context, item flow.AcquisitionItem, path string) (ItemResult, error) {
	result := ItemResult{Key: item.Key, Status: "failed", Notes: []string{}, Attempts: []AttemptRecord{}, Output: OutputRecord{Paths: []string{}, Extent: []float64{}}}
	if e.Catalog == nil {
		return result, fmt.Errorf("数据类别目录不可用")
	}
	category := e.Catalog.Category(item.Category)
	if category == nil {
		return result, fmt.Errorf("数据类别不存在")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return result, fmt.Errorf("提供的文件不可读取")
	}
	paths := []string{path}
	if strings.EqualFold(filepath.Ext(path), ".shp") {
		stem := strings.TrimSuffix(path, filepath.Ext(path))
		for _, ext := range []string{".shx", ".dbf", ".prj", ".cpg"} {
			side := stem + ext
			if f, err := os.Stat(side); err == nil && f.Mode().IsRegular() {
				paths = append(paths, side)
			} else if ext == ".shx" || ext == ".dbf" {
				return result, fmt.Errorf("Shapefile 缺少 %s", ext)
			}
		}
	}
	var stats map[string]any
	if category.Geometry == "tiles" {
		copy := *category
		copy.Geometry = "raster"
		category = &copy
	}
	if category.Geometry == "table" {
		stats, err = inspectProvidedTable(path, item.Fields)
	} else {
		if e.Store == nil || e.Invoke == nil {
			return result, fmt.Errorf("数据核验工具未配置")
		}
		if err = os.MkdirAll(e.Store.Root, 0700); err != nil {
			return result, err
		}
		work, err := os.MkdirTemp(e.Store.Root, ".verify-")
		if err != nil {
			return result, err
		}
		defer os.RemoveAll(work)
		suffix := ".gpkg"
		if category.Geometry == "raster" {
			suffix = ".tif"
		}
		args := map[string]any{"input": map[string]any{"path": path}, "output": filepath.Join(work, "checked"+suffix), "expected_geometry": category.Geometry, "fields": map[string][]string{}, "standard_fields": []string{}}
		value, callErr := e.Invoke(ctx, "data_normalize", args)
		if callErr != nil {
			return result, callErr
		}
		payload, callErr := UnwrapToolResult(value)
		if callErr != nil {
			return result, callErr
		}
		stats = toolData(payload)
	}
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	validation := ValidateStats(category, item, stats)
	if !validation.OK {
		return result, fmt.Errorf("提供的数据未通过核验：%s", strings.Join(validation.Errors, "；"))
	}
	result.Status = "user_provided"
	result.Output = OutputRecord{Paths: paths, Count: int(number(stats["count"])), Extent: floatSlice(stats["extent_wgs84"])}
	result.Notes = append([]string{"用户提供；已检查可读性、类型和声明字段，原文件未修改"}, validation.Warnings...)
	return result, nil
}

// Tables remain Go-owned; the QGIS/Python channel never parses a CSV/XLSX manifest.
func inspectProvidedTable(path string, fields map[string]string) (map[string]any, error) {
	records, err := readProvidedTable(path)
	if err != nil {
		return nil, err
	}
	return inspectTableRecords(records, fields)
}

func readProvidedTable(path string) ([][]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > 16<<20 {
		return nil, fmt.Errorf("表格超过16MB核验限制")
	}
	records := [][]string{}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv", ".tsv":
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if !utf8.Valid(raw) {
			return nil, fmt.Errorf("表格需要UTF-8编码")
		}
		reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(raw), "\ufeff")))
		reader.FieldsPerRecord = -1
		if strings.EqualFold(filepath.Ext(path), ".tsv") {
			reader.Comma = '\t'
		}
		for {
			row, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			records = append(records, row)
		}
	case ".xlsx":
		book, err := xlsx.Open(path)
		if err != nil {
			return nil, err
		}
		for _, sheet := range book.Sheets {
			_, cells, err := book.ReadSheet(sheet)
			if err != nil {
				return nil, err
			}
			numbers := []int{}
			for n := range cells {
				numbers = append(numbers, n)
			}
			sort.Ints(numbers)
			for _, n := range numbers {
				width := 0
				for col := range cells[n] {
					if col+1 > width {
						width = col + 1
					}
				}
				row := make([]string, width)
				for col, value := range cells[n] {
					row[col] = value
				}
				records = append(records, row)
			}
			if len(records) > 1 {
				break
			}
		}
	default:
		return nil, fmt.Errorf("统计表需要CSV、TSV或XLSX文件")
	}
	clean := [][]string{}
	for _, r := range records {
		if strings.TrimSpace(strings.Join(r, "")) != "" {
			clean = append(clean, r)
		}
	}
	records = clean
	return records, nil
}

func inspectTableRecords(records [][]string, fields map[string]string) (map[string]any, error) {
	if len(records) < 2 {
		return nil, fmt.Errorf("表格没有数据行")
	}
	for row, values := range records[1:] {
		if len(values) > len(records[0]) {
			return nil, fmt.Errorf("表格第 %d 行的值多于表头列数", row+2)
		}
	}
	columns := map[string]int{}
	for i, h := range records[0] {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if _, ok := columns[h]; ok {
			return nil, fmt.Errorf("表头重复：%s", h)
		}
		columns[h] = i
	}
	for required := range fields {
		if _, ok := columns[required]; !ok {
			return nil, fmt.Errorf("表格缺少必需列：%s", required)
		}
	}
	ratios := map[string]float64{}
	for name, col := range columns {
		count := 0
		for _, row := range records[1:] {
			if col < len(row) && strings.TrimSpace(row[col]) != "" {
				count++
			}
		}
		ratios[name] = float64(count) / float64(len(records)-1)
	}
	return map[string]any{"count": len(records) - 1, "fields": ratios}, nil
}
