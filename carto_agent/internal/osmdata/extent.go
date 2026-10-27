package osmdata

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Point [2]float64

// Bound request size without silently changing the user's region. Complex
// boundaries must be explicitly simplified for querying, while the original
// remains the exact clipping mask.
const maxPolygonQueryVertices = 20000

type Extent struct {
	Prelude string
	Filters []string
	Clipped bool
	Bounds  []float64
	Source  string
}

func validPoint(p Point) bool {
	return !math.IsNaN(p[0]) && !math.IsNaN(p[1]) && !math.IsInf(p[0], 0) && !math.IsInf(p[1], 0) && p[0] >= -180 && p[0] <= 180 && p[1] >= -90 && p[1] <= 90
}
func numbers(s string, n int) ([]float64, error) {
	parts := strings.Split(s, ",")
	if len(parts) != n {
		return nil, fmt.Errorf("范围参数数量无效")
	}
	v := make([]float64, n)
	for i, p := range parts {
		f, e := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if e != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("范围坐标无效")
		}
		v[i] = f
	}
	return v, nil
}
func ParseExtent(raw string) (Extent, error) {
	// Overpass spatial filters select whole OSM objects. Even a bbox or area
	// match can return ways and relation members extending outside the region.
	// Only a subsequent geometry intersection may claim a clipped output.
	e := Extent{Source: raw}
	kind, value, ok := strings.Cut(raw, ":")
	if !ok {
		return e, fmt.Errorf("范围须使用 area:/bbox:/around:/file:")
	}
	switch kind {
	case "area":
		r, err := ParseRef(value)
		if err != nil {
			return e, err
		}
		if r.Type != "relation" {
			return e, fmt.Errorf("area 只支持关系编号 R")
		}
		e.Prelude = fmt.Sprintf("area(%d)->.a;", 3600000000+r.ID)
		e.Filters = []string{"(area.a)"}
	case "bbox":
		v, err := numbers(value, 4)
		if err != nil {
			return e, err
		}
		if !validPoint(Point{v[1], v[0]}) || !validPoint(Point{v[3], v[2]}) || v[0] >= v[2] || v[1] >= v[3] {
			return e, fmt.Errorf("外包范围顺序应为南、西、北、东")
		}
		e.Filters = []string{fmt.Sprintf("(%g,%g,%g,%g)", v[0], v[1], v[2], v[3])}
		e.Bounds = []float64{v[1], v[0], v[3], v[2]}
	case "around":
		v, err := numbers(value, 3)
		if err != nil {
			return e, err
		}
		if v[0] <= 0 || v[0] > 100000 || !validPoint(Point{v[2], v[1]}) {
			return e, fmt.Errorf("半径或中心点无效")
		}
		e.Filters = []string{fmt.Sprintf("(around:%g,%g,%g)", v[0], v[1], v[2])}
	case "file":
		if strings.ToLower(filepath.Ext(value)) != ".geojson" && strings.ToLower(filepath.Ext(value)) != ".json" {
			return e, fmt.Errorf("范围文件必须是GeoJSON")
		}
		info, err := os.Stat(value)
		if err != nil {
			return e, err
		}
		if info.Size() > 50<<20 {
			return e, fmt.Errorf("范围文件过大")
		}
		raw, err := os.ReadFile(value)
		if err != nil {
			return e, err
		}
		if len(raw) > 50<<20 {
			return e, fmt.Errorf("范围文件过大")
		}
		var doc map[string]json.RawMessage
		if err = json.Unmarshal(raw, &doc); err != nil {
			return e, err
		}
		if crs := doc["crs"]; len(crs) > 0 && string(crs) != "null" && !strings.Contains(string(crs), "4326") && !strings.Contains(string(crs), "CRS84") {
			return e, fmt.Errorf("范围文件必须为 WGS84 GeoJSON")
		}
		rings, err := geoRings(raw)
		if err != nil {
			return e, err
		}
		if len(rings) == 0 {
			return e, fmt.Errorf("范围文件没有面")
		}
		bounds := []float64{180, 90, -180, -90}
		vertices := 0
		for _, ring := range rings {
			vertices += len(ring)
			if vertices > maxPolygonQueryVertices {
				return e, fmt.Errorf("查询边界外环超过 %d 个顶点，请显式简化查询边界并保留原边界用于精裁；未改用外接矩形", maxPolygonQueryVertices)
			}
			for _, p := range ring {
				if !validPoint(p) {
					return e, fmt.Errorf("范围文件含非经纬度坐标")
				}
				bounds[0] = math.Min(bounds[0], p[0])
				bounds[1] = math.Min(bounds[1], p[1])
				bounds[2] = math.Max(bounds[2], p[0])
				bounds[3] = math.Max(bounds[3], p[1])
			}
		}
		e.Bounds = bounds
		for _, ring := range rings {
			var coords []string
			for _, p := range ring {
				coords = append(coords, fmt.Sprintf("%g %g", p[1], p[0]))
			}
			e.Filters = append(e.Filters, "(poly:"+quote(strings.Join(coords, " "))+")")
		}
	default:
		return e, fmt.Errorf("不支持的范围类型")
	}
	return e, nil
}
func geoRings(raw []byte) ([][]Point, error) {
	var obj struct {
		Type        string            `json:"type"`
		Coordinates json.RawMessage   `json:"coordinates"`
		Geometry    json.RawMessage   `json:"geometry"`
		Features    []json.RawMessage `json:"features"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	switch obj.Type {
	case "FeatureCollection":
		var all [][]Point
		for _, f := range obj.Features {
			r, e := geoRings(f)
			if e != nil {
				return nil, e
			}
			all = append(all, r...)
		}
		return all, nil
	case "Feature":
		return geoRings(obj.Geometry)
	case "Polygon":
		var rings [][]Point
		if err := json.Unmarshal(obj.Coordinates, &rings); err != nil {
			return nil, err
		}
		if len(rings) > 0 && len(rings[0]) >= 4 {
			return rings[:1], nil
		}
	case "MultiPolygon":
		var polys [][][]Point
		if err := json.Unmarshal(obj.Coordinates, &polys); err != nil {
			return nil, err
		}
		var out [][]Point
		for _, poly := range polys {
			if len(poly) > 0 && len(poly[0]) >= 4 {
				out = append(out, poly[0])
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("范围 GeoJSON 必须包含面几何")
}
func cross(o, a, b Point) float64 { return (a[0]-o[0])*(b[1]-o[1]) - (a[1]-o[1])*(b[0]-o[0]) }
func ConvexHull(points []Point) []Point {
	p := append([]Point(nil), points...)
	sort.Slice(p, func(i, j int) bool {
		if p[i][0] == p[j][0] {
			return p[i][1] < p[j][1]
		}
		return p[i][0] < p[j][0]
	})
	if len(p) < 3 {
		return p
	}
	h := make([]Point, 0, len(p)*2)
	for _, v := range p {
		for len(h) >= 2 && cross(h[len(h)-2], h[len(h)-1], v) <= 0 {
			h = h[:len(h)-1]
		}
		h = append(h, v)
	}
	lower := len(h)
	for i := len(p) - 2; i >= 0; i-- {
		for len(h) > lower && cross(h[len(h)-2], h[len(h)-1], p[i]) <= 0 {
			h = h[:len(h)-1]
		}
		h = append(h, p[i])
	}
	return h
}
