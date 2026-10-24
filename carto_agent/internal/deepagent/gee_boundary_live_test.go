package deepagent

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/hitl"
	"github.com/autocarto/carto_agent/internal/llm"
	"github.com/autocarto/carto_agent/internal/prompts"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"
)

// This test is deliberately opt-in: it spends model tokens, but never connects
// to QGIS, calls Earth Engine, or executes model-authored Python. All normal
// test runs skip it before reading account configuration or credentials.
func TestGEEBoundaryLiveModelRouting(t *testing.T) {
	if os.Getenv("AUTOCARTO_GEE_BOUNDARY_LIVE") != "1" {
		t.Skip("set AUTOCARTO_GEE_BOUNDARY_LIVE=1 and AUTOCARTO_LIVE_CONFIG_DIR for a controlled real-model regression")
	}
	configDir := strings.TrimSpace(os.Getenv("AUTOCARTO_LIVE_CONFIG_DIR"))
	if !filepath.IsAbs(configDir) {
		t.Fatal("AUTOCARTO_LIVE_CONFIG_DIR must explicitly name an existing absolute config directory")
	}
	// Prevent the constructor's legacy load-or-create path from ever creating
	// or replacing a master key. Only the read-only GetAPIKey method is used.
	master, err := os.ReadFile(filepath.Join(configDir, ".master_key"))
	decoded, decodeErr := base64.StdEncoding.DecodeString(string(master))
	if err != nil || decodeErr != nil || len(decoded) != 32 {
		t.Fatal("existing valid master key required; refusing any config migration or creation")
	}
	paths := []string{filepath.Join(configDir, "config.yaml"), filepath.Join(configDir, ".master_key"), filepath.Join(configDir, "secrets.enc"), filepath.Join(configDir, "agent_configs", "default", "agent.yaml")}
	before := make(map[string][32]byte)
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal("required existing configuration file is unavailable")
		}
		before[path] = sha256.Sum256(data)
	}
	t.Cleanup(func() {
		for path, hash := range before {
			data, readErr := os.ReadFile(path)
			if readErr != nil || sha256.Sum256(data) != hash {
				t.Errorf("configuration changed during live test: %s", filepath.Base(path))
			}
		}
	})
	var appConfig config.Config
	raw, _ := os.ReadFile(paths[0])
	if err := yaml.Unmarshal(raw, &appConfig); err != nil {
		t.Fatal("could not parse existing model configuration")
	}
	var profile config.AgentConfigFile
	raw, _ = os.ReadFile(paths[3])
	if err := yaml.Unmarshal(raw, &profile); err != nil {
		t.Fatal("could not parse existing default agent profile")
	}
	if profile.Runtime == nil {
		t.Fatal("default runtime profile is missing")
	}
	fixture := loadGEESkillIntegrationFixture(t)
	userSkillsDir := filepath.Join(filepath.Dir(fixture.skillsRoot), "user")
	builtinToolsDir := filepath.Join(filepath.Dir(filepath.Dir(fixture.skillsRoot)), "tools", "system")
	userToolsDir := filepath.Join(filepath.Dir(builtinToolsDir), "user")
	if err := fixture.registry.tools.LoadDirectories(config.ResolveRuntimeToolDirsWithPlugin(builtinToolsDir, builtinToolsDir, userToolsDir, configDir, "default")...); err != nil {
		t.Fatal("could not load current runtime tool directories")
	}
	if err := fixture.registry.skills.LoadDirectories(config.ResolveRuntimeSkillDirsWithPlugin(fixture.skillsRoot, fixture.skillsRoot, userSkillsDir, configDir, "default")...); err != nil {
		t.Fatal("could not load current runtime skill directories")
	}
	outputDir := strings.TrimSpace(os.Getenv("AUTOCARTO_LIVE_REPORT_DIR"))
	if outputDir == "" {
		outputDir = t.TempDir()
	}
	if !filepath.IsAbs(outputDir) || outputDir == configDir {
		t.Fatal("live report must use an isolated absolute output directory")
	}
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		t.Fatal(err)
	}
	outputDir, err = filepath.EvalSymlinks(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	probe := &geeBoundaryLiveProbe{cancel: cancel, skillsRoot: fixture.skillsRoot, outputDir: outputDir, python: appConfig.Integrations.GEE.Python, started: time.Now(), callNames: make(map[string]string)}
	t.Cleanup(func() {
		probe.mu.Lock()
		defer probe.mu.Unlock()
		data, _ := json.MarshalIndent(probe.report, "", "  ")
		_ = os.WriteFile(filepath.Join(outputDir, "report.json"), data, 0600)
		t.Logf("live routing report: %s", filepath.Join(outputDir, "report.json"))
	})
	secrets, err := config.NewSecretsManager(configDir)
	if err != nil {
		t.Fatal("read-only secret manager could not be initialized")
	}
	providers := make(map[string]config.LLMProviderConfig)
	for _, provider := range appConfig.LLM.Providers {
		providers[provider.Name] = provider
	}
	models := make(map[string]model.ToolCallingChatModel)
	resolveModel := func(name string) (model.ToolCallingChatModel, error) {
		if existing := models[name]; existing != nil {
			return existing, nil
		}
		provider, ok := providers[name]
		if !ok {
			return nil, fmt.Errorf("configured provider is missing")
		}
		key, keyErr := secrets.GetAPIKey(name)
		if keyErr != nil || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("configured provider credential could not be read")
		}
		cm, modelErr := llm.NewChatModel(&llm.Config{Provider: provider.Provider, Model: provider.Model, APIKey: key, BaseURL: provider.BaseURL, MaxTokens: provider.MaxTokens, Temperature: provider.Temperature, ThinkingEnabled: provider.ThinkingEnabled, ThinkingBudget: provider.ThinkingBudget})
		if modelErr != nil {
			return nil, fmt.Errorf("configured model could not be initialized")
		}
		models[name] = &geeBoundaryLiveModel{underlying: cm, probe: probe}
		return models[name], nil
	}
	defaultModel, err := resolveModel(appConfig.LLM.Active)
	if err != nil {
		t.Fatal(err)
	}
	activeProvider := providers[appConfig.LLM.Active]
	probe.report.Model = activeProvider.Model
	agentLLMs := make(map[string]config.AgentLLMConfig)
	for name, agent := range profile.Runtime.Agents {
		agentLLMs[name] = agent.LLM
	}
	var tools []tool.BaseTool
	for _, definition := range fixture.registry.skills.Definitions() {
		info, infoErr := skill.NewSkillAdapter(definition, nil).Info(ctx)
		if infoErr != nil {
			t.Fatal(infoErr)
		}
		// Match prepareAgentTools: production exposes the typed native HITL
		// schema, not the generic YAML approximation. Execution stays blocked.
		if info.Name == "hitl_request" {
			native, nativeErr := hitl.NewRequestTool()
			if nativeErr != nil {
				t.Fatal(nativeErr)
			}
			info, infoErr = native.Info(ctx)
			if infoErr != nil {
				t.Fatal(infoErr)
			}
		}
		tools = append(tools, &geeBoundaryLiveTool{info: info, probe: probe})
	}
	tools = hitl.WrapHighRiskBaseTools(tools)
	runtimeConfig := config.ConvertToRuntimeSkillConfigWithPlugin(&profile, fixture.skillsRoot, fixture.skillsRoot, userSkillsDir, configDir, "default")
	agent, err := New(ctx, &Config{Name: "orchestrator", Description: "AutoCarto AI 智能GIS助手", ChatModel: defaultModel, Tools: tools, MaxIteration: 12, SkillRuntimeConfig: runtimeConfig, BuiltinSkillsDir: fixture.skillsRoot, UserSkillsDir: userSkillsDir, RuntimeConfigBaseDir: t.TempDir(), ActiveConfigName: "default", RegistryGovernance: fixture.registry, DefaultProviderCfg: &activeProvider, AgentLLMs: agentLLMs, ResolveAgentChatModel: resolveModel, ResolveProviderCfg: func(name string) *config.LLMProviderConfig {
		p, ok := providers[name]
		if !ok {
			return nil
		}
		return &p
	}})
	if err != nil {
		t.Fatalf("construct real default task chain: %v", err)
	}
	modeInstruction, err := prompts.NewRegistry().Render(prompts.TemplateModeBuild, nil)
	if err != nil {
		t.Fatal(err)
	}
	iter := agent.Run(ctx, &adk.AgentInput{Messages: []adk.Message{schema.SystemMessage(modeInstruction), schema.UserMessage("使用gee 下载武汉市遥感数据")}})
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil && ctx.Err() == nil {
			probe.stop("agent_error")
			break
		}
	}
	probe.mu.Lock()
	defer probe.mu.Unlock()
	probe.report.ElapsedSeconds = time.Since(probe.started).Seconds()
	if !probe.report.Delegated || !probe.report.GEESkillRead || !probe.report.BoundarySkillRead || probe.report.HITLRequested || !probe.report.BoundarySelected {
		t.Fatalf("live boundary routing failed: delegated=%v gee_skill=%v boundary_skill=%v boundary_selected=%v hitl=%v terminal=%s", probe.report.Delegated, probe.report.GEESkillRead, probe.report.BoundarySkillRead, probe.report.BoundarySelected, probe.report.HITLRequested, probe.report.TerminalReason)
	}
}

