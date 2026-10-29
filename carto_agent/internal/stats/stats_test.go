package stats

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHenanContentChecksAndSemanticReuse(t *testing.T) {
	names := []string{"郑州市", "开封市", "洛阳市", "平顶山市", "安阳市", "鹤壁市", "新乡市", "焦作市", "濮阳市", "许昌市", "漯河市", "三门峡市", "南阳市", "商丘市", "信阳市", "周口市", "驻马店市", "济源市"}
	codes := []string{"410100", "410200", "410300", "410400", "410500", "410600", "410700", "410800", "410900", "411000", "411100", "411200", "411300", "411400", "411500", "411600", "411700", "419001"}
	// Manually cross-checked against the original table image, not runtime source data.
	values := []int{1301, 471, 708, 492, 538, 157, 613, 352, 370, 438, 237, 202, 950, 766, 605, 867, 677, 73}
	expected := []Unit{}
	text := "地区,value,year,unit\n全省,9815,2023,万人\n"
	for i, name := range names {
		expected = append(expected, Unit{codes[i], name, "市"})
		if name == "济源市" {
			name = "济源示范区"
		}
		text += fmt.Sprintf("%s,%d,2023,万人\n", name, values[i])
	}
	path := filepath.Join(t.TempDir(), "population.csv")
	os.WriteFile(path, []byte(text), 0600)
	need := Need{RegionCode: "410000", Region: "河南省", Level: "市", Year: 2023, Indicator: "常住人口", Caliber: "常住", Unit: "万人", RoundingUnit: 1}
	source := Source{Title: "河南统计年鉴2024", URL: "https://tjj.henan.gov.cn/2024/03-03.jpg", TableRef: "表3-3", Method: "ocr", Official: true}
	data, err := CheckFile(path, need, expected, source)
	if err != nil {
		t.Fatal(err)
	}
	if data.Checks.Sum != 9817 || data.Checks.Tolerance != 9 {
		t.Fatalf("wrong checks: %+v", data.Checks)
	}
	store := New(t.TempDir())
	saved, err := store.Put(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	hit, ok, err := store.Lookup(context.Background(), need)
	if err != nil || !ok || hit.Path != saved.Path {
		t.Fatalf("store miss: %v %v", ok, err)
	}
	for name, input := range map[string]string{"wrong total": strings.Replace(text, "郑州市,1301", "郑州市,4067", 1), "missing Jiyuan": strings.Replace(text, "济源示范区,73,2023,万人\n", "", 1), "blank": strings.Replace(text, "郑州市,1301", "郑州市,", 1)} {
		t.Run(name, func(t *testing.T) {
			os.WriteFile(path, []byte(input), 0600)
			if _, err := CheckFile(path, need, expected, source); err == nil {
				t.Fatal("bad content accepted")
			}
		})
	}
}
