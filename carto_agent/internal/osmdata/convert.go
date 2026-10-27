package osmdata

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// Polygon classification table from osm2geojson-lite 2.0.1, MIT, Xihua Duan.
//
//go:embed polytags.json
var polygonTagsJSON []byte
var polygonRules map[string]struct{ Whitelist, Blacklist []string }

func init() { _ = json.Unmarshal(polygonTagsJSON, &polygonRules) }

type Coordinate struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}
type Member struct {
	Type     string       `json:"type"`
	Ref      int64        `json:"ref"`
	Role     string       `json:"role"`
	Geometry []Coordinate `json:"geometry"`
	Lat      float64      `json:"lat"`
	Lon      float64      `json:"lon"`
}
type Element struct {
	Type     string            `json:"type"`
	ID       int64             `json:"id"`
	Tags     map[string]string `json:"tags"`
	Lat      *float64          `json:"lat,omitempty"`
	Lon      *float64          `json:"lon,omitempty"`
	Center   *Coordinate       `json:"center,omitempty"`
	Nodes    []int64           `json:"nodes"`
	Geometry []Coordinate      `json:"geometry"`
	Members  []Member          `json:"members"`
}
type Response struct {
	Elements []Element `json:"elements"`
	Remark   string    `json:"remark"`
}
type Geometry struct {
	Type        string `json:"type"`
	Coordinates any    `json:"coordinates"`
}
type Feature struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Geometry   Geometry       `json:"geometry"`
}
type Converted struct {
	Features      []Feature
	Dropped       []string
	DroppedFields map[string]int
}

var classes = []string{"amenity", "boundary", "natural", "leisure", "landuse", "highway", "building", "railway", "shop", "tourism", "office", "craft", "waterway", "place", "public_transport"}

