package flow

import shareddiagnostic "github.com/autocarto/carto_agent/internal/shared/diagnostic"

// FlowMetadata 是 FLOW.md 头部声明；编号的目录回退和生成由存储层负责。
type FlowMetadata struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
	Author      string `json:"author,omitempty"`
}

// Diagnostic 是解析、编译和运行校验共用的用户可见诊断。
type Diagnostic = shareddiagnostic.Diagnostic

// FlowDoc 只记录确定性解析结果，不包含方案、模型或运行时绑定。
type FlowDoc struct {
	RoleSlots     []RoleSlotDoc    `json:"role_slots"`
	Requirements  []RequirementDoc `json:"requirements"`
	Meta          FlowMetadata     `json:"meta"`
	Applicability string           `json:"applicability"`
	Inputs        []InputDoc       `json:"inputs"`
	Resources     []ResourceDoc    `json:"resources"`
	Steps         []StepDoc        `json:"steps"`
	Rules         []string         `json:"rules"`
	Deliverables  string           `json:"deliverables"`
	Notes         []DocSection     `json:"notes"`
	Diagnostics   []Diagnostic     `json:"diagnostics"`
}

type InputDoc struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
	Default     string `json:"default,omitempty"`
	// Type 来自括号中的显式类型属性；为空时由确定性规则推断。
	Type              string   `json:"type,omitempty"`
	NoRemember        bool     `json:"no_remember,omitempty"`
	Options           []string `json:"options"`
	Boolean           bool     `json:"boolean,omitempty"`
	Alternatives      []string `json:"alternatives"`
	Attributes        []string `json:"attributes"`
	UnknownAttributes []string `json:"unknown_attributes"`
	Line              int      `json:"line,omitempty"`
}

type ResourceDoc struct {
	FolderInput string `json:"folder_input,omitempty"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Location    string `json:"location"`
	Line        int    `json:"line,omitempty"`
}

type StepDoc struct {
	Capabilities         []string            `json:"capabilities"`
	OptionalCapabilities []string            `json:"optional_capabilities"`
	ID                   string              `json:"id"`
	Index                int                 `json:"index"`
	Title                string              `json:"title"`
	Role                 string              `json:"role,omitempty"`
	Condition            *ConditionDoc       `json:"condition,omitempty"`
	Instructions         string              `json:"instructions"`
	Completion           string              `json:"completion,omitempty"`
	ExpectedOutputs      []OutputDeclaration `json:"expected_outputs"`
	Refs                 DocRefs             `json:"refs"`
	OnFailure            string              `json:"on_failure"`
	Line                 int                 `json:"line,omitempty"`
}

type ConditionDoc struct {
	Text string         `json:"text"`
	Rule *ConditionRule `json:"rule,omitempty"`
}

// ConditionRule.Input 暂存输入显示名，编译时才转换为输入 key。
type ConditionRule struct {
	Op     string          `json:"op"`
	Input  string          `json:"input,omitempty"`
	Value  any             `json:"value,omitempty"`
	Step   string          `json:"step,omitempty"`
	Status string          `json:"status,omitempty"`
	Label  string          `json:"label,omitempty"`
	Args   []ConditionRule `json:"args,omitempty"`
	Arg    *ConditionRule  `json:"arg,omitempty"`
}

type OutputDeclaration struct {
	Label       string `json:"label"`
	Type        string `json:"type"`
	Format      string `json:"format,omitempty"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

type OutputRef struct {
	Step  string `json:"step"`
	Label string `json:"label"`
}

type DocRefs struct {
	Resources  []string    `json:"resources"`
	Inputs     []string    `json:"inputs"`
	Outputs    []OutputRef `json:"outputs"`
	Unresolved []string    `json:"unresolved"`
}

type DocSection struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Line    int    `json:"line,omitempty"`
}

// RoleSlotDoc describes a requested role without binding it to a scheme.
type RoleSlotDoc struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Packages    []string `json:"packages"`
	Executor    string   `json:"executor,omitempty"`
	NeedsVision bool     `json:"needs_vision,omitempty"`
	SchemeOnly  bool     `json:"scheme_only,omitempty"`
	Line        int      `json:"line,omitempty"`
}

// RequirementDoc preserves author-supplied names and references for later binding.
type RequirementDoc struct {
	Name               string            `json:"name"`
	List               string            `json:"list,omitempty"`
	Category           string            `json:"category,omitempty"`
	Level              string            `json:"level,omitempty"`
	Extent             string            `json:"extent"`
	Scale              string            `json:"scale"`
	Params             map[string]string `json:"params"`
	Fields             map[string]string `json:"fields"`
	RecommendedSources []string          `json:"recommended_sources"`
	ExcludedSources    []string          `json:"excluded_sources"`
	Acquisition        string            `json:"acquisition"`
	Line               int               `json:"line,omitempty"`
}
