package flow

import "strings"

// Legacy input metadata is retained only for migration of saved paths.
func deterministicInputType(item InputDoc) (typeName string, implicit bool) {
	switch {
	case item.Type != "":
		return item.Type, false
	case item.Boolean:
		return "boolean", false
	case len(item.Options) > 0:
		return "choice", false
	case strings.Contains(item.Description, "对话") && strings.Contains(item.Description, "原文"):
		return "conversation_text", false
	case strings.Contains(item.Description, "文件夹"):
		return "folder", false
	case strings.Contains(item.Description, "文件"):
		return "file", false
	}
	return "text", true
}

// baseInputs 只依据声明生成输入；输入键等于输入名称，模型增强不会改变它。
func baseInputs(declarations []InputDoc) []InputPlan {
	inputs := make([]InputPlan, 0, len(declarations))
	for _, item := range declarations {
		typeName, _ := deterministicInputType(item)
		var defaultValue any
		if item.Default != "" {
			defaultValue = item.Default
			if typeName == "boolean" {
				switch item.Default {
				case "是", "true":
					defaultValue = true
				case "否", "false":
					defaultValue = false
				}
			}
		}
		inputs = append(inputs, InputPlan{
			Key: item.Label, Label: item.Label, Type: typeName, Required: item.Required,
			Default: defaultValue, Options: append([]string{}, item.Options...), Description: item.Description,
			Remember: (typeName == "file" || typeName == "folder") && !item.NoRemember,
		})
	}
	return inputs
}

// DeclaredInputs exposes the shared deterministic declaration rules to local
// input-memory editing, which must not guess types independently of the plan.
func DeclaredInputs(declarations []InputDoc) []InputPlan { return baseInputs(declarations) }
