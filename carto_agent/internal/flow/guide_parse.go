package flow

import (
	"errors"
	"github.com/autocarto/carto_agent/internal/layers"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/autocarto/carto_agent/internal/resource"
)

var visualReference = regexp.MustCompile(`(内置图层|图层|样式|版面|版式)?[ \t]*「([^」\n]+)」`)
var flowLocalPath = regexp.MustCompile("(?:^|[\\s（(：:，,`\"'])((?:[A-Za-z]:[\\\\/]|/[^/])[^\\s「」<>\"'`，；。]+)")

// ParseGuide only reads metadata and style/layout references. It never turns
// headings, tables, role names, conditions or output prose into execution data.
func ParseGuide(raw string, environments ...GuideEnvironment) *GuideDoc {
	meta, body, offset, headDiagnostics := parseFrontmatter(normalizeMarkdown(raw))
	g := &GuideDoc{Meta: meta, Body: body, Resources: []ResourceDoc{}, Diagnostics: []Diagnostic{}}
	env := GuideEnvironment{}
	if len(environments) > 0 {
		env = environments[0]
	}
	add := func(code string, line int, message string) {
		g.Diagnostics = append(g.Diagnostics, diagnostic("warning", code, "", line, message))
	}
	for _, d := range headDiagnostics {
		add("FLOW_META_MISSING", d.Line, d.Message)
	}
	if strings.TrimSpace(meta.Description) == "" {
		add("FLOW_META_MISSING", 1, "流程头部缺少说明。")
	}
	if utf8.RuneCountInString(meta.Description) > 300 {
		add("FLOW_META_MISSING", 1, "流程说明应不超过 300 字。")
	}
	seen := map[string]bool{}
	for _, match := range visualReference.FindAllStringSubmatchIndex(body, -1) {
		name := strings.TrimSpace(body[match[4]:match[5]])
		typ := ""
		if match[2] >= 0 {
			switch body[match[2]:match[3]] {
			case "样式":
				typ = "style"
			case "版面", "版式":
				typ = "layout"
			}
		}
		if match[2] >= 0 && (body[match[2]:match[3]] == "图层" || body[match[2]:match[3]] == "内置图层") {
			if seen["layer\x00"+name] {
				continue
			}
			seen["layer\x00"+name] = true
			ref := LayerReference{Name: name, Line: offset + lineNumber(body, match[0])}
			if env.Layers != nil {
				item, err := layers.ResolveName(env.Layers, name)
				if err == nil {
					ref.ID = item.ID
					ref.Digest = item.Digest
					if item.Status != "ready" {
						add("FLOW_LAYER_CHANGED", ref.Line, "引用的内置图层需要重新确认："+name)
					}
				} else if errors.Is(err, layers.ErrAmbiguous) {
					add("FLOW_LAYER_AMBIGUOUS", ref.Line, "内置图层别名有歧义："+name)
				} else {
					add("FLOW_LAYER_NOT_FOUND", ref.Line, "需要导入的内置图层："+name)
				}
			}
			g.Layers = append(g.Layers, ref)
			continue
		}
		candidates := []resource.LibraryItem{}
		for _, r := range env.Resources {
			if (r.Type == "style" || r.Type == "layout") && r.Name == name && (typ == "" || r.Type == typ) {
				candidates = append(candidates, r)
			}
		}
		explicit := typ != ""
		if !explicit && len(candidates) == 0 {
			continue
		}
		if typ == "" {
			typ = candidates[0].Type
		}
		key := typ + "\x00" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		line := offset + lineNumber(body, match[0])
		g.Resources = append(g.Resources, ResourceDoc{Label: name, Type: typ, Location: "公共资源库", Line: line})
		if env.Resources != nil && len(candidates) == 0 {
			add("FLOW_RESOURCE_NOT_FOUND", line, "未找到引用的样式或版面：“"+name+"”。")
		}
		if len(candidates) > 1 {
			add("FLOW_RESOURCE_AMBIGUOUS", line, "存在多个同名资源：“"+name+"”，请明确选择。")
		}
	}
	for i, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "负责") || strings.Contains(line, "参与角色") {
			add("FLOW_ROLE_MENTION", offset+i+1, "流程应描述制图要求，不指定负责角色。")
		}
		if flowLocalPath.MatchString(line) {
			add("FLOW_PATH_MENTION", offset+i+1, "流程中包含本机绝对路径；数据位置应在本次对话中提供。")
		}
	}
	return g
}
func containsGuideString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func resourceDocument(raw string, libraries ...*resource.Library) *FlowDoc {
	env := GuideEnvironment{}
	if len(libraries) > 0 && libraries[0] != nil {
		env, _ = GuideEnvironmentFromLibrary(libraries[0])
	}
	g := ParseGuide(raw, env)
	return &FlowDoc{Meta: g.Meta, Resources: g.Resources, Layers: g.Layers, Diagnostics: g.Diagnostics}
}
