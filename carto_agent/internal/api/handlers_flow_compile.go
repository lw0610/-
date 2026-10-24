package api

import (
	"context"

	"github.com/autocarto/carto_agent/internal/flow"
)

// decorateFlow builds the saved document preview locally. Reading or saving a
// flow never starts model analysis; existing successful cached results may be reused.
func (a *API) decorateFlow(ctx context.Context, item *flow.StoredFlow, scheme, reason string) error {
	if !a.hasFlowCompiler() && a.assetCatalogService == nil {
		item.Diagnostics = flow.Parse(item.FlowMD).Diagnostics
		return nil
	}
	compiler, err := a.FlowCompiler()
	if err != nil {
		if reason != "" {
			markSavedFlowPreviewUnavailable(item)
			return nil
		}
		return err
	}
	plan, err := compiler.Plan(ctx, item.Meta.ID, scheme)
	if err != nil {
		if reason != "" {
			markSavedFlowPreviewUnavailable(item)
			return nil
		}
		return err
	}
	item.Plan = plan
	item.Diagnostics = plan.Diagnostics

	return nil
}

// A local environment failure after persistence must never turn a successful
// save into a retryable storage failure or leak configuration/provider details.
func markSavedFlowPreviewUnavailable(item *flow.StoredFlow) {
	item.Plan = nil
	item.Diagnostics = flow.Parse(item.FlowMD).Diagnostics
	item.Diagnostics = append(item.Diagnostics, flow.Diagnostic{Level: "warning", Code: "FLOW_PREVIEW_UNAVAILABLE", Message: "流程已保存，当前环境暂时无法生成步骤预览。请检查当前方案配置后重新打开流程。"})
}
func (a *API) decorateFlowSummaries(ctx context.Context, items []flow.FlowSummary, scheme string) error {
	if !a.hasFlowCompiler() && a.assetCatalogService == nil {
		return nil
	}
	compiler, err := a.FlowCompiler()
	if err != nil {
		return err
	}
	for index := range items {
		item := &items[index]
		item.Enabled, err = compiler.Enabled(ctx, item.ID, scheme)
		if err != nil {
			return err
		}
		plan, err := compiler.Plan(ctx, item.ID, scheme)
		if err != nil {
			return err
		}
		item.PlanStatus = plan.PlanStatus
		item.DiagnosticsSummary = map[string]int{"errors": 0, "warnings": 0, "infos": 0}
		for _, d := range plan.Diagnostics {
			switch d.Level {
			case "error":
				item.DiagnosticsSummary["errors"]++
			case "warning":
				item.DiagnosticsSummary["warnings"]++
			case "info":
				item.DiagnosticsSummary["infos"]++
			}
		}
	}
	return nil
}
func (a *API) relinkFlow(ctx context.Context, item *flow.StoredFlow, scheme string) error {
	return a.decorateFlow(ctx, item, scheme, "")
}
