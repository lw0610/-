package flowrun

import (
	"context"
	"fmt"
	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
	"os"
	"path/filepath"
	"strings"
)

func normalizeAcquisitionChoice(plan *flow.FlowPlan, input flow.InputPlan, value any, root string) (any, bool, *StartupIssue) {
	choice, marked, err := flow.ParseInputChoice(value)
	if !marked {
		return value, false, nil
	}
	issue := func(code, message string) (any, bool, *StartupIssue) {
		return nil, true, &StartupIssue{Code: code, Input: input.Key, Acquirable: input.Acquirable, Message: input.Label + "：" + message}
	}
	if err != nil {
		return issue("FLOW_INPUT_INVALID", err.Error())
	}
	entries := map[string]flow.AcquisitionItem{}
	bound := false
	if plan.Acquisition != nil {
		for _, item := range plan.Acquisition.Items {
			if item.Input == input.Key {
				bound = true
				entries[item.Entry] = item
			}
		}
	}
	if !bound {
		return issue("FLOW_INPUT_ACQUIRE_UNSUPPORTED", "该输入没有数据需求，不能自动获取或补齐")
	}
	if choice.Mode == "fill" && input.Type != "folder" {
		return issue("FLOW_INPUT_ACQUIRE_UNSUPPORTED", "只有文件夹输入支持补齐缺项")
	}
	if input.Type != "file" && input.Type != "layer" && input.Type != "folder" {
		return issue("FLOW_INPUT_ACQUIRE_UNSUPPORTED", "此输入类型不支持自动获取")
	}
	if input.Type == "folder" && choice.Source != "" {
		return issue("FLOW_INPUT_SOURCE_INVALID", "清单来源请按行填写 sources")
	}
	if input.Type != "folder" && (len(choice.Sources) > 0 || len(choice.Files) > 0) {
		return issue("FLOW_INPUT_SOURCE_INVALID", "只有清单文件夹支持逐行来源与文件")
	}
	if choice.Mode == "fill" {
		converted, e := convertInput(input, choice.Path, root)
		if e != nil {
			return issue("FLOW_INPUT_INVALID", e.Error())
		}
		choice.Path = converted.(string)
	}
	for name, source := range choice.Sources {
		if _, exists := entries[name]; !exists {
			return issue("FLOW_INPUT_SOURCE_INVALID", "清单没有此行："+name)
		}
		if strings.TrimSpace(source) == "" {
			delete(choice.Sources, name)
		}
	}
	for name, path := range choice.Files {
		item, exists := entries[name]
		if !exists {
			return issue("FLOW_INPUT_INVALID", "清单没有此行："+name)
		}
		converted, e := convertInput(flow.InputPlan{Type: "file"}, path, root)
		if e != nil || converted == nil {
			return issue("FLOW_INPUT_INVALID", "指定的文件不可用："+name)
		}
		absolute := converted.(string)
		expected := resource.DataKindForFile(item.Filename, false)
		actual := resource.DataKindForFile(absolute, false)
		if expected != "" && actual != expected {
			return issue("FLOW_INPUT_INVALID", "指定文件类型不符合清单："+name)
		}
		if strings.EqualFold(filepath.Ext(absolute), ".shp") {
			for _, ext := range []string{".shx", ".dbf"} {
				info, e := os.Stat(strings.TrimSuffix(absolute, filepath.Ext(absolute)) + ext)
				if e != nil || !info.Mode().IsRegular() {
					return issue("FLOW_INPUT_INVALID", "Shapefile 缺少配套文件："+name+ext)
				}
			}
		}
		choice.Files[name] = absolute
	}
	return choice.Value(), true, nil
}

