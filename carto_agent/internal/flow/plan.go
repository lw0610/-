package flow

import (
	"fmt"
	"time"

	"github.com/autocarto/carto_agent/internal/resource"
)

const PlanAPIVersion = "flow-plan/v1"
const CompilerVersion = "3"

// 计划状态：ready 全部增强完成；enriching 后台任务进行中；partial 部分未完善；basic 没有编译模型。
const (
	PlanStatusReady     = "ready"
	PlanStatusEnriching = "enriching"
	PlanStatusPartial   = "partial"
	PlanStatusBasic     = "basic"
)

// 步骤增强状态：ready 已采用模型结果；pending 尚未完成；failed 最近一次失败；none 无需或无法增强。
const (
	StepEnrichmentReady   = "ready"
	StepEnrichmentPending = "pending"
	StepEnrichmentFailed  = "failed"
	StepEnrichmentNone    = "none"
)

// EnrichmentStatus 汇总执行计划中模型增强的完成情况，供界面显示进度。
// Done/Total 只计步骤；Flow 是流程级增强（隐式输入类型、输入约束、触发词）的状态。
type EnrichmentStatus struct {
	Flow      string        `json:"flow"`
	Done      int           `json:"done"`
	Total     int           `json:"total"`
	JobID     string        `json:"job_id,omitempty"`
	JobState  string        `json:"job_state,omitempty"`
	Failed    []StepFailure `json:"failed"`
	UpdatedAt *time.Time    `json:"updated_at,omitempty"`
}

type StepFailure struct {
	StepID string `json:"step_id"`
	Title  string `json:"title,omitempty"`
	Reason string `json:"reason"`
}

// FlowPlan 是方案相关的只读编译快照；运行前须通过 EnsureRunnable 检查当前指纹。
// CompiledAt 是计划内容最后变化的时间：FLOW.md 保存时间与所用增强记录时间中较晚者。
type FlowPlan struct {
	Requirements       []RequirementDoc  `json:"requirements,omitempty"`
	Acquisition        *AcquisitionPlan  `json:"acquisition_plan,omitempty"`
	AutoAcquire        string            `json:"auto_acquire,omitempty"`
	RoleSlots          []RoleSlotPlan    `json:"role_slots,omitempty"`
	FlowRoles          []FlowRoleSpec    `json:"flow_roles,omitempty"`
	PlanStatus         string            `json:"plan_status"`
	Enrichment         EnrichmentStatus  `json:"enrichment"`
	CompileKey         string            `json:"compile_key"`
	LinkFingerprint    string            `json:"link_fingerprint"`
	LinkedAt           time.Time         `json:"linked_at"`
	CompileDiagnostics []Diagnostic      `json:"compile_diagnostics"`
	LinkDiagnostics    []Diagnostic      `json:"link_diagnostics"`
	FlowDir            string            `json:"flow_dir"`
	Lock               *resource.Lock    `json:"lock,omitempty"`
	APIVersion         string            `json:"api_version"`
	FlowID             string            `json:"flow_id"`
	FlowVersion        string            `json:"flow_version"`
	Name               string            `json:"name"`
	Description        string            `json:"description"`
	Scheme             string            `json:"scheme"`
	SchemeRevision     string            `json:"scheme_revision"`
	CatalogRevision    string            `json:"catalog_revision"`
	CompiledAt         time.Time         `json:"compiled_at"`
	Compiler           CompilerInfo      `json:"compiler"`
	Triggers           []string          `json:"triggers"`
	Applicability      string            `json:"applicability"`
	Inputs             []InputPlan       `json:"inputs"`
	InputConstraints   []InputConstraint `json:"input_constraints"`
	Resources          []ResourcePlan    `json:"resources"`
	Rules              []string          `json:"rules"`
	Notes              []string          `json:"notes"`
	Deliverables       string            `json:"deliverables"`
	Steps              []StepPlan        `json:"steps"`
	Diagnostics        []Diagnostic      `json:"diagnostics"`
	// Roles 保留编译时的有序角色及实际能力，供 auto 步骤确定性选角。
	Roles []RoleSpec `json:"roles"`
}

