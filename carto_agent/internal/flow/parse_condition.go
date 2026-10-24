package flow

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	providedCondition = regexp.MustCompile(`^(未提供|提供了?)\s*「([^」]+)」\s*时执行$`)
	valueCondition    = regexp.MustCompile(`^「([^」]+)」\s*为\s*(.+?)\s*时执行$`)
	stepCondition     = regexp.MustCompile(`^第\s*([0-9]+)\s*步完成时执行$`)
)

// ParseCondition 只转换规范列出的确定性句式，未识别的条件返回 nil。
// 输入显示名保留到编译阶段，解析器不生成输入 key，也不调用模型。
func ParseCondition(raw string) *ConditionRule {
	raw = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(raw), "。.!！"))
	if match := providedCondition.FindStringSubmatch(raw); match != nil {
		op := "provided"
		if match[1] == "未提供" {
			op = "not_provided"
		}
		return &ConditionRule{Op: op, Input: strings.TrimSpace(match[2])}
	}
	if match := valueCondition.FindStringSubmatch(raw); match != nil {
		rule := &ConditionRule{Input: strings.TrimSpace(match[1])}
		switch value := strings.TrimSpace(match[2]); value {
		case "是":
			rule.Op = "truthy"
		case "否":
			rule.Op = "falsy"
		default:
			rule.Op, rule.Value = "equals", value
		}
		return rule
	}
	if match := stepCondition.FindStringSubmatch(raw); match != nil {
		index, err := strconv.Atoi(match[1])
		if err == nil && index > 0 {
			return &ConditionRule{Op: "step_status", Step: "s" + strconv.Itoa(index), Status: "completed"}
		}
	}
	return nil
}
