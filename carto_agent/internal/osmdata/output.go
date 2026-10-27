package osmdata

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Result struct {
	Success        bool           `json:"success"`
	Info           string         `json:"info,omitempty"`
	ArtifactPaths  []string       `json:"artifact_paths,omitempty"`
	FeatureCount   int            `json:"feature_count"`
	Partial        bool           `json:"partial"`
	Clipped        bool           `json:"clipped"`
	Bounds         []float64      `json:"bounds,omitempty"`
	ClipBoundary   string         `json:"clip_boundary,omitempty"`
	ClipStatus     string         `json:"clip_status,omitempty"`
	FailedExtents  []string       `json:"failed_extents,omitempty"`
	Dropped        []string       `json:"dropped,omitempty"`
	DroppedFields  map[string]int `json:"dropped_fields,omitempty"`
	GeometryCounts map[string]int `json:"geometry_counts,omitempty"`
	KeptFields     []string       `json:"kept_fields,omitempty"`
	Warnings       []string       `json:"warnings,omitempty"`
	Nodes          []string       `json:"nodes,omitempty"`
	Queries        []string       `json:"queries,omitempty"`
	Data           any            `json:"data,omitempty"`
}

var outputPublishMu sync.Mutex

// PublishFeatures reuses the normal output naming, provenance and overwrite
// contract for trusted postprocessing. It performs no geometry operations.
func PublishFeatures(options Options, args map[string]any, features []Feature, result *Result) error {
	var err error
	result.Bounds, result.GeometryCounts, err = FeatureStats(features)
	if err != nil {
		return err
	}
	result.FeatureCount = len(features)
	result.ArtifactPaths = nil
	result.KeptFields = nil
	return writeOutputs(options, args, features, result)
}

// ValidateOutput rejects invalid final destinations before a staged download.
func ValidateOutput(options Options, args map[string]any) (string, error) {
	base, err := outputBase(options, stringArg(args, "output"))
	if err != nil {
		return "", err
	}
	if overwrite, _ := args["overwrite"].(bool); overwrite && !within(options.OutputDir, base) {
		return "", fmt.Errorf("仅本会话 OSM 输出目录允许覆盖")
	}
	return base, nil
}

