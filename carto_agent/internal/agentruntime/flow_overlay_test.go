package agentruntime

import (
	"context"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"os"
	"path/filepath"
	"testing"
)

func TestV4OverlayDoesNotChangeProfile(t *testing.T) {
	active := &config.AgentConfigFile{Runtime: &config.RuntimeConfig{SharedToolPackages: []string{"shared"}, Agents: map[string]config.AgentDefData{"main": {Kind: "agent", Enabled: true, IsMain: true}, "worker": {Kind: "agent", Enabled: true, ToolPackages: []string{"base"}, Prompts: map[string]string{"static_instructions": "原提示"}}}}}
	before, _ := json.Marshal(active)
	candidate, id, err := configWithRoleOverlay(active, RoleOverlay{FlowRole: &flow.FlowRoleSpec{ID: "flow_role_test", DisplayName: "临时角色", Kind: "agent", Description: "本步职责", ToolPackages: []string{"declared"}, Provider: "vision"}, ExtraPackages: []string{"extra"}})
	if err != nil {
		t.Fatal(err)
	}
	role := candidate.Runtime.Agents[id]
	if !role.FlowOnly || !role.Enabled || role.LLM.Provider != "vision" || len(role.ToolPackages) != 2 || candidate.Runtime.SharedToolPackages[0] != "shared" {
		t.Fatalf("%+v", role)
	}
	candidate.Runtime.SharedToolPackages[0] = "changed"
	candidate, id, err = configWithRoleOverlay(active, RoleOverlay{RoleID: "worker", ExtraPackages: []string{"extra"}})
	if err != nil || len(candidate.Runtime.Agents[id].ToolPackages) != 2 {
		t.Fatal(candidate, err)
	}
	candidate.Runtime.Agents[id].Prompts["static_instructions"] = "changed"
	after, _ := json.Marshal(active)
	if string(before) != string(after) {
		t.Fatal("original profile mutated")
	}
	next, _, err := configWithRoleOverlay(active, RoleOverlay{RoleID: "worker"})
	if err != nil || len(next.Runtime.Agents["worker"].ToolPackages) != 1 {
		t.Fatal("overlay leaked to following step", err)
	}
}

func TestV4FactoryBuildsSyntheticRoleWithSharedAndAutomaticTools(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"declared", "shared", "foundation"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		raw := "---\nname: " + name + "\ndescription: fixture\nkind: category\ntools: [" + name + "_tool]\n"
		if name == "foundation" {
			raw += "system: true\n"
		}
		raw += "---\n# fixture\nFixture instructions.\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
	probe := newFlowProbe()
	factory, err := NewFactory(&FactoryConfig{DefaultChatModel: probe, DefaultProviderCfg: &config.LLMProviderConfig{Model: "fixture"}, BaseTools: []tool.BaseTool{&fakeInvokableTool{name: "declared_tool"}, &fakeInvokableTool{name: "shared_tool"}, &fakeInvokableTool{name: "foundation_tool"}, &fakeInvokableTool{name: "unassigned"}}, PluginSkillsDir: root, BuiltinSkillsDir: t.TempDir(), UserSkillsDir: t.TempDir(), ConfigBaseDir: t.TempDir(), RegistryGovernance: control.NewRegistryGovernance(control.RegistryGovernanceDeps{ResolveSkillDirs: func() []string { return []string{root} }})})
	if err != nil {
		t.Fatal(err)
	}
	active := &config.AgentConfigFile{Runtime: &config.RuntimeConfig{SharedToolPackages: []string{"shared"}, Agents: map[string]config.AgentDefData{"main": {Kind: "agent", Enabled: true, IsMain: true}}}}
	agent, err := factory.BuildRoleWithOverlay(context.Background(), "fixture", active, RoleOverlay{FlowRole: &flow.FlowRoleSpec{ID: "flow_role_test", Kind: "agent", DisplayName: "临时", Description: "完成当前步骤", ToolPackages: []string{"declared"}}})
	if err != nil {
		t.Fatal(err)
	}
	if probe.capture.calls != 0 {
		t.Fatal("construction called model")
	}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: agent})
	iter := runner.Run(context.Background(), []*schema.Message{schema.UserMessage("执行测试")})
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}
	names := map[string]bool{}
	for _, binding := range probe.capture.bindings {
		for _, info := range binding {
			names[info.Name] = true
		}
	}
	for _, info := range probe.capture.options.Tools {
		names[info.Name] = true
	}
	for _, name := range []string{"declared_tool", "shared_tool", "foundation_tool"} {
		if !names[name] {
			t.Fatalf("missing %s in %+v", name, names)
		}
	}
	if names["unassigned"] || len(active.Runtime.Agents) != 1 || probe.capture.calls != 1 {
		t.Fatal("execution changed profile or unexpected model count", names)
	}
}

func TestV4FactoryBuildsDefaultKnowledgeExecutor(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "knowledge")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw := "---\nname: knowledge\ndescription: fixture\nkind: category\nhidden: true\nauto_mount: executor:route_knowledge\ntools: [marching_route_get_extraction_contract,marching_route_compile_dataset]\n---\n# fixture\nExtraction instructions.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	probe := newFlowProbe()
	factory, err := NewFactory(&FactoryConfig{DefaultChatModel: probe, DefaultProviderCfg: &config.LLMProviderConfig{Model: "fixture"}, BaseTools: []tool.BaseTool{&fakeInvokableTool{name: "marching_route_get_extraction_contract"}, &fakeInvokableTool{name: "marching_route_compile_dataset"}}, PluginSkillsDir: root, BuiltinSkillsDir: t.TempDir(), UserSkillsDir: t.TempDir(), ConfigBaseDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	active := &config.AgentConfigFile{Runtime: &config.RuntimeConfig{Agents: map[string]config.AgentDefData{"main": {Kind: "agent", Enabled: true, IsMain: true}}}}
	agent, err := factory.BuildRoleWithOverlay(context.Background(), "fixture", active, RoleOverlay{FlowRole: &flow.FlowRoleSpec{ID: "flow_role_executor", Kind: "executor", Executor: "route_knowledge", DisplayName: "抽取"}})
	if err != nil || agent == nil {
		t.Fatal(agent, err)
	}
	if agent.Name(context.Background()) != "flow_role_executor" || probe.capture.calls != 0 {
		t.Fatal("unexpected executor construction")
	}
}
