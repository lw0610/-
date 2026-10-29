package flow

import (
	"context"
	"fmt"
	"github.com/autocarto/carto_agent/internal/layers"
	"github.com/autocarto/carto_agent/internal/resource"
	"strings"
)

// ReadContext gives the model the selected flow in full, without guessing a
// filesystem path or deriving execution rules from headings/tables.
func (s *GuideStore) ReadContext(ctx context.Context, id string, library *resource.Library) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	resolved, err := s.Resolve(ctx, id, library)
	if err != nil {
		return "", err
	}
	if !resolved.Package.Enabled {
		return "", fmt.Errorf("流程已停用：%s", id)
	}
	var out strings.Builder
	out.WriteString(resolved.Package.GuideMD)
	out.WriteString("\n\n## 系统附加：引用的样式与版面\n")
	if len(resolved.Scope.Linked) == 0 {
		out.WriteString("此流程未引用样式或版面。\n")
	}
	for _, r := range resolved.Scope.Linked {
		if r.Type != "style" && r.Type != "layout" {
			continue
		}
		label := "样式"
		if r.Type == "layout" {
			label = "版面"
		}
		fmt.Fprintf(&out, "- %s（%s）", r.Label, label)
		if r.Geometry != "" {
			fmt.Fprintf(&out, "；适用几何：%s", r.Geometry)
		}
		if r.Description != "" {
			fmt.Fprintf(&out, "；说明：%s", r.Description)
		}
		if r.LabelField != "" {
			fmt.Fprintf(&out, "；标注字段：%s", r.LabelField)
		}
		if r.Paper != "" {
			fmt.Fprintf(&out, "；纸张：%s", r.Paper)
		}
		if r.Orientation != "" {
			fmt.Fprintf(&out, "；方向：%s", r.Orientation)
		}
		if len(r.Elements) > 0 {
			fmt.Fprintf(&out, "；包含元素：%s", strings.Join(r.Elements, "、"))
		}
		for _, d := range r.Problems {
			fmt.Fprintf(&out, "；提示：%s", d.Message)
		}
		out.WriteByte('\n')
	}
	if len(resolved.Doc.Layers) > 0 {
		out.WriteString("\n## 系统附加：引用的内置图层\n")
		catalog, err := layers.NewDefaultLibrary()
		if err != nil {
			return "", err
		}
		items, err := catalog.List()
		if err != nil {
			return "", err
		}
		for _, ref := range resolved.Doc.Layers {
			item, err := layers.ResolveName(items, ref.Name)
			if err != nil {
				fmt.Fprintf(&out, "- %s：%s；运行时应提示并选择其他来源。\n", ref.Name, err)
				continue
			}
			fmt.Fprintf(&out, "- %s；编号：%s；类别：%s；几何：%s；覆盖区域：%s；覆盖范围：%v；标注字段：%s；默认样式：%s；状态：%s\n", ref.Name, item.ID, item.Category, item.Geometry, strings.Join(item.Coverage.Regions, "、"), item.Coverage.BBox, item.LabelField, item.DefaultStyle, item.Status)
		}
	}
	return out.String(), nil
}
