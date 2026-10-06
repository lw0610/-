package knowledge

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
)

// ReviewRenderingAdaptation records an automatic projection, never a semantic
// correction or a human edit. The source graph remains part of the signed audit.
type ReviewRenderingAdaptation struct {
	SourceRouteID   string `json:"source_route_id"`
	RenderedRouteID string `json:"rendered_route_id"`
	PathID          string `json:"path_id"`
	SourceRole      string `json:"source_role"`
	RenderedRole    string `json:"rendered_role"`
	Operation       string `json:"operation"`
}

// adaptAcceptedRoutes projects saved movement paths onto the six-table format.
// Independent mains never get joined. A secondary path uses only its explicit
// spatial attachment, and repeated adjacent locations are removed on this copy.
func adaptAcceptedRoutes(g Graph) error {
	if err := materializeRouteControls(g); err != nil {
		return err
	}
	geometry := drawingReferenceGraph(g)
	index, err := indexGraph(geometry)
	if err != nil {
		return err
	}
	normalizeRouteAttachments(g, index)
	reserved := map[string]bool{}
	for _, collection := range graphCollections {
		for _, raw := range array(g[collection.key]) {
			if row, ok := raw.(map[string]any); ok {
				reserved[stringValue(row[collection.id])] = true
			}
		}
	}
	for id := range index {
		reserved[id] = true
	}
	routes := []any{}
	for _, raw := range array(g["routes"]) {
		route := raw.(map[string]any)
		paths := array(route["paths"])
		if len(paths) == 0 {
			continue
		}
		for _, rawPath := range paths {
			path := rawPath.(map[string]any)
			role := stringValue(path["role"])
			if role != "main" && role != "branch" && role != "incoming" {
				return fmt.Errorf("路径 %s 的角色 %q 无法解释", path["path_id"], role)
			}
			path["nodes"] = deduplicateDrawingNodes(g, array(path["nodes"]))
		}
		// Preserve supported branch and multi-start encodings without flattening.
		if checkRouteGeometry(geometry, route, true) == nil {
			_ = normalizeTopology(route)
			delete(route, "style_ref")
			for _, rawPath := range paths {
				path := rawPath.(map[string]any)
				path["direction"] = map[string]string{"main": "forward", "branch": "away_from_attach", "incoming": "toward_attach"}[stringValue(path["role"])]
			}
			routes = append(routes, route)
			continue
		}
		for _, rawPath := range paths {
			path := rawPath.(map[string]any)
			role := stringValue(path["role"])
			nodes := array(path["nodes"])
			if role != "main" {
				attach := stringValue(path["attach_ref"])
				if attach != "" {
					if index[attach].kind != "spatial_entities" {
						return fmt.Errorf("路径 %s 的连接点 %s 不是明确地点", path["path_id"], attach)
					}
					junction := map[string]any{"ref_type": "spatial_entity", "ref_id": attach}
					if role == "branch" {
						nodes = append([]any{junction}, nodes...)
					} else {
						nodes = append(append([]any{}, nodes...), junction)
					}
				}
			}
			nodes = deduplicateDrawingNodes(g, nodes)
			if len(nodes) < 2 {
				continue
			}
			copyRoute := map[string]any{}
			for key, value := range route {
				copyRoute[key] = value
			}
			base := stringValue(route["route_id"]) + "__render_" + reviewHash([]byte(stringValue(route["route_id"]) + "\x00" + stringValue(path["path_id"])))[:16]
			id := base
			for suffix := 2; reserved[id]; suffix++ {
				id = fmt.Sprintf("%s_%d", base, suffix)
			}
			reserved[id] = true
			copyRoute["route_id"], copyRoute["topology"] = id, "standard"
			delete(copyRoute, "style_ref")
			path["role"], path["direction"], path["nodes"] = "main", "forward", nodes
			delete(path, "attach_ref")
			copyRoute["paths"] = []any{path}
			routes = append(routes, copyRoute)
		}
	}
	g["routes"] = routes
	return nil
}

