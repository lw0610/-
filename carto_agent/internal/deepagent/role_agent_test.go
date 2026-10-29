package deepagent

import (
	"context"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

type knowledgeTaskContractProbe struct {
	fakeToolCallingChatModel
	tools       []*schema.ToolInfo
	description string
}

func (m *knowledgeTaskContractProbe) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	m.tools = tools
	return m, nil
}

func (m *knowledgeTaskContractProbe) Generate(_ context.Context, _ []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	for _, info := range model.GetCommonOptions(&model.Options{Tools: m.tools}, opts...).Tools {
		if info.Name == "task" {
			m.description = info.Desc
		}
	}
	return schema.AssistantMessage("已读取委派协议", nil), nil
}

func TestConfiguredKnowledgeContractReachesTaskTool(t *testing.T) {
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	ctx := context.Background()
	probe := &knowledgeTaskContractProbe{}
	cfg := roleFactoryFixture()
	cfg.Name, cfg.Instruction, cfg.ChatModel, cfg.MaxIteration = "orchestrator", "根据当前方案分配任务。", probe, 1
	agent, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	iter := agent.Run(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage("处理当前材料")}})
	for event, ok := iter.Next(); ok; event, ok = iter.Next() {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}
	for _, fragment := range []string{"renamed_executor", "当前方案的知识角色", `{"source_ref":"current_task_input"}`, "优先读取当前提供的附件", "source_refs", "current_user_message 只读取用户消息正文", "compile_failed", "review_id/revision", "只有用户明确要求重新抽取时"} {
		if !strings.Contains(probe.description, fragment) {
			t.Fatalf("task tool did not expose knowledge input protocol %q: %s", fragment, probe.description)
		}
	}
	if strings.Count(probe.description, `{"source_ref":"current_task_input"}`) != 1 {
		t.Fatal("factory duplicated executor protocol in the task description")
	}
}

func roleFactoryFixture() *Config {
	tools := []tool.BaseTool{}
	for _, name := range []string{knowledgeContractTool, knowledgeCompileTool, "unassigned"} {
		tools = append(tools, utils.NewEnhancedTool(&schema.ToolInfo{Name: name}, func(context.Context, map[string]any) (*schema.ToolResult, error) { return nil, nil }))
	}
	return &Config{ChatModel: &fakeToolCallingChatModel{}, Tools: tools, SkillRuntimeConfig: &subagents.RuntimeSkillConfig{
		Global: subagents.RuntimeSkillGlobalConfig{PackageTools: map[string][]string{"extract": {knowledgeContractTool, knowledgeCompileTool}}},
		Agents: map[string]subagents.RuntimeSkillAgentConfig{
			"renamed_executor":   {Kind: "executor", Executor: "route_knowledge", FlowOnly: true, Enabled: true, Description: "当前方案的知识角色", ToolPackages: []string{"extract"}, Prompts: subagents.AgentPrompts{StaticInstructions: "角色业务规则"}},
			"knowledge_engineer": {Kind: "agent", Enabled: true, DisplayName: "普通角色", Description: "普通处理"},
		},
	}}
}

func TestBuildRoleAgentDispatchesConfiguredRolesByKind(t *testing.T) {
	ctx := context.Background()
	cfg := roleFactoryFixture()
	agent, err := BuildRoleAgent(ctx, cfg, "renamed_executor")
	if err != nil {
		t.Fatal(err)
	}
	knowledge, ok := agent.(*knowledgeSubAgent)
	if !ok || knowledge.Name(ctx) != "renamed_executor" || knowledge.instructions != "角色业务规则" {
		t.Fatalf("wrong executor: %T", agent)
	}
	if len(knowledge.tools) != 2 || knowledge.tools["unassigned"] != nil {
		t.Fatal("unassigned tools escaped filtering")
	}
	if !strings.Contains(knowledge.Description(ctx), "当前方案的知识角色") || !strings.Contains(knowledge.Description(ctx), "source_ref") || !strings.Contains(knowledge.Description(ctx), "review_id/revision") {
		t.Fatal("configured executor lost its description or delegation contract")
	}
	ordinary, err := BuildRoleAgent(ctx, cfg, "knowledge_engineer")
	if err != nil {
		t.Fatal(err)
	}
	if _, wrong := ordinary.(*knowledgeSubAgent); wrong {
		t.Fatal("legacy role ID overrode declared kind")
	}
	delegates, err := createSubAgents(ctx, cfg, nil, nil)
	if err != nil || len(delegates) != 2 || delegates[0].Name(ctx) != "knowledge_engineer" || delegates[1].Name(ctx) != "renamed_executor" {
		t.Fatalf("configured role missing from delegates: %v %v", delegates, err)
	}
}

