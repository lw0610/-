package hitl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type highRiskSpec struct {
	Objective   string
	Description string
	Impact      string
	Risk        string
}

var explicitHighRiskTools = map[string]highRiskSpec{
	"layout_delete": {
		Objective:   "删除布局",
		Description: "将删除整个布局，可能丢失已排版成果。",
		Impact:      "删除后布局与其所有元素将从工程中移除。",
		Risk:        "操作不可逆，除非手动恢复历史文件。",
	},
	"layout_remove_item": {
		Objective:   "移除布局元素",
		Description: "将从布局中删除指定地图元素。",
		Impact:      "删除后该元素不再参与地图输出。",
		Risk:        "误删会影响最终出图结果。",
	},
	"export_layout_pdf": {
		Objective:   "导出布局 PDF",
		Description: "导出可能覆盖现有 PDF 文件。",
		Impact:      "目标路径已有同名文件时会被覆盖。",
		Risk:        "覆盖后旧成果可能无法恢复。",
	},
	"export_print_layout": {
		Objective:   "导出布局图片",
		Description: "导出可能覆盖现有图片文件。",
		Impact:      "目标路径已有同名文件时会被覆盖。",
		Risk:        "覆盖后旧成果可能无法恢复。",
	},
	"pyqgis_submit_script": {
		Objective:   "提交脚本执行",
		Description: "脚本会对工程/数据执行批量改动。",
		Impact:      "脚本可能进行高影响写操作。",
		Risk:        "脚本执行失败或逻辑错误可能导致数据损坏。",
	},
}

var lowRiskDeleteLikeTools = map[string]struct{}{
	"delete_duplicate_geometries":    {},
	"delete_duplicates_by_attribute": {},
	"delete_column":                  {},
	"drop_geometries":                {},
	"drop_z_values":                  {},
}

// WrapHighRiskTools 为高风险工具增加强制确认守卫。
func WrapHighRiskTools(tools []einotool.InvokableTool) []einotool.InvokableTool {
	if len(tools) == 0 {
		return tools
	}

	wrapped := make([]einotool.InvokableTool, 0, len(tools))
	for _, t := range tools {
		info, err := t.Info(context.Background())
		if err != nil || info == nil {
			wrapped = append(wrapped, t)
			continue
		}

		spec, ok := lookupHighRiskSpec(info.Name)
		if !ok {
			wrapped = append(wrapped, t)
			continue
		}
		wrapped = append(wrapped, &guardedInvokableTool{
			inner:    t,
			toolName: info.Name,
			spec:     spec,
		})
	}
	return wrapped
}

// WrapHighRiskBaseTools 为 BaseTool 集合注入高风险确认守卫，兼容 EnhancedInvokableTool。
func WrapHighRiskBaseTools(tools []einotool.BaseTool) []einotool.BaseTool {
	if len(tools) == 0 {
		return tools
	}

	wrapped := make([]einotool.BaseTool, 0, len(tools))
	for _, t := range tools {
		info, err := t.Info(context.Background())
		if err != nil || info == nil {
			wrapped = append(wrapped, t)
			continue
		}

		spec, ok := lookupHighRiskSpec(info.Name)
		if !ok {
			wrapped = append(wrapped, t)
			continue
		}

		switch typed := t.(type) {
		case einotool.EnhancedInvokableTool:
			wrapped = append(wrapped, &guardedEnhancedTool{
				inner:    typed,
				toolName: info.Name,
				spec:     spec,
			})
		case einotool.InvokableTool:
			wrapped = append(wrapped, &guardedInvokableTool{
				inner:    typed,
				toolName: info.Name,
				spec:     spec,
			})
		default:
			wrapped = append(wrapped, t)
		}
	}
	return wrapped
}

type guardedInvokableTool struct {
	inner    einotool.InvokableTool
	toolName string
	spec     highRiskSpec
}

func (g *guardedInvokableTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return g.inner.Info(ctx)
}

func (g *guardedInvokableTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	if !shouldInterruptHighRiskTool(g.toolName, argumentsInJSON) {
		return g.inner.InvokableRun(ctx, argumentsInJSON, opts...)
	}

	info := buildGuardInterruptInfo(g.toolName, g.spec, argumentsInJSON)

	wasInterrupted, _, _ := einotool.GetInterruptState[any](ctx)
	if !wasInterrupted {
		return "", einotool.Interrupt(ctx, info)
	}

	isResumeTarget, _, _ := einotool.GetResumeContext[any](ctx)
	if !isResumeTarget {
		return "", einotool.Interrupt(ctx, info)
	}

	return g.inner.InvokableRun(ctx, argumentsInJSON, opts...)
}

