package deepagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/governance"
	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
	"github.com/autocarto/carto_agent/internal/workflow"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

type filesystemWorkflowPackageResolver struct {
	skillDirs []string
}

var _ governance.GovernedRegistry = filesystemWorkflowPackageResolver{}

func (r filesystemWorkflowPackageResolver) ResolveWorkflowPackage(packageName string) (skill.SkillPackage, bool, error) {
	registry := skill.NewRegistry()
	if err := registry.LoadDirectories(r.skillDirs...); err != nil {
		return skill.SkillPackage{}, false, err
	}
	pkg, ok := registry.GetPackage(packageName)
	return pkg, ok, nil
}

func (r filesystemWorkflowPackageResolver) ResolveSkillPackage(packageName string) (skill.SkillPackage, bool, error) {
	return r.ResolveWorkflowPackage(packageName)
}

func (r filesystemWorkflowPackageResolver) LoadToolRegistry() (*toolpkg.Registry, error) {
	return nil, nil
}

func (r filesystemWorkflowPackageResolver) LoadSkillRegistry() (*skill.Registry, error) {
	registry := skill.NewRegistry(nil)
	if err := registry.LoadDirectories(r.skillDirs...); err != nil {
		return nil, err
	}
	return registry, nil
}

type recordingEnhancedTool struct {
	name     string
	lastArgs map[string]any
}

func (t *recordingEnhancedTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: t.name}, nil
}

func (t *recordingEnhancedTool) InvokableRun(_ context.Context, toolArgument *schema.ToolArgument, _ ...tool.Option) (*schema.ToolResult, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(toolArgument.Text), &args); err != nil {
		return nil, err
	}
	t.lastArgs = args
	return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: `{"success":true}`}}}, nil
}

func TestBuildWorkflowSubAgentConfigUsesStructuredWorkflowSteps(t *testing.T) {
	pkg := skill.SkillPackage{
		Name: "data-loading-inspection-vector",
		Workflow: &workflow.Definition{
			Metadata: workflow.Metadata{Name: "data-loading-inspection-vector"},
			Spec: workflow.Spec{Steps: []workflow.Step{
				{ID: "load_vector", Type: workflow.StepTypeTool, Tool: "data_load_vector"},
				{ID: "validate_layer", Type: workflow.StepTypeTool, Tool: "get_layers_info"},
			}},
		},
	}
	baseTools := []tool.BaseTool{
		utils.NewEnhancedTool(&schema.ToolInfo{Name: "data_load_vector", Desc: "加载矢量"}, func(ctx context.Context, args map[string]any) (*schema.ToolResult, error) {
			return &schema.ToolResult{}, nil
		}),
		utils.NewEnhancedTool(&schema.ToolInfo{Name: "get_layers_info", Desc: "获取图层"}, func(ctx context.Context, args map[string]any) (*schema.ToolResult, error) {
			return &schema.ToolResult{}, nil
		}),
		utils.NewEnhancedTool(&schema.ToolInfo{Name: "check_validity", Desc: "检查有效性"}, func(ctx context.Context, args map[string]any) (*schema.ToolResult, error) {
			return &schema.ToolResult{}, nil
		}),
	}

	cfg, err := buildWorkflowSubAgentConfig("data_engineer", "数据工程专家", pkg, baseTools)
	if err != nil {
		t.Fatalf("buildWorkflowSubAgentConfig returned error: %v", err)
	}
	if cfg.Name != "data_engineer" || len(cfg.Tools) != 2 {
		t.Fatalf("unexpected workflow subagent config: %#v", cfg)
	}
	if got := cfg.Instruction; got == "" || !containsAll(got, []string{"load_vector", "data_load_vector", "validate_layer", "get_layers_info"}) {
		t.Fatalf("unexpected workflow instruction: %s", got)
	}
}

