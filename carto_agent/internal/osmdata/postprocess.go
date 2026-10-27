package osmdata

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
)

// ReadFeatures reads bounded, completed GeoJSON produced by trusted local GIS
// processing. UseNumber preserves integral OSM identities while re-publishing.
func ReadFeatures(path string) ([]Feature, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 200<<20 {
		return nil, fmt.Errorf("OSM 后处理输出不是有效的有界普通文件")
	}
	decoder := json.NewDecoder(io.LimitReader(f, (200<<20)+1))
	decoder.UseNumber()
	var document struct {
		Type     string    `json:"type"`
		Features []Feature `json:"features"`
	}
	if err = decoder.Decode(&document); err != nil || document.Type != "FeatureCollection" || document.Features == nil {
		return nil, fmt.Errorf("OSM 后处理未生成有效 FeatureCollection: %v", err)
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("OSM 后处理 GeoJSON 含多余内容")
	}
	if _, _, err = FeatureStats(document.Features); err != nil {
		return nil, err
	}
	return document.Features, nil
}

// FeatureStats computes only metadata; geometry intersection remains in QGIS.
func FeatureStats(features []Feature) ([]float64, map[string]int, error) {
	bounds := []float64{180, 90, -180, -90}
	counts := map[string]int{}
	for _, feature := range features {
		if feature.Type != "Feature" || feature.Properties == nil {
			return nil, nil, fmt.Errorf("OSM 后处理包含无效要素或不支持的几何")
		}
		switch feature.Geometry.Type {
		case "Point", "MultiPoint", "LineString", "MultiLineString", "Polygon", "MultiPolygon":
		default:
			return nil, nil, fmt.Errorf("OSM 后处理包含不支持的几何: %s", feature.Geometry.Type)
		}
		raw, err := json.Marshal(feature.Geometry.Coordinates)
		if err != nil {
			return nil, nil, err
		}
		var coordinates any
		if err = json.Unmarshal(raw, &coordinates); err != nil {
			return nil, nil, err
		}
		points := 0
		var visit func(any) error
		visit = func(value any) error {
			values, ok := value.([]any)
			if !ok || len(values) == 0 {
				return fmt.Errorf("OSM 后处理包含空或非法坐标")
			}
			if x, ok := values[0].(float64); ok {
				if len(values) < 2 {
					return fmt.Errorf("OSM 后处理坐标维度无效")
				}
				y, ok := values[1].(float64)
				if !ok || !validPoint(Point{x, y}) {
					return fmt.Errorf("OSM 后处理坐标不是 WGS84 经纬度")
				}
				bounds[0], bounds[1] = math.Min(bounds[0], x), math.Min(bounds[1], y)
				bounds[2], bounds[3] = math.Max(bounds[2], x), math.Max(bounds[3], y)
				points++
				return nil
			}
			for _, item := range values {
				if err := visit(item); err != nil {
					return err
				}
			}
			return nil
		}
		if err = visit(coordinates); err != nil || points == 0 {
			return nil, nil, fmt.Errorf("OSM 后处理坐标无效: %v", err)
		}
		counts[feature.Geometry.Type]++
	}
	if len(features) == 0 {
		bounds = nil
	}
	return bounds, counts, nil
}
