package acquire

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ExtentResolver func(context.Context, string) (*flow.ExtentPlan, error)

var errExtentCRS = errors.New("范围文件须转换到 WGS84")

func EstimateScale(extent *flow.ExtentPlan, inputs map[string]any) int {
	if extent == nil || len(extent.BBox) != 4 {
		return 0
	}
	b := extent.BBox
	widthMeters := (b[2] - b[0]) * 111320 * math.Cos((b[1]+b[3])*math.Pi/360)
	paper, ok := inputs["纸张"].(string)
	if ok {
		widths := map[string]float64{"A0": 1189, "A1": 841, "A2": 594, "A3": 420, "A4": 297}
		for name, width := range widths {
			if strings.Contains(strings.ToUpper(paper), name) {
				if strings.Contains(paper, "纵") || strings.Contains(paper, "竖") {
					width /= math.Sqrt2
				}
				return int(math.Round(widthMeters/(width*.8/1000)/10000)) * 10000
			}
		}
	}
	if extent.Region == "全国" || widthMeters > 2000000 {
		return 4000000
	}
	if widthMeters > 300000 {
		return 1000000
	}
	if widthMeters > 80000 {
		return 250000
	}
	return 50000
}
func expandBBox(box []float64, percent float64) []float64 {
	out := append([]float64(nil), box...)
	if percent <= 0 {
		return out
	}
	dx := (box[2] - box[0]) * percent / 100
	dy := (box[3] - box[1]) * percent / 100
	out[0] = math.Max(-180, out[0]-dx)
	out[1] = math.Max(-90, out[1]-dy)
	out[2] = math.Min(180, out[2]+dx)
	out[3] = math.Min(90, out[3]+dy)
	return out
}

// prepareExtent applies the same finite-area rules to input and runtime extents.
// No distance is specified by the flow contract for buffering a point/flat line;
// require an explicit area instead of inventing a fixed-degree or admin buffer.
func prepareExtent(extent *flow.ExtentPlan) error {
	if extent == nil || len(extent.BBox) != 4 {
		return fmt.Errorf("获取范围需要完整的 WGS84 外包框")
	}
	for _, n := range extent.BBox {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return fmt.Errorf("获取范围含非有限坐标（NaN/Inf）")
		}
	}
	box := extent.BBox
	if box[0] < -180 || box[2] > 180 || box[1] < -90 || box[3] > 90 || box[0] > box[2] || box[1] > box[3] {
		return fmt.Errorf("获取范围不是有效的 WGS84 外包框")
	}
	if box[0] == box[2] || box[1] == box[3] {
		return fmt.Errorf("获取范围没有面积；单点或水平/竖直路线需另行指定有面积的区域或边界")
	}
	if math.IsNaN(extent.ExpandPercent) || math.IsInf(extent.ExpandPercent, 0) || extent.ExpandPercent < 0 || extent.ExpandPercent > 1000 {
		return fmt.Errorf("外扩比例须为 0 至 1000%% 的有限数值")
	}
	if extent.ExpandPercent > 0 {
		extent.BBox = expandBBox(extent.BBox, extent.ExpandPercent)
		extent.Path = "" // An unexpanded polygon would cut off the requested margin.
	}
	return nil
}

func resolveExtent(ctx context.Context, value string, catalog *datasource.Catalog, resolver ExtentResolver) (*flow.ExtentPlan, error) {
	if info, err := os.Stat(value); err == nil {
		if info.IsDir() {
			box, err := routeExtent(filepath.Join(value, "places.csv"))
			return &flow.ExtentPlan{BBox: box, Source: value}, err
		}
		if strings.EqualFold(filepath.Ext(value), ".geojson") || strings.EqualFold(filepath.Ext(value), ".json") {
			box, polygonOnly, err := geoJSONExtent(value)
			if err != nil {
				if errors.Is(err, errExtentCRS) && resolver != nil {
					return resolveExternalExtent(ctx, value, resolver)
				}
				return nil, err
			}
			extent := &flow.ExtentPlan{BBox: box, Source: value}
			if polygonOnly {
				extent.Path = value
			}
			return extent, nil
		}
		if resolver != nil {
			return resolveExternalExtent(ctx, value, resolver)
		}
		return nil, fmt.Errorf("此边界格式需要连接当前 QGIS 实例后检查范围")
	}
	if catalog != nil {
		region, err := catalog.ResolveRegion(value)
		if err == nil {
			return &flow.ExtentPlan{BBox: region.BBox, Region: region.Name, Source: value}, nil
		}
		var ambiguous *datasource.RegionAmbiguity
		if errors.As(err, &ambiguous) {
			return nil, err
		}
	}
	if resolver != nil {
		return resolveExternalExtent(ctx, value, resolver)
	}
	return nil, fmt.Errorf("无法识别范围“%s”，请提供完整区域名、六位代码或边界文件", value)
}