func TestBuildWorkflowSubAgentConfigRejectsMissingWorkflowDefinition(t *testing.T) {
	_, err := buildWorkflowSubAgentConfig("data_engineer", "数据工程专家", skill.SkillPackage{Name: "empty"}, nil)
	if err == nil {
		t.Fatal("expected missing workflow definition error")
	}
	if err.Error() != `workflow package "empty" missing structured workflow definition` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateSubAgentsUsesWorkflowPackageBranch(t *testing.T) {
	root := t.TempDir()
	workflowDir := filepath.Join(root, "data-loading-inspection-vector")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("mkdir workflow dir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "SKILL.md"), []byte("---\nname: data-loading-inspection-vector\ndescription: vector workflow\nkind: workflow\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "workflow.yaml"), []byte("apiVersion: workflow/v1\nkind: serial_workflow\nmetadata:\n  name: data-loading-inspection-vector\nspec:\n  steps:\n    - id: load_vector\n      type: tool\n      tool: data_load_vector\n      with: {}\n"), 0o644); err != nil {
		t.Fatalf("write workflow.yaml failed: %v", err)
	}
	agents, err := createSubAgents(context.Background(), &Config{
		ChatModel: &fakeToolCallingChatModel{},
		Tools: []tool.BaseTool{
			utils.NewEnhancedTool(&schema.ToolInfo{Name: "data_load_vector", Desc: "加载矢量"}, func(ctx context.Context, args map[string]any) (*schema.ToolResult, error) {
				return &schema.ToolResult{}, nil
			}),
			utils.NewEnhancedTool(&schema.ToolInfo{Name: "get_layers_info", Desc: "获取图层"}, func(ctx context.Context, args map[string]any) (*schema.ToolResult, error) {
				return &schema.ToolResult{}, nil
			}),
		},
		SkillsDir:          root,
		RegistryGovernance: filesystemWorkflowPackageResolver{skillDirs: []string{root}},
		SkillRuntimeConfig: &subagents.RuntimeSkillConfig{
			Agents: map[string]subagents.RuntimeSkillAgentConfig{
				"data_engineer": {
					DisplayName:     "数据工程专家",
					Description:     "处理数据",
					Enabled:         true,
					WorkflowPackage: "data-loading-inspection-vector",
				},
			},
		},
	}, nil, []string{root})
	if err != nil {
		t.Fatalf("createSubAgents returned error: %v", err)
	}
	if len(agents) != 1 || agents[0].Name(context.Background()) != "data_engineer" {
		t.Fatalf("unexpected workflow agents: %#v", agents)
	}
}

func TestCreateSubAgentsFailsWhenWorkflowPackageMissingStructuredDefinition(t *testing.T) {
	root := t.TempDir()
	brokenDir := filepath.Join(root, "broken-workflow")
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatalf("mkdir workflow dir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brokenDir, "SKILL.md"), []byte("---\nname: broken-workflow\ndescription: broken\nkind: workflow\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md failed: %v", err)
	}
	_, err := createSubAgents(context.Background(), &Config{
		ChatModel: &fakeToolCallingChatModel{},
		Tools: []tool.BaseTool{
			utils.NewEnhancedTool(&schema.ToolInfo{Name: "data_load_vector", Desc: "加载矢量"}, func(ctx context.Context, args map[string]any) (*schema.ToolResult, error) {
				return &schema.ToolResult{}, nil
			}),
		},
		SkillsDir:          root,
		RegistryGovernance: filesystemWorkflowPackageResolver{skillDirs: []string{root}},
		SkillRuntimeConfig: &subagents.RuntimeSkillConfig{
			Agents: map[string]subagents.RuntimeSkillAgentConfig{
				"data_engineer": {
					DisplayName:     "数据工程专家",
					Description:     "处理数据",
					Enabled:         true,
					WorkflowPackage: "broken-workflow",
				},
			},
		},
	}, nil, []string{root})
	if err == nil {
		t.Fatal("expected workflow branch to fail when structured definition is missing")
	}
}

