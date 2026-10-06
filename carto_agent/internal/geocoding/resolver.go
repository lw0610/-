package geocoding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/model"
)

// ResolutionTimeout bounds the complete plan, provider search and estimation run.
const ResolutionTimeout = 240 * time.Second

// Resolver is safe to reuse between requests. It does not retain keys in
// results, and does not maintain a global cache that could confuse homonyms.
type Resolver struct {
	Model model.BaseChatModel
	// ModelOptions apply only to geocoding calls, never to the shared model's
	// global configuration. In particular Qwen needs an explicit thinking flag.
	ModelOptions []model.Option
	Key          string
	Endpoint     string
	Client       *http.Client
}

type placeWork struct {
	entity      map[string]any
	meta        map[string]any
	plan        queryPlan
	context     string
	neighbors   []map[string]any
	neighborIDs []string
	candidates  []Candidate
	done        bool
}

func (w *placeWork) id() string   { return str(w.entity["entity_id"]) }
func (w *placeWork) name() string { return str(w.entity["name"]) }
func (w *placeWork) modelInput(withCandidates bool) map[string]any {
	m := map[string]any{"entity_id": w.id(), "name": w.name(), "entity_kind": w.entity["entity_kind"], "description": w.entity["description"], "context": w.context, "route_neighbors": w.neighbors}
	if w.plan.ModernName != "" {
		m["modern_location"] = w.plan
	}
	if withCandidates {
		m["candidates"] = w.candidates
		// Avoid repeating candidates, metadata and complete queries inside the
		// same prompt; only the provider outcome is relevant to estimation.
		if warning := w.meta["search_warning"]; warning != nil {
			m["search_outcome"] = warning
		}
		if references := w.meta["reference_candidates"]; references != nil {
			m["nearby_reference_candidates"] = references
		}
	}
	return m
}
func (w *placeWork) selectCandidate(c Candidate, method, reason string) {
	w.entity["lon"], w.entity["lat"], w.entity["coordinate_source"] = c.Lon, c.Lat, "gazetteer"
	delete(w.entity, "coordinate_confidence")
	if reason == "" {
		reason = "依据行政位置与路线上下文选择天地图候选"
	}
	w.meta["status"], w.meta["selection_method"], w.meta["reason"], w.meta["selected"] = "resolved", method, reason, c
	w.done = true
}

