package flow

import (
	"fmt"
	"github.com/autocarto/carto_agent/internal/datasource"
	"regexp"
	"strconv"
	"strings"
)

func (e Environment) AcquisitionEnvironment() datasource.PlanningEnvironment {
	return datasource.PlanningEnvironment{Catalog: e.DataSources, Integrations: e.Integrations, Secrets: e.Secrets, AutoAcquire: e.AutoAcquire, WebSearch: e.WebSearchEnabled, MaxDownloadMB: e.MaxDownloadMB}
}

var extentExpression = regexp.MustCompile(`^「([^」]+)」(?:\s*外扩\s*([0-9]+(?:\.[0-9]+)?)\s*[%％])?$`)

func RequirementFields(raw string) map[string]string {
	result := map[string]string{}
	for _, field := range strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == '；' || r == '、' || r == ',' }) {
		parts := strings.SplitN(strings.TrimSpace(field), "=", 2)
		target := strings.TrimSpace(parts[0])
		if target == "" {
			continue
		}
		standard := target
		if len(parts) == 2 {
			standard = strings.TrimSpace(parts[1])
		}
		result[target] = strings.ToLower(standard)
	}
	return result
}
func ParseScale(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "自动" {
		return 0, nil
	}
	value = strings.TrimPrefix(strings.TrimPrefix(value, "1:"), "1：")
	value = strings.ReplaceAll(value, ",", "")
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("比例尺须为自动或 1:N")
	}
	return n, nil
}
func RequirementExtent(expression string, plan *FlowPlan, catalog *datasource.Catalog) (*ExtentPlan, error) {
	if strings.TrimSpace(expression) == "全国" {
		if catalog == nil {
			return &ExtentPlan{Source: expression, Region: "全国", Pending: true}, nil
		}
		region, err := catalog.ResolveRegion("全国")
		return &ExtentPlan{BBox: region.BBox, Source: expression, Region: "全国"}, err
	}
	matches := extentExpression.FindStringSubmatch(strings.TrimSpace(expression))
	if matches == nil {
		return nil, fmt.Errorf("范围须为全国或「输入或产出」，可附外扩百分比")
	}
	extent := &ExtentPlan{Source: matches[1], Pending: true}
	if matches[2] != "" {
		extent.ExpandPercent, _ = strconv.ParseFloat(matches[2], 64)
		if extent.ExpandPercent > 1000 {
			return nil, fmt.Errorf("外扩比例不能大于 1000%%")
		}
	}
	inputFound := false
	for _, input := range plan.Inputs {
		if input.Key == extent.Source {
			inputFound = true
		}
	}
	for _, step := range plan.Steps {
		for _, output := range step.ExpectedOutputs {
			if output.Label == extent.Source {
				extent.DependencyStep = step.ID
				extent.DependencyOutput = output.Label
				return extent, nil
			}
		}
	}
	if inputFound {
		return extent, nil
	}
	return nil, fmt.Errorf("范围引用不存在：%s", extent.Source)
}
