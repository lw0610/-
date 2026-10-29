package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/deepagent"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// The scripted model requests the same skill tool as a real model. It does not
// call GuideBackend directly, so all factory and authorization wiring is tested.
type factoryGuideProbe struct {
	target string
	calls  int
	result string
}

func (p *factoryGuideProbe) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return p, nil
}

func (p *factoryGuideProbe) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	p.calls++
	if p.calls == 1 {
		args, _ := json.Marshal(map[string]string{"skill": p.target})
		return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
			ID: "read-factory-guide", Type: "function",
			Function: schema.FunctionCall{Name: "skill", Arguments: string(args)},
		}}}, nil
	}
	for _, message := range messages {
		if message.Role == schema.Tool && message.ToolCallID == "read-factory-guide" {
			p.result = message.Content
		}
	}
	return schema.AssistantMessage("已读取", nil), nil
}

func (p *factoryGuideProbe) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := p.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (p *factoryGuideProbe) read(agent adk.Agent, name string) (string, error) {
	p.target, p.calls, p.result = name, 0, ""
	ctx := context.Background()
	iterator := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent}).Run(ctx, []*schema.Message{schema.UserMessage("阅读适用流程或能力手册")})
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return p.result, event.Err
		}
	}
	if p.calls != 2 || p.result == "" {
		return p.result, fmt.Errorf("skill did not return content: model calls=%d", p.calls)
	}
	return p.result, nil
}

func factoryWithRealGuides(t *testing.T) (*Runtime, *factoryGuideProbe, *flow.GuideStore) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	assets := filepath.Join(root, "autocarto")
	home := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", assets)
	t.Setenv("AUTOCARTO_HOME", home)
	skills := filepath.Join(assets, "skills", "system")
	library := resource.NewLibrary(filepath.Join(assets, "resources", "system"), filepath.Join(home, "resources"), nil)
	guides := flow.NewGuideStore(flow.NewStore(filepath.Join(assets, "flows", "system"), filepath.Join(home, "flows"), nil), filepath.Join(home, "guides.yaml"))
	probe := &factoryGuideProbe{}
	factory, err := NewFactory(&FactoryConfig{
		DefaultChatModel: probe,
		GuideBackend:     &deepagent.GuideBackend{Store: guides, Resources: library},
		BaseTools:        []tool.BaseTool{&fakeInvokableTool{name: "data_find"}, &fakeInvokableTool{name: "get_layers_info"}},
		PluginSkillsDir:  skills, BuiltinSkillsDir: skills, UserSkillsDir: filepath.Join(home, "skills"),
		ConfigBaseDir: filepath.Join(home, "config"), MaxIterations: 3,
		ResolveChatModel: func(string) (model.ToolCallingChatModel, error) { return probe, nil },
		RegistryGovernance: control.NewRegistryGovernance(control.RegistryGovernanceDeps{
			ResolveSkillDirs: func() []string { return []string{skills} },
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := factory.Build(context.Background(), "default", loadEmbeddedDefaultAgentConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.BuildAgent == nil || runtime.PlanAgent == nil || len(runtime.ModeIssues) != 0 {
		t.Fatalf("default runtime must have both modes: %+v", runtime.ModeIssues)
	}
	return runtime, probe, guides
}

func TestFactoryDefaultRuntimeReadsAllBuiltinGuidesAndAssignedManual(t *testing.T) {
	runtime, probe, guides := factoryWithRealGuides(t)
	items, err := guides.List()
	if err != nil || len(items) == 0 {
		t.Fatalf("real built-in guides missing: %v", err)
	}
	for _, mode := range []string{"build", "plan"} {
		agent, _ := runtime.ResolveAgent(mode)
		for _, item := range items {
			if !item.Enabled {
				continue
			}
			t.Run(mode+"/"+item.ID, func(t *testing.T) {
				guide, err := guides.Get(item.ID)
				if err != nil {
					t.Fatal(err)
				}
				content, err := probe.read(agent, deepagent.GuideSkillName(item.ID))
				if err != nil || !strings.Contains(content, guide.GuideMD) {
					t.Fatalf("factory-built %s could not read full guide %s: %v", mode, item.ID, err)
				}
			})
		}
		t.Run(mode+"/assigned-manual", func(t *testing.T) {
			content, err := probe.read(agent, "data-service")
			if err != nil || !strings.Contains(content, "data_find") {
				t.Fatalf("ordinary assigned manual no longer readable: %v", err)
			}
		})
	}
}

func TestFactoryGuideReadsReflectUserChangesWithoutRuntimeRebuild(t *testing.T) {
	runtime, probe, guides := factoryWithRealGuides(t)
	const id = "new-user-guide"
	raw := "---\n名称: 用户新增流程\n说明: 在运行时创建后读取\n---\n# 用户自由正文\n必须保留这段用户要求。\n"
	created, err := guides.Create(raw, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"build", "plan"} {
		agent, _ := runtime.ResolveAgent(mode)
		if content, err := probe.read(agent, deepagent.GuideSkillName(id)); err != nil || !strings.Contains(content, created.GuideMD) {
			t.Fatalf("%s cannot read a user guide created after factory build: %v", mode, err)
		}
	}
	updated := strings.Replace(raw, "必须保留这段用户要求。", "更新后的用户要求必须即时可见。", 1)
	saved, err := guides.Save(id, updated)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"build", "plan"} {
		agent, _ := runtime.ResolveAgent(mode)
		if content, err := probe.read(agent, deepagent.GuideSkillName(id)); err != nil || !strings.Contains(content, saved.GuideMD) {
			t.Fatalf("%s did not read the saved guide body: %v", mode, err)
		}
	}
	if _, err := guides.SetEnabled(id, false); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"build", "plan"} {
		agent, _ := runtime.ResolveAgent(mode)
		if content, err := probe.read(agent, deepagent.GuideSkillName(id)); err == nil || strings.Contains(content, saved.GuideMD) {
			t.Fatalf("%s exposed a disabled user guide: %v", mode, err)
		}
	}
}

func TestFactoryGuideBackendDoesNotGrantUnassignedManuals(t *testing.T) {
	runtime, probe, _ := factoryWithRealGuides(t)
	// This manual exists and is assigned to cartographer in the real default
	// profile. Adding guide knowledge must not grant it to the orchestrator.
	if _, err := os.Stat(filepath.Join("..", "..", "..", "autocarto", "skills", "system", "cartography-styling", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"build", "plan"} {
		agent, _ := runtime.ResolveAgent(mode)
		for _, name := range []string{"cartography-styling", "guide-not-a-real-flow"} {
			if content, err := probe.read(agent, name); err == nil || content != "" {
				t.Fatalf("%s exposed unauthorized or nonexistent skill %s: %v", mode, name, err)
			}
		}
	}
}
