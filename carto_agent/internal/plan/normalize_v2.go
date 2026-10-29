package plan

import (
	"fmt"
	"github.com/autocarto/carto_agent/internal/flow"
	"path/filepath"
	"regexp"
	"strings"
)

var unsafeID = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// NormalizeV2 corrects representation mistakes without asking the model again.
// Semantic evidence (file headers, layer coverage) belongs to the local precheck.
func NormalizeV2(p *Plan, capabilities map[string]bool) []string {
	notes := []string{}
	if p.APIVersion == "" {
		p.APIVersion = APIVersion
	}
	if p.Version == 0 {
		p.Version = 1
	}
	used := map[string]bool{}
	refs := map[string][]string{}
	normalizeID := func(original, fallback string) string {
		id := strings.Trim(unsafeID.ReplaceAllString(original, "_"), "_.-")
		if id == "" {
			id = fallback
		}
		if len(id) > 110 {
			id = id[:110]
		}
		base := id
		for n := 2; used[id]; n++ {
			id = fmt.Sprintf("%s_%d", base, n)
		}
		used[id] = true
		if id != original {
			notes = append(notes, fmt.Sprintf("编号 %q 已规范为 %s", original, id))
		}
		return id
	}
	for i := range p.Data {
		d := &p.Data[i]
		old := d.ID
		d.ID = normalizeID(old, fmt.Sprintf("data_%d", i+1))
		if old != "" && len(refs[old]) == 0 {
			refs[old] = []string{d.ID}
		}
		if d.Name == "" {
			d.Name = d.ID
		}
		if d.Source == "" && (filepath.IsAbs(d.Ref) || strings.HasPrefix(d.Ref, "附件:") || d.Ref == "消息原文") {
			d.Source = "user"
			notes = append(notes, d.Name+" 按文件引用使用用户数据")
		}
		if d.Source == "retrieve" && d.Need == nil {
			d.Need = &Need{Category: d.Name}
		}
	}
	outputAt := map[string]Product{}
	names := map[string][]string{}
	for _, d := range p.Data {
		names[d.Name] = append(names[d.Name], d.ID)
	}
	stepRefs := map[string][]string{}
	for i := range p.Steps {
		s := &p.Steps[i]
		old := s.ID
		s.ID = normalizeID(old, fmt.Sprintf("step_%d", i+1))
		if s.Title == "" {
			s.Title = s.ID
		}
		for j := range s.Produces {
			o := &s.Produces[j]
			o.Format = flow.CanonicalOutputFormat(o.Format)
			prior := o.ID
			o.ID = normalizeID(prior, fmt.Sprintf("%s_result_%d", s.ID, j+1))
			if prior != "" && len(refs[prior]) == 0 {
				refs[prior] = []string{o.ID}
			}
			if o.Format == "layout" && (o.Kind == "file" || o.Kind == "") {
				o.Kind, o.Format = "layout", ""
				notes = append(notes, o.ID+" 按明确的 layout 格式规范为工程版面产出")
			}
			if o.Kind == "" {
				if o.Format == "route_dataset" {
					o.Kind = "dataset"
				} else {
					o.Kind = "file"
				}
				notes = append(notes, o.ID+" 补充产出种类 "+o.Kind)
			}
			outputAt[o.ID] = *o
			if o.Name != "" {
				names[o.Name] = append(names[o.Name], o.ID)
			}
			stepRefs[s.ID] = append(stepRefs[s.ID], o.ID)
		}
		if old != "" {
			stepRefs[old] = stepRefs[s.ID]
		}
	}
	resolve := func(ref string) []string {
		if value := refs[ref]; len(value) == 1 {
			return value
		}
		if value := names[ref]; len(value) == 1 {
			return value
		}
		if value := stepRefs[ref]; len(value) > 0 {
			return value
		}
		return []string{ref}
	}
	for i := range p.Steps {
		s := &p.Steps[i]
		inputs := []string{}
		seen := map[string]bool{}
		for _, ref := range s.Inputs {
			ids := resolve(ref)
			if len(ids) != 1 || ids[0] != ref {
				notes = append(notes, s.Title+" 的输入 "+ref+" 对齐为 "+strings.Join(ids, "、"))
			}
			for _, id := range ids {
				if !seen[id] {
					inputs = append(inputs, id)
					seen[id] = true
				}
			}
		}
		s.Inputs = inputs
		before := strings.Join(s.Capabilities, ",")
		declared := []string{}
		capSeen := map[string]bool{}
		for _, id := range s.Capabilities {
			if capabilities[id] && !capSeen[id] {
				declared = append(declared, id)
				capSeen[id] = true
			}
		}
		s.Capabilities = declared
		if capabilities["route_knowledge"] {
			for _, output := range s.Produces {
				if output.Format == "route_dataset" {
					s.Capabilities = []string{"route_knowledge"} // The dedicated executor is a complete role.
					break
				}
			}
		}
		if before != strings.Join(s.Capabilities, ",") {
			notes = append(notes, s.Title+" 的已有能力编号已规范")
		}
	}
	for i := range p.Data {
		if e := p.Data[i].Extent; e != nil && e.From != "" {
			if ids := resolve(e.From); len(ids) == 1 {
				e.From = ids[0]
			}
		}
	}
	for i := range p.Deliverables {
		id := p.Deliverables[i].Label
		if ids := resolve(id); len(ids) == 1 {
			id = ids[0]
		}
		if o, ok := outputAt[id]; ok {
			p.Deliverables[i] = o.Declaration()
		} else {
			p.Deliverables[i].Label = id
		}
	}
	return notes
}
