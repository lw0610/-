package acquire

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/osmdata"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type OSMInvoker func(context.Context, string, map[string]any, osmdata.Options) (*osmdata.Result, error)
type OverpassAdapter struct {
	Options osmdata.Options
	Invoke  OSMInvoker
	Catalog *datasource.Catalog
}

func (OverpassAdapter) Kind() string { return "overpass" }
func (a OverpassAdapter) Fetch(ctx context.Context, r FetchRequest) (FetchResult, error) {
	if r.Source == nil || r.Provide == nil || len(r.Extent.BBox) != 4 {
		return FetchResult{}, fmt.Errorf("OSM 获取需要已确定的范围及来源")
	}
	invoke := a.Invoke
	if invoke == nil {
		invoke = osmdata.Invoke
	}
	options := a.Options
	options.Authorized = true
	options.OutputDir = r.WorkDir
	options.MaxResponseMB = r.Source.Limits.MaxMB
	if options.MaxResponseMB <= 0 {
		options.MaxResponseMB = 200
	}
	options.TimeoutSeconds = r.Source.Limits.TimeoutSeconds
	if options.TimeoutSeconds <= 0 {
		options.TimeoutSeconds = 120
	}
	// Enabled is supplied by current integration configuration; authorization is
	// supplied by the already confirmed acquisition plan, never by model arguments.
	extents := []string{osmBBox(r.Extent.BBox)}
	var extentNotes []string
	// Keep the verified polygon for Overpass selection instead of widening it
	// to its rectangle. The existing normalize stage still intersects the
	// downloaded geometry with the original boundary, including its holes.
	expanded := r.Item.Extent != nil && r.Item.Extent.ExpandPercent > 0
	if r.Extent.Path != "" && !expanded {
		ext := strings.ToLower(filepath.Ext(r.Extent.Path))
		if ext == ".geojson" || ext == ".json" {
			fileExtent := "file:" + r.Extent.Path
			if _, err := osmdata.ParseExtent(fileExtent); err != nil {
				return FetchResult{}, fmt.Errorf("OSM 边界文件不可用于查询，未退回更大的外包范围: %w", err)
			}
			extents = []string{fileExtent}
		} else {
			extentNotes = append(extentNotes, "边界不是 WGS84 GeoJSON，先按外包范围查询，再由规范化阶段用原始边界精确裁剪")
		}
	}
	args := map[string]any{"tags": r.Provide.Tags, "types": strings.Split(r.Provide.Types, ","), "extents": extents}
	probe, err := invoke(ctx, "osm_probe", args, options)
	if err != nil {
		return FetchResult{}, err
	}
	if probe == nil || probe.Partial || !probe.Success {
		return FetchResult{}, fmt.Errorf("OSM 范围统计不完整，未开始下载")
	}
	maximum := r.Source.Limits.MaxFeatures
	if maximum <= 0 {
		maximum = 30000
	}
	if largestProbeCount(probe) > maximum {
		if strings.HasPrefix(extents[0], "file:") {
			return FetchResult{}, fmt.Errorf("OSM 原边界内要素超过 %d，请提供保持原区域约束的分块边界或改用区域提取包；未退回行政区外接矩形", maximum)
		}
		limit := r.Source.Limits.MaxExtents
		if limit <= 0 {
			limit = 12
		}
		extents = administrativePartitions(a.Catalog, r.Extent.BBox, limit)
		if len(extents) == 0 {
			return FetchResult{}, fmt.Errorf("OSM 要素超过 %d，行政区拆分仍超限，请使用区域提取包", maximum)
		}
		args["extents"] = extents
		probe, err = invoke(ctx, "osm_probe", args, options)
		if err != nil {
			return FetchResult{}, err
		}
		if probe == nil || probe.Partial || largestProbeCount(probe) > maximum {
			return FetchResult{}, fmt.Errorf("分块后 OSM 查询仍超限或统计不完整，请换来源")
		}
	}
	fields := []string{"id", "osm_id", "name", "name:zh", "ref", "highway", "railway", "usage", "waterway", "natural", "place", "public_transport", "railway:class"}
	for _, candidates := range r.Provide.Fields {
		fields = append(fields, candidates...)
	}
	sort.Strings(fields)
	args["fields"] = fields
	args["output"] = "features.geojson"
	args["geometry"] = "original"
	if r.Item.Geometry == "point" {
		args["geometry"] = "centroid"
	}
	emitProgress(r.Progress, "download", 0, 0)
	result, err := invoke(ctx, "osm_get_features", args, options)
	if err != nil {
		return FetchResult{}, err
	}
	if result == nil || !result.Success {
		return FetchResult{}, fmt.Errorf("OSM 获取失败")
	}
	if result.Partial && r.Item.Required {
		return FetchResult{}, fmt.Errorf("OSM 必需条目只返回部分数据，核验未通过")
	}
	files := []string{}
	for _, path := range result.ArtifactPaths {
		if strings.EqualFold(filepath.Ext(path), ".geojson") {
			files = append(files, path)
		}
	}
	if len(files) == 0 {
		return FetchResult{}, fmt.Errorf("OSM 没有返回地理数据文件")
	}
	notes := append(extentNotes, result.Warnings...)
	notes = append(notes, result.FailedExtents...)
	notes = append(notes, result.Dropped...)
	if !result.Clipped {
		notes = append(notes, "Overpass 返回完整对象，尚未裁剪；规范化阶段须按原始边界或指定外包范围执行几何裁剪")
	}
	fetched := FetchResult{Files: files, Format: "geojson", Query: strings.Join(result.Queries, "\n"), URLs: []string{}, RetrievedAt: time.Now().UTC(), SHA256: map[string]string{}, Notes: notes}
	for _, node := range result.Nodes {
		fetched.URLs = append(fetched.URLs, RedactURL(node))
	}
	for _, path := range files {
		digest, size, err := fileDigest(path)
		if err != nil {
			return FetchResult{}, err
		}
		fetched.SHA256[path] = digest
		fetched.Bytes += size
	}
	return fetched, nil
}
func osmBBox(b []float64) string {
	return fmt.Sprintf("bbox:%.6f,%.6f,%.6f,%.6f", b[1], b[0], b[3], b[2])
}
func largestProbeCount(result *osmdata.Result) int {
	raw, _ := json.Marshal(result.Data)
	var rows []struct {
		Counts map[string]int `json:"counts"`
	}
	_ = json.Unmarshal(raw, &rows)
	largest := 0
	for _, row := range rows {
		count := row.Counts["total"]
		if count == 0 {
			count = row.Counts["nodes"] + row.Counts["ways"] + row.Counts["relations"]
		}
		if count > largest {
			largest = count
		}
	}
	return largest
}
func administrativePartitions(c *datasource.Catalog, box []float64, limit int) []string {
	if c == nil {
		return nil
	}
	for _, level := range []string{"市", "县"} {
		parts := []string{}
		seen := map[string]bool{}
		for _, r := range c.Regions {
			if r.Level != level || !datasource.Intersects(r.BBox, box) {
				continue
			}
			b := []float64{math.Max(box[0], r.BBox[0]), math.Max(box[1], r.BBox[1]), math.Min(box[2], r.BBox[2]), math.Min(box[3], r.BBox[3])}
			if b[0] >= b[2] || b[1] >= b[3] {
				continue
			}
			part := osmBBox(b)
			if !seen[part] {
				parts = append(parts, part)
				seen[part] = true
			}
		}
		if len(parts) > 1 && len(parts) <= limit {
			sort.Strings(parts)
			return parts
		}
	}
	return nil
}
