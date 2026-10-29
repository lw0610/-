package layers

import (
	"fmt"
	"math"
	"sort"
)

type Requirement struct {
	Name         string
	ExplicitName string
	Category     string
	Params       map[string]string
	Geometry     string
	Fields       []string
	Extent       []float64
	Scale        int
}
type Candidate struct {
	Layer             Item     `json:"layer"`
	CoverageRatio     float64  `json:"coverage_ratio"`
	Reasons           []string `json:"reasons"`
	NeedsConfirmation bool     `json:"needs_confirmation"`
	rank              int
	scale             bool
}

func CoverageRatio(coverage, extent []float64) float64 {
	if len(extent) != 4 {
		return 1
	}
	if len(coverage) != 4 {
		return 0
	}
	area := (extent[2] - extent[0]) * (extent[3] - extent[1])
	if area <= 0 {
		return 0
	}
	w := math.Max(0, math.Min(coverage[2], extent[2])-math.Max(coverage[0], extent[0]))
	h := math.Max(0, math.Min(coverage[3], extent[3])-math.Max(coverage[1], extent[1]))
	return w * h / area
}
func (l *Library) Match(r Requirement) ([]Candidate, error) {
	items, e := l.List()
	if e != nil {
		return nil, e
	}
	return Match(items, r), nil
}
func Match(items []Item, r Requirement) []Candidate {
	out := []Candidate{}
	explicit, explicitErr := ResolveName(items, r.ExplicitName)
	named, nameErr := ResolveName(items, r.Name)
	for _, i := range items {
		if i.Status != "ready" {
			continue
		}
		if r.Geometry != "" && r.Geometry != i.Geometry {
			continue
		}
		ratio := CoverageRatio(i.Coverage.BBox, r.Extent)
		if ratio < .9 {
			continue
		}
		fieldsOK := true
		for _, field := range r.Fields {
			if i.Fields[field] == "" {
				present := false
				for _, actual := range i.Fields {
					if actual == field {
						present = true
					}
				}
				if !present {
					fieldsOK = false
				}
			}
		}
		if !fieldsOK {
			continue
		}
		rank := 3
		reason := "类别与参数匹配"
		if r.Category == i.Category {
			rank = 2
			for k, v := range r.Params {
				if i.Params[k] != v {
					rank = 3
				}
			}
		}
		if nameErr == nil && named.ID == i.ID {
			rank = 1
			reason = "名称匹配"
		}
		if r.ExplicitName != "" && explicitErr == nil && explicit.ID == i.ID {
			rank = 0
			reason = "流程点名"
		}
		if rank == 3 {
			continue
		}
		c := Candidate{Layer: i, CoverageRatio: ratio, Reasons: []string{reason}, rank: rank, scale: r.Scale == 0 || ((i.Scale.BestFrom > 0 || i.Scale.BestTo > 0) && i.Scale.BestFrom <= r.Scale && (i.Scale.BestTo == 0 || r.Scale <= i.Scale.BestTo))}
		if len(r.Extent) == 0 {
			c.Reasons = append(c.Reasons, "范围将在执行前复核")
		}
		out = append(out, c)
	}
	if r.ExplicitName != "" {
		matched := false
		for _, candidate := range out {
			matched = matched || candidate.rank == 0
		}
		if !matched {
			for n := range out {
				out[n].NeedsConfirmation = true
				out[n].Reasons = append(out[n].Reasons, fmt.Sprintf("流程点名的「%s」不满足本次数据要求，建议改用「%s」", r.ExplicitName, out[n].Layer.Name))
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.Layer.Scope != b.Layer.Scope {
			return a.Layer.Scope == "user"
		}
		if a.scale != b.scale {
			return a.scale
		}
		if a.CoverageRatio != b.CoverageRatio {
			return a.CoverageRatio > b.CoverageRatio
		}
		return a.Layer.ID < b.Layer.ID
	})
	return out
}