type geeBoundaryLiveReport struct {
	Model                      string              `json:"model"`
	Delegated                  bool                `json:"delegated_to_data_engineer"`
	GEESkillRead               bool                `json:"gee_skill_read"`
	BoundarySkillRead          bool                `json:"boundary_skill_read"`
	BoundarySelectionRequested bool                `json:"boundary_selection_requested"`
	BoundarySelected           bool                `json:"boundary_selected"`
	BoundaryResult             map[string]any      `json:"boundary_result,omitempty"`
	HITLRequested              bool                `json:"hitl_requested"`
	TerminalReason             string              `json:"terminal_reason"`
	ElapsedSeconds             float64             `json:"elapsed_seconds"`
	Calls                      []map[string]string `json:"tool_calls"`
	LastAnswer                 string              `json:"last_answer,omitempty"`
}

type geeBoundaryLiveProbe struct {
	mu                            sync.Mutex
	cancel                        context.CancelFunc
	skillsRoot, outputDir, python string
	callNames                     map[string]string
	started                       time.Time
	report                        geeBoundaryLiveReport
}

func (p *geeBoundaryLiveProbe) stop(reason string) {
	p.mu.Lock()
	if p.report.TerminalReason == "" {
		p.report.TerminalReason = reason
	}
	p.mu.Unlock()
	p.cancel()
}

