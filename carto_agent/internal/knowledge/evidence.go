package knowledge

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type evidenceAnchor struct {
	Quote      string
	Start, End int // Unicode code points in the original, unmodified source.
	Unique     bool
}

// anchorEvidence prefers literal evidence. The fallback only normalizes
// whitespace and maps the match back to an exact source slice; it never fixes
// words, dates or punctuation and never chooses among ambiguous fallback spans.
func anchorEvidence(source, quote string) (evidenceAnchor, error) {
	if strings.TrimSpace(quote) == "" {
		return evidenceAnchor{}, errors.New("证据不能为空")
	}
	if first, count := evidenceMatches(source, quote); count > 0 {
		if count > 1 {
			return evidenceAnchor{Quote: quote, Start: -1, End: -1}, nil
		}
		start := utf8.RuneCountInString(source[:first])
		return evidenceAnchor{Quote: quote, Start: start, End: start + utf8.RuneCountInString(quote), Unique: true}, nil
	}
	normalizedSource := normalizeEvidenceWhitespace(source)
	normalizedQuote := normalizeEvidenceWhitespace(quote)
	if normalizedQuote.text == "" {
		return evidenceAnchor{}, errors.New("证据不能为空")
	}
	first, count := evidenceMatches(normalizedSource.text, normalizedQuote.text)
	if count == 0 {
		return evidenceAnchor{}, errors.New("证据不在原文中；仅容许空白差异，不容许改字、改日期或拼接不连续内容")
	}
	if count > 1 {
		return evidenceAnchor{}, errors.New("证据忽略空白后匹配到多个原文位置，请提供更完整的原文引用")
	}
	i := utf8.RuneCountInString(normalizedSource.text[:first])
	j := i + utf8.RuneCountInString(normalizedQuote.text)
	start, end := normalizedSource.starts[i], normalizedSource.ends[j-1]
	runes := []rune(source)
	return evidenceAnchor{Quote: string(runes[start:end]), Start: start, End: end, Unique: true}, nil
}

func anchorEvidenceQuotes(source string, quotes []string) ([]string, error) {
	if len(quotes) == 0 {
		return nil, errors.New("缺少原文证据引用")
	}
	anchored := make([]string, len(quotes))
	for i, quote := range quotes {
		anchor, err := anchorEvidence(source, quote)
		if err != nil {
			return nil, err
		}
		anchored[i] = anchor.Quote
	}
	return anchored, nil
}

// Stop after two matches, including overlapping ones: only uniqueness matters.
func evidenceMatches(source, quote string) (first, count int) {
	first = -1
	if quote == "" {
		return first, 0
	}
	_, advance := utf8.DecodeRuneInString(quote)
	for offset := 0; offset <= len(source)-len(quote); {
		index := strings.Index(source[offset:], quote)
		if index < 0 {
			break
		}
		index += offset
		if first < 0 {
			first = index
		}
		count++
		if count == 2 {
			break
		}
		offset = index + advance
	}
	return first, count
}

type evidenceText struct {
	text         string
	starts, ends []int
}

func normalizeEvidenceWhitespace(text string) evidenceText {
	runes := []rune(text)
	var normalized []rune
	out := evidenceText{}
	appendRune := func(r rune, start, end int) {
		normalized = append(normalized, r)
		out.starts, out.ends = append(out.starts, start), append(out.ends, end)
	}
	for i := 0; i < len(runes); {
		if !unicode.IsSpace(runes[i]) {
			appendRune(runes[i], i, i+1)
			i++
			continue
		}
		end := i + 1
		for end < len(runes) && unicode.IsSpace(runes[end]) {
			end++
		}
		// Chinese line wrapping may disappear altogether. Between Latin words
		// or numbers retain a separator: New York != NewYork, and 1 2 != 12.
		if i > 0 && end < len(runes) && !chineseWhitespaceBoundary(runes[i-1], runes[end]) {
			appendRune(' ', i, end)
		}
		i = end
	}
	out.text = string(normalized)
	return out
}

func chineseWhitespaceBoundary(left, right rune) bool {
	return unicode.Is(unicode.Han, left) || unicode.Is(unicode.Han, right) ||
		strings.ContainsRune("。，、；：！？", left) || strings.ContainsRune("。，、；：！？", right)
}

type EvidenceSource struct {
	Ref        string   `json:"ref"`
	Text       string   `json:"text,omitempty"`
	SourceRefs []string `json:"source_refs,omitempty"`
	ChunkID    string   `json:"chunk_id,omitempty"`
	Start      *int     `json:"start,omitempty"`
	End        *int     `json:"end,omitempty"`
}

func sourceSentences(chunk Chunk) []EvidenceSource {
	runes := []rune(chunk.Text)
	var spans []EvidenceSource
	start := 0
	chunkID := chunk.ID
	if chunkID == "" {
		chunkID = "source"
	}
	nextRef := func() string { return fmt.Sprintf("%s:s%03d", chunkID, len(spans)+1) }
	for i, r := range runes {
		if !sentenceEnd(r) && i+1 != len(runes) {
			continue
		}
		end := i + 1
		text := string(runes[start:end])
		// Keep whitespace-only spans with the following sentence, or the last
		// sentence at EOF; never invent words, normalize or discard source text.
		if strings.TrimSpace(text) == "" {
			if end == len(runes) && len(spans) > 0 {
				last := &spans[len(spans)-1]
				last.Text += text
				globalEnd := chunk.Start + end
				last.End = &globalEnd
			} else if end == len(runes) {
				globalStart, globalEnd := chunk.Start, chunk.Start+end
				spans = append(spans, EvidenceSource{Ref: nextRef(), Text: text, ChunkID: chunk.ID, Start: &globalStart, End: &globalEnd})
			}
			continue
		}
		globalStart, globalEnd := chunk.Start+start, chunk.Start+end
		spans = append(spans, EvidenceSource{Ref: nextRef(), Text: text, ChunkID: chunk.ID, Start: &globalStart, End: &globalEnd})
		start = end
	}
	return spans
}