type CompilerInfo struct {
	Version string `json:"version"`
	Model   string `json:"model"`
}

// InputPlan.Key 等于输入名称，基础计划与模型增强后的计划保持同一个键。
type InputPlan struct {
	Acquirable       bool     `json:"acquirable,omitempty"`
	AcquireAvailable bool     `json:"acquire_available,omitempty"`
	Modes            []string `json:"modes,omitempty"`
	Key              string   `json:"key"`
	Label            string   `json:"label"`
	Type             string   `json:"type"`
	Required         bool     `json:"required"`
	Default          any      `json:"default"`
	Options          []string `json:"options"`
	Description      string   `json:"description"`
	// Remember 表示文件 / 文件夹输入在本机记住上次的选择。
	Remember bool `json:"remember,omitempty"`
}

type InputConstraint struct {
	Op     string   `json:"op"`
	Inputs []string `json:"inputs"`
}

type ResourcePlan struct {
	EntryCount       int                      `json:"entry_count"`
	Key              string                   `json:"key"`
	ID               string                   `json:"id"`
	Label            string                   `json:"label"`
	Name             string                   `json:"name"`
	Type             string                   `json:"type"`
	Version          string                   `json:"version"`
	Owner            string                   `json:"owner"`
	Origin           string                   `json:"origin"`
	Location         string                   `json:"location"`
	Root             string                   `json:"root"`
	Entry            string                   `json:"entry"`
	Format           string                   `json:"format"`
	Geometry         string                   `json:"geometry,omitempty"`
	BaseDirFromInput string                   `json:"base_dir_from_input,omitempty"`
	BaseDir          string                   `json:"base_dir,omitempty"`
	BindingKey       string                   `json:"binding_key,omitempty"`
	Digest           string                   `json:"digest"`
	Overrides        string                   `json:"overrides,omitempty"`
	OverriddenBy     string                   `json:"overridden_by,omitempty"`
	Description      string                   `json:"description,omitempty"`
	Source           resource.ResourceSource  `json:"source,omitempty"`
	Legacy           bool                     `json:"legacy"`
	Entries          []resource.ResourceEntry `json:"entries"`
	Problems         []Diagnostic             `json:"problems"`
	BaseDirBinding   string                   `json:"base_dir_binding"`
}

func ResourcePlanFromResolved(r *resource.ResolvedResource) ResourcePlan {
	if r == nil {
		return ResourcePlan{}
	}
	r = r.Clone()
	return ResourcePlan{Key: r.Key, ID: r.ID, Label: r.Label, Name: r.Name, Type: r.Type, Version: r.Version, Owner: r.Owner, Origin: r.Origin, Location: r.Location, Root: r.Root, Entry: r.Entry, Format: r.Format, Geometry: r.Geometry, BaseDir: r.BaseDir, BaseDirFromInput: r.BaseDirFromInput, BindingKey: r.BindingKey, Digest: r.Digest, Overrides: r.Overrides, OverriddenBy: r.OverriddenBy, Description: r.Description, Source: r.Source, Legacy: r.Legacy, Entries: r.Entries, Problems: r.Problems, BaseDirBinding: r.BindingKey}
}
func (r ResourcePlan) ResolvedResource() *resource.ResolvedResource {
	result := &resource.ResolvedResource{Key: r.Key, ID: r.ID, Label: r.Label, Name: r.Name, Type: r.Type, Version: r.Version, Owner: r.Owner, Origin: r.Origin, Location: r.Location, Root: r.Root, Entry: r.Entry, Format: r.Format, Geometry: r.Geometry, BaseDir: r.BaseDir, BaseDirFromInput: r.BaseDirFromInput, BindingKey: r.BindingKey, Digest: r.Digest, Overrides: r.Overrides, OverriddenBy: r.OverriddenBy, Description: r.Description, Source: r.Source, Legacy: r.Legacy, Entries: r.Entries, Problems: r.Problems}
	result.ResolvedPath = r.Entry
	if r.Type == "data_folder" {
		result.ResolvedPath = r.Root
	}
	if result.BindingKey == "" {
		result.BindingKey = r.BaseDirBinding
	}
	return result.Clone()
}