type geeBoundaryLiveModel struct {
	underlying model.ToolCallingChatModel
	probe      *geeBoundaryLiveProbe
}

func (m *geeBoundaryLiveModel) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	bound, err := m.underlying.WithTools(infos)
	if err != nil {
		return nil, err
	}
	return &geeBoundaryLiveModel{underlying: bound, probe: m.probe}, nil
}
func (m *geeBoundaryLiveModel) Generate(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.Message, error) {
	m.probe.mu.Lock()
	for _, message := range input {
		if message.Role != schema.Tool || (message.ToolName != "skill" && m.probe.callNames[message.ToolCallID] != "skill") {
			continue
		}
		if strings.Contains(message.Content, filepath.Join(m.probe.skillsRoot, "gee-remote-sensing")) {
			m.probe.report.GEESkillRead = true
		}
		if strings.Contains(message.Content, filepath.Join(m.probe.skillsRoot, "admin-boundaries")) {
			m.probe.report.BoundarySkillRead = true
		}
	}
	m.probe.mu.Unlock()
	response, err := m.underlying.Generate(ctx, input, options...)
	if err != nil {
		return nil, fmt.Errorf("live model request failed (%T)", err)
	}
	m.probe.mu.Lock()
	defer m.probe.mu.Unlock()
	for _, call := range response.ToolCalls {
		m.probe.callNames[call.ID] = call.Function.Name
		m.probe.report.Calls = append(m.probe.report.Calls, map[string]string{"tool": call.Function.Name, "arguments": call.Function.Arguments})
		if call.Function.Name == "task" && strings.Contains(call.Function.Arguments, "data_engineer") {
			m.probe.report.Delegated = true
		}
		lower := strings.ToLower(call.Function.Name)
		if strings.Contains(lower, "hitl") || strings.Contains(lower, "human_in_the_loop") || strings.Contains(lower, "clarification") || strings.Contains(lower, "ask_user") {
			m.probe.report.HITLRequested = true
			m.probe.report.TerminalReason = "unexpected_hitl"
			m.probe.cancel()
		}
	}
	// Never persist or print ReasoningContent or full model inputs.
	if len(response.ToolCalls) == 0 {
		m.probe.report.LastAnswer = response.Content
	}
	return response, nil
}
func (m *geeBoundaryLiveModel) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	response, err := m.Generate(ctx, input, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{response}), nil
}

