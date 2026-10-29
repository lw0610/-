package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// encoding/json otherwise accepts duplicate keys with last-value-wins. Reject
// ambiguous objects before decoding so neither facts nor questions can vanish.
func rejectDuplicateJSONKeys(text string) error {
	decoder := json.NewDecoder(strings.NewReader(text))
	var visit func() error
	visit = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		seen := map[string]bool{}
		for decoder.More() {
			if delim == '{' {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key := keyToken.(string)
				if seen[key] {
					return fmt.Errorf("JSON 对象含重复字段 %q", key)
				}
				seen[key] = true
			}
			if err := visit(); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	}
	return visit()
}

type modelIssue struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Reason      string   `json:"reason"`
	Field       string   `json:"field"`
	SubjectRefs []string `json:"subject_refs"`
	Message     string   `json:"message"`
	SourceRefs  []string `json:"source_refs"`
}
type fragmentResponse struct {
	Graph      Graph   `json:"graph"`
	Unresolved []Issue `json:"unresolved"`
}
type modelResponse struct {
	Graph      Graph                `json:"graph"`
	Unresolved []modelIssue         `json:"unresolved"`
	Aliases    map[string]string    `json:"aliases,omitempty"`
	Changes    []SemanticChange     `json:"changes,omitempty"`
	Repairs    []FragmentDiagnostic `json:"-"`
}