func TestCreateSubAgentsLoadsWorkflowPackageFromExclusiveRuntimeDir(t *testing.T) {
	root := t.TempDir()
	globalSkillsDir := filepath.Join(root, "skills")
	exclusiveSkillsDir := filepath.Join(root, "agent_configs", "default", "skills")
	workflowDir := filepath.Join(exclusiveSkillsDir, "exclusive-workflow")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("mkdir workflow dir failed: %v", err)
	}
	if err := os.MkdirAll(globalSkillsDir, 0o755); err != nil {
		t.Fatalf("mkdir global dir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "SKILL.md"), []byte("---\nname: exclusive-workflow\ndescription: exclusive workflow\nkind: workflow\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "workflow.yaml"), []byte("apiVersion: workflow/v1\nkind: serial_workflow\nmetadata:\n  name: exclusive-workflow\nspec:\n  steps:\n    - id: load_vector\n      type: tool\n      tool: data_load_vector\n      with: {}\n"), 0o644); err != nil {
		t.Fatalf("write workflow.yaml failed: %v", err)
	}
	skillDirs := config.ResolveRuntimeSkillDirsWithPlugin("", "", globalSkillsDir, root, "default")
	agents, err := createSubAgents(context.Background(), &Config{
		ChatModel: &fakeToolCallingChatModel{},
		Tools: []tool.BaseTool{utils.NewEnhancedTool(&schema.ToolInfo{Name: "data_load_vector", Desc: "加载矢量"}, func(ctx context.Context, args map[string]any) (*schema.ToolResult, error) {
			return &schema.ToolResult{}, nil
		})},
		SkillsDir:            globalSkillsDir,
		RegistryGovernance:   filesystemWorkflowPackageResolver{skillDirs: skillDirs},
		ActiveConfigName:     "default",
		RuntimeConfigBaseDir: root,
		SkillRuntimeConfig: &subagents.RuntimeSkillConfig{
			Agents: map[string]subagents.RuntimeSkillAgentConfig{
				"data_engineer": {DisplayName: "数据工程专家", Description: "处理数据", Enabled: true, WorkflowPackage: "exclusive-workflow"},
			},
		},
	}, nil, skillDirs)
	if err != nil {
		t.Fatalf("createSubAgents returned error: %v", err)
	}
	if len(agents) != 1 || agents[0].Name(context.Background()) != "data_engineer" {
		t.Fatalf("unexpected agents: %#v", agents)
	}
}

func TestCreateSubAgentsLoadsWorkflowPackageFromPluginSkillsDir(t *testing.T) {
	root := t.TempDir()
	globalSkillsDir := filepath.Join(root, "skills")
	pluginSkillsDir := filepath.Join(t.TempDir(), "plugin-skills")
	workflowDir := filepath.Join(pluginSkillsDir, "plugin-workflow")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("mkdir workflow dir failed: %v", err)
	}
	if err := os.MkdirAll(globalSkillsDir, 0o755); err != nil {
		t.Fatalf("mkdir global dir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "SKILL.md"), []byte("---\nname: plugin-workflow\ndescription: plugin workflow\nkind: workflow\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "workflow.yaml"), []byte("apiVersion: workflow/v1\nkind: serial_workflow\nmetadata:\n  name: plugin-workflow\nspec:\n  steps:\n    - id: load_vector\n      type: tool\n      tool: data_load_vector\n      with: {}\n"), 0o644); err != nil {
		t.Fatalf("write workflow.yaml failed: %v", err)
	}
	skillDirs := config.ResolveRuntimeSkillDirsWithPlugin(pluginSkillsDir, "", globalSkillsDir, root, "default")
	agents, err := createSubAgents(context.Background(), &Config{
		ChatModel: &fakeToolCallingChatModel{},
		Tools: []tool.BaseTool{utils.NewEnhancedTool(&schema.ToolInfo{Name: "data_load_vector", Desc: "加载矢量"}, func(ctx context.Context, args map[string]any) (*schema.ToolResult, error) {
			return &schema.ToolResult{}, nil
		})},
		SkillsDir:            globalSkillsDir,
		PluginSkillsDir:      pluginSkillsDir,
		RegistryGovernance:   filesystemWorkflowPackageResolver{skillDirs: skillDirs},
		ActiveConfigName:     "default",
		RuntimeConfigBaseDir: root,
		SkillRuntimeConfig: &subagents.RuntimeSkillConfig{
			Agents: map[string]subagents.RuntimeSkillAgentConfig{
				"data_engineer": {DisplayName: "数据工程专家", Description: "处理数据", Enabled: true, WorkflowPackage: "plugin-workflow"},
			},
		},
	}, nil, skillDirs)
	if err != nil {
		t.Fatalf("createSubAgents returned error: %v", err)
	}
	if len(agents) != 1 || agents[0].Name(context.Background()) != "data_engineer" {
		t.Fatalf("unexpected agents: %#v", agents)
	}
}