// Resolve commits only when every actual entity is located. Optional route
// references are best effort: one missing shape anchor cannot discard located
// places. References never become factual spatial entities or route nodes.
func (r *Resolver) Resolve(ctx context.Context, graph map[string]any, source string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, ResolutionTimeout)
	defer cancel()
	original := entityMaps(graph["spatial_entities"])
	data, err := json.Marshal(original)
	if err != nil {
		return errors.New("无法复制待定位地点数据")
	}
	var copies []map[string]any
	if json.Unmarshal(data, &copies) != nil {
		return errors.New("无法读取待定位地点数据")
	}
	// Bind references to transient lookup records. An unambiguous actual place
	// can supply the same coordinates without another provider/model request.
	type referenceBinding struct {
		destination map[string]any
		lookup      map[string]any
		reusedID    string
	}
	var references []referenceBinding
	neighborIDs := map[string][]string{}
	optionalLookups := map[string]bool{}
	byName := map[string][]map[string]any{}
	usedIDs := map[string]bool{}
	for _, entity := range copies {
		usedIDs[str(entity["entity_id"])] = true
		if str(entity["entity_kind"]) != "cartographic_control" {
			byName[str(entity["name"])] = append(byName[str(entity["name"])], entity)
		}
	}
	for _, route := range entityMaps(graph["routes"]) {
		for _, path := range entityMaps(route["paths"]) {
			for _, constraint := range entityMaps(path["spatial_constraints"]) {
				if str(constraint["kind"]) != "via_relative" {
					continue
				}
				reference, ok := constraint["reference"].(map[string]any)
				if !ok || strings.TrimSpace(str(reference["name"])) == "" {
					continue
				}
				if trustedCoordinates(reference) {
					continue
				}
				name := str(reference["name"])
				if matches := byName[name]; len(matches) == 1 {
					references = append(references, referenceBinding{reference, matches[0], str(matches[0]["entity_id"])})
					continue
				}
				id := fmt.Sprintf("__route_reference_%d", len(references)+1)
				for usedIDs[id] {
					id += "_"
				}
				usedIDs[id] = true
				lookup := map[string]any{"entity_id": id, "name": name, "entity_kind": "place", "description": "路线定位参照：只定位这个真实地名本身，不计算其东部、以东等相对位置；不是途经地点"}
				copies = append(copies, lookup)
				optionalLookups[id] = true
				neighborIDs[id] = []string{str(constraint["after_ref"]), str(constraint["before_ref"])}
				references = append(references, referenceBinding{reference, lookup, ""})
			}
		}
	}
	var pending []*placeWork
	for _, entity := range copies {
		if trustedCoordinates(entity) {
			continue
		}
		if str(entity["name"]) == "" || str(entity["entity_id"]) == "" {
			return errors.New("地点缺少名称或标识，无法自动定位")
		}
		for _, key := range []string{"lon", "lat", "coordinate_source", "coordinate_confidence"} {
			delete(entity, key)
		}
		meta := metadata(str(entity["name"]))
		entity["geocoding"] = meta
		pending = append(pending, &placeWork{entity: entity, meta: meta, context: contextFor(source, str(entity["name"])), candidates: []Candidate{}, neighborIDs: neighborIDs[str(entity["entity_id"])]})
	}
	if r.Model == nil {
		for _, w := range pending {
			if !optionalLookups[w.id()] {
				return errors.New("坐标自动定位所需模型未配置；抽取结果未修改")
			}
		}
	}
	if len(pending) > 0 && r.Model != nil {
		refreshNeighbors(pending, copies, graph)
		planCtx, cancelPlan := context.WithTimeout(ctx, planningBudget)
		r.plan(planCtx, pending)
		cancelPlan()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		searchCtx, cancelSearch := context.WithTimeout(ctx, searchBudget)
		r.searchPlaces(searchCtx, pending)
		cancelSearch()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		refreshNeighbors(pending, copies, graph)
		var choosing []*placeWork
		for _, w := range pending {
			if !w.done && len(w.candidates) > 0 && pointEntity(w.entity) {
				choosing = append(choosing, w)
			}
		}
		selectionCtx, cancelSelection := context.WithTimeout(ctx, selectionBudget)
		r.choose(selectionCtx, choosing)
		cancelSelection()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		refreshNeighbors(pending, copies, graph)
		var estimating []*placeWork
		for _, w := range pending {
			if !w.done {
				estimating = append(estimating, w)
			}
		}
		r.estimate(ctx, estimating)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var missing []string
		for _, w := range pending {
			if (!w.done || !trustedCoordinates(w.entity)) && !optionalLookups[w.id()] {
				missing = append(missing, w.name())
			}
		}
		if len(missing) > 0 {
			names := strings.Join(missing[:min(5, len(missing))], "、")
			return fmt.Errorf("自动定位服务未能为 %d 个地点返回有效坐标（%s）；抽取结果未修改，请重试", len(missing), names)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Commit real coordinates together; optional reference failures keep a
	// nonblocking lookup record for the drawing copy to skip.
	fields := []string{"lon", "lat", "coordinate_source", "coordinate_confidence", "geocoding", "generation_reason"}
	copyFields := func(destination, lookup map[string]any) {
		for _, key := range fields {
			if value, ok := lookup[key]; ok {
				destination[key] = value
			} else {
				delete(destination, key)
			}
		}
	}
	for i, entity := range original {
		if !trustedCoordinates(entity) {
			copyFields(entity, copies[i])
		}
	}
	for _, binding := range references {
		copyFields(binding.destination, binding.lookup)
		delete(binding.destination, "generation_reason") // references are real places, never generated controls
		meta := map[string]any{}
		if existing, ok := binding.destination["geocoding"].(map[string]any); ok {
			for key, value := range existing {
				meta[key] = value
			}
		}
		meta["usage"] = "route_reference"
		if !trustedCoordinates(binding.lookup) {
			for _, key := range []string{"lon", "lat", "coordinate_source", "coordinate_confidence"} {
				delete(binding.destination, key)
			}
			meta["status"] = "unresolved"
			meta["reason"] = "定位参照未获得有效坐标，制图时仅忽略该方位约束，保留实际地点和路线。"
		}
		if binding.reusedID != "" {
			meta["reused_entity_id"] = binding.reusedID
		}
		binding.destination["geocoding"] = meta
	}
	return nil
}

type cachedSearch struct {
	done   chan struct{}
	result searchResult
	err    error
}

func (r *Resolver) searchPlaces(ctx context.Context, work []*placeWork) {
	cache := map[string]*cachedSearch{}
	var mu sync.Mutex
	sem := make(chan struct{}, 4)
	lookup := func(query, admin string) (searchResult, error) {
		key := query + "\x00" + admin
		mu.Lock()
		entry, exists := cache[key]
		if !exists {
			entry = &cachedSearch{done: make(chan struct{})}
			cache[key] = entry
		}
		mu.Unlock()
		if exists {
			select {
			case <-entry.done:
				return entry.result, entry.err
			case <-ctx.Done():
				return searchResult{}, ctx.Err()
			}
		}
		select {
		case sem <- struct{}{}:
			entry.result, entry.err = r.throttledSearch(ctx, query, admin)
			<-sem
		case <-ctx.Done():
			entry.err = ctx.Err()
		}
		close(entry.done)
		return entry.result, entry.err
	}
	var wg sync.WaitGroup
	for _, w := range work {
		wg.Add(1)
		go func(w *placeWork) {
			defer wg.Done()
			if strings.TrimSpace(r.Key) == "" {
				w.meta["search_warning"] = "未配置天地图服务端 Key，使用模型估算"
				return
			}
			p := w.plan
			if p.AdministrativeHint == "" && p.QualifiedQuery == p.ModernName {
				w.meta["search_warning"] = "名称分析未给出有效行政范围，避免全国同名误匹配，转入上下文估算"
				return
			}
			result, err := lookup(p.QualifiedQuery, p.AdministrativeHint)
			w.meta["queries"] = []string{p.QualifiedQuery}
			if err != nil && !retryableSearchError(err) {
				w.meta["search_warning"] = err.Error()
				return
			}
			w.candidates = coherentCandidates(result.Candidates, p)
			if len(w.candidates) == 0 && p.QualifiedQuery != p.ModernName && p.AdministrativeHint != "" {
				retry, retryErr := lookup(p.ModernName, p.AdministrativeHint)
				w.meta["queries"] = []string{p.QualifiedQuery, p.ModernName}
				if retryErr != nil {
					w.meta["search_warning"] = retryErr.Error()
				} else {
					result = retry
					w.candidates = coherentCandidates(retry.Candidates, p)
				}
			}
			if err != nil && len(w.candidates) == 0 && w.meta["search_warning"] == nil {
				w.meta["search_warning"] = err.Error()
			}
			w.meta["candidates"] = w.candidates
			var referenceCandidates []Candidate
			for _, c := range result.Candidates {
				if upperAdministrativeCenter(c, p) {
					referenceCandidates = append(referenceCandidates, c)
				}
			}
			if len(referenceCandidates) > 0 {
				w.meta["reference_candidates"] = referenceCandidates
			}
			if result.AreaOnly {
				w.meta["search_warning"] = "服务只返回行政区中心，将结合路线语境估算代表点"
			}
			if result.Truncated {
				w.meta["truncated"] = true
			}
			if len(w.candidates) == 0 {
				if w.meta["search_warning"] == nil {
					w.meta["search_warning"] = "服务未返回符合行政范围的有效地点，转入模型估算"
				}
				return
			}
			if !pointEntity(w.entity) {
				w.meta["search_warning"] = "区域或制图控制点结合检索位置与路线语境估算代表点"
				return
			}
			var exact []Candidate
			for _, c := range w.candidates {
				if c.Name == p.ModernName && candidateAddressSupported(c, p) {
					exact = append(exact, c)
				}
			}
			if len(exact) == 1 && !result.Truncated {
				w.selectCandidate(exact[0], "exact_match", "现代名称精确匹配，候选行政位置与地点及路线分析一致")
			}
		}(w)
	}
	wg.Wait()
}

func coherentCandidates(candidates []Candidate, p queryPlan) []Candidate {
	result := []Candidate{}
	for _, c := range candidates {
		if candidateCoherent(c, p) {
			result = append(result, c)
		}
	}
	return result
}
func candidateCoherent(c Candidate, p queryPlan) bool {
	if !usableCoordinate(c.Lon, c.Lat) {
		return false
	}
	if upperAdministrativeCenter(c, p) {
		return false
	}
	for _, unsupported := range []string{"政府", "村委", "居委", "公司", "企业", "医院", "学校", "纪念馆", "墓园"} {
		if strings.Contains(c.TypeName, unsupported) && !strings.Contains(p.ModernName, unsupported) {
			return false
		}
	}
	// If the provider includes an address, a conflicting structured province or
	// county must not become a candidate merely because its name is similar.
	if c.Address != "" {
		if p.Province != "" && !strings.Contains(c.Address, p.Province) && (strings.Contains(c.Address, "省") || strings.Contains(c.Address, "自治区")) {
			return false
		}
		cityAddress := c.Address
		// A county-level city may be the only city token in a short address.
		// Remove the expected county before checking for a conflicting city.
		if p.County != "" {
			cityAddress = strings.ReplaceAll(cityAddress, p.County, "")
		}
		if p.City != "" && !strings.Contains(c.Address, p.City) && (strings.Contains(cityAddress, "市") || strings.Contains(cityAddress, "州") || strings.Contains(cityAddress, "盟")) {
			return false
		}
		if p.County != "" && !strings.Contains(c.Address, p.County) && (strings.Contains(c.Address, "县") || strings.Contains(c.Address, "区")) {
			return false
		}
	}
	return true
}

func upperAdministrativeCenter(c Candidate, p queryPlan) bool {
	name := strings.TrimSpace(c.Name)
	if name == "" || name == strings.TrimSpace(p.ModernName) {
		return false
	}
	for _, parent := range []string{p.Province, p.City, p.County, p.Township, p.Village} {
		if parent != "" && name == parent {
			return true
		}
	}
	// A plain higher administrative label is a reference location, not the
	// village, bridge, pass or ferry itself. Exact target names remain allowed.
	for _, suffix := range []string{"省", "自治区", "特别行政区", "自治州", "盟", "市", "县", "区", "乡", "镇", "街道"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}
func candidateAddressSupported(c Candidate, p queryPlan) bool {
	if c.Address == "" {
		return false
	}
	for _, area := range []string{p.County, p.City, p.Province, p.AdministrativeHint} {
		if area != "" && strings.Contains(c.Address, area) {
			return true
		}
	}
	return false
}
func pointEntity(entity map[string]any) bool {
	return str(entity["entity_kind"]) == "place" && !descriptivePlace(str(entity["name"]))
}
func metadata(name string) map[string]any {
	return map[string]any{"provider": "tianditu", "original_name": name, "coordinate_system": "tianditu_lonlat", "coordinate_order": "longitude,latitude", "source_url": documentationURL, "candidates": []Candidate{}}
}
func descriptivePlace(name string) bool {
	for _, marker := range []string{"一带", "附近", "地区", "山区", "流域", "沿岸", "以东", "以西", "以南", "以北", "东部", "西部", "南部", "北部", "周边"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}
func trustedCoordinates(entity map[string]any) bool {
	lon, ok1 := number(entity["lon"])
	lat, ok2 := number(entity["lat"])
	if !ok1 || !ok2 || !usableCoordinate(lon, lat) {
		return false
	}
	switch str(entity["coordinate_source"]) {
	case "llm_estimated":
		confidence, ok := number(entity["coordinate_confidence"])
		return ok && confidence >= 0 && confidence <= 1
	case "manual", "source_text", "gazetteer", "derived_control":
		return true
	}
	return false
}

func retryableSearchError(err error) bool {
	var httpErr *httpStatusError
	if errors.As(err, &httpErr) {
		return httpErr.code == http.StatusBadRequest
	}
	var provider *providerError
	return errors.As(err, &provider) && strings.HasPrefix(provider.code, "200")
}
func usableCoordinate(lon, lat float64) bool {
	return validCoordinate(lon, lat) && (lon != 0 || lat != 0)
}
func number(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case json.Number:
		n, e := v.Float64()
		return n, e == nil
	}
	return 0, false
}
func entityMaps(value any) []map[string]any {
	switch items := value.(type) {
	case []map[string]any:
		return items
	case []any:
		result := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if e, ok := item.(map[string]any); ok {
				result = append(result, e)
			}
		}
		return result
	}
	return nil
}

func refreshNeighbors(work []*placeWork, entities []map[string]any, graph map[string]any) {
	byID := map[string]map[string]any{}
	for _, e := range entities {
		byID[str(e["entity_id"])] = e
	}
	eventPlace := map[string]string{}
	for _, e := range entityMaps(graph["events"]) {
		eventPlace[str(e["event_id"])] = str(e["location_ref"])
	}
	adjacent := map[string][]string{}
	for _, route := range entityMaps(graph["routes"]) {
		for _, path := range entityMaps(route["paths"]) {
			var ids []string
			if nodes, ok := path["nodes"].([]any); ok {
				for _, raw := range nodes {
					id := str(raw)
					if node, ok := raw.(map[string]any); ok {
						id = str(node["ref_id"])
						if str(node["ref_type"]) == "event" {
							id = eventPlace[id]
						}
					}
					if byID[id] != nil {
						ids = append(ids, id)
					}
				}
			}
			for i, id := range ids {
				for j := max(0, i-2); j < min(len(ids), i+3); j++ {
					if j != i {
						adjacent[id] = append(adjacent[id], ids[j])
					}
				}
			}
		}
	}
	for _, w := range work {
		w.neighbors = []map[string]any{}
		seen := map[string]bool{}
		for _, id := range append(append([]string{}, adjacent[w.id()]...), w.neighborIDs...) {
			if seen[id] || len(w.neighbors) >= 8 {
				continue
			}
			seen[id] = true
			e := byID[id]
			if e == nil {
				continue
			}
			m := map[string]any{"entity_id": id, "name": e["name"], "entity_kind": e["entity_kind"]}
			if trustedCoordinates(e) {
				m["lon"], m["lat"], m["coordinate_source"] = e["lon"], e["lat"], e["coordinate_source"]
			}
			w.neighbors = append(w.neighbors, m)
		}
	}
}

// IsConfigurationError identifies upstream credential errors without exposing
// provider messages or request URLs containing the key.
func IsConfigurationError(err error) bool {
	var provider *providerError
	return errors.As(err, &provider) && strings.HasPrefix(provider.code, "301")
}