func within(root, path string) bool {
	if root == "" {
		return false
	}
	r, e := filepath.Abs(root)
	if e != nil {
		return false
	}
	p, e := filepath.Abs(path)
	if e != nil {
		return false
	}
	rel, e := filepath.Rel(r, p)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	for current := p; current != ""; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if current == r || current == filepath.Dir(current) {
			break
		}
	}
	return true
}
func outputBase(options Options, raw string) (string, error) {
	if options.OutputDir == "" {
		return "", fmt.Errorf("缺少可信输出目录")
	}
	if raw == "" {
		raw = "osm-data.geojson"
	}
	if !filepath.IsAbs(raw) {
		raw = filepath.Join(options.OutputDir, raw)
	}
	raw = filepath.Clean(raw)
	if !within(options.OutputDir, raw) && !within(options.ProjectDir, raw) {
		return "", fmt.Errorf("OSM 输出只能位于当前会话或当前工程内")
	}
	if strings.ToLower(filepath.Ext(raw)) != ".geojson" {
		return "", fmt.Errorf("OSM 输出必须为 .geojson")
	}
	return raw, nil
}
func familyPaths(base string) []string {
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	return []string{base, stem + "-points.geojson", stem + "-lines.geojson", stem + "-polygons.geojson"}
}
func writeOutputs(options Options, args map[string]any, features []Feature, result *Result) error {
	outputPublishMu.Lock()
	defer outputPublishMu.Unlock()
	base, err := outputBase(options, stringArg(args, "output"))
	if err != nil {
		return err
	}
	overwrite, _ := args["overwrite"].(bool)
	if overwrite && !within(options.OutputDir, base) {
		return fmt.Errorf("仅本会话 OSM 输出目录允许覆盖")
	}
	if err = os.MkdirAll(filepath.Dir(base), 0700); err != nil {
		return err
	}
	baseOriginal := base
	for n := 1; !overwrite; n++ {
		exists := false
		for _, path := range familyPaths(base) {
			for _, p := range []string{path, path + ".source.json"} {
				if _, e := os.Lstat(p); e == nil {
					exists = true
				}
			}
		}
		if !exists {
			break
		}
		base = strings.TrimSuffix(baseOriginal, filepath.Ext(baseOriginal)) + fmt.Sprintf("-%d.geojson", n)
	}
	groups := map[string][]Feature{}
	if len(features) == 0 {
		groups[""] = []Feature{} // A verified empty intersection is still a valid output.
	}
	result.GeometryCounts = map[string]int{}
	fields := map[string]bool{}
	for _, feature := range features {
		groups[Family(feature.Geometry.Type)] = append(groups[Family(feature.Geometry.Type)], feature)
		result.GeometryCounts[feature.Geometry.Type]++
		for k := range feature.Properties {
			fields[k] = true
		}
	}
	for key := range fields {
		result.KeptFields = append(result.KeptFields, key)
	}
	sort.Strings(result.KeptFields)
	staged := map[string][]byte{}
	for family, rows := range groups {
		path := base
		if len(groups) > 1 {
			path = strings.TrimSuffix(base, filepath.Ext(base)) + "-" + family + ".geojson"
		}
		content, e := json.Marshal(map[string]any{"type": "FeatureCollection", "features": rows})
		if e != nil {
			return e
		}
		staged[path] = content
		bounds, geometryCounts, e := FeatureStats(rows)
		if e != nil {
			return e
		}
		source := map[string]any{"source": "OpenStreetMap", "license": "ODbL 1.0", "attribution": "© OpenStreetMap contributors", "acquired_at": time.Now().UTC().Format(time.RFC3339), "queries": result.Queries, "extents": stringList(args["extents"]), "tags": stringList(args["tags"]), "nodes": result.Nodes, "feature_count": len(rows), "kept_fields": result.KeptFields, "dropped_fields": result.DroppedFields, "partial": result.Partial, "failed_extents": result.FailedExtents, "dropped": result.Dropped, "clipped": result.Clipped, "warnings": result.Warnings, "bounds": result.Bounds, "geometry_counts": result.GeometryCounts}
		if result.ClipStatus != "" {
			source["clip_status"], source["clip_boundary"] = result.ClipStatus, result.ClipBoundary
		}
		source["bounds"], source["geometry_counts"] = bounds, geometryCounts
		staged[path+".source.json"], e = json.MarshalIndent(source, "", "  ")
		if e != nil {
			return e
		}
	}
	temporary, err := os.MkdirTemp(filepath.Dir(base), ".osm-")
	if err != nil {
		return err
	}
	keepTemporary := false
	defer func() {
		if !keepTemporary {
			_ = os.RemoveAll(temporary)
		}
	}()
	var paths []string
	for path := range staged {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for i, path := range paths {
		if err = os.WriteFile(filepath.Join(temporary, fmt.Sprint(i)), staged[path], 0600); err != nil {
			return err
		}
	}
	if overwrite {
		if err = publishReplacement(base, temporary, paths, os.Rename); err != nil {
			keepTemporary = true
			return fmt.Errorf("%w; 发布恢复材料保留在 %s", err, temporary)
		}
		result.ArtifactPaths = paths
		return nil
	}
	var published []string
	for i, path := range paths {
		stage := filepath.Join(temporary, fmt.Sprint(i))
		err = os.Link(stage, path)
		if err != nil {
			for _, p := range published {
				_ = os.Remove(p)
			}
			return err
		}
		published = append(published, path)
	}
	result.ArtifactPaths = paths
	return nil
}

// Each replacement is atomic. Backups allow rollback if a later family member
// fails; callers only receive the new result after the whole family succeeds.
func publishReplacement(base, temporary string, paths []string, rename func(string, string) error) error {
	backups := map[string]string{}
	for _, path := range familyPaths(base) {
		for _, p := range []string{path, path + ".source.json"} {
			info, err := os.Lstat(p)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("拒绝覆盖非普通 OSM 文件: %s", p)
			}
			backup := filepath.Join(temporary, fmt.Sprintf("backup-%d", len(backups)))
			if err = os.Link(p, backup); err != nil {
				return err
			}
			backups[p] = backup
		}
	}
	var changed []string
	rollback := func(cause error) error {
		for i := len(changed) - 1; i >= 0; i-- {
			path := changed[i]
			var err error
			if backup, existed := backups[path]; existed {
				err = os.Rename(backup, path)
			} else {
				err = os.Remove(path)
			}
			if err != nil {
				cause = fmt.Errorf("%w; 回滚 %s 失败: %v", cause, path, err)
			}
		}
		return cause
	}
	written := map[string]bool{}
	for i, path := range paths {
		if err := rename(filepath.Join(temporary, fmt.Sprint(i)), path); err != nil {
			return rollback(err)
		}
		changed = append(changed, path)
		written[path] = true
	}
	for path := range backups {
		if !written[path] {
			if err := os.Remove(path); err != nil {
				return rollback(err)
			}
			changed = append(changed, path)
		}
	}
	return nil
}
