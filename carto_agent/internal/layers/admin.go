package layers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type AdministrativeUnit struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Level string `json:"level"`
}

func AdministrativeUnits(assetsRoot, region, level string) ([]AdministrativeUnit, error) {
	root := filepath.Join(assetsRoot, "tools", "system", "admin-boundaries", "assets", "中国行政区边界", "data")
	names := map[string]string{"省": "province", "市": "city", "县": "county", "province": "province", "city": "city", "county": "county"}
	target, ok := names[level]
	if !ok {
		return nil, fmt.Errorf("未知行政级别：%s", level)
	}
	read := func(kind string) ([]AdministrativeUnit, error) {
		raw, e := os.ReadFile(filepath.Join(root, "china_"+kind+".geojson"))
		if e != nil {
			return nil, e
		}
		var fc struct {
			Features []struct {
				Properties struct {
					Name   string `json:"name"`
					Adcode any    `json:"adcode"`
				} `json:"properties"`
			} `json:"features"`
		}
		if e = json.Unmarshal(raw, &fc); e != nil {
			return nil, e
		}
		out := []AdministrativeUnit{}
		for _, f := range fc.Features {
			out = append(out, AdministrativeUnit{fmt.Sprint(f.Properties.Adcode), f.Properties.Name, kind})
		}
		return out, nil
	}
	prefix := ""
	found := region == "" || region == "中国" || region == "全国"
	for _, kind := range []string{"province", "city", "county"} {
		if found {
			break
		}
		units, e := read(kind)
		if e != nil {
			return nil, e
		}
		for _, u := range units {
			if region == u.Name || region == u.Code {
				if found {
					return nil, fmt.Errorf("地区名称有歧义：%s", region)
				}
				found = true
				prefix = u.Code
				if kind == "province" {
					prefix = prefix[:2]
				} else if kind == "city" && strings.HasSuffix(prefix, "00") {
					prefix = prefix[:4]
				}
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("行政区不存在：%s", region)
	}
	all, e := read(target)
	if e != nil {
		return nil, e
	}
	out := []AdministrativeUnit{}
	for _, u := range all {
		if strings.HasPrefix(u.Code, prefix) {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, nil
}