func modelGraphSchema(contract Contract) (map[string]any, error) {
	var s map[string]any
	if json.Unmarshal(contract.JSONSchema, &s) != nil || s == nil {
		return nil, errors.New("知识图谱 schema 必须是对象")
	}
	omitSchemaFields(s, "evidence")
	properties, _ := s["properties"].(map[string]any)
	if entities, ok := properties["spatial_entities"].(map[string]any); ok {
		if item, ok := entities["items"].(map[string]any); ok {
			omitSchemaFields(item, "geocoding")
			props, _ := item["properties"].(map[string]any)
			props["entity_kind"] = map[string]any{"enum": []any{"place", "region"}}
			props["factual"] = map[string]any{"const": true}
			props["coordinate_source"] = map[string]any{"const": "source_text"}
		}
	}
	if routes, ok := properties["routes"].(map[string]any); ok {
		if route, ok := routes["items"].(map[string]any); ok {
			omitSchemaFields(route, "style_ref", "topology")
			rp, _ := route["properties"].(map[string]any)
			paths, _ := rp["paths"].(map[string]any)
			path, _ := paths["items"].(map[string]any)
			if path != nil {
				omitSchemaFields(path, "direction", "control_options")
				pp, _ := path["properties"].(map[string]any)
				constraints, _ := pp["spatial_constraints"].(map[string]any)
				constraint, _ := constraints["items"].(map[string]any)
				cp, _ := constraint["properties"].(map[string]any)
				if cp != nil {
					cp["evidence_refs"] = sourceRefsSchema()
				}
				if reference, ok := cp["reference"].(map[string]any); ok {
					omitSchemaFields(reference, "lon", "lat", "coordinate_source", "coordinate_confidence", "geocoding")
				}

				pp["actor_refs"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
				pp["evidence_refs"] = sourceRefsSchema()
				path["required"] = append(array(path["required"]), "actor_refs", "evidence_refs")
			}
		}
	}
	return s, nil
}
func sourceRefsSchema() map[string]any {
	return map[string]any{"type": "array", "minItems": 1, "uniqueItems": true, "items": map[string]any{"type": "string", "minLength": 1}}
}
func omitSchemaFields(s map[string]any, keys ...string) {
	p, _ := s["properties"].(map[string]any)
	drop := map[string]bool{}
	for _, k := range keys {
		delete(p, k)
		drop[k] = true
	}
	r := []any{}
	for _, v := range array(s["required"]) {
		if !drop[stringValue(v)] {
			r = append(r, v)
		}
	}
	s["required"] = r
}
func extractionSystem(c Contract) (string, error) { return stageSystem(c, fragmentInstructions) }
func stageSystem(c Contract, stage string) (string, error) {
	s, err := modelGraphSchema(c)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(s)
	return c.Instructions + "\n\n" + stage + "以下 JSON Schema 只描述返回对象中的 graph 字段；schema_version、task 都必须放在 graph 内，外层只放阶段要求的字段。\n" + string(raw), err
}
func fragmentSourceInput(c Chunk) map[string]any {
	return map[string]any{"chunk_id": c.ID, "source_evidence": modelEvidenceSources(sourceSentences(c))}
}

func decodeModelResponse(raw string, c Contract, sources []EvidenceSource, semantic bool) (modelResponse, error) {
	var out modelResponse
	var fields map[string]json.RawMessage
	if err := decodeObject(raw, &fields); err != nil {
		return out, err
	}
	// Some models place the graph's two header fields beside graph after reading
	// its schema. Moving those fields is lossless; disagreement remains an error.
	if fields["schema_version"] != nil || fields["task"] != nil {
		var graphFields map[string]json.RawMessage
		if err := decodeObject(string(fields["graph"]), &graphFields); err != nil {
			return out, fmt.Errorf("graph 字段不合法，无法归位图谱头字段: %w", err)
		}
		for _, key := range []string{"schema_version", "task"} {
			outer, exists := fields[key]
			if !exists {
				continue
			}
			if inner, hasInner := graphFields[key]; hasInner {
				var left, right any
				if json.Unmarshal(outer, &left) != nil || json.Unmarshal(inner, &right) != nil || !reflect.DeepEqual(left, right) {
					return out, fmt.Errorf("graph.%s 与外层 %s 冲突", key, key)
				}
			} else {
				graphFields[key] = outer
			}
			delete(fields, key)
		}
		fields["graph"], _ = json.Marshal(graphFields)
		rawBytes, err := json.Marshal(fields)
		if err != nil {
			return out, err
		}
		raw = string(rawBytes)
	}
	// Protocol metadata must not discard a useful graph. Models sometimes put
	// unresolved inside graph, or omit an empty list altogether.
	var graphFields map[string]json.RawMessage
	if err := decodeObject(string(fields["graph"]), &graphFields); err != nil {
		return out, fmt.Errorf("graph 字段不合法: %w", err)
	}
	if misplaced, ok := graphFields["unresolved"]; ok {
		if fields["unresolved"] == nil || string(fields["unresolved"]) == "null" {
			fields["unresolved"] = misplaced
		}
		delete(graphFields, "unresolved")
		out.Repairs = append(out.Repairs, responseRepair("graph.unresolved", "将疑问列表归位到响应外层"))
	}
	fields["graph"], _ = json.Marshal(graphFields)
	if fields["unresolved"] == nil || string(fields["unresolved"]) == "null" {
		fields["unresolved"] = json.RawMessage(`[]`)
		out.Repairs = append(out.Repairs, responseRepair("unresolved", "省略的疑问列表按空列表处理"))
	}
	for _, key := range []string{"graph"} {
		v, ok := fields[key]
		if !ok || string(v) == "null" {
			return out, fmt.Errorf("必须显式返回 %s", key)
		}
	}
	if semantic {
		for _, key := range []string{"aliases", "changes"} {
			v, ok := fields[key]
			if !ok || string(v) == "null" {
				return out, fmt.Errorf("语义核对必须显式返回 %s", key)
			}
		}
	} else {
		delete(fields, "aliases")
		delete(fields, "changes")
	}
	rawBytes, _ := json.Marshal(fields)
	raw = string(rawBytes)
	if err := decodeObject(raw, &out); err != nil {
		return out, err
	}
	schema, err := modelGraphSchema(c)
	if err != nil {
		return out, err
	}
	for _, col := range graphCollections {
		if col.key != "evidence" && out.Graph[col.key] == nil {
			out.Graph[col.key] = []any{}
		}
	}
	// Guard helpers that interpret nodes before examining optional constraints.
	if _, err = indexGraph(out.Graph); err != nil {
		return out, err
	}
	out.Repairs = append(out.Repairs, normalizeModelConstraints(out.Graph, schema, sources)...)
	for _, raw := range array(out.Graph["spatial_entities"]) {
		entity, _ := raw.(map[string]any)
		if coordinateErr := validateSourceCoordinates(Graph{"spatial_entities": []any{entity}}, sources); coordinateErr != nil {
			// A guessed/malformed coordinate is not a reason to lose its place.
			for _, key := range []string{"lon", "lat", "coordinate_source", "coordinate_confidence", "geocoding"} {
				delete(entity, key)
			}
			out.Repairs = append(out.Repairs, responseRepair(stringValue(entity["entity_id"]), coordinateErr.Error()+"；保留地点并交由定位步骤处理"))
		}
	}
	if err = validateSchemaValue(map[string]any(out.Graph), schema, "graph"); err != nil {
		return out, err
	}
	if err = validateSourceCoordinates(out.Graph, sources); err != nil {
		return out, err
	}
	for _, col := range graphCollections {
		if col.key == "evidence" {
			continue
		}
		if _, ok := out.Graph[col.key]; !ok {
			out.Graph[col.key] = []any{}
		}
	}
	if err = materializeEvidence(out.Graph, sources); err != nil {
		return out, err
	}
	out.Repairs = append(out.Repairs, discardInvalidModelConstraints(out.Graph)...)
	if report := InspectGraphConsistency(out.Graph); len(report.Hard) > 0 {
		return out, fmt.Errorf("图谱引用不合法: %s", report.Hard[0].Message)
	}
	return out, nil
}

func responseRepair(target, message string) FragmentDiagnostic {
	return FragmentDiagnostic{Layer: "response", Code: "recovered_model_field", Target: target, Message: message, RepairKind: "automatic"}
}

// A spatial constraint is optional rendering information, never a prerequisite
// for keeping the places and path. Do not turn ordinary travel bearings into a
// fabricated reference place or guess an unknown within/outside relationship.
func normalizeModelConstraints(g Graph, schema map[string]any, sources []EvidenceSource) []FragmentDiagnostic {
	props, _ := schema["properties"].(map[string]any)
	routes, _ := props["routes"].(map[string]any)
	route, _ := routes["items"].(map[string]any)
	rp, _ := route["properties"].(map[string]any)
	paths, _ := rp["paths"].(map[string]any)
	pathSchema, _ := paths["items"].(map[string]any)
	pp, _ := pathSchema["properties"].(map[string]any)
	constraints, _ := pp["spatial_constraints"].(map[string]any)
	constraintSchema, _ := constraints["items"].(map[string]any)
	repairs := []FragmentDiagnostic{}
	sourceByID := sourceIndex(sources)
	directions := map[string]string{"N": "北", "NE": "东北", "E": "东", "SE": "东南", "S": "南", "SW": "西南", "W": "西", "NW": "西北"}
	for _, rr := range array(g["routes"]) {
		r, _ := rr.(map[string]any)
		for _, rp := range array(r["paths"]) {
			p, _ := rp.(map[string]any)
			value, exists := p["spatial_constraints"]
			if !exists {
				continue
			}
			kept := []any{}
			rows, validArray := value.([]any)
			if !validArray {
				repairs = append(repairs, responseRepair(stringValue(p["path_id"]), "忽略无法读取的可选方位约束；保留路径节点"))
			}
			for i, raw := range rows {
				target := fmt.Sprintf("%s.spatial_constraints[%d]", p["path_id"], i)
				c, ok := raw.(map[string]any)
				if !ok {
					repairs = append(repairs, responseRepair(target, "忽略非对象方位约束"))
					continue
				}
				before, _ := json.Marshal(c)
				if _, exists := c["evidence_refs"]; exists {
					refs, seen := []any{}, map[string]bool{}
					for _, rawRef := range array(c["evidence_refs"]) {
						id := stringValue(rawRef)
						if _, known := sourceByID[id]; known && !seen[id] {
							refs = append(refs, id)
							seen[id] = true
						}
					}
					if len(refs) > 0 {
						c["evidence_refs"] = refs
					} else {
						delete(c, "evidence_refs")
					}
				}
				bearing := strings.ToUpper(strings.TrimSpace(stringValue(c["bearing"])))
				for code, word := range directions {
					if bearing == word || bearing == word+"方" || bearing == word+"方向" {
						bearing = code
					}
				}
				c["bearing"] = bearing
				reason := ""
				switch c["kind"] {
				case "approach_from":
					for _, key := range []string{"after_ref", "before_ref", "reference", "relation", "occurrence"} {
						delete(c, key)
					}
					if stringValue(c["target_ref"]) == "" || stringValue(c["target_ref"]) != controlFirstPlace(g, controlPathNodes(g, p)) || p["role"] == "branch" {
						reason = "来向约束与实际路径起点不符；保留原路径，不添加箭尾"
					}
				case "via_relative":
					delete(c, "target_ref")
					ref, _ := c["reference"].(map[string]any)
					name := strings.TrimSpace(stringValue(ref["name"]))
					if name == "" {
						reason = "行进方向没有参照地点，按已有路径节点绘制"
					} else if c["relation"] != "within" && c["relation"] != "outside" {
						refs := array(c["evidence_refs"])
						if len(refs) == 0 {
							refs = array(p["evidence_refs"])
						}
						text := ""
						for _, id := range refs {
							text += sourceByID[stringValue(id)].Text
						}
						word := directions[bearing]
						within := word != "" && strings.Contains(text, name+word+"部")
						outside := word != "" && strings.Contains(text, name+"以"+word)
						if within != outside {
							if within {
								c["relation"] = "within"
							} else {
								c["relation"] = "outside"
							}
						} else {
							reason = "原文无法确定参照区域内外，不猜测控制点；保留原路径"
						}
					}
				default:
					reason = "未知的可选方位约束；保留原路径"
				}
				if _, valid := compassBearings[bearing]; !valid {
					reason = "无法解释方位；保留原路径"
				}
				if reason == "" && constraintSchema != nil {
					if e := validateSchemaValue(c, constraintSchema, target); e != nil {
						reason = e.Error()
					}
				}
				if reason != "" {
					repairs = append(repairs, responseRepair(target, reason))
					continue
				}
				after, _ := json.Marshal(c)
				if string(before) != string(after) {
					repairs = append(repairs, responseRepair(target, "归一化方位约束的字段表示"))
				}
				kept = append(kept, c)
			}
			if len(kept) == 0 {
				delete(p, "spatial_constraints")
			} else {
				p["spatial_constraints"] = kept
			}
		}
	}
	return repairs
}

// This also catches stale after/before/target IDs and nonadjacent intervals.
// Local geometry metadata is quarantined in the raw response kept in the trace.
func discardInvalidModelConstraints(g Graph) []FragmentDiagnostic {
	report := InspectGraphConsistency(g)
	diagnostics := append(report.Hard, report.Semantic...)
	repairs := []FragmentDiagnostic{}
	for ri, rr := range array(g["routes"]) {
		r, _ := rr.(map[string]any)
		for pi, rp := range array(r["paths"]) {
			p, _ := rp.(map[string]any)
			kept := []any{}
			for ci, c := range array(p["spatial_constraints"]) {
				prefix := fmt.Sprintf("/routes/%d/paths/%d/spatial_constraints/%d", ri, pi, ci)
				bad := false
				for _, d := range diagnostics {
					if d.Target == prefix || strings.HasPrefix(d.Target, prefix+"/") {
						repairs = append(repairs, responseRepair(prefix, d.Message+"；仅忽略该方位约束"))
						bad = true
						break
					}
				}
				if !bad {
					kept = append(kept, c)
				}
			}
			if len(kept) == 0 {
				delete(p, "spatial_constraints")
			} else {
				p["spatial_constraints"] = kept
			}
		}
	}
	return repairs
}

var sourceCoordinateNumber = regexp.MustCompile(`[-+]?(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)`)

// Model extraction can copy decimal-degree coordinates from cited source text.
// Geographic lookup and historical-name resolution belong to the later resolver.
// Checking the actual cited values also rejects guessed coordinates mislabeled
// source_text when the input contains no such coordinate values.
func validateSourceCoordinates(g Graph, sources []EvidenceSource) error {
	byRef := make(map[string]string, len(sources))
	for _, source := range sources {
		byRef[source.Ref] = source.Text
	}
	for _, raw := range array(g["spatial_entities"]) {
		entity, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		lon, hasLon := entity["lon"]
		lat, hasLat := entity["lat"]
		_, hasSource := entity["coordinate_source"]
		_, hasConfidence := entity["coordinate_confidence"]
		if !hasLon && !hasLat && !hasSource && !hasConfidence {
			continue
		}
		if !hasLon || !hasLat || entity["coordinate_source"] != "source_text" {
			return fmt.Errorf("地点 %s 仅可成对抄录原文明示坐标并标记 source_text；未知时省略坐标和来源", stringValue(entity["name"]))
		}
		numbers := map[float64]bool{}
		for _, ref := range array(entity["evidence_refs"]) {
			for _, token := range sourceCoordinateNumber.FindAllString(byRef[stringValue(ref)], -1) {
				if value, err := strconv.ParseFloat(token, 64); err == nil {
					numbers[value] = true
				}
			}
		}
		longitude, lonOK := schemaNumber(lon)
		latitude, latOK := schemaNumber(lat)
		if !lonOK || !latOK || !numbers[longitude] || !numbers[latitude] {
			return fmt.Errorf("地点 %s 的坐标数值未见于所引原文，不能估算或把模型记忆标成 source_text；请省略并留待定位", stringValue(entity["name"]))
		}
	}
	return nil
}

func decodeModelIssues(rows []modelIssue, g Graph, sources []EvidenceSource) ([]Issue, error) {
	out := []Issue{}
	seen := map[string]bool{}
	var problems []error
	for _, row := range rows {
		if seen[row.ID] || row.ID == "" || strings.TrimSpace(row.Message) == "" {
			problems = append(problems, fmt.Errorf("疑问 %q 的ID或说明缺失/重复", row.ID))
			continue
		}
		seen[row.ID] = true
		quotes, err := sourceQuotes(row.SourceRefs, sources)
		if err != nil {
			problems = append(problems, fmt.Errorf("问题 %s 的原文引用无效：%w", row.ID, err))
			continue
		}
		issue := Issue{ID: row.ID, Kind: row.Kind, Reason: row.Reason, Field: canonicalModelIssueField(row.Kind, row.Field), SubjectRefs: row.SubjectRefs, Message: row.Message, EvidenceQuotes: quotes}
		if len(issue.SubjectRefs) == 0 {
			// Missing source content concerns the input as a whole; there may be
			// no extracted object to bind. Preserve that explanation, not a fake ID.
			issue.Kind, issue.Field = "source", "content"
		}
		if err := validateIssueSubjects(g, nil, issue); err != nil {
			problems = append(problems, fmt.Errorf("问题 %s：%w", row.ID, err))
			continue
		}
		out = append(out, issue)
	}
	if len(problems) > 0 {
		// Give the bounded retry all invalid questions at once. Returning only
		// the first one wastes a full graph generation on each later field error.
		return nil, errors.Join(problems...)
	}
	return out, nil
}

func (p *Pipeline) extract(ctx context.Context, r *Result, c Chunk, contract Contract, depth int) error {
	if c.HardCut {
		return fmt.Errorf("片段 %s 无完整句边界，请调整输入或分段预算", c.ID)
	}
	system, err := extractionSystem(contract)
	if err != nil {
		return err
	}
	idx := len(r.FragmentCandidates)
	r.FragmentCandidates = append(r.FragmentCandidates, FragmentCandidate{Chunk: c, Status: "extracting"})
	input := fragmentSourceInput(c)
	recoverDraft := func(cause error) bool {
		for i := len(r.Attempts) - 1; i >= 0; i-- {
			a := r.Attempts[i]
			if a.Stage != "extract" || a.ID != c.ID {
				continue
			}
			g, recoveryErr := recoverPartialFragment(a.Response, c)
			if recoveryErr != nil {
				continue
			}
			candidate := &r.FragmentCandidates[idx]
			candidate.Status = "partial"
			candidate.Validation.Diagnostics = append(candidate.Validation.Diagnostics, responseRepair(c.ID, "已保留可读取的抽取草稿："+cause.Error()))
			r.Chunks = append(r.Chunks, Fragment{Chunk: c, Graph: g})
			return true
		}
		return false
	}
	var out modelResponse
	var issues []Issue
	for attempt := 1; attempt <= 2; attempt++ {
		raw, callErr := p.request(ctx, r, "extract", c.ID, system, input)
		candidate := &r.FragmentCandidates[idx]
		candidate.Raw, candidate.Revision = raw, attempt
		if errors.Is(callErr, errTruncated) || errors.Is(callErr, errInputBudget) {
			candidate.Status = "incomplete"
			if depth < 2 && len([]rune(c.Text)) >= 160 {
				candidate.Status = "split"
				a, b := bisectChunk(c)
				if err := p.extract(ctx, r, a, contract, depth+1); err != nil {
					return err
				}
				return p.extract(ctx, r, b, contract, depth+1)
			}
		}
		if callErr != nil {
			if ctx.Err() == nil && !errors.Is(callErr, context.Canceled) && !errors.Is(callErr, context.DeadlineExceeded) && recoverDraft(callErr) {
				return p.save(r)
			}
			return callErr
		}
		out, err = decodeModelResponse(raw, contract, sourceSentences(c), false)
		if err == nil {
			// Questions describe uncertainty; malformed question metadata must not
			// invalidate the facts it accompanies. Keep raw questions in the trace.
			issues = nil
			for _, row := range out.Unresolved {
				decoded, issueErr := decodeModelIssues([]modelIssue{row}, out.Graph, sourceSentences(c))
				if issueErr != nil {
					out.Repairs = append(out.Repairs, responseRepair("unresolved."+row.ID, issueErr.Error()))
					continue
				}
				issues = append(issues, decoded...)
			}
			if issueErr := validatePathActorIssues(out.Graph, issues); issueErr != nil {
				out.Repairs = append(out.Repairs, responseRepair(c.ID, issueErr.Error()))
			}
			if len(out.Repairs) > 0 {
				r.Attempts[len(r.Attempts)-1].Validation = &FragmentValidationReport{Diagnostics: out.Repairs}
			}
		}
		if err == nil {
			break
		}
		candidate.Status = "invalid"
		candidate.Validation.Diagnostics = append(candidate.Validation.Diagnostics, FragmentDiagnostic{
			Layer: "response", Code: "invalid_model_response", Target: c.ID, Message: err.Error(),
		})
		r.Attempts[len(r.Attempts)-1].Error = err.Error()
		if attempt == 2 {
			if recoverDraft(err) {
				return p.save(r)
			}
			return fmt.Errorf("片段 %s 的模型结果格式或原文引用有误，已重试一次：%w", c.ID, err)
		}
		input = fragmentSourceInput(c)
		input["validation_feedback"] = "上一次回答未通过校验，需同时修正以下问题：\n" + err.Error() + "\n请按 unresolved 协议填写合法 kind/field/reason；对象疑问绑定 graph 中实际存在的 subject_refs，涉及原文整体的问题用 source/content 并保留空 subject_refs；source_refs 仅引用 source_evidence.ref。不得删除真实疑问或更改其含义来绕过校验。重新返回完整结果，保留有原文依据的事实。"
	}
	g, err := NamespaceGraph(out.Graph, c.ID+"__")
	if err != nil {
		return err
	}
	for i := range issues {
		issues[i].ID = c.ID + "__" + issues[i].ID
		for j := range issues[i].SubjectRefs {
			issues[i].SubjectRefs[j] = c.ID + "__" + issues[i].SubjectRefs[j]
		}
	}
	r.FragmentCandidates[idx].Status = "validated"
	r.FragmentCandidates[idx].Validation = FragmentValidationReport{Diagnostics: out.Repairs}
	if len(out.Repairs) > 0 {
		r.FragmentCandidates[idx].Status = "recovered"
	}
	r.Chunks = append(r.Chunks, Fragment{Chunk: c, Graph: g, Unresolved: issues})
	return p.save(r)
}
