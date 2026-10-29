package planexec

import (
	"github.com/autocarto/carto_agent/internal/plan"
	"strings"
)

type Confirmation struct {
	Required bool     `json:"required"`
	Reasons  []string `json:"reasons"`
}

func ConfirmationFor(p *plan.Plan, policy, networkPolicy string, needsNetwork bool) Confirmation {
	result := Confirmation{Reasons: []string{}}
	if policy == "never" {
		return result
	}
	if policy == "always" {
		result.Reasons = append(result.Reasons, "设置为每次确认计划")
	}
	if needsNetwork && networkPolicy != "direct" {
		result.Reasons = append(result.Reasons, "需要联网获取数据")
	}
	for _, a := range p.Assumptions {
		if a.Impact == "high" {
			result.Reasons = append(result.Reasons, "需要确认的假设："+a.Text)
		}
	}
	if len(p.Steps) > 10 {
		result.Reasons = append(result.Reasons, "计划超过十个步骤")
	}
	for _, s := range p.Steps {
		review := false
		for _, c := range s.Capabilities {
			if strings.Contains(c, "质量审查") || c == "quality-review" || c == "quality-feedback" {
				review = true
			}
		}
		for _, o := range s.Outputs {
			if o.Type == "review_initial" || o.Type == "review_final" {
				review = true
			}
		}
		if review {
			result.Reasons = append(result.Reasons, "包含读图审查")
			break
		}
	}
	result.Required = len(result.Reasons) > 0
	return result
}
