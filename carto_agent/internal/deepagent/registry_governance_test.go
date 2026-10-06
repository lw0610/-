package deepagent

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/governance"
	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
	"github.com/autocarto/carto_agent/internal/workflow"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

func TestConfigUsesUnifiedRegistryGovernanceField(t *testing.T) {
	t.Parallel()

	configType := reflect.TypeFor[Config]()
	if _, ok := configType.FieldByName("RegistryGovernance"); !ok {
		t.Fatal("expected Config to expose unified RegistryGovernance field")
	}
	if _, ok := configType.FieldByName("WorkflowPackageResolver"); ok {
		t.Fatal("expected Config to stop exposing WorkflowPackageResolver")
	}
	if _, ok := configType.FieldByName("SkillPackageResolver"); ok {
		t.Fatal("expected Config to stop exposing SkillPackageResolver")
	}
}

var _ governance.GovernedRegistry = (*stubWorkflowPackageResolver)(nil)

type stubWorkflowPackageResolver struct {
	called    bool
	pkg       skill.SkillPackage
	ok        bool
	err       error
	skillPkg  skill.SkillPackage
	skillOK   bool
	skillErr  error
	skillCall []string
}

func (s *stubWorkflowPackageResolver) ResolveWorkflowPackage(string) (skill.SkillPackage, bool, error) {
	s.called = true
	return s.pkg, s.ok, s.err
}

func (s *stubWorkflowPackageResolver) ResolveSkillPackage(packageName string) (skill.SkillPackage, bool, error) {
	s.skillCall = append(s.skillCall, packageName)
	return s.skillPkg, s.skillOK, s.skillErr
}

func (s *stubWorkflowPackageResolver) LoadToolRegistry() (*toolpkg.Registry, error) {
	return nil, nil
}

func (s *stubWorkflowPackageResolver) LoadSkillRegistry() (*skill.Registry, error) {
	return nil, nil
}

func TestAgentConsumesGovernedRegistry(t *testing.T) {
	t.Parallel()

	resolver := &stubWorkflowPackageResolver{
		ok: true,
		pkg: skill.SkillPackage{
			Name: "governed-workflow",
			Workflow: &workflow.Definition{
				Metadata: workflow.Metadata{Name: "governed-workflow"},
				Spec:     workflow.Spec{Steps: []workflow.Step{{ID: "step_1", Type: workflow.StepTypeTool, Tool: "data_load_vector"}}},
			},
		},
	}

	agents, err := createSubAgents(context.Background(), &Config{
		ChatModel: &fakeToolCallingChatModel{},
		Tools: []tool.BaseTool{utils.NewEnhancedTool(&schema.ToolInfo{Name: "data_load_vector", Desc: "加载矢量"}, func(context.Context, map[string]any) (*schema.ToolResult, error) {
			return &schema.ToolResult{}, nil
		})},
		RegistryGovernance: resolver,
		SkillRuntimeConfig: &subagents.RuntimeSkillConfig{
			Agents: map[string]subagents.RuntimeSkillAgentConfig{
				"data_engineer": {
					DisplayName:     "数据工程专家",
					Description:     "处理数据",
					Enabled:         true,
					WorkflowPackage: "governed-workflow",
				},
			},
		},
	}, nil, nil)
	if err != nil {
		t.Fatalf("createSubAgents returned error: %v", err)
	}
	if !resolver.called {
		t.Fatal("expected workflow package resolution to come from governed registry")
	}
	if len(agents) != 1 || agents[0].Name(context.Background()) != "data_engineer" {
		t.Fatalf("unexpected agents: %#v", agents)
	}
}

func TestWorkflowSubAgentRejectsUngovernedWorkflowPackageLookup(t *testing.T) {
	t.Parallel()

	_, err := createSubAgents(context.Background(), &Config{
		ChatModel: &fakeToolCallingChatModel{},
		Tools: []tool.BaseTool{utils.NewEnhancedTool(&schema.ToolInfo{Name: "data_load_vector", Desc: "加载矢量"}, func(context.Context, map[string]any) (*schema.ToolResult, error) {
			return &schema.ToolResult{}, nil
		})},
		SkillRuntimeConfig: &subagents.RuntimeSkillConfig{
			Agents: map[string]subagents.RuntimeSkillAgentConfig{
				"data_engineer": {
					DisplayName:     "数据工程专家",
					Description:     "处理数据",
					Enabled:         true,
					WorkflowPackage: "governed-workflow",
				},
			},
		},
	}, nil, []string{"/tmp/legacy-skills"})
	if err == nil {
		t.Fatal("expected workflow package resolver requirement error")
	}
	if got := err.Error(); !strings.Contains(got, "registry governance is required for workflow package governed-workflow") {
		t.Fatalf("unexpected error: %v", err)
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

	resolver := &stubWorkflowPackageResolver{
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

	resolver := &stubWorkflowPackageResolver{
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
