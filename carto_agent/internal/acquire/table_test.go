package acquire

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTableNormalizationPreservesCSVAndMapsAliasesWithoutGIS(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "original.csv")
	content := "\ufeffregion,year,value\r\n甲,2024,42\r\n"
	if err := os.WriteFile(input, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := normalizeProvidedTable(context.Background(), input, filepath.Join(root, "out", "ready.csv"), map[string][]string{"region": {"地区"}, "year": {"年份"}, "value": {"数值"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(result["path"].(string))
	if err != nil || string(raw) != content || result["count"] != 1 {
		t.Fatalf("already-standard CSV changed: %q %#v %v", raw, result, err)
	}
	if result["crs"] != nil || result["geometry_type"] != nil {
		t.Fatal("table invented spatial metadata")
	}
	input = filepath.Join(root, "original.tsv")
	if err = os.WriteFile(input, []byte("地区\t年份\t数值\n乙\t2025\t12.5\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err = normalizeProvidedTable(context.Background(), input, filepath.Join(root, "out", "converted.tsv"), map[string][]string{"region": {"地区"}, "year": {"年份"}, "value": {"数值"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(result["path"].(string))
	if err != nil || filepath.Ext(result["path"].(string)) != ".csv" {
		t.Fatal(result, err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
	want := [][]string{{"地区", "年份", "数值", "region", "value", "year"}, {"乙", "2025", "12.5", "乙", "12.5", "2025"}}
	if err != nil || !reflect.DeepEqual(rows, want) {
		t.Fatalf("column aliases corrupted values: %#v %v", rows, err)
	}
	if _, err = normalizeProvidedTable(context.Background(), input, filepath.Join(root, "out", "missing.csv"), map[string][]string{"missing": {"不存在"}}); err == nil {
		t.Fatal("missing source column was invented")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = normalizeProvidedTable(ctx, input, filepath.Join(root, "out", "cancelled.csv"), nil); err == nil {
		t.Fatal("cancelled normalization wrote a table")
	}
}