type geeBoundaryLiveTool struct {
	info  *schema.ToolInfo
	probe *geeBoundaryLiveProbe
}

func (t *geeBoundaryLiveTool) Info(context.Context) (*schema.ToolInfo, error) { return t.info, nil }
func (t *geeBoundaryLiveTool) InvokableRun(ctx context.Context, arguments string, _ ...tool.Option) (string, error) {
	switch t.info.Name {
	case "get_project_info", "get_layers_info", "pyqgis_get_context":
		return `{"success":true,"project":{"name":"empty","crs":"EPSG:4326","layer_count":0},"layers":[],"layer_count":0}`, nil
	case "gee_check_environment":
		return `{"success":true,"status":"ready","environment":{"ready":true,"region_ready":true,"download_ready":true,"project":"configured-project","earthengine_api_version":"1.7.47"}}`, nil
	case "gee_run_script", "pyqgis_execute_script", "pyqgis_submit_script":
		var args map[string]any
		_ = json.Unmarshal([]byte(arguments), &args)
		code, _ := args["code"].(string)
		if code == "" {
			code, _ = args["script"].(string)
		}
		if strings.Contains(code, "select_boundary.py") && (strings.Contains(code, "export_boundary") || strings.Contains(code, "find_candidates")) {
			t.probe.mu.Lock()
			t.probe.report.BoundarySelectionRequested = true
			t.probe.mu.Unlock()
			// Parse literal selector arguments, then invoke only the trusted local
			// template. No generated Python statement is evaluated or executed.
			cmd := exec.CommandContext(ctx, t.probe.python, "-c", geeBoundarySafeSelector, filepath.Join(t.probe.skillsRoot, "..", "..", "resources", "system", "data", "中国行政区边界", "templates", "select_boundary.py"), t.probe.outputDir)
			cmd.Stdin = strings.NewReader(code)
			output, err := cmd.Output()
			if err != nil {
				t.probe.stop("trusted_selector_failed")
				return "", fmt.Errorf("trusted boundary selector failed")
			}
			var result map[string]any
			if json.Unmarshal(output, &result) != nil {
				t.probe.stop("trusted_selector_invalid_result")
				return "", fmt.Errorf("trusted boundary selector returned invalid JSON")
			}
			if result["mode"] == "find_candidates" {
				return string(output), nil
			}
			boundary, _ := result["boundary_path"].(string)
			if !strings.HasPrefix(boundary, t.probe.outputDir+string(os.PathSeparator)) {
				t.probe.stop("trusted_selector_invalid_path")
				return "", fmt.Errorf("boundary result is not in isolated directory")
			}
			data, err := os.ReadFile(boundary)
			if err != nil || !strings.Contains(string(data), "420100") || (!strings.Contains(string(data), "Polygon") && !strings.Contains(string(data), "MultiPolygon")) {
				t.probe.stop("trusted_selector_wrong_boundary")
				return "", fmt.Errorf("expected Wuhan administrative polygon was not produced")
			}
			t.probe.mu.Lock()
			t.probe.report.BoundarySelected = true
			t.probe.report.BoundaryResult = result
			t.probe.mu.Unlock()
			t.probe.stop("trusted_local_wuhan_boundary_exported_before_any_imagery_execution")
			return string(output), nil
		}
		// Only retrieve requested trusted source documents. The Python supplied
		// by the model is not executed, even when it looks harmless.
		if strings.Contains(code, "read_text") {
			var parts []string
			var bases []string
			for _, value := range regexp.MustCompile(`["']([^"'\n]+)["']`).FindAllStringSubmatch(code, -1) {
				resolved, err := filepath.EvalSymlinks(value[1])
				if err != nil || !strings.HasPrefix(resolved, t.probe.skillsRoot+string(os.PathSeparator)) {
					continue
				}
				if info, err := os.Stat(resolved); err == nil && info.IsDir() {
					bases = append(bases, resolved)
				}
			}
			seen := make(map[string]bool)
			for _, path := range regexp.MustCompile(`["']([^"'\n]+\.(?:md|py))["']`).FindAllStringSubmatch(code, -1) {
				candidates := []string{path[1]}
				if !filepath.IsAbs(path[1]) {
					for _, base := range bases {
						candidates = append(candidates, filepath.Join(base, path[1]))
					}
				}
				for _, candidate := range candidates {
					resolved, err := filepath.EvalSymlinks(candidate)
					if err != nil || !strings.HasPrefix(resolved, t.probe.skillsRoot+string(os.PathSeparator)) || seen[resolved] {
						continue
					}
					data, err := os.ReadFile(resolved)
					if err == nil && len(data) < 1024*1024 {
						seen[resolved] = true
						parts = append(parts, resolved+"\n"+string(data))
					}
				}
			}
			if len(parts) > 0 {
				data, _ := json.Marshal(map[string]any{"success": true, "stdout": strings.Join(parts, "\n\n")})
				return string(data), nil
			}
		}
	}
	t.probe.stop("blocked_tool_" + t.info.Name)
	return "", fmt.Errorf("controlled live test blocked tool execution")
}

