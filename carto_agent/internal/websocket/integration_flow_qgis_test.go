package websocket

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/agentruntime"
	"github.com/autocarto/carto_agent/internal/compute"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	bridgeprovider "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"github.com/autocarto/carto_agent/internal/runtimecontrol"
	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
	"github.com/autocarto/carto_agent/internal/toolcontract"
	"github.com/autocarto/carto_agent/internal/toolsession"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/gorilla/websocket"
)

// This opt-in test exercises the production compiler, role factory, ADK phase
// runner, controller, task WebSocket and reverse-WS QGIS tool path. Models are
// deterministic offline fixtures; this is not evidence of real LLM planning.
func TestProductionFlowWebSocketRealQGISLoadAndExport(t *testing.T) {
	python := strings.TrimSpace(os.Getenv("AUTOCARTO_QGIS_PYTHON"))
	if python == "" {
		t.Skip("set AUTOCARTO_QGIS_PYTHON to a working QGIS Python launcher for this opt-in integration")
	}
	if _, err := os.Stat(python); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"direct", "orchestrator"} {
		t.Run(origin, func(t *testing.T) { runProductionQGISFlow(t, python, root, origin) })
	}
}

const qgisE2EFlowID = "qgis-load-export"
const qgisE2EFlowMD = `---
名称: 隔离矢量加载导出
编号: qgis-load-export
说明: 使用真实QGIS加载并导出隔离数据
版本: 1.0.0
---

## 适用场景
验证生产流程与真实QGIS工具的连接。

## 需要准备
- 输入数据（必填）：GeoJSON 文件
- 导出位置（必填）：新 GeoJSON 文件路径

## 步骤
### 第1步 加载矢量
- 负责：加载核验角色
- 做法：调用 data_load_vector 加载「输入数据」，返回实际图层标识。
- 完成标准：「已加载图层」（图层）

### 第2步 导出矢量
- 负责：导出核验角色
- 做法：调用 data_export_vector，将「已加载图层」导出到「导出位置」，不覆盖输入数据。
- 完成标准：「导出文件」（文件，geojson）

## 必须遵守
- 工具失败时停止并保留错误，不伪造文件和图层。

## 交付
- 导出文件路径。
`

type qgisE2ECompileModel struct{}

func (qgisE2ECompileModel) Complete(_ context.Context, r flow.ModelRequest) (string, error) {
	if r.Kind == "flow" {
		return `{"inputs":[{"key":"input_data","label":"输入数据","type":"file","options":[]},{"key":"output_path","label":"导出位置","type":"text","options":[]}],"input_constraints":[],"triggers":["核验"]}`, nil
	}
	name, label, kind, format := "data_load_vector", "已加载图层", "layers", ""
	if r.Step == "s2" {
		name, label, kind, format = "data_export_vector", "导出文件", "file", "geojson"
	}
	raw, err := json.Marshal(map[string]any{"suggested_tools": []string{name}, "missing_capabilities": []string{}, "expected_outputs": []flow.OutputDeclaration{{Label: label, Type: kind, Format: format, Required: true}}, "condition": nil, "notes": "确定性离线验收夹具"})
	return string(raw), err
}

type qgisE2EModel struct {
	tool   string
	args   map[string]any
	output string
	tools  []*schema.ToolInfo
}

