package integrations

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/autocarto/carto_agent/internal/osmdata"
)

// GISInvoker is supplied by the application, never by model tool arguments.
type GISInvoker func(context.Context, string, map[string]any) (any, error)

func osmStrings(value any) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []any:
		var result []string
		for _, item := range values {
			value, ok := item.(string)
			if !ok {
				return nil
			}
			result = append(result, value)
		}
		return result
	case string:
		if strings.TrimSpace(values) != "" {
			return []string{values}
		}
	}
	return nil
}

func canonicalOSMPath(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

func invokeOSM(ctx context.Context, name string, args map[string]any, options osmdata.Options, gis GISInvoker) (*osmdata.Result, error) {
	extents := osmStrings(args["extents"])
	if len(extents) == 0 {
		extents = osmStrings(args["extent"])
	}
	if name != "osm_get_features" || len(extents) != 1 || !strings.HasPrefix(extents[0], "file:") || gis == nil {
		return osmdata.Invoke(ctx, name, args, options)
	}
	if _, err := osmdata.ParseExtent(extents[0]); err != nil {
		return nil, err
	}
	boundary, err := canonicalOSMPath(strings.TrimPrefix(extents[0], "file:"))
	if err != nil {
		return nil, err
	}
	boundaryBytes, err := os.ReadFile(boundary)
	if err != nil {
		return nil, err
	}
	boundaryHash := sha256.Sum256(boundaryBytes)
	boundaryBytes = nil
	base, err := osmdata.ValidateOutput(options, args)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(options.OutputDir, 0700); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(options.OutputDir, ".osm-clip-")
	if err != nil {
		return nil, err
	}
	retainStage := false
	defer func() {
		if !retainStage {
			_ = os.RemoveAll(stage)
		}
	}()
	finalArgs := make(map[string]any, len(args)+1)
	rawArgs := make(map[string]any, len(args)+2)
	for key, value := range args {
		finalArgs[key], rawArgs[key] = value, value
	}
	finalArgs["extents"], rawArgs["extents"] = []string{"file:" + boundary}, []string{"file:" + boundary}
	rawArgs["output"], rawArgs["overwrite"] = "download.geojson", false
	rawOptions := options
	rawOptions.OutputDir, rawOptions.ProjectDir = filepath.Join(stage, "raw"), stage

	// The download's provisional completion must not register staging files or
	// show 100% before QGIS has finished the exact intersection.
	callback := osmdata.ProgressCallback(ctx)
	var progressMu sync.Mutex
	var last osmdata.Progress
	started := time.Now()
	downloadCtx := osmdata.WithProgressCallback(ctx, func(p osmdata.Progress) {
		p.Filename, p.ArtifactPaths = filepath.Base(base), nil
		if p.Phase == "completed" || p.Phase == "partial" {
			p.Phase, p.Percent, p.Message = "processing", nil, "下载完成，正在按原始边界精确裁剪"
		}
		progressMu.Lock()
		last = p
		progressMu.Unlock()
		if callback != nil {
			callback(p)
		}
	})
	finish := func(result *osmdata.Result, failure error) {
		if callback == nil {
			return
		}
		progressMu.Lock()
		p := last
		progressMu.Unlock()
		p.ElapsedSeconds, p.Percent = time.Since(started).Seconds(), nil
		p.Phase = "completed"
		if result != nil {
			p.FeatureCount, p.ArtifactPaths, p.Message = result.FeatureCount, nil, result.Info
			for _, path := range result.ArtifactPaths {
				if !isOSMStagingPath(options.OutputDir, path) {
					p.ArtifactPaths = append(p.ArtifactPaths, path)
				}
			}
			if result.Partial {
				p.Phase = "partial"
			}
		}
		if failure != nil {
			p.Phase, p.Message = "failed", failure.Error()
			if errors.Is(failure, context.Canceled) {
				p.Phase = "cancelled"
			}
		} else {
			complete := 100.0
			p.Percent = &complete
		}
		callback(p)
	}
	rawResult, downloadErr := osmdata.Invoke(downloadCtx, name, rawArgs, rawOptions)
	if rawResult == nil || len(rawResult.ArtifactPaths) == 0 {
		finish(rawResult, downloadErr)
		return rawResult, downloadErr
	}
	rawResult.ClipBoundary, rawResult.ClipStatus = boundary, "pending"
	var rawFeatures, clippedFeatures []osmdata.Feature
	var rawPaths []string
	for _, path := range rawResult.ArtifactPaths {
		if strings.HasSuffix(path, ".geojson") {
			rawPaths = append(rawPaths, path)
		}
	}
	for _, path := range rawPaths {
		features, readErr := osmdata.ReadFeatures(path)
		if readErr != nil {
			downloadErr = readErr
			break
		}
		rawFeatures = append(rawFeatures, features...)
	}
	// Preserve original files on all failed overwrite attempts, including a
	// failure after the final writer has rolled back a partial replacement.
	recoverRaw := func(cause error) (*osmdata.Result, error) {
		rawResult.Success, rawResult.Clipped, rawResult.ClipStatus = false, false, "failed"
		rawResult.Info = "OSM 原始数据已下载，但精确裁剪或发布未完成；可继续处理原件，无需重新下载"
		rawResult.Warnings = append(rawResult.Warnings, cause.Error())
		overwrite, _ := args["overwrite"].(bool)
		if len(rawFeatures) == rawResult.FeatureCount && !(overwrite && osmOutputFamilyExists(base)) {
			recoveryArgs := make(map[string]any, len(finalArgs))
			for key, value := range finalArgs {
				recoveryArgs[key] = value
			}
			// An output may appear after the existence check. Failed clipping
			// must never replace another writer's newly completed result.
			recoveryArgs["overwrite"] = false
			if publishErr := osmdata.PublishFeatures(options, recoveryArgs, rawFeatures, rawResult); publishErr == nil {
				failure := fmt.Errorf("%s: %w", rawResult.Info, cause)
				finish(rawResult, failure)
				return rawResult, failure
			} else {
				cause = fmt.Errorf("%w; 原件发布失败: %v", cause, publishErr)
			}
		}
		retainStage = true
		// PublishFeatures clears its output list before starting, so restore only
		// the complete raw artifacts when the final destination could not be used.
		rawResult.ArtifactPaths = nil
		for _, path := range rawPaths {
			rawResult.ArtifactPaths = append(rawResult.ArtifactPaths, path, path+".source.json")
		}
		failure := fmt.Errorf("%s；原件保留在 %s，已有目标未用未裁剪数据替换: %w", rawResult.Info, filepath.Join(stage, "raw"), cause)
		finish(rawResult, failure)
		return rawResult, failure
	}
	if downloadErr != nil {
		return recoverRaw(downloadErr)
	}
	currentBoundary, err := os.ReadFile(boundary)
	if err != nil || sha256.Sum256(currentBoundary) != boundaryHash {
		return recoverRaw(fmt.Errorf("下载期间原始裁剪边界发生变化或无法读取"))
	}
	for index, path := range rawPaths {
		if err := ctx.Err(); err != nil {
			return recoverRaw(err)
		}
		output := filepath.Join(stage, fmt.Sprintf("clipped-%d.geojson", index))
		payload, err := gis(ctx, "data_normalize", map[string]any{
			"input": map[string]any{"path": path}, "clip": map[string]any{"path": boundary}, "output": output,
			"fields": map[string]any{}, "standard_fields": []string{}, "simplify_tolerance_m": 0,
		})
		if err != nil {
			return recoverRaw(err)
		}
		data, err := osmNormalizeData(payload)
		if err != nil {
			return recoverRaw(err)
		}
		clipPath, _ := data["clip_path"].(string)
		clipPath, pathErr := canonicalOSMPath(clipPath)
		if data["clip_applied"] != true || data["clip_kind"] != "polygon" || pathErr != nil || clipPath != boundary || data["crs"] != "EPSG:4326" {
			return recoverRaw(fmt.Errorf("QGIS 未确认按原始边界完成 WGS84 精确裁剪"))
		}
		features, err := osmdata.ReadFeatures(output)
		if err != nil {
			return recoverRaw(err)
		}
		inputFeatures, err := osmdata.ReadFeatures(path)
		if err != nil {
			return recoverRaw(err)
		}
		if data["count"] != float64(len(features)) || data["clip_output_features"] != float64(len(features)) || data["clip_input_features"] != float64(len(inputFeatures)) {
			return recoverRaw(fmt.Errorf("QGIS 精裁计数与输出文件不一致"))
		}
		fields, _ := data["fields"].(map[string]any)
		requiredFields := map[string]bool{}
		for _, feature := range inputFeatures {
			for field := range feature.Properties {
				requiredFields[field] = true
				if _, exists := fields[field]; !exists {
					return recoverRaw(fmt.Errorf("QGIS 精裁丢失原字段 %s", field))
				}
			}
		}
		for _, feature := range features {
			for field := range requiredFields {
				if _, exists := feature.Properties[field]; !exists {
					return recoverRaw(fmt.Errorf("QGIS 精裁文件缺失原字段 %s", field))
				}
			}
		}
		clippedFeatures = append(clippedFeatures, features...)
	}
	if err := ctx.Err(); err != nil {
		return recoverRaw(err)
	}
	currentBoundary, err = os.ReadFile(boundary)
	if err != nil || sha256.Sum256(currentBoundary) != boundaryHash {
		return recoverRaw(fmt.Errorf("精裁期间原始边界发生变化，未发布可能不一致的结果"))
	}
	complete := *rawResult
	complete.Warnings = nil
	for _, warning := range rawResult.Warnings {
		if !strings.HasPrefix(warning, "Overpass 仅按范围筛选完整对象") {
			complete.Warnings = append(complete.Warnings, warning)
		}
	}
	complete.Clipped, complete.ClipStatus = true, "complete"
	complete.Info = fmt.Sprintf("已获取并按原始边界精确裁剪 %d 个 OSM 要素", len(clippedFeatures))
	if complete.Partial {
		complete.Info = "部分下载结果：" + complete.Info
	}
	if err := osmdata.PublishFeatures(options, finalArgs, clippedFeatures, &complete); err != nil {
		return recoverRaw(err)
	}
	finish(&complete, nil)
	return &complete, nil
}

func isOSMStagingPath(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	return strings.HasPrefix(strings.Split(relative, string(filepath.Separator))[0], ".osm-clip-")
}

func osmOutputFamilyExists(base string) bool {
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	for _, path := range []string{base, stem + "-points.geojson", stem + "-lines.geojson", stem + "-polygons.geojson"} {
		for _, file := range []string{path, path + ".source.json"} {
			if _, err := os.Lstat(file); !os.IsNotExist(err) {
				return true
			}
		}
	}
	return false
}

func osmNormalizeData(value any) (map[string]any, error) {
	if encoded, ok := value.(string); ok {
		if err := json.Unmarshal([]byte(encoded), &value); err != nil {
			return nil, err
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err = json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	for depth := 0; depth < 4; depth++ {
		if success, ok := payload["success"].(bool); ok && !success {
			return nil, fmt.Errorf("QGIS 精裁失败: %v", payload["info"])
		}
		if data, ok := payload["data"].(map[string]any); ok {
			if success, ok := data["success"].(bool); ok && !success {
				return nil, fmt.Errorf("QGIS 精裁失败: %v", data["info"])
			}
			return data, nil
		}
		if nested, ok := payload["output"].(map[string]any); ok {
			payload = nested
		} else if nested, ok := payload["result"].(map[string]any); ok {
			payload = nested
		} else {
			break
		}
	}
	return nil, fmt.Errorf("QGIS 精裁未返回可核验结果")
}