func TestWorkflowSubAgentRunsDeterministicSerialToolPlan(t *testing.T) {
	loadTool := &recordingEnhancedTool{name: "data_load_vector"}
	reportTool := &recordingEnhancedTool{name: "get_layers_info"}
	agent, err := createWorkflowSubAgentFromPackage(context.Background(), &Config{
		Tools: []tool.BaseTool{loadTool, reportTool},
	}, "data_engineer", subagents.RuntimeSkillAgentConfig{DisplayName: "数据工程专家", Description: "处理数据"}, skill.SkillPackage{
		Name: "data-loading-inspection-vector",
		Workflow: &workflow.Definition{
			Metadata: workflow.Metadata{Name: "data-loading-inspection-vector"},
			Spec: workflow.Spec{
				Inputs: map[string]workflow.Input{
					"dataset_path": {Type: "string", Required: true},
				},
				Steps: []workflow.Step{
					{ID: "load_vector", Type: workflow.StepTypeTool, Tool: "data_load_vector", With: map[string]any{"file_path": "{{ inputs.dataset_path }}"}},
					{ID: "report", Type: workflow.StepTypeTool, Tool: "get_layers_info", With: map[string]any{}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("createWorkflowSubAgent returned error: %v", err)
	}
	iter := agent.Run(context.Background(), &adk.AgentInput{Messages: []adk.Message{schema.UserMessage(`{"dataset_path":"/tmp/demo.shp"}`)}})
	var final string
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatalf("unexpected workflow event error: %v", event.Err)
		}
		if event.Output != nil && event.Output.MessageOutput != nil && event.Output.MessageOutput.Message != nil {
			final = event.Output.MessageOutput.Message.Content
		}
	}
	if loadTool.lastArgs["file_path"] != "/tmp/demo.shp" {
		t.Fatalf("unexpected workflow input injection: %#v", loadTool.lastArgs)
	}
	if reportTool.lastArgs == nil {
		t.Fatal("expected second tool to run")
	}
	if !strings.Contains(final, "工作流执行完成") {
		t.Fatalf("unexpected final workflow output: %q", final)
	}
}

func TestWorkflowSubAgentFailsPreflightWhenRequiredInputMissing(t *testing.T) {
	agent, err := createWorkflowSubAgentFromPackage(context.Background(), &Config{}, "data_engineer", subagents.RuntimeSkillAgentConfig{DisplayName: "数据工程专家", Description: "处理数据"}, skill.SkillPackage{
		Name: "data-loading-inspection-vector",
		Workflow: &workflow.Definition{
			Metadata: workflow.Metadata{Name: "data-loading-inspection-vector"},
			Spec: workflow.Spec{
				Inputs: map[string]workflow.Input{
					"dataset_path": {Type: "string", Required: true},
				},
				Steps: []workflow.Step{{ID: "load_vector", Type: workflow.StepTypeTool, Tool: "data_load_vector", With: map[string]any{"file_path": "{{ inputs.dataset_path }}"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("createWorkflowSubAgent returned error: %v", err)
	}
	iter := agent.Run(context.Background(), &adk.AgentInput{Messages: []adk.Message{schema.UserMessage(`{}`)}})
	event, ok := iter.Next()
	if !ok || event == nil || event.Err == nil {
		t.Fatalf("expected preflight error event, got %#v ok=%v", event, ok)
	}
	if !strings.Contains(event.Err.Error(), `workflow input "dataset_path" is required`) {
		t.Fatalf("unexpected preflight error: %v", event.Err)
	}
}

func containsAll(text string, subs []string) bool {
	for _, sub := range subs {
		if !strings.Contains(text, sub) {
			return false
		}
	}
	return true
}