// ValidateInputsWithAcquisition is shared by HTTP checks and run startup. It does no downloads.
func ValidateInputsWithAcquisition(ctx context.Context, plan *flow.FlowPlan, values map[string]any, root string, env datasource.PlanningEnvironment, resolver acquire.ExtentResolver) (*flow.FlowPlan, []InputProblem) {
	normalized, err := resolveInputs(plan, values, "", root, true)
	if err != nil {
		_, problems := validateInputPlan(plan, values, root)
		return plan, problems
	}
	linked, problems := validateInputPlan(plan, normalized, root)
	acquisition, issues := acquire.Plan(ctx, plan, normalized, env, resolver)
	linked.Acquisition = &acquisition
	for _, issue := range issues {
		code := issue.Code
		switch code {
		case "FLOW_ACQUIRE_UNAVAILABLE":
			code = "FLOW_INPUT_ACQUIRE_UNAVAILABLE"
		case "FLOW_ACQUIRE_SOURCE_INVALID":
			code = "FLOW_INPUT_SOURCE_INVALID"
		case "FLOW_ACQUIRE_REGION_AMBIGUOUS":
			code = "FLOW_INPUT_REGION_AMBIGUOUS"
		}
		problems = append(problems, InputProblem{Label: issue.Input, Level: "error", Code: code, Item: issue.Item, Message: issue.Message, Candidates: issue.Candidates, Acquirable: true})
	}
	return linked, filterAcquisitionResourceProblems(linked, problems, acquisition)
}

func filterAcquisitionResourceProblems(plan *flow.FlowPlan, problems []InputProblem, acquisition flow.AcquisitionPlan) []InputProblem {
	suppressed := map[string]bool{}
	for i := range plan.Resources {
		r := &plan.Resources[i]
		filtered := []flow.Diagnostic{}
		for _, d := range r.Problems {
			skip := false
			if d.Code == "FLOW_DATA_FILE_MISSING" || d.Code == "FLOW_DATA_SIDECAR_MISSING" {
				for _, item := range acquisition.Items {
					if item.Resource != r.Key {
						continue
					}
					replaced := item.UserPath != "" || (item.Acquirable && item.Available && (item.Mode == "auto" || item.Mode == "fill"))
					if replaced && strings.Contains(d.Message, "“"+item.Entry+"”") {
						skip = true
						break
					}
				}
			}
			if skip {
				suppressed[d.Code+"\x00"+d.Message] = true
			} else {
				filtered = append(filtered, d)
			}
		}
		r.Problems = filtered
	}
	result := []InputProblem{}
	for _, p := range problems {
		if !suppressed[p.Code+"\x00"+p.Message] {
			result = append(result, p)
		}
	}
	links := []flow.Diagnostic{}
	for _, d := range plan.LinkDiagnostics {
		if !suppressed[d.Code+"\x00"+d.Message] {
			links = append(links, d)
		}
	}
	plan.LinkDiagnostics = links
	plan.Diagnostics = append(append([]flow.Diagnostic{}, plan.CompileDiagnostics...), links...)
	return result
}
func (c *Controller) acquisitionInputPlan(ctx context.Context, plan *flow.FlowPlan, values map[string]any, root string) (*flow.FlowPlan, []InputProblem, error) {
	if plan.Acquisition == nil {
		return plan, nil, nil
	}
	loader, ok := c.options.Compiler.(interface {
		AcquisitionEnvironment(context.Context, string) (datasource.PlanningEnvironment, error)
	})
	if !ok {
		return nil, nil, fmt.Errorf("数据获取目录服务尚未就绪")
	}
	env, err := loader.AcquisitionEnvironment(ctx, plan.Scheme)
	if err != nil {
		return nil, nil, err
	}
	linked, problems := ValidateInputsWithAcquisition(ctx, plan, values, root, env, nil)
	return linked, problems, nil
}

// ValidateAcquisitionChoice validates a single remembered choice without running other inputs.
func ValidateAcquisitionChoice(plan *flow.FlowPlan, input flow.InputPlan, value any, root string) (any, *StartupIssue) {
	normalized, marked, issue := normalizeAcquisitionChoice(plan, input, value, root)
	if !marked {
		return nil, &StartupIssue{Code: "FLOW_INPUT_INVALID", Input: input.Key, Message: "需要来源选择对象"}
	}
	return normalized, issue
}
