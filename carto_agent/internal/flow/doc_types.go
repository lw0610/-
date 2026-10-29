package flow

import (
	"github.com/autocarto/carto_agent/internal/flow/legacy"
	shareddiagnostic "github.com/autocarto/carto_agent/internal/shared/diagnostic"
	"strings"
)

// FlowMetadata 是 FLOW.md 头部声明；编号的目录回退和生成由存储层负责。
type FlowMetadata struct {
	ID          string   `json:"id,omitempty"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Version     string   `json:"version"`
	Author      string   `json:"author,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// Diagnostic 是解析、编译和运行校验共用的用户可见诊断。
type Diagnostic = shareddiagnostic.Diagnostic

// FlowDoc 只记录确定性解析结果，不包含方案、模型或运行时绑定。
type FlowDoc struct {
	Layers        []LayerReference `json:"layers"`
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
type ConditionRule = legacy.ConditionRule

type OutputDeclaration struct {
	Geometry    string   `json:"geometry,omitempty"`
	Fields      []string `json:"fields,omitempty"`
	Label       string   `json:"label"`
	Type        string   `json:"type"`
	Format      string   `json:"format,omitempty"`
	Required    bool     `json:"required"`
	Description string   `json:"description,omitempty"`
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

// Legacy declarations remain readable for conversion, never execution.
type RoleSlotDoc = legacy.RoleSlotDoc
type RequirementDoc = legacy.RequirementDoc

// CanonicalOutputFormat is shared by new plan normalization and verification of
// persisted plans, whose format may still be a driver name such as GeoPackage.
func CanonicalOutputFormat(format string) string {
	format = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".")
	if format == "geopackage" {
		return "gpkg"
	}
	return format
}
