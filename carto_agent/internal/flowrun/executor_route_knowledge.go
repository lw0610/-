package flowrun

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/cloudwego/eino/schema"
)

// RouteKnowledgeExecutor adapts a declared knowledge step without copying source text.
type RouteKnowledgeExecutor struct{}

func NewRouteKnowledgeExecutor() ExecutorAdapter { return RouteKnowledgeExecutor{} }

func (RouteKnowledgeExecutor) Type() string { return "route_knowledge" }

func (RouteKnowledgeExecutor) BuildInput(step flow.StepPlan, run *RunState) ([]*schema.Message, error) {
	if run == nil || run.Plan == nil {
		return nil, fmt.Errorf("知识步骤缺少流程快照")
	}
	referenced := map[string]bool{}
	for _, key := range step.Refs.Inputs {
		referenced[key] = true
	}
	foundSource, forceRefresh := false, false
	for _, input := range run.Plan.Inputs {
		if !referenced[input.Key] {
			continue
		}
		if input.Type == "conversation_text" && provided(EffectiveInputs(run)[input.Key]) {
			foundSource = true
		}
		if input.Label == "重新抽取" {
			value := EffectiveInputs(run)[input.Key]
			if value == nil {
				continue
			}
			flag, ok := value.(bool)
			if input.Type != "boolean" || !ok {
				return nil, fmt.Errorf("重新抽取输入须为是/否")
			}
			forceRefresh = flag
		}
	}
	if !foundSource {
		return nil, fmt.Errorf("知识步骤必须引用已提供的 conversation_text 输入")
	}
	if run.Cursor >= 0 && run.Cursor < len(run.Steps) {
		if guidance := strings.TrimSpace(run.Steps[run.Cursor].UserGuidance); guidance != "" {
			step.Instructions += "\n用户补充说明：" + guidance
		}
	}
	payload := map[string]any{"source_ref": "current_task_input", "force_refresh": forceRefresh}
	if strings.TrimSpace(step.Instructions) != "" {
		payload["rules"] = step.Instructions
	}
	for _, key := range []string{"route_style", "symbols"} {
		if value := strings.TrimSpace(step.Parameters[key]); value != "" {
			payload[key] = value
		}
	}
	// Previously saved plans used linked resource handles. New plans carry
	// explicit preset parameters, which take precedence over those snapshots.
	if payload["route_style"] == nil || payload["symbols"] == nil {
		seenTypes := map[string]bool{}
		for _, key := range step.Refs.Resources {
			var selected *flow.ResourcePlan
			for index := range run.Plan.Resources {
				if run.Plan.Resources[index].Key == key {
					selected = &run.Plan.Resources[index]
					break
				}
			}
			if selected == nil {
				return nil, fmt.Errorf("知识步骤引用未知资源: %s", key)
			}
			if selected.Type != "symbols" && selected.Type != "route_style" {
				continue
			}
			if strings.TrimSpace(step.Parameters[selected.Type]) != "" {
				continue
			}
			if seenTypes[selected.Type] {
				return nil, fmt.Errorf("知识步骤引用多个 %s 资源，无法确定本次使用项", selected.Type)
			}
			seenTypes[selected.Type] = true
			handle := selected.ResolvedResource().Handle()
			if handle == nil || selected.ID == "" {
				return nil, fmt.Errorf("知识资源尚未链接为可执行句柄: %s", key)
			}
			if selected.Type == "route_style" {
				payload["route_style"] = handle
			} else {
				payload["symbols"] = handle
			}
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return []*schema.Message{schema.UserMessage(string(encoded))}, nil
}

func (RouteKnowledgeExecutor) ToReceipt(step flow.StepPlan, content string) (Receipt, error) {
	var envelope struct {
		Success *bool          `json:"success"`
		Data    map[string]any `json:"data"`
		Info    string         `json:"info"`
		Message string         `json:"message"`
	}
	if err := json.Unmarshal([]byte(content), &envelope); err != nil || envelope.Success == nil {
		return Receipt{}, fmt.Errorf("知识执行器未返回有效 success/data JSON")
	}
	if !*envelope.Success {
		message := strings.TrimSpace(envelope.Info)
		if message == "" {
			message = strings.TrimSpace(envelope.Message)
		}
		if message == "" {
			message = "知识执行器未完成"
		}
		return Receipt{Status: "failed", Summary: message, Outputs: map[string]any{}, Issues: []string{message}}, nil
	}
	dir, _ := envelope.Data["dataset_dir"].(string)
	if strings.TrimSpace(dir) == "" {
		return Receipt{}, fmt.Errorf("知识执行器成功回包缺少 dataset_dir")
	}
	label := ""
	for _, output := range step.ExpectedOutputs {
		if output.Type == "dataset" {
			label = strings.TrimSpace(output.Label)
			break
		}
	}
	if label == "" {
		return Receipt{}, fmt.Errorf("知识步骤必须声明 dataset 类型产出")
	}
	outputs := map[string]any{}
	for _, key := range []string{"artifact_id", "task_id", "task_graph_file", "counts", "table_counts", "validation", "warnings"} {
		if value, exists := envelope.Data[key]; exists {
			outputs[key] = value
		}
	}
	if _, collision := outputs[label]; collision {
		return Receipt{}, fmt.Errorf("知识数据集产出名称与回包元数据冲突: %s", label)
	}
	outputs[label] = dir
	return Receipt{Status: "completed", Summary: "路线数据集已生成", Outputs: outputs, Issues: []string{}}, nil
}