// External metadata currently has no geometry-type contract. Its WGS84 bbox is
// usable, but an unverified file/layer must not be sent as a polygon clip mask.
func resolveExternalExtent(ctx context.Context, value string, resolver ExtentResolver) (*flow.ExtentPlan, error) {
	extent, err := resolver(ctx, value)
	if err != nil {
		return nil, err
	}
	if extent == nil {
		return nil, fmt.Errorf("范围检查未返回有效范围")
	}
	copy := *extent
	copy.BBox = append([]float64(nil), extent.BBox...)
	copy.Path = ""
	return &copy, nil
}

func routeExtent(path string) ([]float64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	r := csv.NewReader(io.LimitReader(file, 32*1024*1024))
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	lon, lat := -1, -1
	for i, name := range header {
		switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "\ufeff")) {
		case "lon", "longitude", "经度", "x":
			lon = i
		case "lat", "latitude", "纬度", "y":
			lat = i
		}
	}
	if lon < 0 || lat < 0 {
		return nil, fmt.Errorf("places.csv 缺少经纬度列")
	}
	box := []float64{180, 90, -180, -90}
	count := 0
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		x, ex := strconv.ParseFloat(row[lon], 64)
		y, ey := strconv.ParseFloat(row[lat], 64)
		if ex == nil && (math.IsNaN(x) || math.IsInf(x, 0)) || ey == nil && (math.IsNaN(y) || math.IsInf(y, 0)) {
			return nil, fmt.Errorf("places.csv 含非有限坐标（NaN/Inf）")
		}
		if ex != nil || ey != nil {
			continue
		}
		if math.Abs(x) > 180 || math.Abs(y) > 90 {
			continue
		}
		extend(box, x, y)
		count++
	}
	if count == 0 {
		return nil, fmt.Errorf("places.csv 没有有效坐标")
	}
	return box, nil
}
func geoJSONExtent(path string) ([]float64, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	var doc map[string]any
	if err = json.NewDecoder(io.LimitReader(file, 200*1024*1024)).Decode(&doc); err != nil {
		return nil, false, err
	}
	if crs, ok := doc["crs"].(map[string]any); ok {
		raw, _ := json.Marshal(crs)
		text := string(raw)
		if !strings.Contains(text, "4326") && !strings.Contains(text, "CRS84") {
			return nil, false, errExtentCRS
		}
	}
	box := []float64{180, 90, -180, -90}
	count, geometries := 0, 0
	polygonOnly := true
	var visit func(any) error
	visit = func(v any) error {
		switch node := v.(type) {
		case []any:
			if len(node) >= 2 {
				if x, ok := node[0].(float64); ok {
					y, ok := node[1].(float64)
					if !ok || math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) || math.Abs(x) > 180 || math.Abs(y) > 90 {
						return fmt.Errorf("文件含无效或非有限 WGS84 几何坐标")
					}
					extend(box, x, y)
					count++
					return nil
				}
			}
			for _, child := range node {
				if err := visit(child); err != nil {
					return err
				}
			}
		case map[string]any:
			switch node["type"] {
			case "Polygon", "MultiPolygon":
				geometries++
			case "Point", "MultiPoint", "LineString", "MultiLineString":
				geometries++
				polygonOnly = false
			case "GeometryCollection":
				polygonOnly = false
			case "Feature", "FeatureCollection":
			default:
				return fmt.Errorf("文件不是支持的 GeoJSON 几何或要素集合")
			}
			for _, key := range []string{"features", "geometry", "geometries", "coordinates"} {
				if child, ok := node[key]; ok {
					if err := visit(child); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	if err := visit(doc); err != nil {
		return nil, false, err
	}
	if count == 0 || geometries == 0 {
		return nil, false, fmt.Errorf("文件没有有效 WGS84 几何坐标")
	}
	return box, polygonOnly, nil
}
func extend(box []float64, x, y float64) {
	box[0] = math.Min(box[0], x)
	box[1] = math.Min(box[1], y)
	box[2] = math.Max(box[2], x)
	box[3] = math.Max(box[3], y)
}
