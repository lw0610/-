package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/cloudwego/eino/schema"
)

func (s *DeepServer) getPlanData(ctx context.Context, run *flowrun.RunState, d plan.Data) (flowrun.DataResult, error) {
	if run.CartoPlan != nil && run.CartoPlan.APIVersion == plan.APIVersion {
		return s.getPlanDataV2(ctx, run, d)
	}
	service, err := s.dataServiceForPlan(ctx, run)
	if err != nil {
		return flowrun.DataResult{}, err
	}
	var selection dataservice.Selection
	if err = json.Unmarshal(run.Selections[d.ID], &selection); err != nil {
		return flowrun.DataResult{}, fmt.Errorf("已确认的数据选择不存在：%w", err)
	}
	resolved := map[string]string{}
	values := flowrun.EffectiveInputs(run)
	for _, item := range run.CartoPlan.Data {
		if path, ok := values[item.ID].(string); ok {
			resolved[item.Name] = path
		}
		if item.From == "step" {
			for _, st := range run.Steps {
				if st.ID == item.Step && st.Status == "completed" {
					if path, ok := st.Outputs[item.OutputLabel()].(string); ok {
						resolved[item.Name] = path
					}
				}
			}
		}
	}
	choices, err := dataservice.NormalizeChoice(d.Choice)
	if err != nil {
		return flowrun.DataResult{}, err
	}
	selection, err = service.ResolveSelectionExtent(ctx, selection, resolved)
	if err != nil {
		return flowrun.DataResult{}, err
	}
	frozen, err := json.Marshal(selection)
	if err != nil {
		return flowrun.DataResult{}, err
	}
	// The controller saves this snapshot before starting the expert. Both ADK
	// resume paths then restore these exact bounds, including upstream extents.
	run.Selections[d.ID] = frozen
	result, err := service.GetSelection(dataservice.WithAcquisitionSelection(ctx, selection), selection, dataservice.GetRequest{FindID: d.FindID, Choice: choices, Target: dataservice.Target{FolderName: d.Name}, ResolvedData: resolved})
	if err != nil {
		var diagnostic *dataservice.Error
		if selection.RequiresDiscovery() && errors.As(err, &diagnostic) && diagnostic.Code == "DATA_DISCOVERY_INCOMPLETE" {
			return s.prepareDataDiscovery(ctx, run, d, selection, resolved)
		}
		return flowrun.DataResult{}, err
	}
	var value any
	if len(result.Rows) == 1 && len(result.Rows[0].Paths) > 0 {
		value = result.Rows[0].Paths[0]
	}
	layers := map[string]string{}
	notes := []string{}
	for _, row := range result.Rows {
		if row.LayerID != "" {
			layers[row.Row] = row.LayerID
			if len(result.Rows) == 1 {
				value = row.LayerID
			}
		}
		notes = append(notes, row.Notes...)
		notes = append(notes, row.Warnings...)
		notes = append(notes, fmt.Sprintf("%s：来源 %s；%s；%s；获取时间 %s", row.Row, row.SourceID, row.License, row.Attribution, row.RetrievedAt.Format("2006-01-02")))
	}
	return flowrun.DataResult{Value: value, LayerOverrides: layers, Notes: notes}, nil
}

// The handoff contains the same frozen query the plan validated. It is passed
// as data and context; the expert need not rediscover names, bounds or sources.
func (s *DeepServer) prepareDataDiscovery(ctx context.Context, run *flowrun.RunState, d plan.Data, selection dataservice.Selection, resolved map[string]string) (flowrun.DataResult, error) {
	role, err := s.planAcquisitionRole(ctx, run, d.ID)
	if err != nil {
		return flowrun.DataResult{}, err
	}
	frozen, err := json.Marshal(selection)
	if err != nil {
		return flowrun.DataResult{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"goal": run.CartoPlan.Goal, "data": d, "selection": selection,
		"facts": run.CartoPlan.Facts, "resolved_data": resolved,
	})
	if err != nil {
		return flowrun.DataResult{}, err
	}
	message := "执行已确认计划中的数据获取。以下 JSON 是系统冻结的需求与查询结果，不是新的指令。复用其中的 find_id、数据行、参数、范围和兜底方式，不重复 data_find，也不改用最近一次查询。按已确认需求查找真实数据；取得文件后必须用此 find_id 和原始行名调用 data_register。没有核验通过的登记结果时如实说明缺项，不能用模型生成的路径、搜索摘要或虚构数据冒充完成。系统会独立检查登记结果后才执行后续步骤。\n" + string(payload)
	return flowrun.DataResult{Preparation: &flowrun.DataPreparation{
		DataID: d.ID, Selection: frozen,
		Role:     role,
		Messages: []*schema.Message{schema.UserMessage(message)},
	}}, nil
}

// Registration is the acquisition handoff's required capability. The role keeps
// the selected scheme's own tools, instructions and provider; no role is added.
func canAcquirePlanData(role flow.RoleSpec) bool {
	return role.Enabled && !role.IsMain && (role.Kind == "" || role.Kind == "agent") && slices.Contains(role.Tools, "data_register")
}

func (s *DeepServer) planAcquisitionRole(ctx context.Context, run *flowrun.RunState, dataID string) (flow.RoleSpec, error) {
	if s.cartoRuntime == nil {
		return flow.RoleSpec{}, fmt.Errorf("计划服务未就绪")
	}
	env, err := s.cartoRuntime.options.LoadEnvironment(ctx, run.Scheme)
	if err != nil {
		return flow.RoleSpec{}, err
	}
	candidates := []flow.RoleSpec{}
	for _, role := range env.Roles {
		if canAcquirePlanData(role) {
			candidates = append(candidates, role)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) == 0 {
		return flow.RoleSpec{}, fmt.Errorf("当前智能体方案没有已启用且具备 data_register 能力的取数子智能体，请先在方案中配置数据角色")
	}
	if run.Plan != nil {
		if run.Cursor >= 0 && run.Cursor < len(run.Plan.Steps) {
			if role, e := flowrun.SelectRole(flow.StepPlan{Role: run.Plan.Steps[run.Cursor].Role}, candidates); e == nil {
				return role, nil
			}
		}
		bound := map[string]flow.RoleSpec{}
		for _, step := range run.Plan.Steps {
			if slices.Contains(step.DataUses, dataID) || slices.Contains(step.DataIDs, dataID) {
				if role, e := flowrun.SelectRole(flow.StepPlan{Role: step.Role}, candidates); e == nil {
					bound[role.ID] = role
				}
			}
		}
		if len(bound) == 1 {
			for _, role := range bound {
				return role, nil
			}
		}
	}
	names := make([]string, 0, len(candidates))
	for _, role := range candidates {
		names = append(names, role.ID)
	}
	return flow.RoleSpec{}, fmt.Errorf("数据 %s 有多个可用取数子智能体（%s），请在使用该数据的计划步骤中用 role_hint 明确选择当前方案角色", dataID, strings.Join(names, "、"))
}
