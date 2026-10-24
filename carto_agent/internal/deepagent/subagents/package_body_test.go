package subagents

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

func TestCategoryMethodBodiesRespectThresholdOwnershipAndPolicy(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"long": strings.Repeat("中", 200), "short": strings.Repeat("中", 199), "foundation": strings.Repeat("基", 200), "hidden": strings.Repeat("隐", 200), "unassigned": strings.Repeat("外", 200)} {
		writeCategoryPackage(t, root, name, []string{"tool_" + name})
		path := filepath.Join(root, name, "SKILL.md")
		raw, _ := os.ReadFile(path)
		if name == "foundation" {
			raw = []byte(strings.Replace(string(raw), "kind: category", "kind: category\nsystem: true", 1))
		}
		if name == "hidden" {
			raw = []byte(strings.Replace(string(raw), "kind: category", "kind: category\nhidden: true\nauto_mount: executor:test", 1))
		}
		if err := os.WriteFile(path, append(raw, []byte("# "+strings.Repeat("标题", 300)+"\n\n"+body)...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := LoadPackageCatalogMultiDir(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &RuntimeSkillConfig{Global: RuntimeSkillGlobalConfig{PackageTools: catalog.Tools, PackageMetadata: catalog.Metadata}, Agents: map[string]RuntimeSkillAgentConfig{"worker": {SkillPackages: []string{"long", "short"}}, "executor": {Executor: "test"}}}
	if got := cfg.GetAgentMiddlewareSkills("worker"); !reflect.DeepEqual(got, []string{"long", "foundation"}) {
		t.Fatalf("worker methods=%v", got)
	}
	if got := cfg.GetAgentMiddlewareSkills("executor"); !reflect.DeepEqual(got, []string{"foundation", "hidden"}) {
		t.Fatalf("executor methods=%v", got)
	}
	if count := packageBodyCharacters([]byte("---\nname: x\n---\n标题\n===\n# 标题\n\n中 文\n")); count != 2 {
		t.Fatalf("headings or whitespace counted: %d", count)
	}
}

func TestBuiltinGeometryInstructionsReadableThroughActualSkillMiddleware(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../autocarto/skills/system"))
	catalog, err := LoadPackageCatalogMultiDir(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &RuntimeSkillConfig{Global: RuntimeSkillGlobalConfig{PackageTools: catalog.Tools, PackageMetadata: catalog.Metadata}, Agents: map[string]RuntimeSkillAgentConfig{"worker": {SkillPackages: []string{"vector-geometry"}}}}
	mw, err := LoadSkillMiddleware(context.Background(), "worker", []string{root}, nil, cfg)
	if err != nil || mw == nil {
		t.Fatalf("middleware=%v err=%v", mw, err)
	}
	for _, candidate := range mw.AdditionalTools {
		info, err := candidate.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info.Name != "skill" {
			continue
		}
		invokable := candidate.(tool.InvokableTool)
		body, err := invokable.InvokableRun(context.Background(), `{"skill":"vector-geometry"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, "references/缓冲与邻近分析.md") || !strings.Contains(body, "只有分析、量算明确需要时才转换") {
			t.Fatalf("missing actual merged instructions: %s", body)
		}
		return
	}
	t.Fatal("actual middleware did not expose skill reader")
}
