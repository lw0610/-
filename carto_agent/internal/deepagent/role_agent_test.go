package deepagent

import (
	"context"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

func roleFactoryFixture() *Config {
	tools := []tool.BaseTool{}
	for _, name := range []string{knowledgeContractTool, knowledgeCompileTool, "unassigned"} {
		tools = append(tools, utils.NewEnhancedTool(&schema.ToolInfo{Name: name}, func(context.Context, map[string]any) (*schema.ToolResult, error) { return nil, nil }))
	}
	return &Config{ChatModel: &fakeToolCallingChatModel{}, Tools: tools, SkillRuntimeConfig: &subagents.RuntimeSkillConfig{
		Global: subagents.RuntimeSkillGlobalConfig{PackageTools: map[string][]string{"extract": {knowledgeContractTool, knowledgeCompileTool}}},
		Agents: map[string]subagents.RuntimeSkillAgentConfig{
			"renamed_executor":   {Kind: "executor", Executor: "route_knowledge", FlowOnly: true, Enabled: true, ToolPackages: []string{"extract"}, Prompts: subagents.AgentPrompts{StaticInstructions: "角色业务规则"}},
			"knowledge_engineer": {Kind: "agent", Enabled: true, DisplayName: "普通角色", Description: "普通处理"},
		},
	}}
}

func TestBuildRoleAgentDispatchesByKindAndExplicitFlowOnlyRole(t *testing.T) {
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
	ordinary, err := BuildRoleAgent(ctx, cfg, "knowledge_engineer")
	if err != nil {
		t.Fatal(err)
	}
	if _, wrong := ordinary.(*knowledgeSubAgent); wrong {
		t.Fatal("legacy role ID overrode declared kind")
	}
	delegates, err := createSubAgents(ctx, cfg, nil, nil)
	if err != nil || len(delegates) != 1 || delegates[0].Name(ctx) != "knowledge_engineer" {
		t.Fatalf("flow-only role was delegated: %v %v", delegates, err)
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

func TestBuildSystemDataRoleRequiresExecutableDataService(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing_data_service", true: "read_only"}[readOnly], func(t *testing.T) {
			cfg := roleFactoryFixture()
			cfg.SkipSubAgents = readOnly
			if readOnly {
				for _, name := range []string{"data_find", "data_get"} {
					cfg.Tools = append(cfg.Tools, utils.NewEnhancedTool(&schema.ToolInfo{Name: name}, func(context.Context, map[string]any) (*schema.ToolResult, error) { return nil, nil }))
				}
			}
			if _, err := BuildRoleAgent(context.Background(), cfg, DataAcquisitionExpertName); err == nil || !strings.Contains(err.Error(), "不可用") {
				t.Fatalf("unavailable system expert accepted: %v", err)
			}
		})
	}
}
