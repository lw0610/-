package acquire

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/autocarto/carto_agent/internal/config"
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

type PlanningIssue struct {
	Code       string              `json:"code"`
	Input      string              `json:"input,omitempty"`
	Item       string              `json:"item,omitempty"`
	Message    string              `json:"message"`
	Candidates []datasource.Region `json:"candidates,omitempty"`
}

// Plan performs local analysis only. resolver, when supplied, must be a readonly
// local metadata lookup (e.g. a bound QGIS instance), never geocoding or a model.
func Plan(ctx context.Context, plan *flow.FlowPlan, inputs map[string]any, env datasource.PlanningEnvironment, resolver ExtentResolver) (flow.AcquisitionPlan, []PlanningIssue) {
	env = withStoreLookup(ctx, env)
	result := flow.AcquisitionPlan{Items: []flow.AcquisitionItem{}}
	issues := []PlanningIssue{}
	if plan == nil || plan.Acquisition == nil {
		return result, issues
	}
	for _, original := range plan.Acquisition.Items {
		if err := ctx.Err(); err != nil {
			issues = append(issues, PlanningIssue{Code: "FLOW_ACQUIRE_CANCELLED", Message: err.Error()})
			break
		}
		raw, _ := json.Marshal(original)
		var item flow.AcquisitionItem
		_ = json.Unmarshal(raw, &item)
		mode, folder, marker := InputMode(inputs[item.Input])
		item.Mode = mode
		if mode == "user" && item.InputType != "folder" {
			item.UserPath = folder
		}
		if mode == "fill" && folder != "" {
			path := item.Filename
			if !filepath.IsAbs(path) {
				path = filepath.Join(folder, filepath.FromSlash(path))
			}
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				item.Mode = "user"
				item.UserPath = path
			}
		}
		if files, ok := marker["files"].(map[string]any); ok {
			if path, ok := files[item.Entry].(string); ok && path != "" {
				item.Mode = "user"
				item.UserPath = path
			}
		}
		if files, ok := marker["files"].(map[string]string); ok {
			if path := files[item.Entry]; path != "" {
				item.Mode = "user"
				item.UserPath = path
			}
		}
		acquiring := item.Mode == "auto" || item.Mode == "fill"
		if acquiring {
			for key, value := range item.Params {
				if strings.HasPrefix(value, "「") && strings.HasSuffix(value, "」") {
					input := strings.Trim(value, "「」")
					resolved, ok := inputs[input]
					if !ok || resolved == nil || strings.TrimSpace(fmt.Sprint(resolved)) == "" {
						issues = append(issues, PlanningIssue{Code: "FLOW_INPUT_REQUIRED_FOR_ACQUIRE", Input: input, Item: item.Key, Message: "自动获取 " + item.Requirement + " 需要填写" + input})
					} else {
						item.Params[key] = fmt.Sprint(resolved)
					}
				}
			}
		}
		extent, err := flow.RequirementExtent(item.ExtentExpression, plan, env.Catalog)
		if err == nil && extent != nil && extent.Pending {
			value := inputs[extent.Source]
			if value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
				if acquiring && extent.DependencyStep == "" {
					issues = append(issues, PlanningIssue{Code: "FLOW_INPUT_REQUIRED_FOR_ACQUIRE", Input: extent.Source, Item: item.Key, Message: "自动获取 " + item.Requirement + " 需要填写" + extent.Source})
				}
			} else if path, ok := value.(string); ok {
				var resolved *flow.ExtentPlan
				resolved, err = resolveExtent(ctx, path, env.Catalog, resolver)
				if err == nil {
					resolved.Source = extent.Source
					resolved.ExpandPercent = extent.ExpandPercent
					extent = resolved
				}
			} else {
				err = fmt.Errorf("范围输入需要区域名、代码、边界文件或图层")
			}
		}
		if err == nil && extent != nil && !extent.Pending {
			err = prepareExtent(extent)
			if err != nil {
				extent = nil // Never publish or cache a non-finite/invalid extent.
			}
		}
		if err != nil && acquiring {
			issue := PlanningIssue{Code: "FLOW_REQ_EXTENT_INVALID", Input: item.Input, Item: item.Key, Message: err.Error()}
			var ambiguous *datasource.RegionAmbiguity
			if errors.As(err, &ambiguous) {
				issue.Code = "FLOW_ACQUIRE_REGION_AMBIGUOUS"
				issue.Candidates = ambiguous.Candidates
			}
			issues = append(issues, issue)
		}
		item.Extent = extent
		item.Scale, err = flow.ParseScale(item.ScaleExpression)
		if err != nil && acquiring {
			issues = append(issues, PlanningIssue{Code: "FLOW_REQ_PARAM_INVALID", Input: item.Input, Item: item.Key, Message: err.Error()})
		}
		if item.Scale == 0 {
			item.Scale = EstimateScale(extent, inputs)
		}
		if env.Catalog != nil && item.Acquirable && acquiring {
			if _, params, err := env.Catalog.ResolveAcquisitionCategory(item.Category, item.Params); err != nil {
				issues = append(issues, PlanningIssue{Code: "FLOW_REQ_PARAM_INVALID", Input: item.Input, Item: item.Key, Message: err.Error()})
			} else {
				item.Params = params
			}
		}
		item = env.Rank(item)
		source, _ := marker["source"].(string)
		if sources, ok := marker["sources"].(map[string]any); ok {
			if s, ok := sources[item.Entry].(string); ok {
				source = s
			}
		}
		if sources, ok := marker["sources"].(map[string]string); ok && sources[item.Entry] != "" {
			source = sources[item.Entry]
		}
		if source != "" && acquiring {
			choices := append([]flow.SourceChoice{}, item.Alternatives...)
			if item.Chosen != nil {
				choices = append([]flow.SourceChoice{*item.Chosen}, choices...)
			}
			found := false
			for i, choice := range choices {
				if choice.SourceID == source {
					item.Chosen = &choice
					item.Alternatives = append(append([]flow.SourceChoice{}, choices[:i]...), choices[i+1:]...)
					found = true
					break
				}
			}
			if !found {
				issues = append(issues, PlanningIssue{Code: "FLOW_ACQUIRE_SOURCE_INVALID", Input: item.Input, Item: item.Key, Message: "选择的来源当前不可用于 " + item.Key})
			}
		}
		if acquiring && !item.Available && item.Required {
			issues = append(issues, PlanningIssue{Code: "FLOW_ACQUIRE_UNAVAILABLE", Input: item.Input, Item: item.Key, Message: item.Key + "：" + strings.Join(item.Reasons, "；")})
		}
		result.Items = append(result.Items, item)
	}
	return result, issues
}
func InputMode(value any) (mode, path string, marker map[string]any) {
	marker, _ = value.(map[string]any)
	if marker == nil {
		marker = map[string]any{}
	}
	mode = "user"
	path, _ = value.(string)
	if flag, _ := marker["$acquire"].(bool); flag {
		mode = "auto"
	}
	if flag, _ := marker["$fill"].(bool); flag {
		mode = "fill"
		path, _ = marker["path"].(string)
	}
	if value == nil || value == "" {
		mode = "none"
	}
	return
}
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

