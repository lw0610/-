package datasource

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

type Region struct {
	Name  string    `json:"name"`
	Code  string    `json:"code"`
	Level string    `json:"level"`
	BBox  []float64 `json:"bbox"`
}
type RegionAmbiguity struct {
	Name       string
	Candidates []Region
}

func (e *RegionAmbiguity) Error() string {
	return fmt.Sprintf("区域名称“%s”有多个匹配，请使用六位行政区划代码", e.Name)
}
func loadRegions(root string) ([]Region, error) {
	raw, err := os.ReadFile(filepath.Join(root, "regions.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var document struct {
		Regions []Region `json:"regions"`
	}
	if err = json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	return document.Regions, nil
}
func (c *Catalog) ResolveRegion(name string) (Region, error) {
	name = strings.TrimSpace(name)
	if name == "全国" {
		result := Region{Name: name, Level: "全国", BBox: []float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}}
		for _, r := range c.Regions {
			if r.Level != "省" || len(r.BBox) != 4 {
				continue
			}
			for i := 0; i < 2; i++ {
				result.BBox[i] = math.Min(result.BBox[i], r.BBox[i])
				result.BBox[i+2] = math.Max(result.BBox[i+2], r.BBox[i+2])
			}
		}
		if math.IsInf(result.BBox[0], 0) {
			return Region{}, fmt.Errorf("内置行政区范围索引不可用")
		}
		return result, nil
	}
	matches := []Region{}
	for _, r := range c.Regions {
		if r.Name == name || r.Code == name {
			matches = append(matches, r)
		}
	}
	if len(matches) == 0 {
		for _, r := range c.Regions {
			if shortRegion(r.Name) == name {
				matches = append(matches, r)
			}
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		// The same official name and code may occur in both city and county levels.
		same := true
		for _, r := range matches[1:] {
			same = same && r.Code == matches[0].Code && r.Name == matches[0].Name
		}
		if same {
			return matches[0], nil
		}
		return Region{}, &RegionAmbiguity{Name: name, Candidates: matches}
	}
	return Region{}, fmt.Errorf("未找到区域“%s”，请使用完整行政区名称、六位代码或边界文件", name)
}
func shortRegion(name string) string {
	for _, suffix := range []string{"壮族自治区", "回族自治区", "维吾尔自治区", "特别行政区", "自治区", "自治州", "地区", "省", "市", "县", "区"} {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix)
		}
	}
	return name
}
