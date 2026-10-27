package acquire

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/geeruntime"
)

// GEEAdapter compiles only shipped, category-specific templates. JSON parameters
// are decoded separately; source/model text is never interpolated as Python code.
type GEEAdapter struct {
	Invoke     ToolCaller
	Options    geeruntime.Options
	AssetsRoot string
}

func (GEEAdapter) Kind() string { return "gee" }

var geeTemplates = map[string]string{"terrain.dem": "dem.py", "landcover": "landcover.py", "imagery.optical": "optical.py", "population.grid": "population.py", "nightlight": "nightlight.py"}

func (a GEEAdapter) Fetch(ctx context.Context, r FetchRequest) (FetchResult, error) {
	if r.Provide == nil || geeTemplates[r.Item.Category] == "" || r.Provide.Template != geeTemplates[r.Item.Category] {
		return FetchResult{}, fmt.Errorf("GEE只运行该类别的系统模板")
	}
	root := a.AssetsRoot
	if root == "" {
		var err error
		root, err = config.ResolveAssetsRoot()
		if err != nil {
			return FetchResult{}, err
		}
	}
	params, err := geeParameters(r)
	if err != nil {
		return FetchResult{}, err
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return FetchResult{}, err
	}
	code, err := geeruntime.BuildAcquisitionScript(root, r.Provide.Template, params)
	if err != nil {
		return FetchResult{}, err
	}
	emitProgress(r.Progress, "downloading", 0, 0)
	var value any
	if a.Invoke != nil {
		value, err = a.Invoke(ctx, "gee_run_script", map[string]any{"template": r.Provide.Template, "params": params})
	} else {
		options := a.Options
		options.OutputDir = r.WorkDir
		options.OnProgress = func(p geeruntime.Progress) { emitProgress(r.Progress, p.Phase, p.CompletedBytes, p.TotalBytes) }
		value, err = geeruntime.Invoke(ctx, "gee_run_script", map[string]any{"code": code}, options)
	}
	if err != nil {
		return FetchResult{}, err
	}
	payload, err := UnwrapToolResult(value)
	if err != nil {
		return FetchResult{}, err
	}
	files := []string{}
	if artifacts, ok := payload["artifact_paths"].([]any); ok {
		for _, v := range artifacts {
			path, _ := v.(string)
			if strings.HasSuffix(strings.ToLower(path), ".tif") || strings.HasSuffix(strings.ToLower(path), ".tiff") {
				files = append(files, path)
			}
		}
	}
	if len(files) == 0 {
		return FetchResult{}, fmt.Errorf("GEE模板未生成GeoTIFF文件")
	}
	result := FetchResult{Files: files, Format: "geotiff", Query: string(encoded), RetrievedAt: time.Now().UTC(), SHA256: map[string]string{}, URLs: []string{"https://developers.google.com/earth-engine/datasets/catalog/" + strings.ReplaceAll(r.Provide.Dataset, "/", "_")}, Notes: []string{"仅运行系统模板；保留源投影；时段、波段和分辨率见查询记录"}}
	for _, file := range files {
		hash, size, e := fileDigest(file)
		if e != nil {
			return FetchResult{}, e
		}
		result.SHA256[file] = hash
		result.Bytes += size
	}
	return result, nil
}
func geeParameters(r FetchRequest) (map[string]any, error) {
	values := r.Provide.EffectiveParams(r.Item.Params, nil)
	dataset := r.Provide.Dataset
	if values["dataset"] != "" && values["dataset"] != dataset {
		return nil, fmt.Errorf("数据集只能从已校验来源选择")
	}
	scale, err := strconv.ParseFloat(values["scale"], 64)
	if err != nil || scale <= 0 || scale > 100000 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		return nil, fmt.Errorf("GEE分辨率须为有效正数，单位米")
	}
	bands := strings.Split(values["bands"], ",")
	for _, band := range bands {
		if strings.TrimSpace(band) == "" || strings.ContainsAny(band, "\r\n") {
			return nil, fmt.Errorf("GEE波段列表无效")
		}
	}
	p := map[string]any{"dataset": dataset, "bands": bands, "scale": scale}
	if r.Extent.Path != "" {
		p["region_path"] = r.Extent.Path
	} else if len(r.Extent.BBox) == 4 {
		b := r.Extent.BBox
		if b[0] >= b[2] || b[1] >= b[3] || b[0] < -180 || b[2] > 180 || b[1] < -90 || b[3] > 90 {
			return nil, fmt.Errorf("GEE获取范围无效")
		}
		p["bbox"] = b
	} else {
		return nil, fmt.Errorf("GEE获取范围尚未确定")
	}
	if r.Item.Category == "landcover" && values["year"] != "2021" {
		return nil, fmt.Errorf("WorldCover v200仅表示2021年")
	}
	if r.Item.Category == "population.grid" {
		year, e := strconv.Atoi(values["year"])
		if e != nil || year < 2000 || year > 2020 {
			return nil, fmt.Errorf("此WorldPop来源年份须为2000至2020")
		}
		p["year"] = year
	}
	if r.Item.Category == "imagery.optical" || r.Item.Category == "nightlight" {
		start, e := time.Parse("2006-01-02", values["start"])
		if e != nil {
			return nil, fmt.Errorf("GEE开始日期无效")
		}
		end, e := time.Parse("2006-01-02", values["end"])
		if e != nil || !end.After(start) {
			return nil, fmt.Errorf("GEE结束日期须晚于开始日期")
		}
		p["start"] = values["start"]
		p["end"] = values["end"]
	}
	if r.Item.Category == "imagery.optical" {
		cloud, e := strconv.ParseFloat(values["cloud_max"], 64)
		if e != nil || math.IsNaN(cloud) || cloud < 0 || cloud > 100 {
			return nil, fmt.Errorf("云量阈值须为0至100")
		}
		p["cloud_max"] = cloud
	}
	return p, nil
}
