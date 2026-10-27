package acquire

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/geocoding"
)

// No credential enters a tile definition, cache entry or source report.
type TiandituAdapter struct {
	Invoke    ToolCaller
	Resolver  *geocoding.Resolver
	ConfigDir string
}

func (TiandituAdapter) Kind() string { return "tianditu" }

type countyFeature struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Geometry   struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	} `json:"geometry"`
}

func (a TiandituAdapter) Fetch(ctx context.Context, r FetchRequest) (FetchResult, error) {
	if r.Item.Category == "basemap.tiles" {
		style := r.Item.Params["style"]
		if style == "" {
			style = "vec"
		}
		if style != "vec" && style != "img" && style != "ter" {
			return FetchResult{}, fmt.Errorf("天地图底图样式须为vec、img或ter")
		}
		return FetchResult{Format: "online_layer", LayerDefinition: map[string]any{"type": "tianditu_xyz", "style": style, "secret_ref": "tianditu", "requires_project_key_confirmation": true}, RetrievedAt: time.Now().UTC(), URLs: []string{"https://lbs.tianditu.gov.cn/server/MapService.html"}, Notes: []string{"在线瓦片，不是下载的地理数据；当前加载前须明确确认密钥进入QGIS工程，尚未验证可替代tk参数的认证配置"}}, nil
	}
	if r.Item.Category != "settlement.county_seat" || a.Invoke == nil {
		return FetchResult{}, fmt.Errorf("天地图来源不支持此类别或提取工具不可用")
	}
	resolver := a.Resolver
	if resolver == nil {
		key, e := config.LoadTiandituAPIKey(a.ConfigDir)
		if e != nil {
			return FetchResult{}, e
		}
		resolver = &geocoding.Resolver{Key: key}
	}
	within := r.Item.Params["within"]
	if within == "" && r.Item.Extent != nil {
		within = r.Item.Extent.Region
	}
	if within == "" {
		within = "全国"
	}
	if err := os.MkdirAll(r.WorkDir, 0700); err != nil {
		return FetchResult{}, err
	}
	boundPath := filepath.Join(r.WorkDir, "counties.geojson")
	result, err := a.Invoke(ctx, "admin_boundary_extract", map[string]any{"within": within, "level": "县", "load": false, "output": boundPath})
	if err != nil {
		return FetchResult{}, err
	}
	if _, err = UnwrapToolResult(result); err != nil {
		return FetchResult{}, err
	}
	pointPath := filepath.Join(r.WorkDir, "county-representatives.geojson")
	result, err = a.Invoke(ctx, "data_derive", map[string]any{"input": map[string]any{"path": boundPath}, "method": "point_on_surface", "output": pointPath})
	if err != nil {
		return FetchResult{}, err
	}
	if _, err = UnwrapToolResult(result); err != nil {
		return FetchResult{}, err
	}
	var boundaries, points struct {
		Features []countyFeature `json:"features"`
	}
	raw, err := os.ReadFile(boundPath)
	if err != nil {
		return FetchResult{}, err
	}
	if err = json.Unmarshal(raw, &boundaries); err != nil {
		return FetchResult{}, err
	}
	raw, err = os.ReadFile(pointPath)
	if err != nil {
		return FetchResult{}, err
	}
	if err = json.Unmarshal(raw, &points); err != nil {
		return FetchResult{}, err
	}
	byCode := map[string]countyFeature{}
	for _, f := range boundaries.Features {
		code, _ := f.Properties["adcode"].(string)
		byCode[code] = f
	}
	var features []map[string]any
	approx := 0
	for i, f := range points.Features {
		if err = ctx.Err(); err != nil {
			return FetchResult{}, err
		}
		name, _ := f.Properties["name"].(string)
		code, _ := f.Properties["adcode"].(string)
		var point [2]float64
		if f.Geometry.Type != "Point" || json.Unmarshal(f.Geometry.Coordinates, &point) != nil || len(code) != 6 || name == "" {
			return FetchResult{}, fmt.Errorf("县域代表点身份或几何无效")
		}
		if len(r.Extent.BBox) == 4 && !countyOverlaps(byCode[code], r.Extent.BBox) {
			continue
		}
		candidates, e := resolver.SearchCandidates(ctx, name+"人民政府", "156"+code)
		if ctx.Err() != nil {
			return FetchResult{}, ctx.Err()
		}
		selected := -1
		best := math.Inf(1)
		for j, c := range candidates {
			if strings.TrimPrefix(c.CountyCode, "156") != code || !strings.Contains(c.Name, name) || !strings.HasSuffix(c.Name, "人民政府") || !insideCounty(byCode[code], c.Lon, c.Lat) {
				continue
			}
			distance := distanceKM(point[0], point[1], c.Lon, c.Lat)
			if distance < best {
				best = distance
				selected = j
			}
		}
		properties := map[string]any{"name": name, "adcode": code, "approximate": selected < 0, "source": "天地图地名搜索"}
		if selected >= 0 {
			c := candidates[selected]
			point = [2]float64{c.Lon, c.Lat}
			properties["poi_name"] = c.Name
			properties["source_id"] = c.SourceID
			properties["distance_to_representative_km"] = best
		} else {
			approx++
			properties["source"] = "内置行政区县域代表点"
			properties["quality_note"] = "近似：县域代表点，不代表驻地实际位置"
			if e != nil {
				properties["query_status"] = "服务查询失败，使用近似代表点"
			} else {
				properties["query_status"] = "无通过行政区及范围核验的政府候选"
			}
		}
		features = append(features, map[string]any{"type": "Feature", "properties": properties, "geometry": map[string]any{"type": "Point", "coordinates": point}})
		emitProgress(r.Progress, "querying", int64(i+1), int64(len(points.Features)))
	}
	output := filepath.Join(r.WorkDir, "county-seats.geojson")
	content, err := json.Marshal(map[string]any{"type": "FeatureCollection", "features": features})
	if err != nil {
		return FetchResult{}, err
	}
	file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return FetchResult{}, err
	}
	_, err = file.Write(content)
	closeErr := file.Close()
	if err != nil {
		return FetchResult{}, err
	}
	if closeErr != nil {
		return FetchResult{}, closeErr
	}
	digest, size, err := fileDigest(output)
	if err != nil {
		return FetchResult{}, err
	}
	return FetchResult{Files: []string{output}, Format: "geojson", Query: "县人民政府，按县级代码及县域面核验", URLs: []string{geocoding.DefaultEndpoint}, RetrievedAt: time.Now().UTC(), Bytes: size, SHA256: map[string]string{output: digest}, Notes: []string{fmt.Sprintf("%d 个县使用近似县域代表点；服务配额以当前账户控制台为准", approx)}}, nil
}
func distanceKM(x, y, a, b float64) float64 {
	dy := (b - y) * math.Pi / 180
	dx := (a - x) * math.Pi / 180
	v := math.Sin(dy/2)*math.Sin(dy/2) + math.Cos(y*math.Pi/180)*math.Cos(b*math.Pi/180)*math.Sin(dx/2)*math.Sin(dx/2)
	return 6371 * 2 * math.Asin(math.Min(1, math.Sqrt(v)))
}
func insideRing(ring [][2]float64, x, y float64) bool {
	inside := false
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		a, b := ring[i], ring[j]
		if (a[1] > y) != (b[1] > y) && x < (b[0]-a[0])*(y-a[1])/(b[1]-a[1])+a[0] {
			inside = !inside
		}
	}
	return inside
}
func insideCounty(f countyFeature, x, y float64) bool {
	var polys [][][][2]float64
	if f.Geometry.Type == "Polygon" {
		var p [][][2]float64
		if json.Unmarshal(f.Geometry.Coordinates, &p) != nil {
			return false
		}
		polys = append(polys, p)
	} else if f.Geometry.Type == "MultiPolygon" {
		if json.Unmarshal(f.Geometry.Coordinates, &polys) != nil {
			return false
		}
	}
	for _, p := range polys {
		if len(p) == 0 || !insideRing(p[0], x, y) {
			continue
		}
		hole := false
		for _, ring := range p[1:] {
			hole = hole || insideRing(ring, x, y)
		}
		if !hole {
			return true
		}
	}
	return false
}

func countyOverlaps(f countyFeature, b []float64) bool {
	var raw any
	if json.Unmarshal(f.Geometry.Coordinates, &raw) != nil {
		return false
	}
	bounds := []float64{180, 90, -180, -90}
	var visit func(any)
	visit = func(v any) {
		list, ok := v.([]any)
		if !ok {
			return
		}
		if len(list) >= 2 {
			if x, ok := list[0].(float64); ok {
				y, ok := list[1].(float64)
				if !ok {
					return
				}
				bounds[0] = math.Min(bounds[0], x)
				bounds[1] = math.Min(bounds[1], y)
				bounds[2] = math.Max(bounds[2], x)
				bounds[3] = math.Max(bounds[3], y)
				return
			}
		}
		for _, item := range list {
			visit(item)
		}
	}
	visit(raw)
	return bounds[0] <= b[2] && bounds[2] >= b[0] && bounds[1] <= b[3] && bounds[3] >= b[1]
}
