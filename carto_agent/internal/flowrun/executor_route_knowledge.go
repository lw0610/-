package flowrun

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/cloudwego/eino/schema"
)

// RouteKnowledgeExecutor adapts a declared knowledge step without copying source text.
type RouteKnowledgeExecutor struct{}

// KnowledgeReviewReference keeps the confirmed draft attached to its step even
// when dataset generation fails and the remaining plan is revised.
type KnowledgeReviewReference struct {
	ReviewID string `json:"review_id"`
	Revision int64  `json:"revision"`
	Status   string `json:"status"`
}

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
	if !foundSource && (run.CartoPlan == nil || run.CartoPlan.APIVersion != plan.APIVersion) {
		return nil, fmt.Errorf("知识步骤必须引用已提供的 conversation_text 输入")
	}
	if run.Cursor >= 0 && run.Cursor < len(run.Steps) {
		if guidance := strings.TrimSpace(run.Steps[run.Cursor].UserGuidance); guidance != "" {
			step.Instructions += "\n用户补充说明：" + guidance
		}
	}
	payload := map[string]any{"source_ref": "current_task_input", "force_refresh": forceRefresh}
	if run.CartoPlan != nil && run.CartoPlan.APIVersion == plan.APIVersion {
		refs := []string{}
		inputs := EffectiveInputs(run)
		for _, id := range step.Refs.Inputs {
			data, ok := run.CartoPlan.DataByID(id)
			if !ok {
				continue
			}
			if data.Source == "user" && data.Ref == "消息原文" {
				refs = append(refs, "current_user_message")
				continue
			}
			ref, ok := inputs[id].(string)
			if !ok || ref == "" {
				return nil, fmt.Errorf("原文输入 %s 尚未解析为文件引用", data.Name)
			}
			refs = append(refs, ref)
		}
		for _, ref := range step.Refs.Outputs {
			for _, previous := range run.Steps {
				if previous.ID == ref.Step {
					path, ok := previous.Outputs[ref.Label].(string)
					if !ok || path == "" {
						return nil, fmt.Errorf("原文产出 %s 尚未解析为文件引用", ref.Label)
					}
					refs = append(refs, path)
				}
			}
		}
		if len(refs) == 0 {
			return nil, fmt.Errorf("知识步骤缺少已绑定的原文输入")
		}
		if len(refs) == 1 {
			payload["source_ref"] = refs[0]
		} else {
			delete(payload, "source_ref")
			payload["source_refs"] = refs
		}
	}
	if strings.TrimSpace(step.Instructions) != "" {
		payload["rules"] = step.Instructions
	}
	for _, key := range []string{"route_style", "symbols"} {
		if value := strings.TrimSpace(step.Parameters[key]); value != "" {
			payload[key] = value
		}
	}
	if !forceRefresh {
		for _, state := range run.Steps {
			if state.ID != step.ID || len(state.Attempts) == 0 {
				continue
			}
			receipt := state.Attempts[len(state.Attempts)-1].Receipt
			if receipt != nil && receipt.KnowledgeReview != nil && receipt.KnowledgeReview.Status == knowledge.ReviewCompileFailed {
				payload["review_id"] = receipt.KnowledgeReview.ReviewID
				payload["revision"] = receipt.KnowledgeReview.Revision
			}
			break
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
		Success         *bool                     `json:"success"`
		Data            map[string]any            `json:"data"`
		Info            string                    `json:"info"`
		Message         string                    `json:"message"`
		KnowledgeReview *KnowledgeReviewReference `json:"knowledge_review"`
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
		return Receipt{Status: "failed", Summary: message, Outputs: map[string]any{}, Issues: []string{message}, KnowledgeReview: envelope.KnowledgeReview}, nil
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
