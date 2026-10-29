package flow

import (
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/resource"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func guideExample(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "refactor-v6", "05-行军路线图流程示例.md"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.Split(string(raw), "````markdown\n")[1], "\n````")[0]
}
func exampleGuideEnvironment() GuideEnvironment {
	env := GuideEnvironment{Resources: []resource.LibraryItem{}}
	for _, name := range []string{"省域面", "省界", "河流面", "河流线", "高速公路", "国道", "铁路", "县级城市驻地"} {
		env.Resources = append(env.Resources, resource.LibraryItem{ID: "style." + name, Name: name, Type: "style"})
	}
	env.Resources = append(env.Resources, resource.LibraryItem{ID: "layout.test", Name: "四角对齐版式", Type: "layout"})
	return env
}
func TestGuideExample(t *testing.T) {
	raw := guideExample(t)
	g := ParseGuide(raw, exampleGuideEnvironment())
	if len(g.Diagnostics) != 0 || len(g.Resources) != 9 {
		t.Fatalf("references=%+v diagnostics=%+v", g.Resources, g.Diagnostics)
	}
	_, body, _, _ := parseFrontmatter(raw)
	if g.Body != body {
		t.Fatal("body was changed")
	}
	encoded, _ := json.Marshal(g)
	for _, field := range []string{`"data":`, `"method":`, `"checks":`, `"info":`} {
		if strings.Contains(string(encoded), field) {
			t.Fatal("structured execution field survived:", field)
		}
	}
}
func TestGuideDiagnosticCases(t *testing.T) {
	base := "---\n名称: 自由流程\n说明: 自由正文\n版本: 1.0.0\n---\n随意的正文，不需要固定章节。\n"
	empty := GuideEnvironment{Resources: []resource.LibraryItem{}}
	cases := []struct {
		name, raw, code string
		env             GuideEnvironment
	}{
		{"meta", strings.Replace(base, "名称: 自由流程\n", "", 1), "FLOW_META_MISSING", empty},
		{"description", strings.Replace(base, "说明: 自由正文\n", "", 1), "FLOW_META_MISSING", empty},
		{"resource", base + "样式「缺失」", "FLOW_RESOURCE_NOT_FOUND", empty},
		{"layout", base + "版式「缺失」", "FLOW_RESOURCE_NOT_FOUND", empty},
		{"ambiguous", base + "「重复」", "FLOW_RESOURCE_AMBIGUOUS", GuideEnvironment{Resources: []resource.LibraryItem{{ID: "style.a", Name: "重复", Type: "style"}, {ID: "style.b", Name: "重复", Type: "style"}}}},
		{"role", base + "参与角色：制图人员负责制图。", "FLOW_ROLE_MENTION", empty},
		{"path", base + "底图在 /Users/example/map.shp", "FLOW_PATH_MENTION", empty},
		{"windows", base + `底图在 C:\data\map.shp`, "FLOW_PATH_MENTION", empty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := ParseGuide(tc.raw, tc.env)
			found := false
			for _, d := range g.Diagnostics {
				if d.Code == tc.code {
					found = true
				}
				if d.Level == "error" {
					t.Fatalf("advisory became a blocker: %+v", d)
				}
			}
			if !found {
				t.Fatalf("missing %s: %+v", tc.code, g.Diagnostics)
			}
		})
	}
	good := ParseGuide(base+"普通数据名「人口」不属于资源。引用 https://example.test/data；相对路径 examples/map.md。样式「河流线」和版面「四角对齐版式」。", exampleGuideEnvironment())
	if len(good.Diagnostics) != 0 || len(good.Resources) != 2 {
		t.Fatalf("false diagnostics or nonvisual refs: %+v", good)
	}
}
func TestGuideBodyIsOpaque(t *testing.T) {
	body := "## 自己决定的标题\n| 数据 | 可以来自 |\n|---|---|\n| 不存在 | 也不存在 |\n\n## 制作要点\n能力和产出都由模型理解。\n"
	g := ParseGuide("---\n名称: 测试\n说明: 测试\n---\n" + body)
	if g.Body != body || len(g.Diagnostics) != 0 || len(g.Resources) != 0 {
		t.Fatalf("body interpreted: %+v", g)
	}
}