type StepPlan struct {
	AcquisitionRequirement string        `json:"acquisition_requirement,omitempty"`
	RoleSlot               string        `json:"role_slot,omitempty"`
	RoleSource             string        `json:"role_source,omitempty"`
	RequiredPackages       []string      `json:"required_packages,omitempty"`
	GrantedPackages        []string      `json:"granted_packages,omitempty"`
	Unresolved             string        `json:"unresolved,omitempty"`
	ID                     string        `json:"id"`
	Index                  int           `json:"index"`
	Title                  string        `json:"title"`
	Kind                   string        `json:"kind"`
	Role                   string        `json:"role"`
	RoleLabel              string        `json:"role_label"`
	Executor               string        `json:"executor,omitempty"`
	Condition              *ConditionDoc `json:"condition"`
	Instructions           string        `json:"instructions"`
	// Completion 保留原文，用于步骤消息及回执修复；不以推断产出替代验收原文。
	Completion          string              `json:"completion"`
	Refs                PlanRefs            `json:"refs"`
	SuggestedTools      []string            `json:"suggested_tools"`
	ExpectedOutputs     []OutputDeclaration `json:"expected_outputs"`
	OnFailure           string              `json:"on_failure"`
	MissingCapabilities []string            `json:"missing_capabilities"`
	Notes               string              `json:"notes,omitempty"`
	Enrichment          string              `json:"enrichment,omitempty"`
}

type PlanRefs struct {
	Resources []string    `json:"resources"`
	Inputs    []string    `json:"inputs"`
	Outputs   []OutputRef `json:"outputs"`
}

// RoleSpec.ToolPackages 和 Tools 是合并共享包、系统包及声明式自动挂载后的实际授权。
type RoleSpec struct {
	ID           string   `json:"id"`
	DisplayName  string   `json:"display_name"`
	Description  string   `json:"description,omitempty"`
	Kind         string   `json:"kind"`
	Executor     string   `json:"executor,omitempty"`
	FlowOnly     bool     `json:"flow_only"`
	Enabled      bool     `json:"enabled"`
	IsMain       bool     `json:"is_main"`
	ToolPackages []string `json:"tool_packages"`
	Tools        []string `json:"tools"`
}

func HasErrors(diagnostics []Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Level == "error" {
			return true
		}
	}
	return false
}

func (p *FlowPlan) HasErrors() bool { return p != nil && HasErrors(p.Diagnostics) }

// ValidationError 让运行控制器取得完整校验结果，不能仅因编译返回计划就启动执行。
type ValidationError struct {
	Plan        *FlowPlan
	Diagnostics []Diagnostic
}

func (e *ValidationError) Error() string {
	count := 0
	for _, diagnostic := range e.Diagnostics {
		if diagnostic.Level == "error" {
			count++
		}
	}
	return fmt.Sprintf("流程存在 %d 个错误，请修复后再运行", count)
}

type RoleSlotPlan struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Packages    []string       `json:"packages"`
	Executor    string         `json:"executor,omitempty"`
	NeedsVision bool           `json:"needs_vision,omitempty"`
	Resolution  RoleResolution `json:"resolution"`
}
type RoleResolution struct {
	Source    string   `json:"source"`
	RoleID    string   `json:"role_id,omitempty"`
	RoleLabel string   `json:"role_label,omitempty"`
	Granted   []string `json:"granted"`
	Reason    string   `json:"reason,omitempty"`
	Note      string   `json:"note,omitempty"`
}
type FlowRoleSpec struct {
	ID           string   `json:"id"`
	Slot         string   `json:"slot"`
	DisplayName  string   `json:"display_name"`
	Description  string   `json:"description,omitempty"`
	Kind         string   `json:"kind"`
	Executor     string   `json:"executor,omitempty"`
	ToolPackages []string `json:"tool_packages"`
	Provider     string   `json:"provider,omitempty"`
}

func (r FlowRoleSpec) RoleSpec() RoleSpec {
	return RoleSpec{ID: r.ID, DisplayName: r.DisplayName, Description: r.Description, Kind: r.Kind, Executor: r.Executor, Enabled: true, FlowOnly: true, ToolPackages: append([]string{}, r.ToolPackages...)}
}
