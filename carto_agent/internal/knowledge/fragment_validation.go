package knowledge

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"unicode/utf8"
)

// Historical candidate metadata is still readable, but there are no separate
// citation and issue-repair model protocols in the current pipeline.
type FragmentCandidate struct {
	Chunk         Chunk                    `json:"chunk"`
	Revision      int                      `json:"revision"`
	Raw           string                   `json:"raw"`
	ResponseShape string                   `json:"response_shape,omitempty"`
	Validation    FragmentValidationReport `json:"validation"`
	Status        string                   `json:"status"`
}
type FragmentDiagnostic struct {
	Layer      string `json:"layer"`
	Code       string `json:"code"`
	Target     string `json:"target"`
	Message    string `json:"message"`
	RepairKind string `json:"repair_kind,omitempty"`
}
type FragmentValidationReport struct {
	Diagnostics         []FragmentDiagnostic         `json:"diagnostics"`
	SemanticDiagnostics []GraphConsistencyDiagnostic `json:"semantic_diagnostics,omitempty"`
}

func (r FragmentValidationReport) Valid() bool { return len(r.Diagnostics) == 0 }
func (r FragmentValidationReport) Error() string {
	parts := []string{}
	for _, d := range r.Diagnostics {
		parts = append(parts, d.Target+": "+d.Message)
	}
	return strings.Join(parts, "; ")
}

// The compiler schema uses this bounded subset of JSON Schema. Validation
// rejects unknown fields instead of allowing model-owned protocol extensions.
func validateSchemaValue(value any, s map[string]any, path string) error {
	fail := func(message string) error { return fmt.Errorf("%s: %s", path, message) }
	if value == nil {
		return fail("不接受 null；未知可选字段请省略")
	}
	if constant, ok := s["const"]; ok && !reflect.DeepEqual(constant, value) {
		return fail("值不符合固定约束")
	}
	if choices, ok := s["enum"].([]any); ok {
		found := false
		for _, x := range choices {
			found = found || reflect.DeepEqual(x, value)
		}
		if !found {
			return fail("值不属于允许的枚举")
		}
	}
	switch s["type"] {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return fail("必须为对象")
		}
		props, _ := s["properties"].(map[string]any)
		for _, raw := range array(s["required"]) {
			key := stringValue(raw)
			if _, ok := obj[key]; !ok {
				return fail("缺少 " + key)
			}
		}
		for key, child := range obj {
			def, known := props[key].(map[string]any)
			if !known {
				if s["additionalProperties"] == false {
					return fail("未知字段 " + key)
				}
				continue
			}
			if err := validateSchemaValue(child, def, path+"."+key); err != nil {
				return err
			}
		}
	case "array":
		rows, ok := value.([]any)
		if !ok {
			return fail("必须为数组")
		}
		if limit, ok := schemaNumber(s["minItems"]); ok && len(rows) < int(limit) {
			return fail("数组条目不足")
		}
		if limit, ok := schemaNumber(s["maxItems"]); ok && len(rows) > int(limit) {
			return fail("数组条目过多")
		}
		seen := map[string]bool{}
		def, _ := s["items"].(map[string]any)
		for i, x := range rows {
			if s["uniqueItems"] == true {
				encoded, _ := json.Marshal(x)
				if seen[string(encoded)] {
					return fail("数组条目重复")
				}
				seen[string(encoded)] = true
			}
			if def != nil {
				if err := validateSchemaValue(x, def, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return fail("必须为字符串")
		}
		if n, ok := schemaNumber(s["minLength"]); ok && utf8.RuneCountInString(text) < int(n) {
			return fail("字符串过短")
		}
	case "number", "integer":
		n, ok := schemaNumber(value)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return fail("必须为有限数值")
		}
		if s["type"] == "integer" && n != math.Trunc(n) {
			return fail("必须为整数")
		}
		if v, ok := schemaNumber(s["minimum"]); ok && n < v {
			return fail("低于最小值")
		}
		if v, ok := schemaNumber(s["maximum"]); ok && n > v {
			return fail("超过最大值")
		}
		if v, ok := schemaNumber(s["exclusiveMinimum"]); ok && n <= v {
			return fail("须大于最小值")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fail("必须为布尔值")
		}
	}
	return nil
}
func schemaNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}
func validateFragment(g Graph, c Chunk) error {
	if g["schema_version"] != "1.0" {
		return fmt.Errorf("未知图谱版本")
	}
	if _, ok := g["task"].(map[string]any); !ok {
		return fmt.Errorf("缺少task")
	}
	for _, col := range graphCollections {
		if _, ok := g[col.key].([]any); !ok {
			return fmt.Errorf("%s须为数组", col.key)
		}
	}
	if report := InspectGraphConsistency(g); len(report.Hard) > 0 {
		return fmt.Errorf("%s", report.Hard[0].Message)
	}
	index, _ := indexGraph(g)
	for _, raw := range array(g["evidence"]) {
		e := raw.(map[string]any)
		if _, err := anchorEvidence(c.Text, stringValue(e["quote"])); err != nil {
			return err
		}
	}
	for _, obj := range index {
		if obj.kind == "evidence" {
			continue
		}
		if obj.data["factual"] == false {
			continue
		}
		if obj.kind == "actors" || obj.kind == "spatial_entities" || obj.kind == "events" || obj.kind == "routes" || obj.kind == "paths" {
			if len(array(obj.data["evidence_refs"])) == 0 && obj.kind != "paths" {
				return fmt.Errorf("%s缺少证据", obj.kind)
			}
		}
	}
	return nil
}

