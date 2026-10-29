package acquire

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// normalizeProvidedTable uses the same parser and quality gate as supplied
// tables. Original files stay in the store's raw directory; the usable result
// is a CSV, without inventing geometry, CRS, coordinates or statistical values.
func normalizeProvidedTable(ctx context.Context, input, output string, fields map[string][]string) (map[string]any, error) {
	records, err := readProvidedTable(input)
	if err != nil {
		return nil, err
	}
	if _, err = inspectTableRecords(records, nil); err != nil {
		return nil, err
	}
	columns := map[string]int{}
	for col, name := range records[0] {
		columns[strings.TrimSpace(name)] = col
	}
	targets := make([]string, 0, len(fields))
	for target := range fields {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	aliases := map[string]string{}
	required := map[string]string{}
	for _, target := range targets {
		required[target] = target
		if _, exists := columns[target]; exists {
			continue
		}
		source, found := "", false
		for _, candidate := range fields[target] {
			if _, exists := columns[candidate]; exists {
				source, found = candidate, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("表格缺少字段 %s 及其声明的候选列", target)
		}
		col, width := columns[source], len(records[0])
		for row := range records {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			value := ""
			if row == 0 {
				value = target
			} else if col < len(records[row]) {
				value = records[row][col]
			}
			for len(records[row]) < width {
				records[row] = append(records[row], "")
			}
			records[row] = append(records[row], value)
		}
		columns[target], aliases[target] = width, source
	}
	stats, err := inspectTableRecords(records, required)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	output = strings.TrimSuffix(output, filepath.Ext(output)) + ".csv"
	if err = os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Ext(input), ".csv") && len(aliases) == 0 {
		// Preserve the exact existing CSV, including BOM, quoting and newlines.
		err = copyReadonly(input, output)
	} else {
		var file *os.File
		file, err = os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			writer := csv.NewWriter(file)
			err = writer.WriteAll(records)
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
		}
	}
	if err != nil {
		return nil, err
	}
	stats["path"], stats["paths"] = output, []string{output}
	if len(aliases) > 0 {
		stats["field_aliases"] = aliases
	}
	return stats, nil
}
