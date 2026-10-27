package resource

import (
	"encoding/csv"
	"fmt"
	"github.com/autocarto/carto_agent/internal/shared/xlsx"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxTableBytes = 16 << 20

type tableRow struct {
	line   int
	values map[string]string
	fields map[string]any
}

func requiredColumns(typ string) []string {
	result := []string{}
	for _, column := range Columns(typ) {
		if column.Required {
			result = append(result, column.Key)
		}
	}
	return result
}
func tableHeaders(values []string, typ string) (map[string]int, error) {
	headers := map[string]int{}
	for i, value := range values {
		name := canonicalColumn(typ, value)
		if name == "" {
			continue
		}
		if _, exists := headers[name]; exists {
			return nil, fmt.Errorf("表头列“%s”重复", value)
		}
		headers[name] = i
	}
	for _, name := range requiredColumns(typ) {
		if _, exists := headers[name]; !exists {
			return nil, fmt.Errorf("对照表缺少必填列 %s", name)
		}
	}
	return headers, nil
}
func blankRecord(record []string) bool {
	for _, value := range record {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}
func rowsFromRecords(records [][]string, numbers []int, typ string) ([]tableRow, error) {
	for len(records) > 0 && blankRecord(records[0]) {
		records = records[1:]
		numbers = numbers[1:]
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("对照表为空")
	}
	headers, err := tableHeaders(records[0], typ)
	if err != nil {
		return nil, err
	}
	rows := []tableRow{}
	for i, record := range records[1:] {
		if blankRecord(record) {
			continue
		}
		if len(record) > len(records[0]) && !blankRecord(record[len(records[0]):]) {
			return nil, fmt.Errorf("对照表第 %d 行超出表头列数", numbers[i+1])
		}
		row := tableRow{line: numbers[i+1], values: map[string]string{}, fields: map[string]any{}}
		for name, index := range headers {
			if index < len(record) {
				row.values[name] = strings.TrimSpace(record[index])
			} else {
				row.values[name] = ""
			}
		}
		for _, column := range Columns(typ) {
			// Optional acquisition metadata does not change legacy resource snapshots.
			if typ == "data_list" && (column.Key == "category" || column.Key == "fields" || column.Key == "acquire" || column.Key == "source_hint") {
				if _, present := headers[column.Key]; !present {
					continue
				}
			}
			raw := row.values[column.Key]
			if raw == "" && column.Required {
				return nil, fmt.Errorf("对照表第 %d 行的 %s 不能为空", row.line, column.Key)
			}
			value, err := normalizeValue(column, raw)
			if err != nil {
				return nil, fmt.Errorf("对照表第 %d 行的 %s：%w", row.line, column.Key, err)
			}
			row.fields[column.Key] = value
			// Retain source cells for display, but preserve the historic normalized enum view.
			if len(column.Values) > 0 && raw != "" {
				row.values[column.Key] = value.(string)
			}
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("对照表没有数据行")
	}
	if err := validateTableRules(typ, rows); err != nil {
		return nil, err
	}
	return rows, nil
}
func normalizeValue(column ColumnFormat, raw string) (any, error) {
	if raw == "" && column.Default != nil {
		raw = fmt.Sprint(column.Default)
	}
	if len(column.Values) > 0 && raw != "" {
		value, ok := valueFromMap(raw, column.Values)
		if !ok {
			return nil, fmt.Errorf("取值无法识别：%s", raw)
		}
		return value, nil
	}
	switch column.Kind {
	case "boolean":
		if raw == "" {
			return false, nil
		}
		for truth, aliases := range formatSet.Booleans {
			for _, alias := range aliases {
				if canonicalText(raw) == canonicalText(alias) {
					return truth == "true", nil
				}
			}
		}
		return nil, fmt.Errorf("只能填写是或否")
	case "number":
		if raw == "" {
			return nil, nil
		}
		number, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("必须是有限数字")
		}
		if column.MinimumExclusive != nil && number <= *column.MinimumExclusive {
			return nil, fmt.Errorf("必须大于 %v", *column.MinimumExclusive)
		}
		return number, nil
	case "list":
		values := splitList(raw)
		if column.ItemValues != nil {
			for i, value := range values {
				normalized, ok := valueFromMap(value, column.ItemValues)
				if !ok {
					return nil, fmt.Errorf("取值无法识别：%s", value)
				}
				values[i] = normalized
			}
		}
		return values, nil
	}
	return raw, nil
}
func validateTableRules(typ string, rows []tableRow) error {
	spec := formatSet.Types[typ].Table
	constraints := [][]string{}
	for _, value := range spec.Unique {
		switch v := value.(type) {
		case string:
			constraints = append(constraints, []string{v})
		case []any:
			columns := []string{}
			for _, c := range v {
				columns = append(columns, fmt.Sprint(c))
			}
			constraints = append(constraints, columns)
		}
	}
	for _, columns := range constraints {
		seen := map[string]bool{}
		for _, row := range rows {
			values := []string{}
			for _, name := range columns {
				values = append(values, fmt.Sprint(row.fields[name]))
			}
			key := strings.Join(values, "\x00")
			if seen[key] {
				return fmt.Errorf("对照表第 %d 行重复定义 %s", row.line, strings.Join(columns, "、"))
			}
			seen[key] = true
		}
	}
	for _, rule := range spec.Rules {
		seen := map[string]bool{}
		for _, row := range rows {
			keys := []string{}
			switch rule {
			case "expanded_default_sides_unique":
				sides, _ := row.fields["default_sides"].([]string)
				if len(sides) == 0 {
					sides = []string{fmt.Sprint(row.fields["side"])}
				}
				for _, side := range sides {
					keys = append(keys, fmt.Sprint(row.fields["topology"])+"/"+side)
				}
			case "preset_ref_unique":
				key, _ := row.fields["preset_ref"].(string)
				if key == "" {
					key = fmt.Sprint(row.fields["topology"]) + "_" + fmt.Sprint(row.fields["side"])
				}
				keys = []string{key}
			case "event_types_unique":
				keys, _ = row.fields["event_types"].([]string)
			default:
				return fmt.Errorf("未知资源表校验规则 %s", rule)
			}
			local := map[string]bool{}
			for _, key := range keys {
				if local[key] {
					continue
				}
				if seen[key] {
					return fmt.Errorf("对照表第 %d 行违反 %s：%s 重复", row.line, rule, key)
				}
				seen[key] = true
				local[key] = true
			}
		}
	}
	return nil
}
func readTable(path, typ string) ([]tableRow, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxTableBytes {
		return nil, fmt.Errorf("对照表超过 16 MiB")
	}
	if strings.EqualFold(filepath.Ext(path), ".xlsx") {
		return readWorkbookTable(path, typ)
	}
	raw, err := boundedFile(path, maxTableBytes)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("CSV 必须使用 UTF-8 编码")
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(raw), "\ufeff")))
	reader.FieldsPerRecord = -1
	records := [][]string{}
	numbers := []int{}
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("CSV 无法解析：%v", err)
		}
		line, _ := reader.FieldPos(0)
		records = append(records, record)
		numbers = append(numbers, line)
	}
	return rowsFromRecords(records, numbers, typ)
}
func readWorkbookTable(path, typ string) ([]tableRow, error) {
	workbook, err := xlsx.Open(path)
	if err != nil {
		return nil, err
	}
	required := requiredColumns(typ)
	for _, sheet := range workbook.Sheets {
		_, cells, err := workbook.ReadSheet(sheet)
		if err != nil {
			return nil, err
		}
		numbers := []int{}
		for number, cell := range cells {
			nonblank := false
			for _, value := range cell {
				if strings.TrimSpace(value) != "" {
					nonblank = true
				}
			}
			if nonblank {
				numbers = append(numbers, number)
			}
		}
		sort.Ints(numbers)
		if len(numbers) == 0 {
			continue
		}
		available := map[string]bool{}
		for _, value := range cells[numbers[0]] {
			available[canonicalColumn(typ, value)] = true
		}
		matches := true
		for _, name := range required {
			if !available[name] {
				matches = false
			}
		}
		if !matches {
			continue
		}
		// The shared workbook reader remains permissive for dataset consumers. Resource
		// tables additionally require cached formula values and reject error cells.
		var rawSheet struct {
			Rows []struct {
				Cells []struct {
					Ref     string  `xml:"r,attr"`
					Type    string  `xml:"t,attr"`
					Formula *string `xml:"f"`
					Cached  *string `xml:"v"`
				} `xml:"c"`
			} `xml:"sheetData>row"`
		}
		if err = workbook.XML(sheet.Path, &rawSheet); err != nil {
			return nil, err
		}
		for _, row := range rawSheet.Rows {
			for _, cell := range row.Cells {
				if cell.Type == "e" {
					return nil, fmt.Errorf("工作表 %s 的 %s 是错误值", sheet.Name, cell.Ref)
				}
				if cell.Formula != nil && cell.Cached == nil {
					return nil, fmt.Errorf("工作表 %s 的 %s 公式缺少缓存值", sheet.Name, cell.Ref)
				}
			}
		}
		records := [][]string{}
		for _, number := range numbers {
			width := 0
			for column := range cells[number] {
				if column > width {
					width = column
				}
			}
			record := make([]string, width)
			for column, value := range cells[number] {
				record[column-1] = value
			}
			records = append(records, record)
		}
		return rowsFromRecords(records, numbers, typ)
	}
	return nil, fmt.Errorf("工作簿中没有包含必填列 %s 的工作表", strings.Join(required, "、"))
}
