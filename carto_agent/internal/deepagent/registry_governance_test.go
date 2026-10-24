package deepagent

import (
	"context"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/governance"
	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigUsesUnifiedRegistryGovernanceField(t *testing.T) {
	typ := reflect.TypeFor[Config]()
	if _, ok := typ.FieldByName("RegistryGovernance"); !ok {
		t.Fatal("missing unified registry")
	}
	for _, old := range []string{"WorkflowPackageResolver", "SkillPackageResolver"} {
		if _, ok := typ.FieldByName(old); ok {
			t.Fatal("legacy resolver remains", old)
		}
	}
}

var _ governance.GovernedRegistry = (*stubGovernedPackageResolver)(nil)

type stubGovernedPackageResolver struct {
	skillPkg  skill.SkillPackage
	skillOK   bool
	skillErr  error
	skillCall []string
}

func (s *stubGovernedPackageResolver) ResolveSkillPackage(name string) (skill.SkillPackage, bool, error) {
	s.skillCall = append(s.skillCall, name)
	return s.skillPkg, s.skillOK, s.skillErr
}
func (*stubGovernedPackageResolver) LoadToolRegistry() (*toolpkg.Registry, error) { return nil, nil }
func (*stubGovernedPackageResolver) LoadSkillRegistry() (*skill.Registry, error)  { return nil, nil }

func TestAgentConsumesGovernedToolPackageInstructions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "data-methods")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: data-methods\nkind: category\ndescription: 数据方法\ntools: [data_load_vector]\n---\n" + strings.Repeat("使用实际参数核对图层。", 30)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	resolver := &stubGovernedPackageResolver{skillOK: true, skillPkg: skill.SkillPackage{Name: "data-methods", Dir: dir, Source: root}}
	agents, err := createSubAgents(context.Background(), &Config{ChatModel: &fakeToolCallingChatModel{}, BuiltinSkillsDir: root, RegistryGovernance: resolver,
		Tools:              []tool.BaseTool{utils.NewEnhancedTool(&schema.ToolInfo{Name: "data_load_vector", Desc: "加载矢量"}, func(context.Context, map[string]any) (*schema.ToolResult, error) { return &schema.ToolResult{}, nil })},
		SkillRuntimeConfig: &subagents.RuntimeSkillConfig{Global: subagents.RuntimeSkillGlobalConfig{PackageTools: map[string][]string{"data-methods": {"data_load_vector"}}, PackageMetadata: map[string]subagents.RuntimeSkillPackageMetadata{"data-methods": {BodyCharacters: 300}}}, Agents: map[string]subagents.RuntimeSkillAgentConfig{"worker": {Enabled: true, Kind: "agent", DisplayName: "数据专家", Description: "数据处理", ToolPackages: []string{"data-methods"}}}},
	}, nil, nil)
	if err != nil || len(agents) != 1 || agents[0].Name(context.Background()) != "worker" || len(resolver.skillCall) != 1 || resolver.skillCall[0] != "data-methods" {
		t.Fatal("role bypassed governed package middleware", agents, resolver.skillCall, err)
	}
}
func TestCreateSubAgentsRejectsUngovernedMiddlewarePackageLookup(t *testing.T) {
	t.Parallel()

	skillRoot := t.TempDir()
	skillDir := filepath.Join(skillRoot, "governed-knowledge")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: governed-knowledge\ndescription: governed knowledge\nkind: knowledge\n---\n\n# governed\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	_, err := createSubAgents(context.Background(), &Config{
		ChatModel: &fakeToolCallingChatModel{},
		SkillRuntimeConfig: &subagents.RuntimeSkillConfig{
			Agents: map[string]subagents.RuntimeSkillAgentConfig{
				"analyst": {
					DisplayName:   "分析专家",
					Description:   "分析知识",
					Enabled:       true,
					SkillPackages: []string{"governed-knowledge"},
				},
			},
		},
	}, nil, []string{skillRoot})
	if err == nil {
		t.Fatal("expected middleware package governance requirement error")
	}
	if got := err.Error(); !strings.Contains(got, "registry governance is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveMiddlewareSkillDirsUsesGovernedPackageDirs(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	packageDir := filepath.Join(rootDir, "governed-knowledge")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatalf("mkdir package dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte("---\nname: governed-knowledge\ndescription: test\nkind: workflow\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	resolver := &stubGovernedPackageResolver{
		skillOK: true,
		skillPkg: skill.SkillPackage{
			Name:   "governed-knowledge",
			Dir:    packageDir,
			Source: rootDir,
		},
	}

	dirs, err := resolveMiddlewareSkillDirs("analyst", &subagents.RuntimeSkillConfig{
		Agents: map[string]subagents.RuntimeSkillAgentConfig{
			"analyst": {
				DisplayName:   "分析专家",
				Description:   "分析知识",
				Enabled:       true,
				SkillPackages: []string{"governed-knowledge"},
			},
		},
	}, resolver)
	if err != nil {
		t.Fatalf("resolveMiddlewareSkillDirs returned error: %v", err)
	}
	if len(dirs) != 1 || dirs[0] != rootDir {
		t.Fatalf("expected governed skills root dir, got %#v", dirs)
	}
	if len(resolver.skillCall) != 1 || resolver.skillCall[0] != "governed-knowledge" {
		t.Fatalf("expected governed middleware lookup, got %#v", resolver.skillCall)
	}
}

func TestResolveMiddlewareSkillDirsNormalizesSinglePackageSourcePath(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	packageDir := filepath.Join(rootDir, "governed-knowledge")

	resolver := &stubGovernedPackageResolver{
		skillOK: true,
		skillPkg: skill.SkillPackage{
			Name:   "governed-knowledge",
			Dir:    packageDir,
			Source: packageDir,
		},
	}

	dirs, err := resolveMiddlewareSkillDirs("analyst", &subagents.RuntimeSkillConfig{
		Agents: map[string]subagents.RuntimeSkillAgentConfig{
			"analyst": {
				DisplayName:   "分析专家",
				Description:   "分析知识",
				Enabled:       true,
				SkillPackages: []string{"governed-knowledge"},
			},
		},
	}, resolver)
	if err != nil {
		t.Fatalf("resolveMiddlewareSkillDirs returned error: %v", err)
	}
	if len(dirs) != 1 || dirs[0] != rootDir {
		t.Fatalf("expected normalized skills root dir, got %#v", dirs)
	}
}
