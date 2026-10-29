package planexec

import (
	"fmt"
	"time"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
)

func buildV2(p *plan.Plan, options BuildOptions) (*flow.FlowPlan, map[string]any, error) {
	out := &flow.FlowPlan{APIVersion: flow.PlanAPIVersion, FlowID: p.ID, Name: p.Goal, Scheme: options.Environment.Scheme, Roles: append([]flow.RoleSpec{}, options.Environment.Roles...), PlanStatus: flow.PlanStatusReady, CompiledAt: time.Now().UTC(), FlowDir: options.Scope.FlowDir, Lock: options.Scope.Lock, CompileKey: fmt.Sprintf("%s/v%d", p.ID, p.Version)}
	if p.Guide != nil {
		out.FlowID = p.Guide.ID
		out.FlowVersion = p.Guide.Version
	}
	if options.Guide != nil {
		out.Rules = []string{options.Guide.Body}
	}
	values := map[string]any{}
	for _, d := range p.Data {
		out.Inputs = append(out.Inputs, flow.InputPlan{Key: d.ID, Label: d.Name, Type: d.Kind})
		if v, ok := options.Bindings[d.ID]; ok {
			values[d.ID] = v
		}
	}
	for _, f := range p.Facts {
		key := "fact:" + f.Name
		out.Inputs = append(out.Inputs, flow.InputPlan{Key: key, Label: f.Name, Type: "text"})
		values[key] = f.Value
	}
	for _, r := range options.Scope.Linked {
		out.Resources = append(out.Resources, flow.ResourcePlanFromResolved(r))
	}
	for index, step := range p.Steps {
		s := flow.StepPlan{ID: step.ID, Index: index + 1, Title: step.Title, Instructions: step.Instructions, Parameters: step.Parameters, DataUses: append([]string{}, step.Inputs...), Optional: step.Optional, OnFailure: "retry_then_ask", Enrichment: flow.StepEnrichmentReady}
		for _, o := range step.Produces {
			s.ExpectedOutputs = append(s.ExpectedOutputs, o.Declaration())
		}
		if err := flow.AssignPlanRole(out, &s, step.Capabilities, step.RoleHint, options.Environment); err != nil {
			return nil, nil, err
		}
		for _, id := range step.Inputs {
			if o, producer, ok := p.ProductByID(id); ok {
				s.Refs.Outputs = append(s.Refs.Outputs, flow.OutputRef{Step: producer, Label: o.ID})
			} else {
				s.Refs.Inputs = append(s.Refs.Inputs, id)
			}
		}
		for _, r := range out.Resources {
			s.Refs.Resources = append(s.Refs.Resources, r.Key)
		}
		out.Steps = append(out.Steps, s)
	}
	return out, values, nil
}