func TestBuildRoleAgentRetainsProviderAndAllowsPerRunRulesForEmptyRoleInstructions(t *testing.T) {
	cfg := roleFactoryFixture()
	role := cfg.SkillRuntimeConfig.Agents["renamed_executor"]
	role.Prompts.StaticInstructions = ""
	cfg.SkillRuntimeConfig.Agents["renamed_executor"] = role
	selected := &fakeToolCallingChatModel{}
	cfg.AgentLLMs = map[string]config.AgentLLMConfig{"renamed_executor": {Provider: "dedicated"}}
	cfg.ResolveAgentChatModel = func(provider string) (model.ToolCallingChatModel, error) {
		if provider != "dedicated" {
			t.Fatal(provider)
		}
		return selected, nil
	}
	cfg.ResolveProviderCfg = func(string) *config.LLMProviderConfig {
		return &config.LLMProviderConfig{Provider: "dedicated", Model: "test", MaxTokens: 2048, MaxContextTokens: 12000}
	}
	agent, err := BuildRoleAgent(context.Background(), cfg, "renamed_executor")
	if err != nil {
		t.Fatal(err)
	}
	knowledge := agent.(*knowledgeSubAgent)
	if knowledge.chatModel != selected || knowledge.config.MaxOutputTokens != 2048 || knowledge.config.MaxContextTokens != 12000 || knowledge.modelIdentity.Provider != "dedicated" {
		t.Fatal("independent provider budgets lost")
	}
}

func TestBuildRoleAgentRejectsMissingDisabledAndUnsupportedKinds(t *testing.T) {
	cfg := roleFactoryFixture()
	for _, role := range []subagents.RuntimeSkillAgentConfig{{Kind: "other", Enabled: true}, {Kind: "executor", Executor: "other", Enabled: true}, {Kind: "agent", Executor: "route_knowledge", Enabled: true}, {Kind: "agent", Enabled: false}} {
		cfg.SkillRuntimeConfig.Agents["bad"] = role
		if _, err := BuildRoleAgent(context.Background(), cfg, "bad"); err == nil {
			t.Fatal("invalid role accepted")
		}
	}
	if _, err := BuildRoleAgent(context.Background(), cfg, "missing"); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatal(err)
	}
	if _, err := BuildRoleAgent(context.Background(), nil, "role"); err == nil {
		t.Fatal("nil config accepted")
	}
}

func TestBuildRoleAgentDoesNotCreateImplicitAcquisitionRole(t *testing.T) {
	cfg := roleFactoryFixture()
	for _, name := range []string{"data_find", "data_get", "data_register"} {
		cfg.Tools = append(cfg.Tools, utils.NewTool(&schema.ToolInfo{Name: name, Desc: name}, func(context.Context, map[string]any) (string, error) { return `{}`, nil }))
	}
	cfg.SkillRuntimeConfig.Agents["disabled"] = subagents.RuntimeSkillAgentConfig{Kind: "agent", Enabled: false}
	agents, err := createSubAgents(context.Background(), cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 || agents[0].Name(context.Background()) != "knowledge_engineer" || agents[1].Name(context.Background()) != "renamed_executor" {
		t.Fatalf("delegates differ from enabled configured roles: %v", agents)
	}
	if _, err := BuildRoleAgent(context.Background(), cfg, "system_data_acquisition"); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("unconfigured acquisition role accepted: %v", err)
	}
}
