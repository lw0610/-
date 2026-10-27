package osmdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var errOSMTotalDeadline = errors.New("OSM 下载总时限已到")

func Handles(name string) bool {
	switch name {
	case "osm_check_environment", "osm_search", "osm_probe", "osm_get_objects", "osm_get_features":
		return true
	}
	return false
}
func stringArg(args map[string]any, name string) string {
	s, _ := args[name].(string)
	return strings.TrimSpace(s)
}
func stringList(v any) []string {
	switch raw := v.(type) {
	case []string:
		return raw
	case []any:
		out := []string{}
		for _, r := range raw {
			if s, ok := r.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.TrimSpace(raw) != "" {
			return []string{raw}
		}
	}
	return nil
}
func Invoke(ctx context.Context, name string, args map[string]any, options Options) (result *Result, err error) {
	if timeout, ok := args["timeout"].(float64); ok {
		options.TimeoutSeconds = int(timeout)
	}
	if timeout, ok := args["timeout"].(int); ok {
		options.TimeoutSeconds = timeout
	}
	c, err := newClient(options)
	if err != nil {
		return nil, err
	}
	if !Handles(name) {
		return nil, fmt.Errorf("未知 OSM 工具")
	}
	// One budget covers health checks, every extent, failover and object
	// fallback. A successful response does not reset the operation deadline.
	ctx, cancel := context.WithTimeoutCause(ctx, time.Duration(c.options.TotalTimeoutSeconds)*time.Second, errOSMTotalDeadline)
	defer cancel()
	if name == "osm_check_environment" {
		// An explicit check must observe a repaired service/proxy instead of
		// replaying the health result cached before the repair.
		c.invalidateHealth()
		_, health := c.rank(ctx)
		nctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		_, ne := c.nominatim(nctx, "Beijing")
		ok := false
		for _, h := range health {
			ok = ok || h.Available
		}
		data := map[string]any{"endpoints": health, "nominatim_available": ne == nil, "proxy_url": options.ProxyURL}
		if ne != nil {
			data["nominatim_error"] = ne.Error()
		}
		return &Result{Success: ok, Data: data, Info: "服务检查完成"}, nil
	}
	if !options.Enabled {
		return nil, fmt.Errorf("请先在设置 → 扩展服务启用 OSM")
	}
	switch name {
	case "osm_search":
		return c.search(ctx, args)
	case "osm_probe":
		return c.probe(ctx, args)
	}
	if !options.Authorized {
		return nil, fmt.Errorf("OSM 下载未获可信运行入口授权")
	}
	ctx, finish := beginProgress(ctx, name, stringArg(args, "output"))
	defer func() {
		if err != nil && result != nil {
			result.Success = false
		}
		if err != nil && errors.Is(context.Cause(ctx), errOSMTotalDeadline) && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errOSMTotalDeadline)) {
			err = fmt.Errorf("OSM 下载已达到总时限（最多 %d 秒）；可缩小范围或在设置中调整下载总时限: %w", c.options.TotalTimeoutSeconds, err)
		}
		finish(result, err)
	}()
	result = &Result{Success: true, DroppedFields: map[string]int{}}
	var elements []Element
	var acquisitionErr error
	tags := stringList(args["tags"])
	if len(tags) == 0 {
		tags = stringList(args["query"])
	}
	fields := stringList(args["fields"])
	mode := stringArg(args, "geometry")
	if mode != "" && mode != "original" && mode != "centroid" {
		return nil, fmt.Errorf("geometry须为original或centroid")
	}
	if name == "osm_get_objects" {
		ids := stringList(args["ids"])
		if len(ids) == 0 || len(ids) > 10 {
			return nil, fmt.Errorf("对象编号数量应为1至10")
		}
		refs := []Ref{}
		for _, id := range ids {
			r, e := ParseRef(id)
			if e != nil {
				return nil, e
			}
			refs = append(refs, r)
		}
		q := ObjectsQuery(refs, c.options.TimeoutSeconds)
		raw, node, e := c.query(ctx, q)
		acquisitionErr = e
		result.Queries = append(result.Queries, q)
		if node != "" {
			result.Nodes = append(result.Nodes, node)
		}
		available := map[string]Element{}
		if e == nil {
			for _, el := range raw.Elements {
				available[el.Type+fmt.Sprint(el.ID)] = el
			}
		}
		for _, ref := range refs {
			el, ok := available[ref.Type+fmt.Sprint(ref.ID)]
			if ok {
				_, e = GeometryOf(el)
			}
			if !ok || e != nil {
				el, e = c.full(ctx, ref)
				if e != nil {
					acquisitionErr = e
					result.Dropped = append(result.Dropped, ref.String()+": "+e.Error())
					continue
				}
			}
			elements = append(elements, el)
		}
	} else {
		extents := stringList(args["extents"])
		if len(extents) == 0 {
			extents = stringList(args["extent"])
		}
		if len(extents) == 0 || len(extents) > 12 {
			return nil, fmt.Errorf("每次范围数量须为1至12")
		}
		types, e := Types(stringList(args["types"]))
		if e != nil {
			return nil, e
		}
		// Validate every range before requesting any data. A typo in a later
		// range must not discard data already downloaded for an earlier one.
		prepared := make([]string, 0, len(extents))
		for _, raw := range extents {
			ext, e := ParseExtent(raw)
			if e != nil {
				return nil, e
			}
			q, e := BuildQuery(ext, tags, types, c.options.TimeoutSeconds, "out geom;")
			if e != nil {
				return nil, e
			}
			prepared = append(prepared, q)
		}
		result.Warnings = append(result.Warnings, "Overpass 仅按范围筛选完整对象，尚未执行几何裁剪；跨界线和关系成员可能超出范围，交付前须用原始边界精确裁剪。")
		for index, raw := range extents {
			q := prepared[index]
			result.Queries = append(result.Queries, q)
			queryCtx := withProgressMetadata(ctx, Progress{ExtentIndex: index + 1, ExtentCount: len(extents)})
			data, node, e := c.query(queryCtx, q)
			if e != nil {
				acquisitionErr = e
				result.FailedExtents = append(result.FailedExtents, raw+": "+e.Error())
				continue
			}
			result.Nodes = append(result.Nodes, node)
			elements = append(elements, data.Elements...)
		}
		args = copyArgs(args)
		args["extents"] = extents
		args["tags"] = tags
	}
	converted := Convert(elements, tags, fields)
	result.Dropped = append(result.Dropped, converted.Dropped...)
	result.DroppedFields = converted.DroppedFields
	if mode == "centroid" {
		for i := range converted.Features {
			converted.Features[i] = ToCentroid(converted.Features[i])
		}
	}
	result.Partial = len(result.Dropped) > 0 || len(result.FailedExtents) > 0 || ctx.Err() != nil
	result.FeatureCount = len(converted.Features)
	for _, e := range elements {
		if e.Tags["boundary"] == "administrative" {
			g, ge := GeometryOf(e)
			if ge == nil && intersectsChina(g) {
				result.Warnings = append(result.Warnings, "OSM 行政边界不代表我国标准地图，制图请使用内置行政区边界")
				break
			}
		}
	}
	result.Info = fmt.Sprintf("已获取 %d 个 OSM 要素", result.FeatureCount)
	if result.Partial {
		result.Info = "部分结果：" + result.Info
	}
	if len(converted.Features) > 0 {
		reportProgress(ctx, Progress{Phase: "saving", FeatureCount: result.FeatureCount})
		if err = writeOutputs(c.options, args, converted.Features, result); err != nil {
			return result, err
		}
	}
	if result.Partial && result.FeatureCount == 0 {
		result.Success = false
		if acquisitionErr != nil {
			return result, fmt.Errorf("OSM 未得到可用完整要素: %w", acquisitionErr)
		}
		return result, fmt.Errorf("OSM 未得到可用完整要素")
	}
	return result, nil
}
func copyArgs(args map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range args {
		out[k] = v
	}
	return out
}
func intersectsChina(g Geometry) bool {
	bounds := []float64{180, 90, -180, -90}
	var add func(any)
	add = func(v any) {
		switch p := v.(type) {
		case Point:
			if p[0] < bounds[0] {
				bounds[0] = p[0]
			}
			if p[1] < bounds[1] {
				bounds[1] = p[1]
			}
			if p[0] > bounds[2] {
				bounds[2] = p[0]
			}
			if p[1] > bounds[3] {
				bounds[3] = p[1]
			}
		case []Point:
			for _, q := range p {
				add(q)
			}
		case [][]Point:
			for _, q := range p {
				add(q)
			}
		case [][][]Point:
			for _, q := range p {
				add(q)
			}
		}
	}
	add(g.Coordinates)
	return bounds[0] <= 136 && bounds[2] >= 73 && bounds[1] <= 54 && bounds[3] >= 3
}
func (c *client) probe(ctx context.Context, args map[string]any) (*Result, error) {
	extents := stringList(args["extents"])
	if len(extents) == 0 || len(extents) > 12 {
		return nil, fmt.Errorf("统计范围数量须为1至12")
	}
	types, err := Types(stringList(args["types"]))
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	result := &Result{Success: true}
	for _, raw := range extents {
		ext, e := ParseExtent(raw)
		if e != nil {
			return nil, e
		}
		tags := stringList(args["tags"])
		mode := "out count;"
		key := stringArg(args, "key")
		if key != "" {
			tags = []string{key}
			mode = "out tags 30001;"
		}
		query, e := BuildQuery(ext, tags, types, c.options.TimeoutSeconds, mode)
		if e != nil {
			return nil, e
		}
		data, node, e := c.query(ctx, query)
		row := map[string]any{"extent": raw, "node": node}
		if e != nil {
			row["error"] = e.Error()
			result.Partial = true
		} else if key != "" {
			counts := map[string]int{}
			for _, element := range data.Elements {
				counts[element.Tags[key]]++
			}
			row["distribution"] = counts
			row["truncated"] = len(data.Elements) >= 30001
		} else if len(data.Elements) > 0 {
			counts := map[string]int{}
			for k, v := range data.Elements[0].Tags {
				counts[k], _ = strconv.Atoi(v)
			}
			row["counts"] = counts
		}
		rows = append(rows, row)
	}
	result.Data = rows
	return result, nil
}