func (m *qgisE2EModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	copy := *m
	copy.tools = tools
	return &copy, nil
}
func (m *qgisE2EModel) Generate(_ context.Context, messages []*schema.Message, options ...model.Option) (*schema.Message, error) {
	available := false
	for _, info := range model.GetCommonOptions(&model.Options{Tools: m.tools}, options...).Tools {
		available = available || info.Name == m.tool
	}
	if !available {
		return nil, fmt.Errorf("production role lost authorized tool %s", m.tool)
	}
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Role != schema.Tool || (message.ToolName != m.tool && message.ToolCallID != "qgis-e2e-call") {
			continue
		}
		text := message.Content
		if text == "" {
			for _, part := range message.UserInputMultiContent {
				if part.Type == schema.ChatMessagePartTypeText {
					text += part.Text
				}
			}
		}
		var result any
		if err := json.Unmarshal([]byte(text), &result); err != nil {
			return nil, fmt.Errorf("tool returned non-JSON: %w", err)
		}
		if qgisE2EFailed(result) {
			return nil, fmt.Errorf("real tool %s returned failure: %s", m.tool, text)
		}
		if m.tool == "flow_start" {
			return schema.AssistantMessage("流程已登记，等待生产控制器执行。", nil), nil
		}
		value := m.output
		if m.tool == "data_load_vector" {
			value = qgisE2EString(result, "layer_id")
			if value == "" {
				return nil, fmt.Errorf("real load did not return layer_id")
			}
		}
		if m.tool == "data_export_vector" {
			value = qgisE2EString(result, "output")
			if !filepath.IsAbs(value) || filepath.Base(value) != filepath.Base(m.output) {
				return nil, fmt.Errorf("real export result omitted its actual absolute path: %q", value)
			}
		}
		var output any = value
		label := "导出文件"
		if m.tool == "data_load_vector" {
			label = "已加载图层"
			output = []string{value}
		}
		raw, _ := json.Marshal(flowrun.Receipt{Status: "completed", Summary: "真实QGIS工具调用完成", Outputs: map[string]any{label: output}, Issues: []string{}})
		return schema.AssistantMessage(string(raw), nil), nil
	}
	args, _ := json.Marshal(m.args)
	return schema.AssistantMessage("", []schema.ToolCall{{ID: "qgis-e2e-call", Type: "function", Function: schema.FunctionCall{Name: m.tool, Arguments: string(args)}}}), nil
}
func (m *qgisE2EModel) Stream(ctx context.Context, messages []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	result, err := m.Generate(ctx, messages, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{result}), nil
}
func qgisE2EFailed(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		if success, ok := v["success"].(bool); ok && !success {
			return true
		}
		for _, item := range v {
			if qgisE2EFailed(item) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if qgisE2EFailed(item) {
				return true
			}
		}
	}
	return false
}
func qgisE2EString(value any, key string) string {
	switch v := value.(type) {
	case map[string]any:
		if found, ok := v[key].(string); ok && found != "" {
			return found
		}
		for _, item := range v {
			if found := qgisE2EString(item, key); found != "" {
				return found
			}
		}
	case []any:
		for _, item := range v {
			if found := qgisE2EString(item, key); found != "" {
				return found
			}
		}
	}
	return ""
}
func qgisE2EWrite(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

type qgisE2ETrace struct {
	mu    sync.Mutex
	calls []map[string]any
}

func runProductionQGISFlow(t *testing.T, python, root, origin string) {
	t.Helper()
	home := t.TempDir()
	if evidence := os.Getenv("AUTOCARTO_FLOW_QGIS_EVIDENCE_DIR"); evidence != "" {
		if err := os.MkdirAll(evidence, 0700); err != nil {
			t.Fatal(err)
		}
		var err error
		home, err = os.MkdirTemp(evidence, origin+"-")
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AUTOCARTO_HOME", home)
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", filepath.Join(root, "autocarto"))
	t.Setenv("AUTOCARTO_DISABLE_KEYRING", "1")
	assets := filepath.Join(root, "autocarto")
	input, output, project := filepath.Join(home, "input.geojson"), "export.geojson", filepath.Join(home, "work", "project.qgz")
	source := []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"核验甲","value":7},"geometry":{"type":"Point","coordinates":[110,30]}},{"type":"Feature","properties":{"name":"核验乙","value":11},"geometry":{"type":"Point","coordinates":[111,31]}}]}`)
	if err := os.WriteFile(input, source, 0600); err != nil {
		t.Fatal(err)
	}
	tools := toolpkg.NewRegistry()
	if err := tools.LoadDirectories(filepath.Join(assets, "tools", "system")); err != nil {
		t.Fatal(err)
	}
	registry := skill.NewRegistry(tools)
	if err := registry.LoadDirectories(filepath.Join(assets, "skills", "system")); err != nil {
		t.Fatal(err)
	}
	authority, err := toolpkg.LoadAuthorityDefinitions(filepath.Join(assets, "tools", "system"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := toolpkg.BuildExecutionManifestEntries(authority)
	instances := discovery.NewInstanceRegistry()
	t.Cleanup(instances.Close)
	provider := bridgeprovider.NewProvider(discovery.ServiceNameQGISBridge, instances)
	provider.SetDefaultInstanceSource(discovery.SourceAgentReverseWS)
	provider.SetToolContractProvider(func() ([]toolcontract.ExecutionManifestEntry, error) { return manifest, nil })
	s := NewDeepServer(0, nil)
	s.SetLocalBridgeProvider(provider)
	provider.SetCatalogChangedHook(func() { s.HandleLocalBridgeStateChanged() })
	t.Cleanup(func() { _ = s.Shutdown(context.Background()); _ = provider.Close() })
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.HandleConnection)
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)
	parsed, _ := url.Parse(httpServer.URL)
	if err := os.MkdirAll(filepath.Join(home, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "data", ".port"), []byte(parsed.Port()), 0600); err != nil {
		t.Fatal(err)
	}
	logfile, err := os.Create(filepath.Join(home, "qgis-worker.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logfile.Close() })
	command := exec.Command(python, filepath.Join(assets, "tests", "integration", "flow_qgis_ws_worker.py"))
	command.Env = append(os.Environ(), "QT_QPA_PLATFORM=offscreen", "PYTHONDONTWRITEBYTECODE=1")
	command.Stdout = logfile
	command.Stderr = logfile
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	processDone := make(chan error, 1)
	go func() { processDone <- command.Wait(); close(processDone) }()
	t.Cleanup(func() {
		_ = command.Process.Signal(os.Interrupt)
		forced := false
		select {
		case <-processDone:
		case <-time.After(5 * time.Second):
			forced = true
			_ = command.Process.Kill()
			<-processDone
		}
		code := -1
		if command.ProcessState != nil {
			code = command.ProcessState.ExitCode()
		}
		qgisE2EWrite(t, filepath.Join(home, "worker-exit.json"), map[string]any{"pid": command.Process.Pid, "wait_completed": command.ProcessState != nil, "exit_code": code, "forced_kill": forced})
	})
	deadline := time.Now().Add(30 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		select {
		case err := <-processDone:
			t.Fatalf("QGIS worker exited before registration: %v; log %s", err, logfile.Name())
		default:
		}
		var observed struct {
			Tools       []string `json:"tools"`
			QGISVersion string   `json:"qgis_version"`
		}
		readyJSON, readErr := os.ReadFile(filepath.Join(home, "worker-ready.json"))
		if readErr == nil && json.Unmarshal(readyJSON, &observed) == nil && slices.Contains(provider.ConnectedInstanceIDs(), "t53-production-qgis") && slices.Contains(observed.Tools, "data_load_vector") && slices.Contains(observed.Tools, "data_export_vector") && slices.Contains(observed.Tools, "get_layers_info") {
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("real QGIS authority handshake timed out; log %s", logfile.Name())
	}
	trace := &qgisE2ETrace{}
	bridge := bridgeprovider.NewComputeToolInvoker(provider)
	backend := skill.NewQGISBackend(func(ctx context.Context, name string, args map[string]any) (*compute.InvokeResponse, error) {
		response, err := bridge.InvokeTool(ctx, name, args)
		trace.mu.Lock()
		trace.calls = append(trace.calls, map[string]any{"name": name, "arguments": args, "response": response, "session_id": toolsession.SessionIDFromContext(ctx), "instance_id": toolsession.InstanceIDFromContext(ctx)})
		trace.mu.Unlock()
		return response, err
	}, nil, nil, func(ctx context.Context, args map[string]any) { s.InjectToolRuntimeArgs(ctx, args) })
	sharedResources := resource.NewLibrary(filepath.Join(assets, "resources", "system"), filepath.Join(home, "resources"), bindings.New(filepath.Join(home, "config", "resource_bindings.yaml")))
	engine := skill.NewEngine(registry, backend, skill.NewInternalBackend(runtimecontrol.Invoke))
	engine.SetResourceResolver(sharedResources)
	baseTools, err := skill.BuildToolsFromRegistry(registry, engine)
	if err != nil {
		t.Fatal(err)
	}
	layerName := "验收矢量-" + origin
	models := map[string]*qgisE2EModel{
		"main":     {tool: "flow_start", args: map[string]any{"flow_id": qgisE2EFlowID, "inputs": map[string]any{"input_data": input, "output_path": output}}},
		"loader":   {tool: "data_load_vector", args: map[string]any{"path": input, "layer_name": layerName, "reuse_existing": false}},
		"exporter": {tool: "data_export_vector", args: map[string]any{"layer": layerName, "output": output, "format": "GeoJSON"}, output: output},
	}
	profile := &config.AgentConfigFile{Version: config.AgentConfigVersionV20, Runtime: &config.RuntimeConfig{Agents: map[string]config.AgentDefData{
		"orchestrator": {Kind: "agent", Enabled: true, IsMain: true, DisplayName: "编排核验角色", ToolPackages: []string{"flow-control"}, LLM: config.AgentLLMConfig{Provider: "main"}, Prompts: map[string]string{"static_instructions": "按用户要求启动核验流程。"}},
		"loader":       {Kind: "agent", Enabled: true, FlowOnly: true, DisplayName: "加载核验角色", ToolPackages: []string{"data-loading"}, LLM: config.AgentLLMConfig{Provider: "loader"}, Prompts: map[string]string{"static_instructions": "执行加载工具并返回真实图层标识。"}},
		"exporter":     {Kind: "agent", Enabled: true, FlowOnly: true, DisplayName: "导出核验角色", ToolPackages: []string{"data-loading"}, LLM: config.AgentLLMConfig{Provider: "exporter"}, Prompts: map[string]string{"static_instructions": "执行导出工具并返回真实文件路径。"}},
	}}, Flows: &config.FlowConfig{Enabled: []string{qgisE2EFlowID}}}
	configBase := filepath.Join(home, "config")
	governance := control.NewRegistryGovernance(control.RegistryGovernanceDeps{
		ResolveToolDirs:  func() []string { return []string{filepath.Join(assets, "tools", "system")} },
		ResolveSkillDirs: func() []string { return []string{filepath.Join(assets, "skills", "system")} },
	})
	factory, err := agentruntime.NewFactory(&agentruntime.FactoryConfig{DefaultChatModel: models["main"], RegistryGovernance: governance, ResolveChatModel: func(provider string) (model.ToolCallingChatModel, error) {
		selected := models[provider]
		if selected == nil {
			return nil, fmt.Errorf("unexpected model provider %s", provider)
		}
		return selected, nil
	}, BaseTools: baseTools, MaxIterations: 5, BuiltinSkillsDir: filepath.Join(assets, "skills", "system"), PluginSkillsDir: filepath.Join(assets, "skills", "system"), ConfigBaseDir: configBase})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := factory.Build(context.Background(), "qgis-e2e", profile)
	if err != nil {
		t.Fatal(err)
	}
	s.SetRuntime(runtime)
	s.SetRuntimeFactory(factory)
	runtimeConfig := config.ConvertToRuntimeSkillConfigWithPlugin(profile, filepath.Join(assets, "skills", "system"), "", "", configBase, "qgis-e2e")
	roles := []flow.RoleSpec{}
	for _, name := range []string{"orchestrator", "loader", "exporter"} {
		role := profile.Runtime.Agents[name]
		roles = append(roles, flow.RoleSpec{ID: name, DisplayName: role.DisplayName, Kind: role.Kind, Enabled: role.Enabled, FlowOnly: role.FlowOnly, IsMain: role.IsMain, ToolPackages: runtimeConfig.GetAgentSkills(name), Tools: runtimeConfig.GetAgentAllowedTools(name)})
	}
	environment := flow.Environment{Scheme: "qgis-e2e", SchemeRevision: "isolated-e2e-v2", CatalogRevision: "real-authority", Roles: roles, Model: qgisE2ECompileModel{}, ModelName: "deterministic-offline-fixture"}
	owners := map[string][]string{}
	for _, pkg := range registry.Packages() {
		if pkg.Kind != "category" {
			continue
		}
		environment.Packages = append(environment.Packages, flow.PackageSpec{ID: pkg.Name, DisplayName: pkg.DisplayName, Enabled: pkg.Enabled, Tools: pkg.ToolRefs})
		for _, name := range pkg.ToolRefs {
			owners[name] = append(owners[name], pkg.Name)
		}
	}
	for _, definition := range authority {
		if len(owners[definition.Metadata.Name]) > 0 {
			environment.Capabilities = append(environment.Capabilities, flow.CapabilitySpec{Name: definition.Metadata.Name, DisplayName: definition.Metadata.DisplayName, Description: definition.Spec.Description, Keywords: definition.Metadata.Keywords, PackageIDs: owners[definition.Metadata.Name]})
		}
	}
	store := flow.NewStore(filepath.Join(home, "empty-builtins"), filepath.Join(home, "flows"), sharedResources.Bindings)
	if _, err := store.Create(qgisE2EFlowMD, qgisE2EFlowID); err != nil {
		t.Fatal(err)
	}
	compiler := flow.NewCompiler(flow.CompilerOptions{Store: store, Resources: sharedResources, CacheDir: filepath.Join(home, "compile-cache"), LoadEnvironment: func(context.Context, string) (flow.Environment, error) { return environment, nil }})
	plan, err := compiler.EnsureCompiled(context.Background(), qgisE2EFlowID, "qgis-e2e")
	if err != nil {
		t.Fatalf("production compile: %v plan=%+v", err, plan)
	}
	if len(plan.Steps) != 2 || plan.Steps[0].Role != "loader" || plan.Steps[1].Role != "exporter" {
		t.Fatalf("actual compiler selected unexpected steps: %+v", plan.Steps)
	}
	if err := s.SetFlowCompiler(compiler); err != nil {
		t.Fatal(err)
	}
	s.HandleLocalBridgeStateChanged()
	sessionID := "qgis-flow-" + origin
	if err := s.SaveSession(map[string]any{"session_id": sessionID, "messages": []any{}, "project_path": project}, project); err != nil {
		t.Fatal(err)
	}
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": []string{"http://localhost:3000"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	data := map[string]any{"project_path": project, "instance_id": "t53-production-qgis", "mode": "build"}
	if origin == "direct" {
		data["flow"] = map[string]any{"id": qgisE2EFlowID, "inputs": map[string]any{"input_data": input, "output_path": output}}
	}
	if err := conn.WriteJSON(Message{Type: MsgTypeTaskCreate, SessionID: sessionID, RequestID: "qgis-" + origin, IdempotencyKey: "qgis-" + origin, Content: "加载隔离GeoJSON并导出新的GeoJSON文件。", Data: data}); err != nil {
		t.Fatal(err)
	}
	events := []Message{}
	finished, ack, stopped := false, false, false
	lastSeq := uint64(0)
	_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
	for !(finished && ack && stopped) {
		var message Message
		if err := conn.ReadJSON(&message); err != nil {
			qgisE2EWrite(t, filepath.Join(home, "events.json"), events)
			t.Fatalf("production websocket did not finish: %v; artifacts %s", err, home)
		}
		events = append(events, message)
		if message.SessionID != sessionID {
			continue
		}
		if message.Type == MsgTypeTaskAck {
			ack = message.Data["accepted"] == true
		}
		if message.Type == MsgTypeError {
			t.Fatalf("production websocket error: %s %+v; artifacts %s", message.Content, message.Data, home)
		}
		if message.Type == MsgTypeRunEvent {
			seq := parseUint64Like(message.Data["seq"])
			if seq <= lastSeq {
				t.Fatalf("run event sequence regressed %d -> %d", lastSeq, seq)
			}
			lastSeq = seq
			if message.Data["event"] == "flow.finished" {
				finished = true
			}
			if message.Data["source_type"] == MsgTypeTaskRunningChange {
				payload, _ := message.Data["payload"].(map[string]any)
				if payload["running"] == false {
					stopped = true
				}
			}
		}
		if message.Type == MsgTypeFlowEvent && message.Data["event"] == "flow.finished" {
			finished = true
		}
		if message.Type == MsgTypeTaskRunningChange && message.Data["running"] == false {
			stopped = true
		}
	}
	_, controller := s.flowDependencies()
	state, err := controller.Snapshot(sessionID)
	if err != nil || state == nil {
		t.Fatalf("missing production controller snapshot: %v", err)
	}
	if state.Status != "completed" || state.Origin != origin || len(state.Steps) != 2 {
		t.Fatalf("unexpected production run: %+v", state)
	}
	for _, step := range state.Steps {
		if step.Status != "completed" || len(step.Attempts) != 1 || !step.Attempts[0].Verification.OK {
			t.Fatalf("step did not pass first attempt and authoritative verification: %+v", step)
		}
	}
	persisted, err := os.ReadFile(state.RunJSONPath)
	if err != nil {
		t.Fatal(err)
	}
	var stored flowrun.RunState
	if err := json.Unmarshal(persisted, &stored); err != nil || stored.Status != "completed" || stored.PlanCacheKey != plan.CompileKey {
		t.Fatalf("run.json lost completed plan: %v", err)
	}
	actualOutput, ok := state.Steps[1].Outputs["导出文件"].(string)
	if !ok || !filepath.IsAbs(actualOutput) {
		t.Fatal("verified receipt did not retain the actual export path")
	}
	relativeOutput, err := filepath.Rel(filepath.Clean(home), actualOutput)
	if err != nil || relativeOutput == ".." || strings.HasPrefix(relativeOutput, ".."+string(os.PathSeparator)) {
		t.Fatal("real export escaped the isolated runtime home", err)
	}
	exported, err := os.ReadFile(actualOutput)
	if err != nil {
		t.Fatal(err)
	}
	var geo struct {
		Features []struct {
			Properties struct {
				Name  string `json:"name"`
				Value int    `json:"value"`
			} `json:"properties"`
			Geometry struct {
				Type        string    `json:"type"`
				Coordinates []float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(exported, &geo); err != nil {
		t.Fatal(err)
	}
	if len(geo.Features) != 2 {
		t.Fatalf("real export lost features: %s", exported)
	}
	for index, feature := range geo.Features {
		if feature.Properties.Name != []string{"核验甲", "核验乙"}[index] || feature.Properties.Value != []int{7, 11}[index] || feature.Geometry.Type != "Point" || !slices.Equal(feature.Geometry.Coordinates, []float64{110 + float64(index), 30 + float64(index)}) {
			t.Fatalf("real export changed attributes or coordinates: %+v", geo.Features)
		}
	}
	after, err := os.ReadFile(input)
	if err != nil || string(after) != string(source) {
		t.Fatal("input fixture was modified", err)
	}
	trace.mu.Lock()
	calls := append([]map[string]any(nil), trace.calls...)
	trace.mu.Unlock()
	counts := map[string]int{}
	for _, call := range calls {
		counts[call["name"].(string)]++
		if call["instance_id"] != "t53-production-qgis" || call["session_id"] != sessionID {
			t.Fatalf("trusted target context lost: %+v", call)
		}
	}
	if counts["data_load_vector"] != 1 || counts["data_export_vector"] != 1 || counts["get_layers_info"] < 1 {
		t.Fatalf("real bridge calls were missing or repeated: %+v", counts)
	}
	sum := sha256.Sum256(source)
	qgisE2EWrite(t, filepath.Join(home, "result.json"), map[string]any{"origin": origin, "status": state.Status, "real_qgis": true, "real_llm": false, "compiler": "production with deterministic JSON model", "role_factory": "production agentruntime.Factory", "source_sha256": hex.EncodeToString(sum[:]), "source_unchanged": true, "features": len(geo.Features), "bridge_calls": counts, "run_json": state.RunJSONPath, "export": actualOutput, "requested_output": output, "event_seq": lastSeq})
	qgisE2EWrite(t, filepath.Join(home, "events.json"), events)
	qgisE2EWrite(t, filepath.Join(home, "bridge-calls.json"), calls)
	qgisE2EWrite(t, filepath.Join(home, "plan.json"), plan)
	t.Logf("production WS/QGIS %s passed; artifacts: %s", origin, home)
}