// ast.literal_eval is applied only to literals (or previously declared literal
// variables), never to arbitrary expressions. The only executable code is the
// trusted installed boundary template, with output redirected to the harness.
const geeBoundarySafeSelector = `
import ast, json, runpy, sys
source = ast.parse(sys.stdin.read())
namespace = runpy.run_path(sys.argv[1])
bindings = {}
def call_name(node):
    fn = node.func
    if isinstance(fn, ast.Name): return fn.id
    if isinstance(fn, ast.Attribute): return fn.attr
    if isinstance(fn, ast.Subscript):
        try: return ast.literal_eval(fn.slice)
        except (ValueError, TypeError): pass
    return None
def literal(node):
    if isinstance(node, ast.Name):
        return bindings[node.id]
    if isinstance(node, ast.Subscript):
        return literal(node.value)[literal(node.slice)]
    if isinstance(node, ast.Call) and call_name(node) == 'find_candidates':
        arguments = {kw.arg:literal(kw.value) for kw in node.keywords}
        if set(arguments) - {'name','adcode','level','contains'}: raise ValueError('Unexpected selector arguments')
        return namespace['find_candidates'](**arguments)
    return ast.literal_eval(node)
for node in source.body:
    if isinstance(node, ast.Assign):
        try:
            value = literal(node.value)
        except (ValueError, TypeError, KeyError, SyntaxError):
            continue
        for target in node.targets:
            if isinstance(target, ast.Name):
                bindings[target.id] = value
calls = [node for node in ast.walk(source) if isinstance(node, ast.Call) and call_name(node) in ('export_boundary', 'find_candidates')]
if not calls: raise ValueError('No supported boundary selector call')
node = next((n for n in calls if call_name(n) == 'export_boundary'), calls[0])
kwargs = {}
allowed = {'adcode', 'name', 'level', 'expected_name', 'contains'}
for keyword in node.keywords:
    if keyword.arg in allowed:
        kwargs[keyword.arg] = literal(keyword.value)
if not kwargs.get('adcode') and not kwargs.get('name'): raise ValueError('An explicit local boundary selector is required')
if call_name(node) == 'find_candidates':
    print(json.dumps({'mode':'find_candidates','candidates':namespace['find_candidates'](**kwargs)},ensure_ascii=False))
else:
    kwargs.pop('contains',None)
    result = namespace['export_boundary'](sys.argv[2],filename='live-boundary.geojson',**kwargs)
    print(json.dumps(result,ensure_ascii=False))
`