// Partial extraction is an editable model draft, not a validated graph. Raw
// responses stay in FragmentCandidate; only container shapes and editor IDs are
// repaired here. Unknown middle nodes remain in place, never joining neighbours.
func recoverPartialFragment(raw string, chunk Chunk) (Graph, error) {
	var response map[string]any
	if err := decodeObject(raw, &response); err != nil {
		return nil, err
	}
	g, ok := response["graph"].(map[string]any)
	if !ok || g == nil {
		return nil, fmt.Errorf("响应没有可恢复的图谱对象")
	}
	objectRows := func(value any) []any {
		rows, ok := value.([]any)
		if !ok {
			if row, valid := value.(map[string]any); valid && row != nil {
				rows = []any{row}
			}
		}
		out := []any{}
		for _, raw := range rows {
			if row, valid := raw.(map[string]any); valid && row != nil {
				out = append(out, row)
			}
		}
		return out
	}
	type record struct {
		row       map[string]any
		key, base string
	}
	var records []record
	var missingNodes []map[string]any
	usable := false
	for _, col := range graphCollections {
		g[col.key] = objectRows(g[col.key])
		for index, raw := range array(g[col.key]) {
			row := raw.(map[string]any)
			records = append(records, record{row, col.id, fmt.Sprintf("partial_%s_%d", col.key, index+1)})
			if col.key != "evidence" && col.key != "point_presentations" {
				for _, field := range []string{"name", "title", "description", "body"} {
					usable = usable || strings.TrimSpace(stringValue(row[field])) != ""
				}
				usable = usable || strings.TrimSpace(stringValue(row["location_ref"])) != ""
				if lon, a := schemaNumber(row["lon"]); a {
					if lat, b := schemaNumber(row["lat"]); b && math.Abs(lon) <= 180 && math.Abs(lat) <= 90 {
						usable = true
					}
				}
			}
			if col.key != "routes" {
				continue
			}
			row["paths"] = objectRows(row["paths"])
			for pi, rp := range array(row["paths"]) {
				path := rp.(map[string]any)
				records = append(records, record{path, "path_id", fmt.Sprintf("partial_path_%d_%d", index+1, pi+1)})
				nodes, isArray := path["nodes"].([]any)
				if !isArray && path["nodes"] != nil {
					nodes = []any{path["nodes"]}
				}
				if nodes == nil {
					nodes = []any{}
				}
				for ni, node := range nodes {
					item, valid := node.(map[string]any)
					if !valid || item == nil {
						item = map[string]any{"ref_type": "", "ref_id": stringValue(node)}
					}
					if strings.TrimSpace(stringValue(item["ref_id"])) != "" {
						usable = true
					} else {
						missingNodes = append(missingNodes, item)
					}
					item["ref_type"] = stringValue(item["ref_type"])
					nodes[ni] = item
				}
				path["nodes"] = nodes
				path["spatial_constraints"] = objectRows(path["spatial_constraints"])
				for _, rc := range array(path["spatial_constraints"]) {
					constraint := rc.(map[string]any)
					if ref, valid := constraint["reference"].(map[string]any); !valid || ref == nil {
						delete(constraint, "reference")
					}
				}
			}
		}
	}
	if !usable {
		return nil, fmt.Errorf("响应没有可恢复的地点、事件、部队、路线或注记内容")
	}
	// Duplicate declarations get separate draft IDs; their ambiguous old
	// references stay dangling instead of silently selecting the first object.
	counts := map[string]int{}
	for _, item := range records {
		if id := strings.TrimSpace(stringValue(item.row[item.key])); id != "" {
			counts[id]++
		}
	}
	freshID := func(base string) string {
		id := base
		for suffix := 2; counts[id] != 0; suffix++ {
			id = fmt.Sprintf("%s_%d", base, suffix)
		}
		counts[id] = 1
		return id
	}
	prefix := chunk.ID + "__"
	for _, item := range records {
		id := strings.TrimSpace(stringValue(item.row[item.key]))
		if id == "" || counts[id] != 1 {
			id = freshID(item.base)
		}
		item.row[item.key] = prefix + id
	}
	for index, node := range missingNodes {
		node["ref_id"] = freshID(fmt.Sprintf("partial_missing_node_%d", index+1))
	}
	// Only actual source labels receive program-authored quotations; invalid
	// labels remain unresolved metadata and never acquire invented support.
	sources := sourceSentences(chunk)
	evidenceIDs := map[string]string{}
	for index, source := range sources {
		evidenceIDs[source.Ref] = prefix + freshID(fmt.Sprintf("partial_evidence_%d", index+1))
	}
	usedSources := map[string]bool{}
	var normalize func(any)
	normalize = func(value any) {
		switch row := value.(type) {
		case map[string]any:
			for _, field := range []string{"name", "title", "description", "body", "quote", "time_text", "event_type", "entity_kind", "map_role", "coordinate_source", "role", "direction", "side", "topology", "anchor_ref_type", "kind", "bearing", "relation", "source_id", "image_path", "template", "leader_style", "preferred_side"} {
				if value, present := row[field]; present {
					if _, valid := value.(string); !valid {
						delete(row, field)
					}
				}
			}
			if aliases, present := row["aliases"]; present {
				clean := []any{}
				if value, valid := aliases.(string); valid {
					clean = append(clean, value)
				} else {
					for _, value := range array(aliases) {
						if text, valid := value.(string); valid {
							clean = append(clean, text)
						}
					}
				}
				row["aliases"] = clean
			}
			for key, child := range row {
				if !graphReferenceKeys[key] {
					normalize(child)
					continue
				}
				if key == "actor_refs" || key == "evidence_refs" {
					refs, isArray := child.([]any)
					if !isArray {
						refs = []any{child}
					}
					clean, seen := []any{}, map[string]bool{}
					for _, raw := range refs {
						id := strings.TrimSpace(stringValue(raw))
						if id == "" || seen[id] {
							continue
						}
						seen[id] = true
						if key == "evidence_refs" && evidenceIDs[id] != "" {
							usedSources[id] = true
							clean = append(clean, evidenceIDs[id])
						} else {
							clean = append(clean, prefix+id)
						}
					}
					row[key] = clean
				} else if id := strings.TrimSpace(stringValue(child)); id != "" {
					row[key] = prefix + id
				} else {
					delete(row, key)
				}
			}
		case []any:
			for _, child := range row {
				normalize(child)
			}
		}
	}
	normalize(g)
	g["evidence"] = []any{}
	for _, source := range sources {
		if usedSources[source.Ref] && source.Start != nil && source.End != nil {
			g["evidence"] = append(array(g["evidence"]), map[string]any{"evidence_id": evidenceIDs[source.Ref], "quote": source.Text, "start": *source.Start, "end": *source.End})
		}
	}
	// Header defaults describe the transport, not historical facts. Do not
	// reinterpret another CRS's coordinates as degrees in the existing editor.
	task, _ := g["task"].(map[string]any)
	if task == nil {
		task = map[string]any{}
	}
	if crs := stringValue(task["input_crs"]); crs != "" && crs != "EPSG:4326" {
		var stripCoordinates func(any)
		stripCoordinates = func(value any) {
			switch row := value.(type) {
			case map[string]any:
				for _, field := range coordinateFields {
					delete(row, field)
				}
				for _, child := range row {
					stripCoordinates(child)
				}
			case []any:
				for _, child := range row {
					stripCoordinates(child)
				}
			}
		}
		stripCoordinates(g)
	}
	g["schema_version"], task["input_crs"] = "1.0", "EPSG:4326"
	g["task"] = task
	if err := validatePartialGraph(g); err != nil {
		return nil, err
	}
	return g, nil
}

func HasPartialExtraction(result *Result) bool {
	if result != nil {
		for _, candidate := range result.FragmentCandidates {
			if candidate.Status == "partial" {
				return true
			}
		}
	}
	return false
}

// The editor can show unresolved string references, but not null nested rows
// or ambiguous object IDs used as editable item keys.
func validatePartialGraph(graph Graph) error {
	if err := ValidateReviewDraft(graph); err != nil {
		return err
	}
	if _, err := indexGraph(graph); err != nil {
		return err
	}
	for _, rr := range array(graph["routes"]) {
		route := rr.(map[string]any)
		for _, rp := range array(route["paths"]) {
			path := rp.(map[string]any)
			for _, field := range []string{"nodes", "spatial_constraints"} {
				value, exists := path[field]
				if field == "spatial_constraints" && !exists {
					continue
				}
				rows, ok := value.([]any)
				if !ok {
					return fmt.Errorf("草稿路径 %s 的 %s 必须为数组", path["path_id"], field)
				}
				for _, raw := range rows {
					row, valid := raw.(map[string]any)
					if !valid || row == nil {
						return fmt.Errorf("草稿路径 %s 的 %s 含无效条目", path["path_id"], field)
					}
				}
			}
		}
	}
	return nil
}