func deduplicateDrawingNodes(g Graph, nodes []any) []any {
	out := make([]any, 0, len(nodes))
	previous := ""
	for _, node := range nodes {
		id := resolvedNode(g, node)
		if id == "" || id != previous {
			out = append(out, node)
		}
		previous = id
	}
	return out
}

func reviewRenderingAdaptations(source, prepared Graph) []ReviewRenderingAdaptation {
	type origin struct {
		routeID string
		path    map[string]any
	}
	origins := map[string]origin{}
	for _, raw := range array(source["routes"]) {
		route, _ := raw.(map[string]any)
		for _, rawPath := range array(route["paths"]) {
			path, _ := rawPath.(map[string]any)
			origins[stringValue(path["path_id"])] = origin{stringValue(route["route_id"]), path}
		}
	}
	out := []ReviewRenderingAdaptation{}
	seen := map[string]bool{}
	for _, raw := range array(prepared["routes"]) {
		route := raw.(map[string]any)
		for _, rawPath := range array(route["paths"]) {
			path := rawPath.(map[string]any)
			id := stringValue(path["path_id"])
			seen[id] = true
			before, exists := origins[id]
			if !exists {
				continue
			}
			routeID := stringValue(route["route_id"])
			operation := "normalize_path"
			if routeID != before.routeID {
				operation = "split_path"
			} else if reflect.DeepEqual(before.path, path) {
				continue
			}
			out = append(out, ReviewRenderingAdaptation{before.routeID, routeID, id, stringValue(before.path["role"]), stringValue(path["role"]), operation})
		}
	}
	for _, raw := range array(source["routes"]) {
		route, _ := raw.(map[string]any)
		for _, rawPath := range array(route["paths"]) {
			path, _ := rawPath.(map[string]any)
			id := stringValue(path["path_id"])
			if !seen[id] {
				out = append(out, ReviewRenderingAdaptation{stringValue(route["route_id"]), "", id, stringValue(path["role"]), "", "point_only"})
			}
		}
	}
	return out
}

// Human acceptance covers factual annotations. Only geometry references are
// needed to interpret movement; metadata remains unchanged in the signed graph.
func drawingReferenceGraph(g Graph) Graph {
	copy, _ := cloneGraph(g)
	delete(copy, "actors")
	delete(copy, "evidence")
	var strip func(any)
	strip = func(value any) {
		switch row := value.(type) {
		case map[string]any:
			delete(row, "actor_refs")
			delete(row, "evidence_refs")
			// Optional shape hints are handled individually on the drawing copy.
			// A broken hint must not invalidate the actual route references.
			delete(row, "spatial_constraints")
			for _, child := range row {
				strip(child)
			}
		case []any:
			for _, child := range row {
				strip(child)
			}
		}
	}
	for _, value := range copy {
		strip(value)
	}
	return copy
}

// An approach constraint keeps a one-place semantic path alive until coordinates
// are available. Its unnamed tail is created only on the drawing copy.
func hasApproachConstraint(g Graph, path map[string]any) bool {
	nodes := controlPathNodes(g, path)
	first := controlFirstPlace(g, nodes)
	if first == "" || path["role"] == "branch" {
		return false
	}
	for _, raw := range array(path["spatial_constraints"]) {
		c, _ := raw.(map[string]any)
		_, valid := compassBearings[stringValue(c["bearing"])]
		if c["kind"] == "approach_from" && valid && stringValue(c["target_ref"]) == first {
			return true
		}
	}
	return false
}

func controlFirstPlace(g Graph, nodes []any) string {
	controls := map[string]bool{}
	for _, raw := range array(g["spatial_entities"]) {
		e, _ := raw.(map[string]any)
		if e["entity_kind"] == "cartographic_control" {
			controls[stringValue(e["entity_id"])] = true
		}
	}
	for _, node := range nodes {
		id := resolvedNode(g, node)
		if id != "" && !controls[id] {
			return id
		}
	}
	return ""
}

