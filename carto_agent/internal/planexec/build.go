package planexec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/resource"
)

type BuildOptions struct {
	Environment    flow.Environment
	Guide          *flow.GuideDoc
	Scope          resource.Scope
	Bindings       map[string]any
	OriginalSource string
}

// Build translates a validated visible plan into the existing controller's execution structure.
func Build(p *plan.Plan, options BuildOptions) (*flow.FlowPlan, map[string]any, error) {
	if p == nil {
		return nil, nil, fmt.Errorf("计划为空")
	}
	if p.APIVersion == plan.APIVersion {
		return buildV2(p, options)
	}
	normalized, err := plan.Clone(p)
	if err != nil {
		return nil, nil, err
	}
	if diagnostics := plan.NormalizeReferences(normalized); flow.HasErrors(diagnostics) {
		return nil, nil, plan.NewValidationError(diagnostics, plan.Environment{})
	}
	p = normalized
	out := &flow.FlowPlan{APIVersion: flow.PlanAPIVersion, FlowID: p.ID, Name: p.Goal, Scheme: options.Environment.Scheme, Roles: append([]flow.RoleSpec{}, options.Environment.Roles...), PlanStatus: flow.PlanStatusReady, CompiledAt: time.Now().UTC(), FlowDir: options.Scope.FlowDir, Lock: options.Scope.Lock, CompileKey: fmt.Sprintf("%s/v%d", p.ID, p.Version)}
	if p.Guide != nil {
		out.FlowID = p.Guide.ID
		out.FlowVersion = p.Guide.Version
	}
	if options.Guide != nil {
		out.Rules = []string{options.Guide.Body}
	}
	values := map[string]any{}
	byName := map[string]string{}
	for _, d := range p.Data {
		byName[d.Name] = d.ID
		input := flow.InputPlan{Key: d.ID, Label: d.Name, Type: "text"}
		switch d.From {
		case "conversation":
			input.Type = "conversation_text"
			sum := sha256.Sum256([]byte(options.OriginalSource))
			values[d.ID] = map[string]any{"$ref": "current_user_message", "sha256": hex.EncodeToString(sum[:]), "length": len(options.OriginalSource)}
		case "user":
			values[d.ID] = d.Path
		case "project":
			input.Type = "layer"
			values[d.ID] = d.Layer
		case "session":
			if d.Kind != "" {
				input.Type = d.Kind
			}
			values[d.ID] = options.Bindings[d.ID]
		}
		if v, ok := options.Bindings[d.ID]; ok {
			values[d.ID] = v
		}
		out.Inputs = append(out.Inputs, input)
	}
	for _, f := range p.Facts {
		key := "fact:" + f.Name
		typ := "text"
		if _, ok := f.Value.(bool); ok {
			typ = "boolean"
		}
		out.Inputs = append(out.Inputs, flow.InputPlan{Key: key, Label: f.Name, Type: typ})
		values[key] = f.Value
	}
	for _, r := range options.Scope.Linked {
		entry := flow.ResourcePlanFromResolved(r)
		if id, ok := byName[entry.BaseDirFromInput]; ok {
			entry.BaseDirFromInput = id
		}
		if value, ok := values[entry.BaseDirFromInput].(string); ok {
			entry.BaseDir = value
		}
		out.Resources = append(out.Resources, entry)
	}
	acquired := map[string]bool{}
	explicit := map[string]bool{}
	stepIDs := map[string]bool{}
	for _, s := range p.Steps {
		stepIDs[s.ID] = true
		if s.Kind == "acquire" {
			for _, id := range s.Data {
				explicit[id] = true
			}
		}
	}
	for _, step := range p.Steps {
		// Plans may omit a separate acquire step. Insert it only immediately before
		// its first consumer, after any prerequisite-producing earlier steps.
		for _, id := range step.Uses {
			d, ok := p.DataByID(id)
			if ok && d.From == "acquire" && !explicit[id] && !acquired[id] {
				base := "acquire-" + id
				if len(base) > 110 {
					base = base[:110]
				}
				generatedID := base
				for n := 1; stepIDs[generatedID]; n++ {
					generatedID = fmt.Sprintf("%s-%d", base, n)
				}
				stepIDs[generatedID] = true
				out.Steps = append(out.Steps, flow.StepPlan{ID: generatedID, Title: "获取" + d.Name, Kind: "acquire", DataIDs: []string{id}, OnFailure: "retry_then_ask"})
				acquired[id] = true
			}
		}
		s := flow.StepPlan{ID: step.ID, Title: step.Title, Kind: step.Kind, Instructions: step.Instructions, Parameters: step.Parameters, ExpectedOutputs: append([]flow.OutputDeclaration{}, step.Outputs...), OnFailure: "retry_then_ask", DataIDs: append([]string{}, step.Data...), DataUses: append([]string{}, step.Uses...), GuideSections: append([]string{}, step.GuideSections...), LayerNames: append([]string{}, step.Layers...), Optional: step.Optional, Enrichment: flow.StepEnrichmentReady}
		for i := range s.ExpectedOutputs {
			s.ExpectedOutputs[i].Required = true
		}
		if s.Kind != "acquire" && s.Kind != "confirm" {
			if err := flow.AssignPlanRole(out, &s, step.Capabilities, step.RoleHint, options.Environment); err != nil {
				return nil, nil, err
			}
		}
		for _, id := range step.Uses {
			d, ok := p.DataByID(id)
			if !ok {
				continue
			}
			if d.From == "step" {
				s.Refs.Outputs = append(s.Refs.Outputs, flow.OutputRef{Step: d.Step, Label: d.OutputLabel()})
			} else {
				s.Refs.Inputs = append(s.Refs.Inputs, id)
			}
		}
		for _, id := range step.DependsOn {
			previous, ok := p.StepByID(id)
			if ok {
				for _, o := range previous.Outputs {
					s.Refs.Outputs = append(s.Refs.Outputs, flow.OutputRef{Step: id, Label: o.Label})
				}
			}
		}
		referenceText := step.Instructions
		if options.Guide != nil {
			referenceText += "\n【流程全文】\n" + options.Guide.Body
		}
		for _, r := range out.Resources {
			if s.Kind != "executor" || strings.Contains(referenceText, "「"+r.Label+"」") {
				s.Refs.Resources = append(s.Refs.Resources, r.Key)
			}
		}
		if s.Kind == "executor" {
			s.Instructions = referenceText
			for _, input := range out.Inputs {
				if input.Label == "重新抽取" {
					s.Refs.Inputs = append(s.Refs.Inputs, input.Key)
				}
			}
		}
		out.Steps = append(out.Steps, s)
	}
	for i := range out.Steps {
		out.Steps[i].Index = i + 1
	}
	// Validate executor inputs after role selection and reference conversion, at
	// the boundary between a model proposal and the executable plan.
	inputTypes := make(map[string]string, len(out.Inputs))
	for _, input := range out.Inputs {
		inputTypes[input.Key] = input.Type
	}
	for _, step := range out.Steps {
		if step.Kind != "executor" {
			continue
		}
		spec, _ := flow.FindExecutorSpec(step.Executor)
		for _, required := range spec.RequiredInputTypes {
			found := false
			for _, key := range step.Refs.Inputs {
				found = found || inputTypes[key] == required
			}
			if !found {
				message := fmt.Sprintf("步骤「%s」必须引用 %s 输入。", step.Title, required)
				if required == "conversation_text" {
					message += "请在 data 中声明 from=conversation 的原文，并由本步骤 uses 引用；已完成的抽取成果应作为现有数据使用，不新增重复抽取步骤。"
				}
				out.Diagnostics = append(out.Diagnostics, flow.Diagnostic{Level: "error", Code: "PLAN_EXECUTOR_INPUT_MISSING", Step: step.ID, Message: message})
			}
		}

	}
	if out.HasErrors() {
		return nil, nil, &plan.ValidationError{Diagnostics: out.Diagnostics}
	}
	return out, values, nil
}
