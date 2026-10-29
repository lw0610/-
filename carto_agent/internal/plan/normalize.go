package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/flow"
)

// NormalizeReferences resolves only unambiguous references, in place. It is
// idempotent; unresolved references are retained so Validate can report them.
// Callers that persist/execute a plan normalize it before validation. Validate
// itself works on a copy and never changes the caller's plan.
func NormalizeReferences(p *Plan) []flow.Diagnostic {
	if p != nil && p.APIVersion == APIVersion {
		return nil
	}
	if p == nil {
		return nil
	}
	diagnostics := []flow.Diagnostic{}
	add := func(code, step, message string, fix map[string]any) {
		diagnostics = append(diagnostics, flow.Diagnostic{Level: "error", Code: code, Step: step, Message: message, Fix: fix})
	}
	steps, stepCounts := map[string]int{}, map[string]int{}
	dataIDs, names := map[string]bool{}, map[string]bool{}
	for i, step := range p.Steps {
		steps[step.ID], stepCounts[step.ID] = i, stepCounts[step.ID]+1
	}
	for _, data := range p.Data {
		dataIDs[data.ID], names[data.Name] = true, true
	}
	for i := range p.Data {
		data := &p.Data[i]
		if data.From != "step" {
			continue
		}
		at, exists := steps[data.Step]
		if !exists || stepCounts[data.Step] != 1 {
			add("PLAN_REF_INVALID", data.Step, fmt.Sprintf("数据 %s 的 step=%q 必须指向唯一存在的生产步骤", data.ID, data.Step), map[string]any{"field": "data[].step", "data_id": data.ID, "valid_step_ids": planStepIDs(p)})
			continue
		}
		producer := p.Steps[at]
		label := data.Output
		if label == "" {
			// Legacy plans used the display name as their output key. A sole
			// declared output is also deterministic, regardless of display name.
			if outputCount(producer, data.Name) == 1 {
				label = data.Name
			} else if len(producer.Outputs) == 1 {
				label = producer.Outputs[0].Label
			}
		}
		if label == "" || outputCount(producer, label) != 1 {
			add("PLAN_OUTPUT_INVALID", data.Step, fmt.Sprintf("数据 %s（显示名 %q）的 output=%q 必须选择步骤 %s 的一个明确 outputs[].label；可选产出：%s。name 仅作显示，不用改成产出标识。", data.ID, data.Name, data.Output, data.Step, outputSummary(producer.Outputs)), map[string]any{"field": "data[].output", "data_id": data.ID, "producer_step": data.Step, "valid_outputs": producer.Outputs, "next_action": "select_output"})
			continue
		}
		data.Output = label
	}

	for i := range p.Steps {
		consumer := &p.Steps[i]
		if len(consumer.Uses) == 0 {
			continue
		}
		uses := make([]string, 0, len(consumer.Uses))
		seen := map[string]bool{}
		appendUse := func(id string) {
			if !seen[id] {
				uses = append(uses, id)
				seen[id] = true
			}
		}
		for _, reference := range consumer.Uses {
			// Explicit data IDs always win, even if a step has the same ID.
			if dataIDs[reference] {
				appendUse(reference)
				continue
			}
			at, exists := steps[reference]
			fix := map[string]any{"field": "steps[].uses", "reference": reference, "valid_data_ids": precedingDataIDs(p, i)}
			if !exists || stepCounts[reference] != 1 {
				add("PLAN_REF_INVALID", consumer.ID, fmt.Sprintf("uses 中 %q 不是已声明的数据 ID，也不是唯一的前序步骤；可用数据 ID：%s。引用步骤产出时声明 data:{id,name,from:step,step,output} 后在 uses 使用该 data.id。", reference, strings.Join(precedingDataIDs(p, i), "、")), fix)
				appendUse(reference)
				continue
			}
			producer := p.Steps[at]
			fix["producer_step"], fix["valid_outputs"] = producer.ID, producer.Outputs
			if at >= i {
				add("PLAN_REF_INVALID", consumer.ID, fmt.Sprintf("uses=%q 指向当前或后续步骤；只能消费前序步骤的产出，不能形成前向或循环依赖。可用数据 ID：%s", reference, strings.Join(precedingDataIDs(p, i), "、")), fix)
				appendUse(reference)
				continue
			}
			if producer.Kind == "acquire" {
				valid := len(producer.Data) > 0
				for _, id := range producer.Data {
					data, exists := p.DataByID(id)
					valid = valid && exists && data.From == "acquire"
				}
				if valid {
					for _, id := range producer.Data {
						appendUse(id)
					}
					continue
				}
				fix["acquire_data_ids"] = producer.Data
				add("PLAN_REF_INVALID", consumer.ID, fmt.Sprintf("获取步骤 %s 未声明有效的 acquire 数据 ID；先修正该步骤的 data，uses 才能绑定其数据。", producer.ID), fix)
				appendUse(reference)
				continue
			}
			if len(producer.Outputs) != 1 || strings.TrimSpace(producer.Outputs[0].Label) == "" {
				fix["next_action"] = "select_output"
				add("PLAN_OUTPUT_AMBIGUOUS", consumer.ID, fmt.Sprintf("uses=%q 指向的步骤没有唯一产出，不能猜测要消费哪个结果；可选产出：%s。请声明 data:{id,name,from:step,step:%q,output:<产出label>} 并在 uses 使用该 data.id；已有可用数据 ID：%s", reference, outputSummary(producer.Outputs), producer.ID, strings.Join(precedingDataIDs(p, i), "、")), fix)
				appendUse(reference)
				continue
			}
			label := producer.Outputs[0].Label
			id := ""
			for _, data := range p.Data {
				if data.From == "step" && data.Step == producer.ID && data.OutputLabel() == label {
					id = data.ID
					break
				}
			}
			if id == "" {
				digest := sha256.Sum256([]byte(producer.ID + "\x00" + label))
				base := "output-" + hex.EncodeToString(digest[:8])
				id = base
				for n := 2; dataIDs[id]; n++ {
					id = fmt.Sprintf("%s-%d", base, n)
				}
				name := label
				if names[name] {
					name = label + "（" + producer.Title + "）"
				}
				if names[name] {
					name += " " + id
				}
				p.Data = append(p.Data, Data{ID: id, Name: name, From: "step", Step: producer.ID, Output: label})
				dataIDs[id], names[name] = true, true
			}
			appendUse(id)
		}
		consumer.Uses = uses
	}
	return diagnostics
}

func outputCount(step Step, label string) int {
	count := 0
	for _, output := range step.Outputs {
		if output.Label == label {
			count++
		}
	}
	return count
}

func planStepIDs(p *Plan) []string {
	ids := make([]string, 0, len(p.Steps))
	for _, step := range p.Steps {
		ids = append(ids, step.ID)
	}
	return ids
}

func precedingDataIDs(p *Plan, consumer int) []string {
	availableSteps := map[string]bool{}
	acquired, declaredAcquisitions := map[string]bool{}, map[string]bool{}
	for i, step := range p.Steps {
		availableSteps[step.ID] = i < consumer
		if step.Kind == "acquire" {
			for _, id := range step.Data {
				declaredAcquisitions[id] = true
				acquired[id] = i < consumer
			}
		}
	}
	ids := []string{}
	for _, data := range p.Data {
		if data.From == "step" && !availableSteps[data.Step] || data.From == "acquire" && declaredAcquisitions[data.ID] && !acquired[data.ID] {
			continue
		}
		ids = append(ids, data.ID)
	}
	return ids
}
