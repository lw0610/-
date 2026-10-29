package acquire

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type ValidationResult struct {
	OK       bool     `json:"ok"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}
type AttemptRecord struct {
	SourceID   string           `json:"source_id"`
	StartedAt  time.Time        `json:"started_at"`
	FinishedAt time.Time        `json:"finished_at"`
	Fetch      *FetchResult     `json:"fetch,omitempty"`
	Normalize  map[string]any   `json:"normalize,omitempty"`
	Validate   ValidationResult `json:"validate"`
	Error      string           `json:"error,omitempty"`
}
type OutputRecord struct {
	LayerDefinition map[string]any `json:"layer_definition,omitempty"`
	LayerID         string         `json:"layer_id,omitempty"`
	Paths           []string       `json:"paths"`
	Count           int            `json:"count"`
	Extent          []float64      `json:"extent"`
}
type ItemResult struct {
	Key      string          `json:"key"`
	Status   string          `json:"status"`
	SourceID string          `json:"source_id,omitempty"`
	StoreKey string          `json:"store_key,omitempty"`
	Attempts []AttemptRecord `json:"attempts"`
	Output   OutputRecord    `json:"output"`
	Notes    []string        `json:"notes"`
	Error    string          `json:"error,omitempty"`
}
type Engine struct {
	Catalog  *datasource.Catalog
	Store    *Store
	Adapters map[string]Adapter
	Invoke   ToolCaller
	Config   config.AcquisitionConfig
}

var workerPools sync.Map

func gate(key string, limit int) chan struct{} {
	if limit <= 0 {
		limit = 1
	}
	value, _ := workerPools.LoadOrStore(key, make(chan struct{}, limit))
	return value.(chan struct{})
}
func acquireGate(ctx context.Context, ch chan struct{}) (func(), error) {
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (e *Engine) RunItem(ctx context.Context, item flow.AcquisitionItem, progress func(Progress)) (ItemResult, error) {
	result := ItemResult{Key: item.Key, Status: "running", Attempts: []AttemptRecord{}, Notes: append([]string{}, item.Notes...), Output: OutputRecord{Paths: []string{}, Extent: []float64{}}}
	if item.Mode == "user" || item.Mode == "none" {
		result.Status = "user_provided"
		if item.Mode == "none" {
			result.Status = "skipped"
		}
		if item.UserPath != "" {
			result.Output.Paths = []string{item.UserPath}
		}
		return result, nil
	}
	if item.Extent == nil || item.Extent.Pending || len(item.Extent.BBox) != 4 {
		return result, fmt.Errorf("数据获取范围尚未确定")
	}
	if e.Store == nil || e.Catalog == nil {
		return result, fmt.Errorf("数据获取执行器尚未配置")
	}
	cfg := e.Config.WithDefaults()
	release, err := acquireGate(ctx, gate(e.Store.Root+"|global", cfg.Concurrency))
	if err != nil {
		return result, err
	}
	defer release()
	choices := []flow.SourceChoice{}
	if item.Chosen != nil {
		choices = append(choices, *item.Chosen)
	}
	choices = append(choices, item.Alternatives...)
	for index, choice := range choices {
		if index > 0 && (!cfg.FallbackEnabled() || choice.Tier > 6) {
			break
		}
		source := e.Catalog.Source(choice.SourceID)
		if source == nil {
			continue
		}
		adapter := e.Adapters[source.Kind]
		if adapter == nil {
			result.Error = "该来源的获取适配器尚未启用"
			continue
		}
		attempt := AttemptRecord{SourceID: source.ID, StartedAt: time.Now().UTC(), Validate: ValidationResult{Errors: []string{}, Warnings: []string{}}}
		host := source.ID
		if len(source.Hosts) > 0 {
			host = source.Hosts[0]
		}
		releaseHost, err := acquireGate(ctx, gate(e.Store.Root+"|host|"+host, cfg.PerHost))
		if err != nil {
			return result, err
		}
		record, reused, runErr := e.materialize(ctx, item, choice, *source, adapter, &attempt, progress)
		releaseHost()
		attempt.FinishedAt = time.Now().UTC()
		if runErr != nil {
			attempt.Error = runErr.Error()
			result.Attempts = append(result.Attempts, attempt)
			result.Error = runErr.Error()
			if ctx.Err() != nil {
				result.Status = "failed"
				return result, ctx.Err()
			}
			continue
		}
		result.Attempts = append(result.Attempts, attempt)
		result.Status = "completed"
		if reused {
			attempt.Validate = ValidationResult{OK: true, Errors: []string{}, Warnings: []string{}}
			if data, err := json.Marshal(record.Metadata["validate"]); err == nil {
				_ = json.Unmarshal(data, &attempt.Validate)
			}
			result.Attempts[len(result.Attempts)-1] = attempt
			result.Status = "reused"
		}
		result.SourceID = source.ID
		result.StoreKey = record.Key
		result.Output.Paths = record.Outputs()
		if definition, ok := record.Metadata["layer_definition"].(map[string]any); ok {
			result.Output.LayerDefinition = definition
			result.Status = "waiting_user"
		}
		result.Notes = append(result.Notes, record.Notes...)
		result.Error = ""
		if stats, ok := record.Metadata["normalize"].(map[string]any); ok {
			result.Output.Count = int(number(stats["count"]))
			result.Output.Extent = floatSlice(stats["extent_wgs84"])
			if len(result.Output.Extent) == 0 {
				result.Output.Extent = floatSlice(stats["extent"])
			}
		}
		return result, nil
	}
	if result.Error == "" {
		result.Error = "没有可用的数据来源"
	}
	result.Status = "failed"
	return result, fmt.Errorf("%s", result.Error)
}
func (e *Engine) materialize(ctx context.Context, item flow.AcquisitionItem, choice flow.SourceChoice, source datasource.Source, adapter Adapter, attempt *AttemptRecord, progress func(Progress)) (StoreRecord, bool, error) {
	provide := choice.Provide
	category := e.Catalog.Category(item.Category)
	if category == nil {
		return StoreRecord{}, false, fmt.Errorf("类别不存在")
	}
	spec, err := cacheSpecification(e.Catalog, item, choice, source)
	if err != nil {
		return StoreRecord{}, false, err
	}
	params := spec.Params
	filter, notes := generalizedFilter(category, provide, item.Scale)
	return e.Store.Materialize(ctx, spec, source.RefreshDays, func(dir string) (Manifest, error) {
		rawDir := filepath.Join(dir, "raw")
		fetchItem := item
		fetchItem.Params = params
		request := FetchRequest{Source: &source, Provide: &provide, Item: fetchItem, Extent: Extent{BBox: item.Extent.BBox, Path: item.Extent.Path}, WorkDir: rawDir, Progress: progress}
		emitProgress(progress, "fetch", 0, 0)
		var fetched FetchResult
		cached, contains, err := e.Store.FindContaining(ctx, spec)
		if err == nil && contains {
			encoded, _ := json.Marshal(cached.Metadata["fetch"])
			_ = json.Unmarshal(encoded, &fetched)
			fetched.Files = cached.Raw()
			fetched.Notes = append(fetched.Notes, "复用较大范围的已缓存原件，按本次范围重新规范化")
		} else if err == nil {
			fetched, err = adapter.Fetch(ctx, request)
		}
		if err != nil {
			return Manifest{}, err
		}
		attempt.Fetch = &fetched
		fetched.URLs = safeProvenanceURLs(fetched.URLs)
		if fetched.LayerDefinition != nil {
			output := filepath.Join(dir, "out", "tiles.layer.json")
			if err = atomicJSON(output, fetched.LayerDefinition); err != nil {
				return Manifest{}, err
			}
			attempt.Validate = ValidationResult{OK: true, Errors: []string{}, Warnings: []string{}}
			return Manifest{OutputFiles: []string{"out/tiles.layer.json"}, RawFiles: []string{}, License: source.License, Attribution: source.Attribution, Notes: fetched.Notes, Metadata: map[string]any{"layer_definition": fetched.LayerDefinition}}, nil
		}
		if len(fetched.Files) == 0 {
			return Manifest{}, fmt.Errorf("数据源没有返回可供核验的文件")
		}
		files := []string{}
		for _, path := range fetched.Files {
			relative, relErr := canonicalRelative(dir, path)
			if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				target := filepath.Join(rawDir, filepath.Base(path))
				if err = copyReadonly(path, target); err != nil {
					return Manifest{}, err
				}
				path = target
			} else {
				path = filepath.Join(dir, relative)
			}
			files = append(files, path)
		}
		rawFiles := append([]string{}, files...)
		if fetched.Format == "zip" || fetched.Format == "tar.gz" {
			if e.Invoke == nil {
				return Manifest{}, fmt.Errorf("压缩数据需要已配置的解压工具")
			}
			emitProgress(progress, "extract", 0, 0)
			value, err := e.Invoke(ctx, "archive_extract", map[string]any{"input": files[0], "output": filepath.Join(rawDir, "unpacked"), "max_files": 10000, "max_bytes": int64(e.Config.WithDefaults().MaxDownloadMB) * 4 * 1024 * 1024})
			if err != nil {
				return Manifest{}, err
			}
			if _, err = UnwrapToolResult(value); err != nil {
				return Manifest{}, err
			}
			files = nil
			err = filepath.WalkDir(filepath.Join(rawDir, "unpacked"), func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if !entry.IsDir() && isGeodata(path) {
					files = append(files, path)
				}
				return nil
			})
			if err != nil {
				return Manifest{}, err
			}
		}
		input, err := selectInput(files, provide, item.Geometry, choice.Derivation != nil)
		if err != nil {
			return Manifest{}, err
		}
		output := filepath.Join(dir, "out", outputFilename(item))
		normalOutput := output
		geometry := item.Geometry
		if choice.Derivation != nil {
			normalOutput = filepath.Join(dir, "raw", "prepared.gpkg")
			if base := e.Catalog.Category(choice.Derived); base != nil {
				geometry = base.Geometry
			}
			if geometry == "raster" {
				normalOutput = filepath.Join(dir, "raw", "prepared.tif")
			}
		}
		fieldMap := map[string][]string{}
		for target, standard := range item.Fields {
			candidates := []string(provide.Fields[standard])
			if len(candidates) == 0 {
				candidates = []string{standard}
			}
			fieldMap[target] = candidates
		}
		if len(fieldMap) > 0 {
			for standard, candidates := range provide.Fields {
				if _, exists := fieldMap[standard]; !exists && standard != "name" {
					fieldMap[standard] = []string(candidates)
				}
			}
		}
		clip := map[string]any{"bbox": item.Extent.BBox}
		if item.Extent.Path != "" && item.Extent.ExpandPercent == 0 {
			clip = map[string]any{"path": item.Extent.Path}
		}
		normalizeArgs := map[string]any{"input": map[string]any{"path": input, "layer": provide.Layer}, "category": item.Category, "expected_geometry": geometry, "filter": filter, "clip": clip, "fields": fieldMap, "output": normalOutput, "simplify_tolerance_m": category.SimplifyMM * float64(item.Scale) / 1000}
		emitProgress(progress, "normalize", 0, 0)
		var stats map[string]any
		if category.Geometry == "table" {
			if choice.Derivation != nil {
				return Manifest{}, fmt.Errorf("统计表不能执行空间派生")
			}
			// Tables have no spatial geometry or CRS. Keep their validation and
			// publication in Go instead of sending them to QGIS vector tools.
			stats, err = normalizeProvidedTable(ctx, input, output, fieldMap)
		} else {
			if e.Invoke == nil {
				return Manifest{}, fmt.Errorf("空间数据规范化工具未配置")
			}
			var value any
			value, err = e.Invoke(ctx, "data_normalize", normalizeArgs)
			if err == nil {
				var payload map[string]any
				payload, err = UnwrapToolResult(value)
				stats = toolData(payload)
			}
		}
		if err != nil {
			return Manifest{}, err
		}
		if choice.Derivation != nil {
			emitProgress(progress, "derive", 0, 0)
			value, err := e.Invoke(ctx, "data_derive", map[string]any{"input": map[string]any{"path": normalOutput}, "method": choice.Derivation.Method, "output": output, "boundary_kind": params["level"] + "界", "include_outer": false})
			if err != nil {
				return Manifest{}, err
			}
			payload, err := UnwrapToolResult(value)
			if err != nil {
				return Manifest{}, err
			}
			stats = toolData(payload)
			if choice.Derivation.Note != "" {
				notes = append(notes, choice.Derivation.Note)
			}
		}
		attempt.Normalize = stats
		emitProgress(progress, "validate", 0, 0)
		attempt.Validate = ValidateStats(category, item, stats)
		if !attempt.Validate.OK {
			return Manifest{}, fmt.Errorf("数据核验失败：%s", strings.Join(attempt.Validate.Errors, "；"))
		}
		primary, _ := stats["path"].(string)
		if primary == "" {
			primary = output
		}
		outputs := []string{primary}
		primaryRel, err := canonicalRelative(dir, primary)
		if err != nil {
			return Manifest{}, err
		}
		for _, member := range stringSlice(stats["paths"]) {
			rel, err := canonicalRelative(dir, member)
			if err != nil {
				return Manifest{}, err
			}
			if rel != primaryRel {
				outputs = append(outputs, member)
			}
		}
		manifest := Manifest{RawFiles: []string{}, OutputFiles: []string{}, License: source.License, Attribution: source.Attribution, Notes: append(append(append(notes, fetched.Notes...), attempt.Validate.Warnings...), source.Notes...), Metadata: map[string]any{"fetch": fetched, "normalize": stats, "validate": attempt.Validate}}
		for _, path := range rawFiles {
			relative, err := canonicalRelative(dir, path)
			if err != nil {
				return Manifest{}, err
			}
			manifest.RawFiles = append(manifest.RawFiles, filepath.ToSlash(relative))
		}
		for _, path := range outputs {
			relative, err := canonicalRelative(dir, path)
			if err != nil {
				return Manifest{}, err
			}
			manifest.OutputFiles = append(manifest.OutputFiles, filepath.ToSlash(relative))
		}
		emitProgress(progress, "store", 0, 0)
		return manifest, nil
	})
}
func ValidateStats(category *datasource.Category, item flow.AcquisitionItem, stats map[string]any) ValidationResult {
	result := ValidationResult{OK: true, Errors: []string{}, Warnings: []string{}}
	fail := func(s string) { result.Errors = append(result.Errors, s); result.OK = false }
	if category.Geometry == "raster" {
		if fmt.Sprint(stats["crs"]) == "" || stats["crs"] == nil {
			fail("栅格坐标系未定义")
		}
		if number(stats["nodata_ratio"]) >= .5 {
			result.Warnings = append(result.Warnings, "栅格无数据像元超过一半")
		}
	} else if category.Geometry != "table" && category.Geometry != "tiles" {
		minimum := category.Validate.MinFeatures
		if minimum < 1 {
			minimum = 1
		}
		if int(number(stats["count"])) < minimum {
			fail("要素数不足")
		}
		geometry := strings.ToLower(fmt.Sprint(stats["geometry_type"]))
		wanted := category.Geometry
		if wanted == "line" {
			wanted = "line"
		}
		if !strings.Contains(geometry, wanted) {
			fail("几何类型与类别不一致")
		}
		if stats["crs"] == nil || fmt.Sprint(stats["crs"]) == "" {
			fail("坐标系未定义")
		}
		fields := mapNumbers(stats["fields"])
		for target, standard := range item.Fields {
			ratio, ok := fields[target]
			if !ok {
				fail("缺少必需字段：" + target)
				continue
			}
			if standard == "name" && category.Validate.NameFillRatio > 0 {
				if ratio < category.Validate.NameFillRatio*.5 {
					fail("名称字段缺失过多：" + target)
				} else if ratio < category.Validate.NameFillRatio {
					result.Warnings = append(result.Warnings, "部分名称字段为空："+target)
				}
			}
		}
	}
	extent := floatSlice(stats["extent_wgs84"])
	if len(extent) != 4 && strings.Contains(fmt.Sprint(stats["crs"]), "4326") {
		extent = floatSlice(stats["extent"])
	}
	if len(extent) == 4 && item.Extent != nil && len(item.Extent.BBox) == 4 {
		target := item.Extent.BBox
		if !datasource.Intersects(target, extent) {
			fail("数据范围与需求不相交")
		} else if (category.Geometry == "polygon" || category.Geometry == "raster") && area(target) > 0 {
			// Feature bounds are not query coverage for point/line datasets: a
			// valid station or straight railway can have a zero-area envelope.
			overlap := math.Max(0, math.Min(target[2], extent[2])-math.Max(target[0], extent[0])) * math.Max(0, math.Min(target[3], extent[3])-math.Max(target[1], extent[1])) / area(target)
			if overlap < .3 {
				fail("数据范围覆盖不足 30%")
			} else if overlap < .8 {
				result.Warnings = append(result.Warnings, "数据范围覆盖不足 80%")
			}
		}
	}
	result.Warnings = append(result.Warnings, stringSlice(stats["warnings"])...)
	return result
}
func toolData(payload map[string]any) map[string]any {
	if data, ok := payload["data"].(map[string]any); ok {
		return data
	}
	return payload
}
func number(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		n, _ := v.Float64()
		return n
	}
	return 0
}
func floatSlice(value any) []float64 {
	switch v := value.(type) {
	case []float64:
		return v
	case []any:
		r := []float64{}
		for _, n := range v {
			r = append(r, number(n))
		}
		return r
	}
	return nil
}
func stringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		r := []string{}
		for _, s := range v {
			if text, ok := s.(string); ok {
				r = append(r, text)
			}
		}
		return r
	}
	return nil
}
func mapNumbers(value any) map[string]float64 {
	raw, _ := json.Marshal(value)
	r := map[string]float64{}
	_ = json.Unmarshal(raw, &r)
	return r
}
func outputFilename(item flow.AcquisitionItem) string {
	name := filepath.Base(item.Filename)
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = item.Entry
		if name == "" {
			name = item.Requirement
		}
	}
	if filepath.Ext(name) == "" {
		ext := ".gpkg"
		if item.Geometry == "raster" {
			ext = ".tif"
		}
		if item.Geometry == "table" {
			ext = ".csv"
		}
		name += ext
	}
	return name
}
func isGeodata(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".shp", ".gpkg", ".geojson", ".fgb", ".tif", ".tiff", ".osm", ".pbf", ".csv", ".tsv", ".xlsx":
		return true
	}
	return false
}
func selectInput(files []string, p datasource.Provide, geometry string, derived bool) (string, error) {
	candidates := []string{}
	for _, path := range files {
		if isGeodata(path) {
			candidates = append(candidates, path)
		}
	}
	sort.Strings(candidates)
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	suffix := map[string]string{"line": "-lines.geojson", "polygon": "-polygons.geojson", "point": "-points.geojson"}[geometry]
	if !derived && suffix != "" {
		for _, path := range candidates {
			if strings.HasSuffix(path, suffix) {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("来源包含零个或多个无法确定的图层，请明确来源图层")
}
func copyReadonly(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
func generalizedFilter(category *datasource.Category, p datasource.Provide, scale int) (string, []string) {
	filter := p.Filter
	if filter == "" {
		filter = "TRUE"
	}
	notes := []string{}
	if scale <= 0 || len(category.Generalize) == 0 {
		return filter, notes
	}
	for _, rule := range category.Generalize {
		if rule.MaxDenominator > 0 && scale > rule.MaxDenominator {
			continue
		}
		candidates := p.Fields["class"]
		if len(candidates) == 0 {
			break
		}
		values := []string{}
		for _, class := range rule.KeepClass {
			values = append(values, "'"+strings.ReplaceAll(class, "'", "''")+"'")
		}
		// Provider class vocabularies can differ. Only apply an OSM rule to providers
		// explicitly exposing highway, never guess equivalent classes in other data.
		if hasCandidate(candidates, "highway") {
			filter = "(" + filter + ") AND \"highway\" IN (" + strings.Join(values, ",") + ")"
			notes = append(notes, fmt.Sprintf("按 1:%d 保留道路等级：%s", scale, strings.Join(rule.KeepClass, "、")))
		}
		break
	}
	return filter, notes
}
func hasCandidate(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// Ensure source URLs saved by an adapter do not accidentally acquire credentials.
func safeProvenanceURLs(urls []string) []string {
	out := []string{}
	for _, raw := range urls {
		if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
			out = append(out, RedactURL(raw))
		}
	}
	return out
}

func cacheSpecification(catalog *datasource.Catalog, item flow.AcquisitionItem, choice flow.SourceChoice, source datasource.Source) (CacheSpec, error) {
	category := catalog.Category(item.Category)
	if category == nil || item.Extent == nil || item.Extent.Pending || len(item.Extent.BBox) != 4 {
		return CacheSpec{}, fmt.Errorf("获取参数尚未确定")
	}
	provide := choice.Provide
	params := provide.EffectiveParams(item.Params, choice.Derivation)
	filter, _ := generalizedFilter(category, provide, item.Scale)
	normalizeSpec := map[string]any{"fields": item.Fields, "filter": filter, "geometry": item.Geometry, "derivation": choice.Derivation, "filename": outputFilename(item), "simplify_mm": category.SimplifyMM}
	query, _ := json.Marshal(provide)
	spec := CacheSpec{SourceID: source.ID, SourceVersion: source.Version, Category: item.Category, Params: params, Query: string(query), BBox: item.Extent.BBox, ScaleBand: item.Scale, Normalize: normalizeSpec, AdapterVersion: "v4.1"}
	if item.Extent.Path != "" {
		if hash, _, err := fileDigest(item.Extent.Path); err == nil {
			spec.MaskDigest = hash
		}
	}
	return spec, nil
}

func canonicalRelative(root, path string) (string, error) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(realRoot, realPath)
	if err != nil {
		return "", err
	}
	return relative, nil
}
