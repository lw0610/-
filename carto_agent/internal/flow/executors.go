package flow

// ExecutorSpec declares validation requirements without importing execution code.
type ExecutorSpec struct {
	Type               string   `json:"type"`
	DisplayName        string   `json:"display_name"`
	RequiredInputTypes []string `json:"required_input_types"`
}

func ExecutorSpecs() []ExecutorSpec {
	return []ExecutorSpec{{Type: "route_knowledge", DisplayName: "路线知识抽取", RequiredInputTypes: []string{"conversation_text"}}}
}

func FindExecutorSpec(executor string) (ExecutorSpec, bool) {
	for _, spec := range ExecutorSpecs() {
		if spec.Type == executor {
			return spec, true
		}
	}
	return ExecutorSpec{}, false
}
