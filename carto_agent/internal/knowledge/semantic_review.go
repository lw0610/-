package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// All semantic work uses one protocol. Batches cover every source chunk and
// every candidate pair, including non-adjacent route passages. Names select
// candidate context only; they never imply entity identity or route continuity.
func (p *Pipeline) reviewSemantics(ctx context.Context, r *Result, source string, contract Contract) error {
	system, err := stageSystem(contract, semanticReviewInstructions)
	if err != nil {
		return err
	}
	groups, err := p.planSemanticGroups(r, system)
	if err != nil {
		return err
	}
	calls := 0
	maxCalls := max(1, 2*len(r.Chunks))
	for i, ids := range groups {
		if err := ctx.Err(); err != nil {
			return err
		}
		record := SemanticReview{ID: fmt.Sprintf("s%04d", i+1), ChunkIDs: append([]string{}, ids...)}
		scope, err := makeSemanticScope(r, ids)
		if err == nil {
			record.SubjectRefs = sortedIDs(scope.ids)
			for _, s := range scope.sources {
				record.SourceRefs = append(record.SourceRefs, s.Ref)
			}
			if calls >= maxCalls {
				err = fmt.Errorf("本次语义核对已达%d次请求预算，该关联保留人工核对", maxCalls)
			} else if !p.semanticFits(system, scope.input) {
				err = fmt.Errorf("相关原文与完整候选超出当前核对预算，保留完整草稿供人工核对")
			}
		}
		if err == nil {
			input := scope.input
			for attempt := 1; attempt <= 2; attempt++ {
				if calls >= maxCalls || !p.semanticFits(system, input) {
					err = fmt.Errorf("语义核对修正请求超出预算，保留人工核对：%w", err)
					break
				}
				calls++
				raw, callErr := p.request(ctx, r, "semantic_review", record.ID, system, input)
				if callErr != nil {
					err = callErr
					break
				}
				out, validationErr := decodeModelResponse(raw, contract, scope.sources, true)
				var issues []Issue
				if validationErr == nil {
					issues, validationErr = validateSemanticChange(scope.graph, out, scope.issues, scope.sources)
				}
				if validationErr == nil {
					err = applySemanticScope(r, scope, out, issues)
					if err == nil {
						record.Changes = out.Changes
					}
					break
				}
				err = validationErr
				r.Attempts[len(r.Attempts)-1].Error = err.Error()
				if attempt == 2 {
					break
				}
				input = map[string]any{}
				for key, value := range scope.input {
					input[key] = value
				}
				input["validation_feedback"] = "上一次核对结果未被接受：" + err.Error() + "。请按 source_evidence.ref 和 changes 要求重新返回完整核对结果，保留有依据的对象。"
			}
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if err != nil {
			record.Error = err.Error()
			if len(r.Attempts) > 0 {
				a := &r.Attempts[len(r.Attempts)-1]
				if a.Stage == "semantic_review" && a.ID == record.ID {
					a.Error = err.Error()
				}
			}
		} else {
			record.Applied = true
		}
		r.SemanticReviews = append(r.SemanticReviews, record)
		if err := p.save(r); err != nil {
			return err
		}
	}
	r.Completion.SemanticAttempted = true
	return nil
}

func sortedIDs(ids map[string]bool) []string {
	out := []string{}
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
func (p *Pipeline) semanticFits(system string, input map[string]any) bool {
	raw, err := json.Marshal(input)
	if err != nil {
		return false
	}
	graph, _ := json.Marshal(input["graph"])
	// Reserve response space for changes and questions. The byte guard remains a
	// conservative estimate, not a claimed tokenizer measurement.
	if len(graph)+1024 > p.cfg.MaxOutputTokens {
		return false
	}
	return p.cfg.MaxContextTokens <= 0 || len(system)+len(raw)+p.cfg.MaxOutputTokens+requestReserve <= p.cfg.MaxContextTokens
}
func (p *Pipeline) planSemanticGroups(r *Result, system string) ([][]string, error) {
	all := []string{}
	for _, f := range r.Chunks {
		all = append(all, f.Chunk.ID)
	}
	full, err := makeSemanticScope(r, all)
	if err != nil {
		return nil, err
	}
	if p.semanticFits(system, full.input) {
		return [][]string{all}, nil
	}
	// First cover every fragment exactly once in the largest fitting base batches.
	groups := [][]string{}
	current := []string{}
	for _, id := range all {
		try := append(append([]string{}, current...), id)
		scope, e := makeSemanticScope(r, try)
		if len(current) > 0 && (e != nil || !p.semanticFits(system, scope.input)) {
			groups = append(groups, current)
			current = []string{id}
		} else {
			current = try
		}
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}
	covered := map[string]bool{}
	mark := func(group []string) {
		for i, a := range group {
			for _, b := range group[i+1:] {
				covered[unorderedPair(a, b)] = true
			}
		}
	}
	for _, g := range groups {
		mark(g)
	}
	// A name/alias/actor-reference index discovers related non-adjacent passages.
	// Consecutive occurrences of a key form a bounded context chain; unlike a
	// Cartesian product of all routes this grows linearly for a long same-army text.
	occurrences := map[string][]string{}
	hasQuestions := map[string]bool{}
	for _, fragment := range r.Chunks {
		keys := map[string]bool{}
		index, _ := indexGraph(fragment.Graph)
		for _, collection := range []string{"actors", "spatial_entities", "events"} {
			for _, raw := range array(fragment.Graph[collection]) {
				obj := raw.(map[string]any)
				names := append([]any{obj["name"]}, array(obj["aliases"])...)
				for _, name := range names {
					if text := strings.ToLower(strings.TrimSpace(stringValue(name))); text != "" {
						keys[collection+":"+text] = true
					}
				}
			}
		}
		for _, obj := range index {
			if obj.kind == "paths" {
				for _, ref := range array(obj.data["actor_refs"]) {
					actor := index[stringValue(ref)]
					if text := strings.ToLower(strings.TrimSpace(stringValue(actor.data["name"]))); text != "" {
						keys["actors:"+text] = true
					}
				}
			}
		}
		for key := range keys {
			occurrences[key] = append(occurrences[key], fragment.Chunk.ID)
		}
		hasQuestions[fragment.Chunk.ID] = len(fragment.Unresolved) > 0
	}
	pairs := map[string][]string{}
	for _, ids := range occurrences {
		for i := 1; i < len(ids); i++ {
			pair := []string{ids[i-1], ids[i]}
			pairs[unorderedPair(pair[0], pair[1])] = pair
		}
	}
	// Genuine unresolved references receive adjacent context as well; the global
	// index remains visible and unresolved facts remain human-review questions.
	for i, id := range all {
		if !hasQuestions[id] {
			continue
		}
		if i > 0 {
			pairs[unorderedPair(all[i-1], id)] = []string{all[i-1], id}
		}
		if i+1 < len(all) {
			pairs[unorderedPair(id, all[i+1])] = []string{id, all[i+1]}
		}
	}
	pairKeys := []string{}
	for key := range pairs {
		pairKeys = append(pairKeys, key)
	}
	sort.Strings(pairKeys)
	for _, key := range pairKeys {
		if covered[key] {
			continue
		}
		pair := pairs[key]
		added := false
		// Coalesce related candidates into an existing fitting batch instead of
		// spending one model request per name occurrence.
		for i, g := range groups {
			overlap := containsString(g, pair[0]) || containsString(g, pair[1])
			if !overlap {
				continue
			}
			try := append([]string{}, g...)
			for _, id := range pair {
				if !containsString(try, id) {
					try = append(try, id)
				}
			}
			sort.Strings(try)
			scope, e := makeSemanticScope(r, try)
			if e == nil && p.semanticFits(system, scope.input) {
				groups[i] = try
				mark(try)
				added = true
				break
			}
		}
		if !added {
			groups = append(groups, pair)
			mark(pair)
		}
	}
	return groups, nil
}

func makeSemanticScope(r *Result, chunkIDs []string) (semanticScope, error) {
	s := semanticScope{ids: map[string]bool{}}
	wanted := map[string]bool{}
	for _, id := range chunkIDs {
		wanted[id] = true
	}
	index, err := indexGraph(r.Graph)
	if err != nil {
		return s, err
	}
	evidenceChunk := map[string]string{}
	for _, f := range r.Chunks {
		for _, source := range sourceSentences(f.Chunk) {
			for id, obj := range index {
				if obj.kind == "evidence" {
					start, a := evidenceOffset(obj.data["start"])
					end, b := evidenceOffset(obj.data["end"])
					if a && b && start >= *source.Start && end <= *source.End {
						evidenceChunk[id] = f.Chunk.ID
					}
				}
			}
		}
	}
	for id, obj := range index {
		include := false
		for _, ref := range array(obj.data["evidence_refs"]) {
			include = include || wanted[evidenceChunk[stringValue(ref)]]
		}
		for _, c := range chunkIDs {
			include = include || strings.HasPrefix(id, c+"__")
		}
		if include && obj.kind != "evidence" {
			s.ids[id] = true
			if obj.owner != nil {
				s.ids[stringValue(obj.owner["route_id"])] = true
			}
		}
	}
	// Complete the graph-reference closure and include source chunks backing any
	// substituted/merged objects. No dangling hidden references enter the model.
	for changed := true; changed; {
		changed = false
		for id := range s.ids {
			obj := index[id]
			if obj.kind == "routes" {
				for _, raw := range array(obj.data["paths"]) {
					pid := stringValue(raw.(map[string]any)["path_id"])
					if !s.ids[pid] {
						s.ids[pid] = true
						changed = true
					}
				}
			}
			var visit func(any)
			visit = func(v any) {
				switch node := v.(type) {
				case map[string]any:
					for key, x := range node {
						if graphReferenceKeys[key] {
							values := []any{x}
							if a, ok := x.([]any); ok {
								values = a
							}
							for _, value := range values {
								ref := stringValue(value)
								if _, ok := index[ref]; ok && !s.ids[ref] {
									s.ids[ref] = true
									changed = true
								}
							}
						} else {
							visit(x)
						}
					}
				case []any:
					for _, x := range node {
						visit(x)
					}
				}
			}
			visit(obj.data)
		}
	}
	for id := range s.ids {
		if c := evidenceChunk[id]; c != "" {
			wanted[c] = true
		}
	}
	for _, f := range r.Chunks {
		if wanted[f.Chunk.ID] {
			s.chunks = append(s.chunks, f.Chunk)
			s.sources = append(s.sources, sourceSentences(f.Chunk)...)
		}
	}
	s.graph = Graph{}
	for key, value := range r.Graph {
		if key == "task" || key == "schema_version" || key == "output_constraints" {
			s.graph[key] = value
		}
	}
	for _, col := range graphCollections {
		rows := []any{}
		for _, raw := range array(r.Graph[col.key]) {
			obj := raw.(map[string]any)
			if s.ids[stringValue(obj[col.id])] {
				rows = append(rows, obj)
			}
		}
		s.graph[col.key] = rows
	}
	for _, issue := range r.Issues {
		inScope := len(issue.SubjectRefs) > 0
		for _, ref := range issue.SubjectRefs {
			inScope = inScope && s.ids[ref]
		}
		if inScope {
			s.issues = append(s.issues, issue)
		}
	}
	view, err := graphModelView(s.graph, s.sources)
	if err != nil {
		return s, err
	}
	// The selected source groups already carry the full facts needed for a
	// decision. Only similarly named outside objects are useful discovery hints;
	// sending every object in a long document can crowd out the actual evidence.
	keys := map[string]bool{}
	for _, col := range graphCollections {
		if col.key == "evidence" || col.key == "point_presentations" || col.key == "callouts" {
			continue
		}
		for _, raw := range array(s.graph[col.key]) {
			obj := raw.(map[string]any)
			for _, name := range append([]any{obj["name"]}, array(obj["aliases"])...) {
				if value := strings.ToLower(strings.TrimSpace(stringValue(name))); value != "" {
					keys[col.key+":"+value] = true
				}
			}
		}
	}
	candidates := []any{}
	for _, col := range graphCollections {
		if col.key == "evidence" || col.key == "point_presentations" || col.key == "callouts" {
			continue
		}
		for _, raw := range array(r.Graph[col.key]) {
			obj := raw.(map[string]any)
			id := stringValue(obj[col.id])
			if s.ids[id] {
				continue
			}
			related := false
			for _, name := range append([]any{obj["name"]}, array(obj["aliases"])...) {
				if value := strings.ToLower(strings.TrimSpace(stringValue(name))); value != "" && keys[col.key+":"+value] {
					related = true
				}
			}
			if !related {
				continue
			}
			candidates = append(candidates, map[string]any{"id": id, "kind": col.key, "name": obj["name"], "aliases": obj["aliases"]})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i].(map[string]any), candidates[j].(map[string]any)
		return stringValue(left["kind"])+":"+stringValue(left["id"]) < stringValue(right["kind"])+":"+stringValue(right["id"])
	})
	if len(candidates) > 64 {
		candidates = candidates[:64]
	}
	s.input = map[string]any{"graph": view, "source_evidence": modelEvidenceSources(s.sources), "unresolved": semanticIssueView(s.issues, s.sources), "global_candidates": candidates}
	return s, nil
}