func controlPathNodes(g Graph, path map[string]any) []any {
	nodes := array(path["nodes"])
	attach := stringValue(path["attach_ref"])
	junction := map[string]any{"ref_type": "spatial_entity", "ref_id": attach}
	if attach != "" && path["role"] == "branch" && (len(nodes) == 0 || resolvedNode(g, nodes[0]) != attach) {
		return append([]any{junction}, nodes...)
	}
	if attach != "" && path["role"] == "incoming" && (len(nodes) == 0 || resolvedNode(g, nodes[len(nodes)-1]) != attach) {
		return append(append([]any{}, nodes...), junction)
	}
	return nodes
}

var compassBearings = map[string]float64{"N": 0, "NE": 45, "E": 90, "SE": 135, "S": 180, "SW": 225, "W": 270, "NW": 315}

const routeControlGenerator = "route_spatial_constraint_v1"

// materializeRouteControls never infers a named historical place. Constraints
// and source-backed nodes remain in the accepted graph; the caller owns a copy.
// Repeating projection discards only controls generated by this function.
func materializeRouteControls(g Graph) error {
	generated := map[string]bool{}
	entities := []any{}
	for _, raw := range array(g["spatial_entities"]) {
		e, _ := raw.(map[string]any)
		meta, _ := e["geocoding"].(map[string]any)
		if e["entity_kind"] == "cartographic_control" && meta["generator"] == routeControlGenerator {
			generated[stringValue(e["entity_id"])] = true
			continue
		}
		entities = append(entities, raw)
	}
	// Do not change graphs without constraints or earlier generated controls.
	hasConstraints := false
	for _, rr := range array(g["routes"]) {
		route, _ := rr.(map[string]any)
		for _, rp := range array(route["paths"]) {
			p, _ := rp.(map[string]any)
			if value, exists := p["control_options"]; exists {
				options, _ := value.(map[string]any)
				clean := map[string]any{}
				for _, key := range []string{"display_length_m", "offset_m"} {
					if n := controlOption(options, key, 0); n > 0 {
						clean[key] = n
					}
				}
				if len(clean) > 0 {
					p["control_options"] = clean
				} else {
					delete(p, "control_options")
				}
			}
			if value, exists := p["spatial_constraints"]; exists {
				if constraints, ok := value.([]any); ok && len(constraints) > 0 {
					hasConstraints = true
				} else {
					delete(p, "spatial_constraints")
				}
			}
			if len(generated) > 0 {
				nodes := []any{}
				for _, node := range array(p["nodes"]) {
					if !generated[resolvedNode(g, node)] {
						nodes = append(nodes, node)
					}
				}
				p["nodes"] = nodes
			}
		}
	}
	if !hasConstraints && len(generated) == 0 {
		return nil
	}
	g["spatial_entities"] = entities
	if !hasConstraints {
		return nil
	}
	task, _ := g["task"].(map[string]any)
	if crs := stringValue(task["input_crs"]); crs != "" && crs != "EPSG:4326" && crs != "OGC:CRS84" {
		// The factual route already uses the project's CRS. Only this optional
		// geodesic projection requires WGS84; omit it without guessing a transform.
		for _, rr := range array(g["routes"]) {
			route, _ := rr.(map[string]any)
			for _, rp := range array(route["paths"]) {
				path, _ := rp.(map[string]any)
				delete(path, "spatial_constraints")
			}
		}
		return nil
	}
	index, err := indexGraph(g)
	if err != nil {
		return err
	}
	position := func(id string) (float64, float64, error) {
		e := index[id].data
		x, y, valid := controlCoordinate(e)
		if !valid {
			return 0, 0, fmt.Errorf("路线定位地点 %s 缺少有效经纬度", id)
		}
		return x, y, nil
	}
	// A robust median adapts the display tail to the route's point spacing;
	// unrelated very long jumps cannot force every schematic arrow to be huge.
	spans := []float64{}
	for _, rr := range array(g["routes"]) {
		for _, rp := range array(rr.(map[string]any)["paths"]) {
			nodes := []any{}
			for _, node := range controlPathNodes(g, rp.(map[string]any)) {
				if index[resolvedNode(g, node)].data["entity_kind"] != "cartographic_control" {
					nodes = append(nodes, node)
				}
			}
			for i := 1; i < len(nodes); i++ {
				a, b := index[resolvedNode(g, nodes[i-1])].data, index[resolvedNode(g, nodes[i])].data
				ax, ay, av := controlCoordinate(a)
				bx, by, bv := controlCoordinate(b)
				if av && bv && a["entity_kind"] != "cartographic_control" && b["entity_kind"] != "cartographic_control" {
					if d := controlDistance(ax, ay, bx, by); d > 100 {
						spans = append(spans, d)
					}
				}
			}
		}
	}
	defaultLength := 40000.0
	if len(spans) > 0 {
		sort.Float64s(spans)
		defaultLength = spans[len(spans)/2]
		if len(spans)%2 == 0 {
			defaultLength = (defaultLength + spans[len(spans)/2-1]) / 2
		}
		defaultLength = math.Max(20000, math.Min(80000, defaultLength))
	}
	for _, rr := range array(g["routes"]) {
		route := rr.(map[string]any)
		for _, rp := range array(route["paths"]) {
			path := rp.(map[string]any)
			nodes := array(path["nodes"])
			// Secondary paths omit their attachment in the stored node list.
			// Include it while interpreting the interval, then let the existing
			// attachment normalizer restore the plugin's compact encoding.
			if len(array(path["spatial_constraints"])) > 0 {
				nodes = controlPathNodes(g, path)
			}
			options, _ := path["control_options"].(map[string]any)
			insertions := map[int][]any{}
			accepted := []any{}
			for _, raw := range array(path["spatial_constraints"]) {
				c := drawingSpatialConstraint(raw, index)
				if c == nil {
					continue
				}
				bearing := compassBearings[stringValue(c["bearing"])]
				x, y, insertAt, distance, reason := 0.0, 0.0, 0, 0.0, ""
				switch c["kind"] {
				case "approach_from":
					if path["role"] == "branch" {
						continue
					}
					if len(nodes) == 0 || stringValue(c["target_ref"]) != controlFirstPlace(g, nodes) || len(insertions[0]) > 0 {
						continue
					}
					tx, ty, err := position(stringValue(c["target_ref"]))
					if err != nil {
						continue
					}
					distance = controlOption(options, "display_length_m", defaultLength)
					x, y = controlDestination(tx, ty, bearing, distance)
					reason = fmt.Sprintf("%s 来向的匿名示意箭尾；距目标 %.0f 米为制图长度，不是历史起点或行军距离。默认取实际相邻地点距离中位数并限制为 20–80 公里，无可用线段时取 40 公里，可在路线中覆盖。", c["bearing"], distance)
				case "via_relative":
					matches := 0
					occurrence := controlOption(c, "occurrence", 1)
					previous := ""
					found := false
					for i, node := range nodes {
						id := resolvedNode(g, node)
						if index[id].data["entity_kind"] == "cartographic_control" {
							continue
						}
						if previous == stringValue(c["after_ref"]) && id == stringValue(c["before_ref"]) {
							matches++
							if float64(matches) == occurrence {
								insertAt, found = i, true
							}
						}
						previous = id
					}
					if !found {
						continue
					}
					ref, _ := c["reference"].(map[string]any)
					rx, ry, valid := controlCoordinate(ref)
					if !valid {
						continue
					}
					ax, ay, err := position(stringValue(c["after_ref"]))
					if err != nil {
						continue
					}
					bx, by, err := position(stringValue(c["before_ref"]))
					if err != nil {
						continue
					}
					span := math.Min(controlDistance(ax, ay, bx, by), math.Min(controlDistance(ax, ay, rx, ry), controlDistance(bx, by, rx, ry)))
					ratio := 0.08
					if c["relation"] == "outside" {
						ratio = 0.16
					} else if c["relation"] != "within" {
						continue
					}
					distance = controlOption(options, "offset_m", math.Max(3000, math.Min(20000, span*ratio)))
					x, y = controlDestination(rx, ry, bearing, distance)
					scope := "无行政区界，按定位参照与相邻路线点计算方位示意，不代表精确边界内外"
					if xx, yy, used := controlFromBounds(ref, bearing, distance, c["relation"] == "within", options["offset_m"] != nil); used {
						x, y, scope = xx, yy, "采用定位服务提供的范围框作区域方位示意，范围框不等同于行政边界"
						scope += "；区域内部默认取中心至边框的 60%，手动偏移最多取 90%"
					}
					reason = fmt.Sprintf("%s 的 %s 方位（%s）路线控制；偏移参数 %.0f 米；%s。不是独立地点，不显示地名或点符号。", ref["name"], c["bearing"], c["relation"], distance, scope)
				default:
					continue
				}
				accepted = append(accepted, c)
				ci := len(accepted) - 1
				base := "control_" + reviewHash([]byte(fmt.Sprintf("%s\x00%d", path["path_id"], ci)))[:20]
				id := base
				for suffix := 2; index[id].kind != ""; suffix++ {
					id = fmt.Sprintf("%s_%d", base, suffix)
				}
				entity := map[string]any{"entity_id": id, "name": id, "entity_kind": "cartographic_control", "factual": false, "map_role": "control", "lon": x, "lat": y, "coordinate_source": "derived_control", "generation_reason": reason, "evidence_refs": []any{}, "geocoding": map[string]any{"generator": routeControlGenerator, "path_id": path["path_id"], "constraint_index": ci, "bearing_deg": bearing, "display_parameter_m": distance}}
				g["spatial_entities"] = append(array(g["spatial_entities"]), entity)
				index[id] = graphObject{kind: "spatial_entities", data: entity}
				insertions[insertAt] = append(insertions[insertAt], map[string]any{"ref_type": "spatial_entity", "ref_id": id})
			}
			if len(accepted) > 0 {
				path["spatial_constraints"] = accepted
			} else {
				delete(path, "spatial_constraints")
			}
			if len(insertions) > 0 {
				out := []any{}
				for i, node := range nodes {
					out = append(out, insertions[i]...)
					out = append(out, node)
				}
				path["nodes"] = out
			}
		}
	}
	return nil
}

