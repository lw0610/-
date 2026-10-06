package knowledge

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Chunk offsets count Unicode code points, matching Python's source_text[start:end].
// Text is never trimmed: concatenating the chunks reproduces the original source.
type Chunk struct {
	ID      string `json:"id"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Text    string `json:"text"`
	HardCut bool   `json:"hard_cut"`
}

func sentenceEnd(r rune) bool { return strings.ContainsRune("。！？!?；;\n", r) }

func SplitText(source string, target, maximum int) []Chunk {
	if target <= 0 {
		target = 1000
	}
	if maximum < target {
		maximum = target + target/2
	}
	return splitTextByCost(source, target, maximum, func(rune) int { return 1 })
}

// Match encoding/json's escaped string cost (without the surrounding quotes).
// This includes Chinese/emoji and JSON escapes; it is not a model tokenizer.
func jsonRuneCost(r rune) int {
	switch r {
	case '"', '\\', '\b', '\f', '\n', '\r', '\t':
		return 2
	case '<', '>', '&', '\u2028', '\u2029':
		return 6
	}
	if r < 0x20 {
		return 6
	}
	return utf8.RuneLen(r)
}

func sourceJSONCost(source string) int {
	cost := 0
	for _, r := range source {
		cost += jsonRuneCost(r)
	}
	return cost
}

func splitTextByBudget(source string, budget int) []Chunk {
	budget = max(1, budget)
	return splitTextByCost(source, max(1, budget-budget/4), budget, jsonRuneCost)
}

func splitTextByCost(source string, target, maximum int, weight func(rune) int) []Chunk {
	runes := []rune(source)
	prefix := make([]int, len(runes)+1)
	for i, r := range runes {
		prefix[i+1] = prefix[i] + weight(r)
	}
	var chunks []Chunk
	for start := 0; start < len(runes); {
		end := len(runes)
		hard := false
		if prefix[end]-prefix[start] > maximum {
			end = start
			for end < len(runes) && prefix[end+1]-prefix[start] <= maximum {
				end++
			}
			// A budget smaller than one code point cannot produce an empty chunk.
			if end == start {
				end++
			}
			best, distance := -1, maximum
			for i := start; i < end; i++ {
				if sentenceEnd(runes[i]) {
					d := prefix[i+1] - prefix[start] - target
					if d < 0 {
						d = -d
					}
					if d < distance {
						best, distance = i+1, d
					}
				}
			}
			if best > start {
				end = best
			} else {
				hard = true
			}
		}
		chunks = append(chunks, Chunk{Start: start, End: end, Text: string(runes[start:end]), HardCut: hard})
		start = end
	}
	return chunks
}

// Used only after a truncated response. A bounded retry uses smaller source spans,
// rather than accepting a syntactically valid but incomplete JSON response.
func bisectChunk(c Chunk) (Chunk, Chunk) {
	runes := []rune(c.Text)
	mid, best, distance := len(runes)/2, -1, len(runes)
	for i := len(runes) / 4; i < len(runes)*3/4; i++ {
		if sentenceEnd(runes[i]) {
			d := i + 1 - mid
			if d < 0 {
				d = -d
			}
			if d < distance {
				best, distance = i+1, d
			}
		}
	}
	hard := best < 0
	if !hard {
		mid = best
	}
	return Chunk{ID: c.ID + "a", Start: c.Start, End: c.Start + mid, Text: string(runes[:mid]), HardCut: hard},
		Chunk{ID: c.ID + "b", Start: c.Start + mid, End: c.End, Text: string(runes[mid:]), HardCut: c.HardCut}
}

const (
	requestReserve = 512 // Message framing and estimation uncertainty.
	retryReserve   = 512 // Error feedback on a structured-output retry.
	// A planning estimate, not an output cap or a guarantee about model behavior.
	// Dense graph JSON can expand beyond this; length failures still split/retry.
	outputExpansionEstimate = 8
)

// Budget is persisted alongside extraction results so model limits and chunk
// planning are visible. Without a provider-specific tokenizer, UTF-8/JSON bytes
// are a conservative proxy, not an exact token count for an arbitrary model.
type Budget struct {
	Mode             string `json:"mode"`
	Estimation       string `json:"estimation"`
	MaxOutputTokens  int    `json:"max_output_tokens"`
	MaxContextTokens int    `json:"max_context_tokens"`
	FixedInput       int    `json:"fixed_input_estimate"`
	SourceBudget     int    `json:"source_budget_estimate"`
	OutputExpansion  int    `json:"output_expansion_estimate"`
	LimitingFactor   string `json:"limiting_factor"`
}

func planBudget(cfg Config, source string, contract Contract) (Budget, error) {
	system, err := extractionSystem(contract)
	if err != nil {
		return Budget{}, err
	}
	emptyInput, _ := json.Marshal(fragmentSourceInput(Chunk{ID: "c0001"}))
	b := Budget{
		Mode: "model_settings", Estimation: "conservative_utf8_json_bytes",
		MaxOutputTokens: cfg.MaxOutputTokens, MaxContextTokens: cfg.MaxContextTokens,
		FixedInput:      len(system) + len(emptyInput) + retryReserve,
		SourceBudget:    max(1, cfg.MaxOutputTokens/outputExpansionEstimate),
		OutputExpansion: outputExpansionEstimate, LimitingFactor: "output_budget",
	}
	if cfg.TargetChars > 0 {
		b.Mode = "explicit_character_limits"
	}
	if cfg.MaxContextTokens > 0 {
		room := cfg.MaxContextTokens - cfg.MaxOutputTokens - requestReserve
		if room <= b.FixedInput {
			return b, fmt.Errorf("模型设置无法容纳抽取请求：上下文 Token=%d，最大输出=%d，固定提示及重试保守估算=%d，额外预留=%d；请核对该模型的真实上下文容量或调整最大输出，未自动降低输出设置", cfg.MaxContextTokens, cfg.MaxOutputTokens, b.FixedInput, requestReserve)
		}
		// Numbered evidence has real serialization overhead, especially for
		// dense short sentences. Measure this document's wire expansion instead
		// of assuming the request still contains only chunk.text.
		wire, _ := json.Marshal(fragmentSourceInput(Chunk{ID: "c0001", Text: source, End: len([]rune(source))}))
		cost := max(1, sourceJSONCost(source))
		wireExpansion := max(1, (len(wire)-len(emptyInput)+cost-1)/cost)
		if available := (room - b.FixedInput) / wireExpansion; available < b.SourceBudget {
			b.SourceBudget, b.LimitingFactor = available, "context_budget"
		}
	}
	return b, nil
}