type candidate struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Category   string      `json:"category,omitempty"`
	AdminLevel string      `json:"admin_level,omitempty"`
	Center     *Coordinate `json:"center,omitempty"`
	Sources    []string    `json:"sources"`
	BusNoise   bool        `json:"bus_noise"`
	Members    []Member    `json:"members,omitempty"`
}

func (c *client) nominatim(ctx context.Context, query string) ([]candidate, error) {
	u, err := url.Parse(c.options.NominatimURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "jsonv2")
	q.Set("limit", "40")
	q.Set("extratags", "1")
	u.RawQuery = q.Encode()
	raw, err := c.request(ctx, "GET", u.String(), "", true)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Type  string            `json:"osm_type"`
		ID    int64             `json:"osm_id"`
		Name  string            `json:"display_name"`
		Lat   string            `json:"lat"`
		Lon   string            `json:"lon"`
		Class string            `json:"class"`
		Kind  string            `json:"type"`
		Extra map[string]string `json:"extratags"`
	}
	if err = json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	var out []candidate
	for _, r := range rows {
		if r.ID <= 0 || (r.Type != "node" && r.Type != "way" && r.Type != "relation") {
			continue
		}
		lat, _ := strconv.ParseFloat(r.Lat, 64)
		lon, _ := strconv.ParseFloat(r.Lon, 64)
		out = append(out, candidate{ID: Ref{r.Type, r.ID}.String(), Name: r.Name, Category: r.Class + "=" + r.Kind, AdminLevel: r.Extra["admin_level"], Center: &Coordinate{lat, lon}, Sources: []string{"nominatim"}, BusNoise: r.Extra["route"] == "bus"})
	}
	return out, nil
}
func (c *client) search(ctx context.Context, args map[string]any) (*Result, error) {
	query := stringArg(args, "query")
	q, e := NameQuery(query, stringArg(args, "category"), c.options.TimeoutSeconds)
	if e != nil {
		return nil, e
	}
	rows, ne := c.nominatim(ctx, strings.TrimSpace(query+" "+stringArg(args, "context")))
	raw, _, oe := c.query(ctx, q)
	if oe == nil {
		if sites, _, siteErr := c.query(ctx, SiteQuery(query, c.options.TimeoutSeconds)); siteErr == nil {
			byRef := map[int64]bool{}
			for _, el := range raw.Elements {
				if el.Type == "relation" {
					byRef[el.ID] = true
				}
			}
			for _, el := range sites.Elements {
				if !byRef[el.ID] {
					raw.Elements = append(raw.Elements, el)
				} else {
					for i := range raw.Elements {
						if raw.Elements[i].Type == "relation" && raw.Elements[i].ID == el.ID {
							raw.Elements[i].Members = el.Members
						}
					}
				}
			}
		}
	}
	result := &Result{Success: true, Partial: ne != nil || oe != nil}
	byID := map[string]int{}
	for i, r := range rows {
		byID[r.ID] = i
	}
	if oe == nil {
		for _, el := range raw.Elements {
			if el.Type != "node" && el.Type != "way" && el.Type != "relation" {
				continue
			}
			id := Ref{el.Type, el.ID}.String()
			if i, ok := byID[id]; ok {
				rows[i].Sources = append(rows[i].Sources, "overpass")
				rows[i].Members = el.Members
				continue
			}
			p, _ := properties(el, nil, nil)
			cat, _ := p["category"].(string)
			center := el.Center
			if center == nil && el.Lat != nil && el.Lon != nil {
				center = &Coordinate{*el.Lat, *el.Lon}
			}
			rows = append(rows, candidate{ID: id, Name: el.Tags["name"], Category: cat, AdminLevel: el.Tags["admin_level"], Center: center, Sources: []string{"overpass"}, BusNoise: el.Tags["route"] == "bus", Members: el.Members})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return len(rows[i].Sources) > len(rows[j].Sources) })
	result.Data = rows
	if ne != nil {
		result.Warnings = append(result.Warnings, "Nominatim: "+ne.Error())
	}
	if oe != nil {
		result.Warnings = append(result.Warnings, "Overpass: "+oe.Error())
	}
	if ne != nil && oe != nil {
		return result, fmt.Errorf("OSM 两个搜索引擎均不可达")
	}
	return result, nil
}