// drawingSpatialConstraint retains only an interpretable optional shape hint.
// It never fills a missing direction, region relation, or location. Metadata is
// projected onto the drawing contract; the review's original hint is untouched.
func drawingSpatialConstraint(raw any, index map[string]graphObject) map[string]any {
	c, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	bearing := stringValue(c["bearing"])
	if _, valid := compassBearings[bearing]; !valid {
		return nil
	}
	out := map[string]any{"kind": c["kind"], "bearing": bearing}
	place := func(key string) bool {
		id := stringValue(c[key])
		if id == "" || index[id].kind != "spatial_entities" || index[id].data["entity_kind"] == "cartographic_control" {
			return false
		}
		out[key] = id
		return true
	}
	switch c["kind"] {
	case "approach_from":
		if !place("target_ref") {
			return nil
		}
	case "via_relative":
		if !place("after_ref") || !place("before_ref") || (c["relation"] != "within" && c["relation"] != "outside") {
			return nil
		}
		out["relation"] = c["relation"]
		if value, exists := c["occurrence"]; exists {
			n, ok := schemaNumber(value)
			if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < 1 || n != math.Trunc(n) {
				return nil
			}
			out["occurrence"] = n
		}
		ref, _ := c["reference"].(map[string]any)
		x, y, valid := controlCoordinate(ref)
		name := strings.TrimSpace(stringValue(ref["name"]))
		if name == "" || !valid {
			return nil
		}
		reference := map[string]any{"name": name, "lon": x, "lat": y}
		switch ref["coordinate_source"] {
		case "source_text", "llm_estimated", "manual", "gazetteer":
			reference["coordinate_source"] = ref["coordinate_source"]
		}
		if n, ok := schemaNumber(ref["coordinate_confidence"]); ok && n >= 0 && n <= 1 {
			reference["coordinate_confidence"] = n
		}
		if meta, ok := ref["geocoding"].(map[string]any); ok {
			reference["geocoding"] = meta
		}
		out["reference"] = reference
	default:
		return nil
	}
	if refs, ok := c["evidence_refs"].([]any); ok {
		kept := []any{}
		for _, rawRef := range refs {
			if id, ok := rawRef.(string); ok && index[id].kind == "evidence" {
				kept = append(kept, id)
			}
		}
		out["evidence_refs"] = kept
	}
	return out
}