type guardedEnhancedTool struct {
	inner    einotool.EnhancedInvokableTool
	toolName string
	spec     highRiskSpec
}

func (g *guardedEnhancedTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return g.inner.Info(ctx)
}

func (g *guardedEnhancedTool) InvokableRun(ctx context.Context, toolArgument *schema.ToolArgument, opts ...einotool.Option) (*schema.ToolResult, error) {
	argumentsInJSON := ""
	if toolArgument != nil {
		argumentsInJSON = toolArgument.Text
	}
	if !shouldInterruptHighRiskTool(g.toolName, argumentsInJSON) {
		return g.inner.InvokableRun(ctx, toolArgument, opts...)
	}

	info := buildGuardInterruptInfo(g.toolName, g.spec, "")
	if toolArgument != nil {
		info = buildGuardInterruptInfo(g.toolName, g.spec, toolArgument.Text)
	}

	wasInterrupted, _, _ := einotool.GetInterruptState[any](ctx)
	if !wasInterrupted {
		return nil, einotool.Interrupt(ctx, info)
	}

	isResumeTarget, _, _ := einotool.GetResumeContext[any](ctx)
	if !isResumeTarget {
		return nil, einotool.Interrupt(ctx, info)
	}

	return g.inner.InvokableRun(ctx, toolArgument, opts...)
}

func shouldInterruptHighRiskTool(toolName string, argumentsInJSON string) bool {
	name := strings.ToLower(strings.TrimSpace(toolName))
	switch name {
	case "export_layout_pdf", "export_print_layout":
		return exportTargetExists(argumentsInJSON)
	default:
		_, ok := lookupHighRiskSpec(name)
		return ok
	}
}

func exportTargetExists(argumentsInJSON string) bool {
	outputPath := extractOutputPath(argumentsInJSON)
	if outputPath == "" {
		return false
	}
	if !filepath.IsAbs(outputPath) {
		return false
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func extractOutputPath(argumentsInJSON string) string {
	raw := strings.TrimSpace(argumentsInJSON)
	if raw == "" || raw == "{}" {
		return ""
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	for _, key := range []string{"OUTPUT", "output"} {
		if value, ok := payload[key].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func buildGuardInterruptInfo(toolName string, spec highRiskSpec, argumentsInJSON string) InterruptPayload {
	argsSummary := summarizeArgs(argumentsInJSON)

	plan := &RequestPlan{
		Objective: spec.Objective,
		Steps: []RequestPlanStep{
			{
				Agent:  "safety_guard",
				Action: fmt.Sprintf("执行工具 %s", toolName),
				Impact: spec.Impact,
			},
		},
		Risks:    []string{spec.Risk},
		Fallback: "取消后本次操作不会执行。",
	}
	if argsSummary != "" {
		plan.Steps = []RequestPlanStep{
			{
				Agent:  "safety_guard",
				Action: fmt.Sprintf("执行工具 %s（参数: %s）", toolName, argsSummary),
				Impact: spec.Impact,
			},
		}
	}

	return InterruptPayload{
		Type:        "plan_confirmation",
		Title:       "高风险操作确认",
		Description: spec.Description,
		Plan:        plan,
	}
}

func summarizeArgs(argumentsInJSON string) string {
	raw := strings.TrimSpace(argumentsInJSON)
	if raw == "" || raw == "{}" {
		return ""
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		if len(raw) > 120 {
			return raw[:120] + "..."
		}
		return raw
	}
	if len(m) == 0 {
		return ""
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	if len(keys) > 5 {
		keys = keys[:5]
		keys = append(keys, "...")
	}
	return strings.Join(keys, ",")
}

func lookupHighRiskSpec(toolName string) (highRiskSpec, bool) {
	name := strings.ToLower(strings.TrimSpace(toolName))
	if spec, ok := explicitHighRiskTools[name]; ok {
		return spec, true
	}

	if _, ok := lowRiskDeleteLikeTools[name]; ok {
		return highRiskSpec{}, false
	}

	// 批量覆盖删除/丢弃类数据操作。
	if strings.HasPrefix(name, "delete_") || strings.HasPrefix(name, "drop_") || strings.Contains(name, "discard") {
		return highRiskSpec{
			Objective:   "删除/丢弃数据",
			Description: "该操作会对数据执行删除或丢弃。",
			Impact:      "可能导致数据不可逆变化。",
			Risk:        "执行后可能无法恢复原始数据。",
		}, true
	}

	return highRiskSpec{}, false
}