func properties(e Element, tags, fields []string) (map[string]any, []string) {
	p := map[string]any{"osm_id": e.ID, "osm_type": e.Type, "name": nil, "category": nil}
	if name := e.Tags["name"]; name != "" {
		p["name"] = name
	}
	derived := ""
	for _, k := range classes {
		if v, ok := e.Tags[k]; ok {
			derived = k
			p["category"] = k + "=" + v
			break
		}
	}
	keep := map[string]bool{}
	all := false
	for _, tag := range tags {
		keep[TagKey(tag)] = true
	}
	for _, field := range fields {
		if field == "*" {
			all = true
		} else {
			keep[field] = true
		}
	}
	var dropped []string
	for k, v := range e.Tags {
		if k == "name" {
			continue
		}
		if keep[k] {
			p[k] = v
		} else if k != derived && !all {
			dropped = append(dropped, k)
		}
	}
	if all {
		raw, _ := json.Marshal(e.Tags)
		p["osm_tags"] = string(raw)
	}
	return p, dropped
}
func isPolygon(tags map[string]string) bool {
	if tags["area"] == "no" {
		return false
	}
	for key, rule := range polygonRules {
		value, ok := tags[key]
		if !ok || value == "no" {
			continue
		}
		if rule.Whitelist != nil {
			for _, v := range rule.Whitelist {
				if v == value {
					return true
				}
			}
			continue
		}
		skip := false
		for _, v := range rule.Blacklist {
			if v == value {
				skip = true
			}
		}
		if !skip {
			return true
		}
	}
	return false
}
func points(coords []Coordinate) ([]Point, error) {
	p := make([]Point, len(coords))
	for i, c := range coords {
		p[i] = Point{c.Lon, c.Lat}
		if !validPoint(p[i]) {
			return nil, fmt.Errorf("无效坐标")
		}
	}
	return p, nil
}
func reverse(p []Point) []Point {
	out := make([]Point, len(p))
	for i := range p {
		out[i] = p[len(p)-1-i]
	}
	return out
}
func joinLines(lines [][]Point) [][]Point {
	remaining := append([][]Point(nil), lines...)
	var output [][]Point
	for len(remaining) > 0 {
		chain := append([]Point(nil), remaining[0]...)
		remaining = remaining[1:]
		for {
			found := false
			for i, line := range remaining {
				if len(chain) == 0 || len(line) == 0 {
					continue
				}
				a, z := chain[0], chain[len(chain)-1]
				b, y := line[0], line[len(line)-1]
				switch {
				case z == b:
					chain = append(chain, line[1:]...)
				case z == y:
					chain = append(chain, reverse(line)[1:]...)
				case a == y:
					chain = append(append([]Point(nil), line[:len(line)-1]...), chain...)
				case a == b:
					rev := reverse(line)
					chain = append(rev[:len(rev)-1], chain...)
				default:
					continue
				}
				remaining = append(remaining[:i], remaining[i+1:]...)
				found = true
				break
			}
			if !found {
				break
			}
		}
		output = append(output, chain)
	}
	return output
}
func containsPoint(ring []Point, p Point) bool {
	inside := false
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		a, b := ring[i], ring[j]
		if (a[1] > p[1]) != (b[1] > p[1]) && p[0] < (b[0]-a[0])*(p[1]-a[1])/(b[1]-a[1])+a[0] {
			inside = !inside
		}
	}
	return inside
}
func GeometryOf(e Element) (Geometry, error) {
	switch e.Type {
	case "node":
		if e.Lat != nil && e.Lon != nil && validPoint(Point{*e.Lon, *e.Lat}) {
			return Geometry{"Point", Point{*e.Lon, *e.Lat}}, nil
		}
	case "way":
		p, err := points(e.Geometry)
		if err != nil {
			return Geometry{}, err
		}
		if len(p) < 2 {
			break
		}
		if len(p) >= 4 && p[0] == p[len(p)-1] && isPolygon(e.Tags) {
			return Geometry{"Polygon", [][]Point{p}}, nil
		}
		return Geometry{"LineString", p}, nil
	case "relation":
		var outer, inner, lines [][]Point
		polygon := e.Tags["type"] == "multipolygon" || e.Tags["type"] == "boundary"
		for _, m := range e.Members {
			if m.Type == "relation" {
				return Geometry{}, fmt.Errorf("嵌套关系无法完整组装，拒绝残缺输出")
			}
			if m.Type != "way" {
				continue
			}
			p, err := points(m.Geometry)
			if err != nil || len(p) < 2 {
				return Geometry{}, fmt.Errorf("成员线缺少完整几何")
			}
			if !polygon {
				lines = append(lines, p)
			} else if m.Role == "inner" {
				inner = append(inner, p)
			} else if m.Role == "outer" || m.Role == "" {
				outer = append(outer, p)
			}
		}
		if !polygon {
			lines = joinLines(lines)
			if len(lines) == 1 {
				return Geometry{"LineString", lines[0]}, nil
			}
			if len(lines) > 1 {
				return Geometry{"MultiLineString", lines}, nil
			}
			break
		}
		outer = joinLines(outer)
		inner = joinLines(inner)
		if len(outer) == 0 {
			break
		}
		polys := make([][][]Point, len(outer))
		for i, ring := range outer {
			if len(ring) < 4 || ring[0] != ring[len(ring)-1] {
				return Geometry{}, fmt.Errorf("外环未闭合")
			}
			polys[i] = [][]Point{ring}
		}
		for _, ring := range inner {
			if len(ring) < 4 || ring[0] != ring[len(ring)-1] {
				return Geometry{}, fmt.Errorf("内环未闭合")
			}
			target := -1
			for i, poly := range polys {
				if containsPoint(poly[0], ring[0]) {
					if target < 0 || ringArea(poly[0]) < ringArea(polys[target][0]) {
						target = i
					}
				}
			}
			if target < 0 {
				return Geometry{}, fmt.Errorf("内环没有包含它的外环")
			}
			polys[target] = append(polys[target], ring)
		}
		if len(polys) == 1 {
			return Geometry{"Polygon", polys[0]}, nil
		}
		return Geometry{"MultiPolygon", polys}, nil
	}
	return Geometry{}, fmt.Errorf("无法组装 %s%d", e.Type, e.ID)
}
func Convert(elements []Element, tags, fields []string) Converted {
	c := Converted{DroppedFields: map[string]int{}}
	seen := map[string]bool{}
	for _, e := range elements {
		if e.ID <= 0 || len(e.Tags) == 0 || (e.Type != "node" && e.Type != "way" && e.Type != "relation") {
			continue
		}
		id := Ref{e.Type, e.ID}.String()
		if seen[id] {
			continue
		}
		seen[id] = true
		g, err := GeometryOf(e)
		if err != nil {
			c.Dropped = append(c.Dropped, id)
			continue
		}
		p, dropped := properties(e, tags, fields)
		for _, k := range dropped {
			c.DroppedFields[k]++
		}
		c.Features = append(c.Features, Feature{"Feature", p, g})
	}
	return c
}

// HydrateFull resolves a single target's node/member references in /full.json.
func HydrateFull(elements []Element, target Ref) (Element, error) {
	index := map[string]Element{}
	for _, e := range elements {
		index[e.Type+fmt.Sprint(e.ID)] = e
	}
	var resolve func(Ref, int) (Element, error)
	resolve = func(r Ref, depth int) (Element, error) {
		if depth > 8 {
			return Element{}, fmt.Errorf("关系嵌套过深")
		}
		e, ok := index[r.Type+fmt.Sprint(r.ID)]
		if !ok {
			return e, fmt.Errorf("对象缺失")
		}
		if e.Type == "way" {
			e.Geometry = nil
			for _, id := range e.Nodes {
				n, ok := index["node"+fmt.Sprint(id)]
				if !ok || n.Lat == nil || n.Lon == nil {
					return e, fmt.Errorf("原始节点缺失，拒绝残缺输出")
				}
				e.Geometry = append(e.Geometry, Coordinate{*n.Lat, *n.Lon})
			}
		}
		if e.Type == "relation" {
			for i, m := range e.Members {
				if m.Type != "way" {
					continue
				}
				way, err := resolve(Ref{m.Type, m.Ref}, depth+1)
				if err != nil {
					return e, err
				}
				e.Members[i].Geometry = way.Geometry
			}
		}
		return e, nil
	}
	return resolve(target, 0)
}
func Family(kind string) string {
	if strings.Contains(kind, "Point") {
		return "points"
	}
	if strings.Contains(kind, "LineString") {
		return "lines"
	}
	return "polygons"
}
