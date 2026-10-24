package flow

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

type CapabilitySpec struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords"`
	PackageIDs  []string `json:"package_ids"`
	Parameters  []string `json:"parameters,omitempty"`
}

type PackageSpec struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Tools       []string `json:"tools"`
	Enabled     bool     `json:"enabled"`
}

// searchTokens 对连续中文取字符二元组，其他字母/数字按完整词检索。
func searchTokens(text string) []string {
	var tokens []string
	var chinese []rune
	var word []rune
	flushChinese := func() {
		if len(chinese) == 1 {
			tokens = append(tokens, string(chinese))
		}
		for i := 1; i < len(chinese); i++ {
			tokens = append(tokens, string(chinese[i-1:i+1]))
		}
		chinese = nil
	}
	flushWord := func() {
		if len(word) > 0 {
			tokens = append(tokens, string(word))
		}
		word = nil
	}
	for _, character := range strings.ToLower(text) {
		if unicode.Is(unicode.Han, character) {
			flushWord()
			chinese = append(chinese, character)
		} else {
			flushChinese()
			if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' {
				word = append(word, character)
			} else {
				flushWord()
			}
		}
	}
	flushChinese()
	flushWord()
	return tokens
}

// capabilityIndex 是能力目录的 BM25 统计；同一流程的各步骤共用一份，避免逐步重复分词。
type capabilityIndex struct {
	catalog           []CapabilitySpec
	frequencies       []map[string]int
	lengths           []int
	documentFrequency map[string]int
	average           float64
}

func newCapabilityIndex(catalog []CapabilitySpec) *capabilityIndex {
	index := &capabilityIndex{catalog: catalog, frequencies: make([]map[string]int, len(catalog)), lengths: make([]int, len(catalog)), documentFrequency: map[string]int{}}
	totalLength := 0.0
	for position, capability := range catalog {
		words := searchTokens(capability.DisplayName + " " + capability.Description + " " + strings.Join(capability.Keywords, " "))
		index.frequencies[position] = map[string]int{}
		for _, word := range words {
			index.frequencies[position][word]++
		}
		for word := range index.frequencies[position] {
			index.documentFrequency[word]++
		}
		index.lengths[position] = len(words)
		totalLength += float64(len(words))
	}
	index.average = totalLength / math.Max(1, float64(len(catalog)))
	if index.average == 0 {
		index.average = 1
	}
	return index
}

// CandidateCapabilities 总是包含角色已授权工具，再加其他包相关度最高的工具。
func CandidateCapabilities(query string, roleTools []string, catalog []CapabilitySpec, limit int) []CapabilitySpec {
	return newCapabilityIndex(catalog).candidates(query, roleTools, limit)
}

func (x *capabilityIndex) candidates(query string, roleTools []string, limit int) []CapabilitySpec {
	catalog := x.catalog
	owned := make(map[string]bool, len(roleTools))
	for _, name := range roleTools {
		owned[name] = true
	}
	// 固定求和顺序：同样的输入总得到同样的分数与候选，增强内容键因此稳定。
	queryWords := []string{}
	seenWords := map[string]bool{}
	for _, word := range searchTokens(query) {
		if !seenWords[word] {
			seenWords[word] = true
			queryWords = append(queryWords, word)
		}
	}
	sort.Strings(queryWords)
	type scored struct {
		index int
		score float64
	}
	var additional []scored
	var result []CapabilitySpec
	seen := map[string]bool{}
	for index, capability := range catalog {
		if seen[capability.Name] {
			continue
		}
		seen[capability.Name] = true
		if owned[capability.Name] {
			result = append(result, capability)
			continue
		}
		score := 0.0
		for _, word := range queryWords {
			frequency := float64(x.frequencies[index][word])
			if frequency == 0 {
				continue
			}
			df := float64(x.documentFrequency[word])
			idf := math.Log(1 + (float64(len(catalog))-df+0.5)/(df+0.5))
			score += idf * frequency * 2.2 / (frequency + 1.2*(0.25+0.75*float64(x.lengths[index])/x.average))
		}
		if score > 0 {
			additional = append(additional, scored{index, score})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	sort.Slice(additional, func(i, j int) bool {
		if additional[i].score == additional[j].score {
			return catalog[additional[i].index].Name < catalog[additional[j].index].Name
		}
		return additional[i].score > additional[j].score
	})
	if limit < 0 {
		limit = 0
	}
	if len(additional) > limit {
		additional = additional[:limit]
	}
	for _, match := range additional {
		result = append(result, catalog[match.index])
	}
	return result
}