func controlCoordinate(e map[string]any) (float64, float64, bool) {
	x, a := schemaNumber(e["lon"])
	y, b := schemaNumber(e["lat"])
	return x, y, a && b && !math.IsNaN(x) && !math.IsNaN(y) && !math.IsInf(x, 0) && !math.IsInf(y, 0) && math.Abs(x) <= 180 && math.Abs(y) <= 90
}
func controlOption(options map[string]any, key string, fallback float64) float64 {
	if n, ok := schemaNumber(options[key]); ok && n > 0 && !math.IsInf(n, 0) && !math.IsNaN(n) {
		return n
	}
	return fallback
}

// Spherical geodesics in metres are sufficient for illustrative, non-factual
// controls. Real gazetteer/source coordinates are never adjusted by this rule.
func controlDistance(x1, y1, x2, y2 float64) float64 {
	const rad = math.Pi / 180
	a := math.Pow(math.Sin((y2-y1)*rad/2), 2) + math.Cos(y1*rad)*math.Cos(y2*rad)*math.Pow(math.Sin((x2-x1)*rad/2), 2)
	return 6371008.8 * 2 * math.Asin(math.Sqrt(math.Min(1, math.Max(0, a))))
}
func controlDestination(x, y, bearing, distance float64) (float64, float64) {
	const rad = math.Pi / 180
	lat, lon, angle, d := y*rad, x*rad, bearing*rad, distance/6371008.8
	lat2 := math.Asin(math.Sin(lat)*math.Cos(d) + math.Cos(lat)*math.Sin(d)*math.Cos(angle))
	lon2 := lon + math.Atan2(math.Sin(angle)*math.Sin(d)*math.Cos(lat), math.Cos(d)-math.Sin(lat)*math.Sin(lat2))
	return math.Mod(lon2/rad+540, 360) - 180, lat2 / rad
}
func controlFromBounds(ref map[string]any, bearing, offset float64, within, override bool) (float64, float64, bool) {
	meta, _ := ref["geocoding"].(map[string]any)
	bounds := array(meta["bounds"])
	if len(bounds) != 4 {
		return 0, 0, false
	}
	v := [4]float64{}
	for i := range v {
		n, ok := schemaNumber(bounds[i])
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, 0, false
		}
		v[i] = n
	}
	if v[0] >= v[2] || v[1] >= v[3] || v[0] < -180 || v[2] > 180 || v[1] <= -90 || v[3] >= 90 {
		return 0, 0, false
	}
	cx, cy := (v[0]+v[2])/2, (v[1]+v[3])/2
	dx, dy := math.Sin(bearing*math.Pi/180), math.Cos(bearing*math.Pi/180)
	east := controlDistance(cx, cy, v[2], cy)
	north := controlDistance(cx, cy, cx, v[3])
	edge := math.Inf(1)
	if math.Abs(dx) > 1e-8 {
		edge = math.Min(edge, east/math.Abs(dx))
	}
	if math.Abs(dy) > 1e-8 {
		edge = math.Min(edge, north/math.Abs(dy))
	}
	distance := edge + offset
	if within {
		distance = edge * 0.6
		if override {
			distance = math.Min(offset, edge*0.9)
		}
	}
	x, y := controlDestination(cx, cy, bearing, distance)
	return x, y, true
}