// The model sees only stable sentence labels and text. Character offsets stay
// inside the program and are attached to evidence after the response is checked.
func modelEvidenceSources(sources []EvidenceSource) []map[string]string {
	out := make([]map[string]string, 0, len(sources))
	for _, source := range sources {
		out = append(out, map[string]string{"ref": source.Ref, "text": source.Text})
	}
	return out
}

func sourceIndex(sources []EvidenceSource) map[string]EvidenceSource {
	out := map[string]EvidenceSource{}
	for _, s := range sources {
		out[s.Ref] = s
	}
	return out
}

func sourceQuotes(refs []string, sources []EvidenceSource) ([]string, error) {
	index := sourceIndex(sources)
	quotes := []string{}
	seen := map[string]bool{}
	if len(refs) == 0 {
		return nil, fmt.Errorf("缺少原文编号")
	}
	for _, ref := range refs {
		s, ok := index[ref]
		if !ok || seen[ref] || strings.TrimSpace(s.Text) == "" {
			return nil, fmt.Errorf("原文编号未知、重复或为空: %s", ref)
		}
		seen[ref] = true
		quotes = append(quotes, s.Text)
	}
	return quotes, nil
}

// Materialize program-authored evidence once. Models only name source refs.
func materializeEvidence(g Graph, sources []EvidenceSource) error {
	index := sourceIndex(sources)
	used := map[string]bool{}
	var visit func(any) error
	visit = func(v any) error {
		switch node := v.(type) {
		case map[string]any:
			for key, value := range node {
				if key == "evidence_refs" {
					refs, ok := value.([]any)
					if !ok || len(refs) == 0 {
						return fmt.Errorf("evidence_refs 必须是非空原文编号数组")
					}
					local := map[string]bool{}
					for _, raw := range refs {
						ref, ok := raw.(string)
						s, exists := index[ref]
						if !ok || !exists || local[ref] || strings.TrimSpace(s.Text) == "" {
							return fmt.Errorf("无效或重复原文编号: %v", raw)
						}
						used[ref] = true
						local[ref] = true
					}
				} else if err := visit(value); err != nil {
					return err
				}
			}
		case []any:
			for _, x := range node {
				if err := visit(x); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if _, exists := g["evidence"]; exists {
		return fmt.Errorf("模型不应生成 evidence，事实 evidence_refs 直接引用原文编号")
	}
	if err := visit(map[string]any(g)); err != nil {
		return err
	}
	rows := []any{}
	for _, s := range sources {
		if used[s.Ref] {
			rows = append(rows, map[string]any{"evidence_id": s.Ref, "quote": s.Text, "start": *s.Start, "end": *s.End})
		}
	}
	g["evidence"] = rows
	return nil
}

func evidenceSourceRefs(evidence map[string]any, sources []EvidenceSource) ([]string, error) {
	start, sOK := evidenceOffset(evidence["start"])
	end, eOK := evidenceOffset(evidence["end"])
	quote := stringValue(evidence["quote"])
	refs := []string{}
	for _, s := range sources {
		if s.Start == nil || s.End == nil {
			continue
		}
		if sOK && eOK {
			if *s.Start >= start && *s.End <= end {
				refs = append(refs, s.Ref)
			}
		} else if s.Text == quote {
			refs = append(refs, s.Ref)
		}
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("证据 %s 不在核对原文范围内", evidence["evidence_id"])
	}
	return refs, nil
}

func graphModelView(g Graph, sources []EvidenceSource) (Graph, error) {
	out, err := cloneGraph(g)
	if err != nil {
		return nil, err
	}
	refs := map[string][]string{}
	for _, raw := range array(out["evidence"]) {
		e := raw.(map[string]any)
		ids, err := evidenceSourceRefs(e, sources)
		if err != nil {
			return nil, err
		}
		refs[stringValue(e["evidence_id"])] = ids
	}
	delete(out, "evidence")
	var visit func(any) error
	visit = func(v any) error {
		switch node := v.(type) {
		case map[string]any:
			for k, x := range node {
				if k == "evidence_refs" {
					ids := []any{}
					seen := map[string]bool{}
					for _, raw := range array(x) {
						known := refs[stringValue(raw)]
						if len(known) == 0 {
							return fmt.Errorf("证据引用 %s 不存在", raw)
						}
						for _, id := range known {
							if !seen[id] {
								ids = append(ids, id)
								seen[id] = true
							}
						}
					}
					node[k] = ids
				} else if err := visit(x); err != nil {
					return err
				}
			}
		case []any:
			for _, x := range node {
				if err := visit(x); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(map[string]any(out)); err != nil {
		return nil, err
	}
	for _, raw := range array(out["routes"]) {
		r := raw.(map[string]any)
		delete(r, "topology")
		delete(r, "style_ref")
		for _, p := range array(r["paths"]) {
			path := p.(map[string]any)
			delete(path, "direction")
			delete(path, "control_options")
			for _, rawConstraint := range array(path["spatial_constraints"]) {
				constraint, _ := rawConstraint.(map[string]any)
				if reference, ok := constraint["reference"].(map[string]any); ok {
					for _, field := range []string{"lon", "lat", "coordinate_source", "coordinate_confidence", "geocoding"} {
						delete(reference, field)
					}
				}
			}
		}
	}
	return out, nil
}

func evidenceOffset(value any) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, true
	case float64:
		return int(n), n == float64(int(n))
	}
	return 0, false
}
