package flowrun

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
)

//go:embed templates/step_message.tmpl
var stepMessageTemplate string
var parsedStepTemplate = template.Must(template.New("step").Parse(stepMessageTemplate))

func BuildStepMessage(step flow.StepPlan, run *RunState) (string, bool, error) {
	resources := []string{}
	referenced := map[string]bool{}
	for _, key := range step.Refs.Resources {
		referenced[key] = true
	}
	if step.Kind != "executor" {
		for _, resolved := range run.Plan.Resources {
			if !referenced[resolved.Key] {
				continue
			}
			label := resolved.Type
			if spec, ok := resource.TypeInfo(resolved.Type); ok {
				label = spec.Label
			}
			text := "「" + resolved.Label + "」（" + label
			if len(resolved.Entries) > 0 {
				text += fmt.Sprintf("，%d 项", len(resolved.Entries))
			}
			if resolved.Type == "data_list" && resolved.BaseDirFromInput != "" {
				text += "，位于「" + resolved.BaseDirFromInput + "」"
			}
			text += "）"
			if resolved.Type == "doc" {
				text += "：用 reference_read 读取 " + resolved.Entry
			}
			warnings := 0
			for _, problem := range resolved.Problems {
				if problem.Level == "warning" {
					warnings++
				}
			}
			if warnings > 0 {
				text += fmt.Sprintf("；有 %d 项提醒，可用 resource_get 查看", warnings)
			}
			resources = append(resources, text)
		}
	}
	inputs := []string{}
	for _, input := range run.Plan.Inputs {
		value := EffectiveInputs(run)[input.Key]
		if !provided(value) {
			continue
		}
		text := ""
		if input.Type == "conversation_text" {
			text = "见本次对话原文（系统已保存，工具会直接读取）"
		} else if s, ok := value.(string); ok {
			text = s
		} else {
			raw, _ := json.Marshal(value)
			text = string(raw)
		}
		inputs = append(inputs, input.Label+"："+text)
	}
	if len(run.SourceNotes) > 0 {
		inputs = append(inputs, "数据来源说明："+strings.Join(run.SourceNotes, "；"))
	}
	upstream := map[string]any{}
	for _, previous := range run.Steps {
		if previous.Status == "completed" {
			upstream[fmt.Sprintf("第%d步 %s", previous.Index, previous.Title)] = previous.Outputs
		}
	}
	raw, err := json.Marshal(upstream)
	if err != nil {
		return "", false, err
	}
	truncated := len(raw) > 20*1024
	if truncated {
		upstream = map[string]any{}
		for _, previous := range run.Steps {
			if previous.Status != "completed" {
				continue
			}
			outputs := map[string]any{}
			for _, ref := range step.Refs.Outputs {
				if ref.Step == previous.ID {
					if value, ok := previous.Outputs[ref.Label]; ok {
						outputs[ref.Label] = value
					}
				}
			}
			summary := ""
			for i := len(previous.Attempts) - 1; i >= 0; i-- {
				if previous.Attempts[i].Receipt != nil {
					summary = previous.Attempts[i].Receipt.Summary
					break
				}
			}
			upstream[fmt.Sprintf("第%d步 %s", previous.Index, previous.Title)] = map[string]any{"summary": summary, "outputs": outputs}
		}
		raw, err = json.Marshal(upstream)
		if err != nil {
			return "", truncated, err
		}
	}
	upstreamText := string(raw)
	if len(upstream) == 0 {
		upstreamText = ""
	}
	state := run.Steps[run.Cursor]
	previousOutputs := ""
	for i := len(state.Attempts) - 1; i >= 0; i-- {
		if receipt := state.Attempts[i].Receipt; receipt != nil && len(receipt.Outputs) > 0 {
			raw, _ := json.Marshal(receipt.Outputs)
			previousOutputs = string(raw)
			break
		}
	}
	data := struct {
		FlowName                                        string
		Index, Total                                    int
		Title, Instructions                             string
		Rules, Resources, Inputs, Tools                 []string
		Upstream, Completion, LastFailure, UserGuidance string
		PreviousOutputs                                 string
		Outputs                                         []flow.OutputDeclaration
	}{
		FlowName: run.Plan.Name, Index: step.Index, Total: len(run.Plan.Steps), Title: step.Title, Instructions: step.Instructions, Rules: run.Plan.Rules, Resources: resources, Inputs: inputs, Tools: step.SuggestedTools, Upstream: upstreamText, Completion: step.Completion, Outputs: step.ExpectedOutputs, LastFailure: state.LastFailure, UserGuidance: state.UserGuidance,
		PreviousOutputs: previousOutputs,
	}
	// Old run snapshots predate enrichment metadata and retain their suggestions.
	if step.Enrichment != "" && step.Enrichment != flow.StepEnrichmentReady {
		data.Tools = nil
	}
	var buffer bytes.Buffer
	if err = parsedStepTemplate.Execute(&buffer, data); err != nil {
		return "", truncated, err
	}
	return strings.TrimSpace(planStepContext(step, run) + buffer.String()), truncated, nil
}
