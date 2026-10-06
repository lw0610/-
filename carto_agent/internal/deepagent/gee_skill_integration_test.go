package deepagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"
)

var geeIntegrationToolNames = []string{
	"gee_check_environment", "gee_run_script", "gee_task_status", "gee_cancel_task",
}

// Exercise the shipped profile, package registry, filtering, middleware, and
// Eino task -> specialist -> skill chain together. A category package exposes
// tools but is deliberately excluded from the readable skill catalog; testing
// the Python helpers alone cannot catch a missing workflow assignment.
func TestGEESkillReachableThroughDefaultTaskDelegation(t *testing.T) {
	ctx := context.Background()
	fixture := loadGEESkillIntegrationFixture(t)
	for _, agentName := range []string{"data_engineer", "spatial_analyst", "scripting_specialist"} {
		t.Run(agentName, func(t *testing.T) {
			// Isolate the intended delegation while preserving both agents' actual
			// shipped package assignments and prompts, including global packages.
			runtimeConfig := *fixture.runtimeConfig
			runtimeConfig.Agents = map[string]subagents.RuntimeSkillAgentConfig{
				"orchestrator": fixture.runtimeConfig.Agents["orchestrator"],
				agentName:      fixture.runtimeConfig.Agents[agentName],
			}
			probe := &geeSkillProbeState{agentName: agentName, skillDir: fixture.skillDir, skillBody: fixture.skillBody}
			agent, err := New(ctx, &Config{
				Name: "orchestrator", Description: "GEE integration regression",
				ChatModel: &geeSkillProbeModel{state: probe}, MaxIteration: 6,
				Tools: fixture.tools, SkillRuntimeConfig: &runtimeConfig,
				BuiltinSkillsDir: fixture.skillsRoot, RuntimeConfigBaseDir: t.TempDir(),
				RegistryGovernance: fixture.registry,
			})
			if err != nil {
				t.Fatalf("create default agent: %v", err)
			}
			iter := agent.Run(ctx, &adk.AgentInput{Messages: []adk.Message{schema.UserMessage("下载武汉市遥感影像，并加载到 QGIS 中。")}})
			for {
				event, ok := iter.Next()
				if !ok {
					break
				}
				if event.Err != nil {
					t.Fatalf("task/skill chain failed: %v", event.Err)
				}
			}
			if !probe.delegated || !probe.skillRead || !probe.resultReturned {
				t.Fatalf("incomplete task -> skill -> task result chain: delegated=%v skillRead=%v returned=%v", probe.delegated, probe.skillRead, probe.resultReturned)
			}
		})
	}
}

func TestGEESkillDefaultOrchestratorCannotReadSpecialistWorkflow(t *testing.T) {
	ctx := context.Background()
	fixture := loadGEESkillIntegrationFixture(t)
	dirs, err := resolveMiddlewareSkillDirs("orchestrator", fixture.runtimeConfig, fixture.registry)
	if err != nil {
		t.Fatal(err)
	}
	mw, err := subagents.LoadSkillMiddleware(ctx, "orchestrator", dirs, []string{fixture.skillsRoot}, fixture.runtimeConfig)
	if err != nil || mw == nil {
		t.Fatalf("load real orchestrator middleware: middleware=%v error=%v", mw, err)
	}
	for _, candidate := range mw.AdditionalTools {
		info, err := candidate.Info(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if info.Name != "skill" {
			continue
		}
		if strings.Contains(info.Desc, "gee-scripting") {
			t.Fatal("specialist GEE workflow leaked into orchestrator skill catalog")
		}
		invokable, ok := candidate.(tool.InvokableTool)
		if !ok {
			t.Fatal("actual Eino skill tool is not invokable")
		}
		// A model guessing the skill name must not bypass the same allowlist
		// that filters the catalog. The real package exists on the filesystem.
		result, err := invokable.InvokableRun(ctx, `{"skill":"gee-scripting"}`)
		if err == nil || !strings.Contains(err.Error(), "允许列表") || result != "" {
			t.Fatalf("expected allowlist rejection, got result=%q err=%v", result, err)
		}
		return
	}
	t.Fatal("orchestrator skill middleware did not expose the actual skill tool")
}

func TestGEEToolsRemainAvailableWhenExplicitlyAssignedToOrchestrator(t *testing.T) {
	ctx := context.Background()
	fixture := loadGEESkillIntegrationFixture(t)
	runtimeConfig := *fixture.runtimeConfig
	main := runtimeConfig.Agents["orchestrator"]
	main.SkillPackages = append(append([]string{}, main.SkillPackages...), "gee-remote-sensing")
	runtimeConfig.Agents = map[string]subagents.RuntimeSkillAgentConfig{
		"orchestrator":  main,
		"data_engineer": fixture.runtimeConfig.Agents["data_engineer"],
	}
	probe := &geeSkillProbeState{agentName: "data_engineer", skillDir: fixture.skillDir, skillBody: fixture.skillBody, mainGEEAllowed: true}
	agent, err := New(ctx, &Config{Name: "orchestrator", ChatModel: &geeSkillProbeModel{state: probe}, Tools: fixture.tools, MaxIteration: 6,
		SkillRuntimeConfig: &runtimeConfig, BuiltinSkillsDir: fixture.skillsRoot, RuntimeConfigBaseDir: t.TempDir(), RegistryGovernance: fixture.registry})
	if err != nil {
		t.Fatal(err)
	}
	iter := agent.Run(ctx, &adk.AgentInput{Messages: []adk.Message{schema.UserMessage("使用 GEE 下载遥感数据")}})
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}
	if !probe.delegated || !probe.skillRead || !probe.resultReturned {
		t.Fatal("explicit main assignment broke specialist task/skill delegation")
	}
}

