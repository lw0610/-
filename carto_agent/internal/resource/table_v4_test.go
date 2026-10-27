package resource

import "testing"

func TestV4DataListColumns(t *testing.T) {
	rows, err := rowsFromRecords([][]string{{"名称", "路径", "类别", "字段", "自动获取", "推荐来源", "无关列"}, {"铁路", "rail.shp", "transport.railway", "NAME=name", "否", "甲、乙", "忽略"}}, []int{1, 2}, "data_list")
	if err != nil {
		t.Fatal(err)
	}
	f := rows[0].fields
	if f["category"] != "transport.railway" || f["fields"] != "NAME=name" || f["acquire"] != false || len(f["source_hint"].([]string)) != 2 {
		t.Fatalf("%+v", f)
	}
	if _, exists := f["无关列"]; exists {
		t.Fatal("unknown columns must remain ignored")
	}
	rows, err = rowsFromRecords([][]string{{"名称", "路径"}, {"旧数据", "old.shp"}}, []int{1, 2}, "data_list")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := rows[0].fields["acquire"]; exists {
		t.Fatal("legacy rows must not gain fields")
	}
}
