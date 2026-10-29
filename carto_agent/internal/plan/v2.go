package plan

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/autocarto/carto_agent/internal/flow"
)

// Need describes the result, independently of the provider used to acquire it.
type Need struct {
	Category    string         `json:"category,omitempty"`
	Params      map[string]any `json:"params,omitempty"`
	Fields      []string       `json:"fields,omitempty"`
	Description string         `json:"description,omitempty"`
	Region      string         `json:"region,omitempty"`
	Level       string         `json:"level,omitempty"`
	Year        int            `json:"year,omitempty"`
	Indicator   string         `json:"indicator,omitempty"`
	Caliber     string         `json:"caliber,omitempty"`
	Unit        string         `json:"unit,omitempty"`
	Exclude     []string       `json:"exclude,omitempty"`
}
type Extent struct {
	Region string    `json:"region,omitempty"`
	BBox   []float64 `json:"bbox,omitempty"`
	From   string    `json:"from,omitempty"`
	Expand float64   `json:"expand,omitempty"`
}
type Product struct {
	ID       string   `json:"id"`
	Name     string   `json:"name,omitempty"`
	Kind     string   `json:"kind"`
	Format   string   `json:"format,omitempty"`
	Geometry string   `json:"geometry,omitempty"`
	Fields   []string `json:"fields,omitempty"`
}

func (p Product) Declaration() flow.OutputDeclaration {
	kind := p.Kind
	if kind == "layer" {
		kind = "layers"
	}

	return flow.OutputDeclaration{Label: p.ID, Type: kind, Format: p.Format, Required: true, Geometry: p.Geometry, Fields: p.Fields}
}

// v1 is decoded only to display saved plans and finish an already running job.
// The v2 wire format exposes guide IDs and output IDs, not execution metadata.
func (p *Plan) UnmarshalJSON(b []byte) error {
	type plain Plan
	var wire struct {
		*plain
		Guide        json.RawMessage `json:"guide"`
		Deliverables json.RawMessage `json:"deliverables"`
	}
	wire.plain = (*plain)(p)
	if err := json.Unmarshal(b, &wire); err != nil {
		return err
	}
	if len(wire.Guide) > 0 && string(wire.Guide) != "null" {
		if wire.Guide[0] == '"' {
			var id string
			if err := json.Unmarshal(wire.Guide, &id); err != nil {
				return err
			}
			p.Guide = &GuideRef{ID: id}
		} else if err := json.Unmarshal(wire.Guide, &p.Guide); err != nil {
			return err
		}
	}
	if len(wire.Deliverables) > 0 {
		var ids []string
		if err := json.Unmarshal(wire.Deliverables, &ids); err == nil {
			p.Deliverables = nil
			for _, id := range ids {
				p.Deliverables = append(p.Deliverables, flow.OutputDeclaration{Label: id})
			}
		} else if err := json.Unmarshal(wire.Deliverables, &p.Deliverables); err != nil {
			return fmt.Errorf("deliverables 应为产出 ID 数组: %w", err)
		}
	}
	return nil
}
func (p Plan) MarshalJSON() ([]byte, error) {
	type plain Plan
	if p.APIVersion != APIVersion {
		return json.Marshal(plain(p))
	}
	ids := make([]string, 0, len(p.Deliverables))
	for _, d := range p.Deliverables {
		ids = append(ids, d.Label)
	}
	guide := ""
	if p.Guide != nil {
		guide = p.Guide.ID
	}
	return json.Marshal(struct {
		plain
		Guide        string   `json:"guide,omitempty"`
		Deliverables []string `json:"deliverables"`
	}{plain: plain(p), Guide: guide, Deliverables: ids})
}
func (p *Plan) ProductByID(id string) (Product, string, bool) {
	for _, s := range p.Steps {
		for _, o := range s.Produces {
			if o.ID == id {
				return o, s.ID, true
			}
		}
	}
	return Product{}, "", false
}

func ValidateV2(p *Plan) []flow.Diagnostic {
	var out []flow.Diagnostic
	add := func(code, step, message string) {
		out = append(out, flow.Diagnostic{Level: "error", Code: code, Step: step, Message: message, Fix: map[string]any{"example": "inputs: [src, route]; extent: {from: route, expand: 0.2}; produces: [{id: route, kind: dataset, format: route_dataset}]"}})
	}
	if strings.TrimSpace(p.Goal) == "" || len(p.Steps) == 0 {
		add("PLAN_SCHEMA_INVALID", "", "请提供目标和至少一个执行步骤")
	}
	at := map[string]int{}
	for _, d := range p.Data {
		at[d.ID] = -1
	}
	for i, s := range p.Steps {
		for _, o := range s.Produces {
			at[o.ID] = i
		}
	}
	for _, d := range p.Data {
		if d.Source == "retrieve" && (d.Need == nil || d.Need.Category == "") {
			add("PLAN_NEED_MISSING", "", d.Name+" 尚不能确定需要什么数据，请填写 need.category")
		}
		if e := d.Extent; e != nil {
			if e.From != "" {
				if _, ok := at[e.From]; !ok {
					add("PLAN_EXTENT_INVALID", "", d.Name+" 的 extent.from 不存在："+e.From)
				}
			}
			if len(e.BBox) > 0 && !ValidBBox(e.BBox) {
				add("PLAN_EXTENT_INVALID", "", d.Name+" 的 bbox 应为 [西,南,东,北]，经纬度范围有效且有面积")
			}
			if e.Expand < 0 || math.IsNaN(e.Expand) || math.IsInf(e.Expand, 0) {
				add("PLAN_EXTENT_INVALID", "", d.Name+" 的外扩比例必须为非负数")
			}
		}
	}
	for i, s := range p.Steps {
		if len(s.Produces) == 0 {
			add("PLAN_OUTPUT_INVALID", s.ID, "步骤需要声明可核验的 produces 产出")
		}
		for _, id := range s.Inputs {
			producer, ok := at[id]
			if !ok || producer >= i {
				add("PLAN_REF_INVALID", s.ID, "输入 "+id+" 不存在或尚未产生；请引用 data 或前序步骤的 produces.id")
				continue
			}
			if d, found := p.DataByID(id); found && d.Extent != nil && d.Extent.From != "" {
				if source, ok := at[d.Extent.From]; !ok || source >= i {
					add("PLAN_EXTENT_INVALID", s.ID, d.Name+" 的范围依赖必须在使用该数据之前产生")
				}
			}
		}
	}
	// Data-to-data extent references may be independent of step order, but must not cycle.
	edges := map[string]string{}
	for _, d := range p.Data {
		if d.Extent != nil {
			edges[d.ID] = d.Extent.From
		}
	}
	for start := range edges {
		seen := map[string]bool{}
		for id := start; id != ""; id = edges[id] {
			if seen[id] {
				add("PLAN_EXTENT_CYCLE", "", "数据范围存在循环依赖："+start)
				break
			}
			seen[id] = true
		}
	}
	for _, d := range p.Deliverables {
		if _, _, ok := p.ProductByID(d.Label); !ok {
			add("PLAN_OUTPUT_INVALID", "", "成果 "+d.Label+" 未在 produces 中声明")
		}
	}
	return out
}
func ValidBBox(b []float64) bool {
	if len(b) != 4 {
		return false
	}
	for _, v := range b {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return b[0] >= -180 && b[2] <= 180 && b[1] >= -90 && b[3] <= 90 && b[0] < b[2] && b[1] < b[3]
}