type geeSkillIntegrationFixture struct {
	runtimeConfig *subagents.RuntimeSkillConfig
	registry      *geeIntegrationRegistry
	tools         []tool.BaseTool
	skillsRoot    string
	skillDir      string
	skillBody     string
}

func loadGEESkillIntegrationFixture(t *testing.T) geeSkillIntegrationFixture {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	skillsRoot := filepath.Join(root, "autocarto", "skills", "system")
	raw, err := os.ReadFile(filepath.Join(root, "carto_agent", "defaults", "agent_configs", "default", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var profile config.AgentConfigFile
	if err := yaml.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}
	runtimeConfig := config.ConvertToRuntimeSkillConfig(&profile, skillsRoot, "", t.TempDir(), "default")
	if runtimeConfig == nil {
		t.Fatal("default profile did not produce a runtime skill config")
	}
	toolRegistry := toolpkg.NewRegistry()
	if err := toolRegistry.LoadDirectories(filepath.Join(root, "autocarto", "tools", "system")); err != nil {
		t.Fatal(err)
	}
	skillRegistry := skill.NewRegistry(toolRegistry)
	if err := skillRegistry.LoadDirectories(skillsRoot); err != nil {
		t.Fatal(err)
	}
	fixture := geeSkillIntegrationFixture{
		runtimeConfig: runtimeConfig, registry: &geeIntegrationRegistry{tools: toolRegistry, skills: skillRegistry},
		skillsRoot: skillsRoot, skillDir: filepath.Join(skillsRoot, "gee-scripting"),
	}
	for _, name := range append(append([]string{}, geeIntegrationToolNames...), "get_project_info") {
		definition, ok := skillRegistry.GetTool(name)
		if !ok {
			t.Fatalf("real GEE tool definition missing: %s", name)
		}
		// Use the real tool schemas. No Earth Engine backend is attached: this
		// test must remain offline, and invoking a GEE execution tool must fail.
		fixture.tools = append(fixture.tools, skill.NewSkillAdapter(definition, nil))
	}
	raw, err = os.ReadFile(filepath.Join(fixture.skillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(raw), "---", 3)
	if len(parts) != 3 || strings.TrimSpace(parts[2]) == "" {
		t.Fatal("real GEE workflow has no skill body")
	}
	fixture.skillBody = strings.TrimSpace(parts[2])
	return fixture
}

// Registry governance backed by the actual loaded assets, without constructing
// settings, account storage, QGIS connections, or online integrations.
type geeIntegrationRegistry struct {
	tools  *toolpkg.Registry
	skills *skill.Registry
}

func (r *geeIntegrationRegistry) LoadToolRegistry() (*toolpkg.Registry, error) {
	return r.tools, nil
}
func (r *geeIntegrationRegistry) LoadSkillRegistry() (*skill.Registry, error) {
	return r.skills, nil
}
func (r *geeIntegrationRegistry) ResolveSkillPackage(name string) (skill.SkillPackage, bool, error) {
	pkg, ok := r.skills.GetPackage(name)
	return pkg, ok, nil
}
func (r *geeIntegrationRegistry) ResolveWorkflowPackage(name string) (skill.SkillPackage, bool, error) {
	return r.ResolveSkillPackage(name)
}

type geeSkillProbeState struct {
	mu                                   sync.Mutex
	agentName, skillDir, skillBody       string
	delegated, skillRead, resultReturned bool
	mainGEEAllowed                       bool
}

type geeSkillProbeModel struct {
	state *geeSkillProbeState
	tools []*schema.ToolInfo
}

func (m *geeSkillProbeModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return &geeSkillProbeModel{state: m.state, tools: tools}, nil
}

func (m *geeSkillProbeModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (m *geeSkillProbeModel) Generate(_ context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	infos := model.GetCommonOptions(&model.Options{Tools: m.tools}, opts...).Tools
	available := make(map[string]*schema.ToolInfo, len(infos))
	for _, info := range infos {
		available[info.Name] = info
	}
	if _, main := available["task"]; main {
		for _, name := range geeIntegrationToolNames {
			if (available[name] != nil) != m.state.mainGEEAllowed {
				return nil, fmt.Errorf("orchestrator tool %s does not match its package assignment (allowed=%v)", name, m.state.mainGEEAllowed)
			}
		}
		if available["get_project_info"] == nil {
			return nil, fmt.Errorf("orchestrator lost its globally assigned project-state tool")
		}
		if info := available["skill"]; info != nil && strings.Contains(info.Desc, "gee-scripting") {
			return nil, fmt.Errorf("orchestrator unexpectedly has specialist GEE workflow")
		}
		if !m.state.delegated {
			m.state.delegated = true
			args, _ := json.Marshal(map[string]string{"subagent_type": m.state.agentName, "description": "读取 gee-scripting，依据流程准备武汉遥感影像任务。"})
			return geeProbeToolCall("delegate-gee", "task", string(args)), nil
		}
		for _, message := range input {
			if message.Role == schema.Tool && strings.Contains(message.Content, "gee-workflow-read-verified") {
				m.state.resultReturned = true
				return schema.AssistantMessage("delegation complete", nil), nil
			}
		}
		return nil, fmt.Errorf("delegated specialist did not return its verified skill result")
	}
	for _, name := range geeIntegrationToolNames {
		if available[name] == nil {
			return nil, fmt.Errorf("%s lost GEE tool %s", m.state.agentName, name)
		}
	}
	info := available["skill"]
	if info == nil || !strings.Contains(info.Desc, "gee-scripting") {
		return nil, fmt.Errorf("%s cannot discover gee-scripting in actual skill tool catalog", m.state.agentName)
	}
	for _, message := range input {
		if message.Role != schema.Tool || message.ToolCallID != "read-gee-skill" {
			continue
		}
		if !strings.Contains(message.Content, m.state.skillBody) {
			return nil, fmt.Errorf("skill tool did not return the full shipped GEE workflow body")
		}
		if !strings.Contains(message.Content, m.state.skillDir) || !filepath.IsAbs(m.state.skillDir) {
			return nil, fmt.Errorf("skill tool did not return its real absolute base directory")
		}
		for _, relative := range []string{"references/image-composite.md", "references/classification.md", "references/change-detection.md", "templates/single-date-image.md", "templates/single_date_image.py"} {
			content, err := os.ReadFile(filepath.Join(m.state.skillDir, filepath.FromSlash(relative)))
			if err != nil || len(content) == 0 {
				return nil, fmt.Errorf("skill reference %s inaccessible from returned directory: %v", relative, err)
			}
		}
		m.state.skillRead = true
		return schema.AssistantMessage("gee-workflow-read-verified", nil), nil
	}
	return geeProbeToolCall("read-gee-skill", "skill", `{"skill":"gee-scripting"}`), nil
}

func geeProbeToolCall(id, name, arguments string) *schema.Message {
	return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
		ID: id, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: arguments},
	}}}
}
