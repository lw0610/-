package plan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/flow"
)

type Environment struct {
	ProjectRoot  string
	Capabilities map[string]bool
	// Callbacks are local lookups only. A missing callback fails closed.
	ValidateSource func(findID string, choice any) error
	ValidateData   func(Data) error
	StyleExists    func(string) bool
}

func Decode(raw []byte) (*Plan, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	// The model boundary ignores unrelated metadata; graph semantics are checked once below.
	var p Plan
	if err := d.Decode(&p); err != nil {
		return nil, fmt.Errorf("PLAN_SCHEMA_INVALID: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("PLAN_SCHEMA_INVALID: 只能包含一个计划对象")
	}
	if p.APIVersion == "" {
		p.APIVersion = APIVersion
	}
	if p.APIVersion != APIVersion {
		return nil, fmt.Errorf("PLAN_LEGACY_READ_ONLY: 旧计划仅供查看，请按 carto-plan/v2 重新生成")
	}
	return &p, nil
}
func Validate(p *Plan, env Environment) []flow.Diagnostic {
	if p != nil && p.APIVersion == APIVersion {
		return ValidateV2(p)
	}
	ds := []flow.Diagnostic{}
	add := func(code, step, msg string) {
		ds = append(ds, flow.Diagnostic{Level: "error", Code: code, Step: step, Message: msg})
	}
	if p == nil {
		add("PLAN_SCHEMA_INVALID", "", "计划为空")
		return ds
	}
	copy, err := Clone(p)
	if err != nil {
		add("PLAN_SCHEMA_INVALID", "", "计划不是可序列化的 JSON："+err.Error())
		return ds
	}
	p = copy
	ds = append(ds, NormalizeReferences(p)...)
	if p.APIVersion != LegacyAPIVersion || !Identifier.MatchString(p.ID) || p.Version < 1 || strings.TrimSpace(p.Goal) == "" || len(p.Steps) == 0 || len(p.Steps) > 100 {
		add("PLAN_SCHEMA_INVALID", "", "版本、编号、目标或步骤数量无效")
	}
	if p.Guide != nil && (!Identifier.MatchString(p.Guide.ID) || p.Guide.Version == "") {
		add("PLAN_SCHEMA_INVALID", "", "指南编号或版本无效")
	}
	for _, f := range p.Facts {
		if strings.TrimSpace(f.Name) == "" || f.Value == nil || strings.TrimSpace(f.From) == "" {
			add("PLAN_SCHEMA_INVALID", "", "已知信息必须包含名称、值与来源")
		}
	}
	for _, a := range p.Assumptions {
		if a.Text == "" || (a.Impact != "low" && a.Impact != "high") {
			add("PLAN_SCHEMA_INVALID", "", "假设必须包含内容和 low/high 影响")
		}
	}
	steps := map[string]int{}
	acquireAt := map[string]int{}
	data := map[string]Data{}
	names := map[string]bool{}
	for i, s := range p.Steps {
		if _, exists := steps[s.ID]; exists || !Identifier.MatchString(s.ID) {
			add("PLAN_REF_INVALID", s.ID, "步骤编号无效或重复")
		}
		steps[s.ID] = i
		if s.Kind == "acquire" {
			for _, id := range s.Data {
				if _, exists := acquireAt[id]; exists {
					add("PLAN_REF_INVALID", s.ID, "同一数据不能重复获取："+id)
				}
				acquireAt[id] = i
			}
		}
	}
	for _, d := range p.Data {
		if d.Kind != "" && d.Kind != "file" && d.Kind != "dataset" {
			add("PLAN_SCHEMA_INVALID", "", d.Name+" 的 kind 必须为 file 或 dataset；几何类型用 geometry 声明")
		}
		if _, exists := data[d.ID]; exists || !Identifier.MatchString(d.ID) || d.Name == "" || names[d.Name] {
			add("PLAN_REF_INVALID", "", "数据编号或名称无效或重复")
		}
		data[d.ID] = d
		names[d.Name] = true
		switch d.From {
		case "conversation":
		case "user":
			path := d.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join(env.ProjectRoot, path)
			}
			path = strings.Split(path, "|layername=")[0]
			if d.Path == "" {
				add("PLAN_DATA_MISSING", "", d.Name+" 缺少路径")
			} else if _, err := os.Stat(path); err != nil {
				add("PLAN_DATA_MISSING", "", d.Name+" 路径不存在")
			} else if env.ValidateData != nil {
				if err := env.ValidateData(d); err != nil {
					add("PLAN_DATA_MISSING", "", d.Name+"："+err.Error())
				}
			} else if d.Geometry != "" {
				add("PLAN_DATA_MISSING", "", d.Name+" 无法核验几何类型")
			}
		case "project", "session":
			if env.ValidateData == nil {
				add("PLAN_DATA_MISSING", "", d.Name+" 无法核实当前数据")
			} else if err := env.ValidateData(d); err != nil {
				add("PLAN_DATA_MISSING", "", d.Name+"："+err.Error())
			}
		case "step":
			// Producer/output binding is checked by NormalizeReferences. Name is
			// a display label and does not select an output after normalization.
		case "acquire":
			if d.FindID == "" || env.ValidateSource == nil {
				add("PLAN_SOURCE_NOT_FROM_FIND", "", d.Name+" 缺少有效候选查询")
			} else if err := env.ValidateSource(d.FindID, d.Choice); err != nil {
				add("PLAN_SOURCE_NOT_FROM_FIND", "", d.Name+"："+err.Error())
			}
		default:
			add("PLAN_SCHEMA_INVALID", "", d.Name+" 的数据来路无效")
		}
		if d.Style != "" && (env.StyleExists == nil || !env.StyleExists(d.Style)) {
			add("PLAN_STYLE_NOT_FOUND", "", d.Name+" 引用的样式不存在："+d.Style)
		}
	}

	produced := map[string]flow.OutputDeclaration{}
	for i, s := range p.Steps {
		if s.Title == "" || (s.Kind != "" && s.Kind != "step" && s.Kind != "acquire" && s.Kind != "confirm") {
			add("PLAN_SCHEMA_INVALID", s.ID, "步骤标题或类型无效")
		}
		if s.Kind != "acquire" && s.Kind != "confirm" && (len(s.Capabilities) == 0 || s.Instructions == "" || len(s.Outputs) == 0) {
			add("PLAN_SCHEMA_INVALID", s.ID, "普通步骤必须声明能力、做法与可核验产出")
		}
		if s.Kind == "acquire" && len(s.Data) == 0 {
			add("PLAN_SCHEMA_INVALID", s.ID, "获取步骤必须指明数据")
		}
		for _, id := range s.DependsOn {
			idx, ok := steps[id]
			if !ok || idx >= i {
				add("PLAN_REF_INVALID", s.ID, "依赖必须指向更早的步骤："+id)
			}
		}
		for refIndex, id := range append(append([]string{}, s.Uses...), s.Data...) {
			isUse := refIndex < len(s.Uses)
			d, ok := data[id]
			if !ok {
				// Unresolved uses already carry precise normalization diagnostics,
				// including output choices and valid data IDs. Do not obscure them
				// with a second generic "data missing" message.
				if !isUse {
					add("PLAN_REF_INVALID", s.ID, fmt.Sprintf("data 中未声明的数据 ID %q；可用数据 ID：%s", id, strings.Join(precedingDataIDs(p, i), "、")))
				}
				continue
			}
			if d.From == "step" {
				if idx, exists := steps[d.Step]; !exists || idx >= i {
					add("PLAN_REF_INVALID", s.ID, "不能使用尚未产生的数据："+id)
				}
			}
			if d.From == "acquire" && (isUse || s.Kind != "acquire") {
				if at, explicit := acquireAt[id]; explicit && at >= i {
					add("PLAN_REF_INVALID", s.ID, "获取步骤必须在使用数据之前："+id)
				}
			}
		}
		for _, id := range s.Data {
			if d, ok := data[id]; ok && d.From != "acquire" && s.Kind == "acquire" {
				add("PLAN_REF_INVALID", s.ID, "获取步骤仅能获取 acquire 数据："+id)
			}
		}
		unknown := []string{}
		for _, c := range s.Capabilities {
			if !env.Capabilities[c] {
				unknown = append(unknown, c)
			}
		}
		if len(unknown) > 0 {
			add("PLAN_CAPABILITY_UNKNOWN", s.ID, "capabilities 必须使用当前能力目录的工具包 ID，不能填具体工具名；未知 ID："+strings.Join(unknown, "、")+"。从返回的 allowed_capabilities 中选择，不猜测替代名称。")
		}
		labels := map[string]bool{}
		for _, o := range s.Outputs {
			if !ValidOutput(o) || labels[o.Label] {
				add("PLAN_OUTPUT_INVALID", s.ID, fmt.Sprintf("步骤 outputs 中 label 必须非空且在本步骤内唯一；type 只能是 %s；当前 label=%q、type=%q", strings.Join(outputTypes, "/"), o.Label, o.Type))
			}
			labels[o.Label] = true
			produced[o.Label] = o
		}
	}

	for _, o := range p.Deliverables {
		source, ok := produced[o.Label]
		if !ValidOutput(o) || !ok || source.Type != o.Type || (o.Format != "" && source.Format != o.Format) {
			add("PLAN_OUTPUT_INVALID", "", fmt.Sprintf("deliverables 的 label、type 和指定的 format 必须匹配某一步 outputs 中的同一产出；当前 label=%q、type=%q、format=%q；已声明产出：%s", o.Label, o.Type, o.Format, declaredOutputs(p)))
		}
	}
	return ds
}
func ValidOutput(o flow.OutputDeclaration) bool {
	if strings.TrimSpace(o.Label) == "" {
		return false
	}
	for _, allowed := range outputTypes {
		if o.Type == allowed {
			return true
		}
	}
	return false
}
func RequireValid(p *Plan, env Environment) error {
	ds := Validate(p, env)
	if flow.HasErrors(ds) {
		return NewValidationError(ds, env)
	}
	return nil
}