// ResolveRuntimeExtent resolves a dependency only after its producing step finishes.
func ResolveRuntimeExtent(ctx context.Context, item flow.AcquisitionItem, value string, inputs map[string]any, env datasource.PlanningEnvironment, resolver ExtentResolver) (flow.AcquisitionItem, error) {
	extent, err := resolveExtent(ctx, value, env.Catalog, resolver)
	if err != nil {
		return item, err
	}
	if item.Extent != nil {
		extent.ExpandPercent = item.Extent.ExpandPercent
		extent.Source = item.Extent.Source
	}
	if err := prepareExtent(extent); err != nil {
		return item, err
	}
	item.Extent = extent
	if item.ScaleExpression == "" || item.ScaleExpression == "自动" {
		item.Scale = EstimateScale(extent, inputs)
	}
	return withStoreLookup(ctx, env).Rank(item), nil
}

func withStoreLookup(ctx context.Context, env datasource.PlanningEnvironment) datasource.PlanningEnvironment {
	if env.CacheLookup != nil || env.Catalog == nil {
		return env
	}
	root, err := config.ResolveAutoCartoHome()
	if err != nil {
		return env
	}
	store := NewStore(filepath.Join(root, "data", "acquired"), 0)
	env.CacheLookup = func(item datasource.AcquisitionItem, choice datasource.SourceChoice) (string, bool) {
		source := env.Catalog.Source(choice.SourceID)
		if source == nil {
			return "", false
		}
		spec, err := cacheSpecification(env.Catalog, item, choice, *source)
		if err != nil {
			return "", false
		}
		_, ok, err := store.Lookup(ctx, spec.Key())
		return spec.Key(), ok && err == nil
	}
	return env
}
