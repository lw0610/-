// 本程序是离线验收适配器：真实编译、控制器、存储和 QGIS 工具，固定角色动作，无模型请求。
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"

	"github.com/autocarto/carto_agent/internal/api"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/tool"
	"gopkg.in/yaml.v3"
)

type scenario struct {
	// Optional additions let acceptance scenarios consume newly exported artifacts.
	// Existing built-in scenarios omit these fields and retain their original path.
	FlowPackage     string `json:"flow_package"`
	ResourceImports []struct {
		Source string `json:"source"`
		Type   string `json:"type"`
		Name   string `json:"name"`
	} `json:"resource_imports"`
	ID       string         `json:"id"`
	FlowID   string         `json:"flow_id"`
	Root     string         `json:"root"`
	Out      string         `json:"out"`
	Python   string         `json:"python"`
	Examples string         `json:"examples"`
	Inputs   map[string]any `json:"inputs"`
	Model    struct {
		Flow       json.RawMessage                `json:"flow"`
		Conditions map[string]*flow.ConditionRule `json:"conditions"`
	} `json:"model"`
	Steps map[string]struct {
		Actions []struct {
			Tool string `json:"tool"`
		} `json:"actions"`
	} `json:"steps"`
}
type fixedModel struct{ s scenario }

func (m fixedModel) Complete(_ context.Context, r flow.ModelRequest) (string, error) {
	if r.Kind == "flow" {
		return string(m.s.Model.Flow), nil
	}
	tools := []string{}
	seen := map[string]bool{}
	for _, a := range m.s.Steps[r.Step].Actions {
		if a.Tool != "" && !seen[a.Tool] {
			tools = append(tools, a.Tool)
			seen[a.Tool] = true
		}
	}
	b, e := json.Marshal(map[string]any{"suggested_tools": tools, "missing_capabilities": []string{}, "expected_outputs": []any{}, "condition": m.s.Model.Conditions[r.Step], "notes": "确定性验收模型夹具，无网络请求"})
	return string(b), e
}

type bridge struct {
	mu         sync.Mutex
	in         io.WriteCloser
	out        *bufio.Scanner
	command    *exec.Cmd
	transcript *os.File
}

func (b *bridge) call(value any) (map[string]any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	raw, e := json.Marshal(value)
	if e != nil {
		return nil, e
	}
	if _, e = b.in.Write(append(raw, '\n')); e != nil {
		return nil, e
	}
	if !b.out.Scan() {
		return nil, fmt.Errorf("QGIS 通道终止：%v", b.out.Err())
	}
	response := map[string]any{}
	if e = json.Unmarshal(b.out.Bytes(), &response); e != nil {
		return nil, fmt.Errorf("QGIS 通道非JSON：%s", b.out.Text())
	}
	json.NewEncoder(b.transcript).Encode(map[string]any{"request": value, "response": response})
	if response["ok"] != true {
		return nil, fmt.Errorf("QGIS 执行失败：%v", response["error"])
	}
	return response, nil
}
func (b *bridge) Run(ctx context.Context, r flowrun.StepRequest) (flowrun.StepResult, error) {
	if r.Mode != "step" {
		return flowrun.StepResult{}, fmt.Errorf("确定性验收不代替用户处理 %s", r.Mode)
	}
	resources := []map[string]any{}
	for _, selected := range r.Resources {
		if selected != nil {
			resources = append(resources, map[string]any{"key": selected.Key, "label": selected.Label, "handle": selected.Handle()})
		}
	}
	response, e := b.call(map[string]any{"op": "step", "step": r.Step, "role": r.Role, "messages": r.Messages, "resources": resources})
	if e != nil {
		return flowrun.StepResult{}, e
	}
	raw, e := json.Marshal(response["receipt"])
	return flowrun.StepResult{LastContent: string(raw)}, e
}
func (b *bridge) Invoke(ctx context.Context, name string, params map[string]any) (any, error) {
	r, e := b.call(map[string]any{"op": "verify", "tool": name, "params": params})
	if e != nil {
		return nil, e
	}
	return r["result"], nil
}

type events struct {
	f   *os.File
	seq int64
}

func (e *events) Emit(_ context.Context, event flowrun.Event) int64 {
	e.seq++
	json.NewEncoder(e.f).Encode(map[string]any{"seq": e.seq, "event": event})
	return e.seq
}
func writeJSON(path string, v any) error {
	raw, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(raw, '\n'), 0600)
}
func fail(e error) { fmt.Fprintln(os.Stderr, e); os.Exit(1) }
func main() {
	if e := run(); e != nil {
		fail(e)
	}
}
func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("请传入验收配置JSON路径")
	}
	raw, e := os.ReadFile(os.Args[1])
	if e != nil {
		return e
	}
	var s scenario
	if e = json.Unmarshal(raw, &s); e != nil {
		return e
	}
	ctx := context.Background()
	assets := filepath.Join(s.Root, "autocarto")
	home := filepath.Join(s.Out, "controller-runtime")
	for k, v := range map[string]string{"AUTOCARTO_PROJECT_ROOT": s.Root, "AUTOCARTO_ASSETS_ROOT": assets, "AUTOCARTO_HOME": home} {
		os.Setenv(k, v)
	}
	profileRaw, e := os.ReadFile(filepath.Join(s.Root, "carto_agent/defaults/agent_configs/default/agent.yaml"))
	if e != nil {
		return e
	}
	var profile config.AgentConfigData
	if e = yaml.Unmarshal(profileRaw, &profile); e != nil {
		return e
	}
	// 只修改隔离的验收方案，允许导入两个新流程；不修改工作方案或默认文件。
	if profile.Flows == nil {
		profile.Flows = &config.FlowConfig{}
	}
	profile.Flows.Enabled = append(profile.Flows.Enabled, s.FlowID)
	profileRaw, e = yaml.Marshal(profile)
	if e != nil {
		return e
	}
	configDir := filepath.Join(home, "config/agent_configs")
	profilePath := filepath.Join(configDir, "acceptance/agent.yaml")
	os.MkdirAll(filepath.Dir(profilePath), 0700)
	if e = os.WriteFile(profilePath, profileRaw, 0600); e != nil {
		return e
	}
	manager := config.NewAgentConfigManager(configDir)
	service := settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{SkillsDir: filepath.Join(assets, "skills/system"), BuiltinSkillsDir: filepath.Join(assets, "skills/system"), UserSkillsDir: filepath.Join(home, "skills"), GlobalSkillsDir: filepath.Join(home, "skills"), BuiltinToolsDir: filepath.Join(assets, "tools/system"), UserToolsDir: filepath.Join(home, "tools"), RuntimeToolDirs: []string{filepath.Join(assets, "tools/system")}, RuntimeSkillDirs: []string{filepath.Join(assets, "skills/system")}}, manager)
	library, e := resource.NewDefaultLibrary()
	if e != nil {
		return e
	}
	importedResources := []*resource.ResolvedResource{}
	for _, request := range s.ResourceImports {
		imported, err := library.ImportNamed(request.Source, request.Type, request.Name)
		if err != nil {
			return fmt.Errorf("导入验收资源 %s：%w", request.Name, err)
		}
		if resource.HasErrors(imported.Problems) {
			return fmt.Errorf("验收资源 %s 校验失败：%+v", request.Name, imported.Problems)
		}
		importedResources = append(importedResources, imported)
	}
	if len(importedResources) > 0 {
		if e = writeJSON(filepath.Join(s.Out, "imported-resources.json"), importedResources); e != nil {
			return e
		}
	}
	store, e := flow.NewDefaultStore(library)
	if e != nil {
		return e
	}
	entries, e := os.ReadDir(s.Examples)
	if e != nil {
		return e
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if _, e = store.Import(filepath.Join(s.Examples, entry.Name()), flow.ImportOptions{}); e != nil {
				return e
			}
		}
	}

	if s.FlowPackage != "" {
		imported, err := store.Import(s.FlowPackage, flow.ImportOptions{}, library)
		if err != nil {
			return fmt.Errorf("导入验收流程：%w", err)
		}
		if imported.Meta.ID != s.FlowID {
			return fmt.Errorf("导入流程编号 %s 与场景编号 %s 不一致", imported.Meta.ID, s.FlowID)
		}
	}

	a := api.NewAPI(nil, manager, nil, service, filepath.Join(assets, "skills/system"), nil)
	a.SetFlowStore(store)
	a.SetResourceLibrary(library)
	production, e := a.FlowCompiler()
	if e != nil {
		return e
	}
	base, e := production.BasePlan(ctx, s.FlowID, "acceptance")
	if e != nil {
		return e
	}
	if e = writeJSON(filepath.Join(s.Out, "diagnostics-fallback.json"), base.Diagnostics); e != nil {
		return e
	}
	if base.HasErrors() {
		return fmt.Errorf("正式方案校验错误：%+v", base.Diagnostics)
	}
	defs, e := tool.LoadAuthorityDefinitions(filepath.Join(assets, "tools/system"))
	if e != nil {
		return e
	}
	manifest := tool.BuildExecutionManifestEntries(defs)
	if e = writeJSON(filepath.Join(s.Out, "authority-manifest.json"), manifest); e != nil {
		return e
	}
	env := flow.Environment{Scheme: "acceptance", SchemeRevision: base.SchemeRevision, CatalogRevision: base.CatalogRevision, Roles: base.Roles, Model: fixedModel{s}, ModelName: "确定性验收夹具"}
	packages, e := service.ListSkillPackages()
	if e != nil {
		return e
	}
	owners := map[string][]string{}
	for _, p := range packages {
		if p.Kind != "category" {
			continue
		}
		d, e := service.GetSkillPackage(p.Name)
		if e != nil {
			return e
		}
		id := p.PackageID
		if id == "" {
			id = p.Name
		}
		env.Packages = append(env.Packages, flow.PackageSpec{ID: id, DisplayName: p.DisplayName, Tools: d.ToolRefs, Enabled: p.Enabled})
		for _, name := range d.ToolRefs {
			owners[name] = append(owners[name], id)
		}
	}
	for _, d := range defs {
		if len(owners[d.Metadata.Name]) == 0 {
			continue
		}
		env.Capabilities = append(env.Capabilities, flow.CapabilitySpec{Name: d.Metadata.Name, DisplayName: d.Metadata.DisplayName, Description: d.Spec.Description, Keywords: d.Metadata.Keywords, PackageIDs: owners[d.Metadata.Name]})
	}
	sort.Slice(env.Packages, func(i, j int) bool { return env.Packages[i].ID < env.Packages[j].ID })
	sort.Slice(env.Capabilities, func(i, j int) bool { return env.Capabilities[i].Name < env.Capabilities[j].Name })
	compiler := flow.NewCompiler(flow.CompilerOptions{Store: store, Resources: library, CacheDir: filepath.Join(home, "data/acceptance-cache"), LoadEnvironment: func(context.Context, string) (flow.Environment, error) { return env, nil }})
	// Enrich explicitly inside this offline harness, using only its fixed model.
	// Production request paths and Controller.Start use the nonblocking Plan.
	plan, e := compiler.Compile(ctx, s.FlowID, "acceptance", false)
	if e != nil {
		return e
	}
	if e = writeJSON(filepath.Join(s.Out, "plan.json"), plan); e != nil {
		return e
	}
	command := exec.CommandContext(ctx, s.Python, filepath.Join(assets, "tests/integration/acceptance/qgis_worker.py"), os.Args[1])
	errlog, e := os.Create(filepath.Join(s.Out, "qgis.log"))
	if e != nil {
		return e
	}
	defer errlog.Close()
	command.Stderr = errlog
	in, e := command.StdinPipe()
	if e != nil {
		return e
	}
	out, e := command.StdoutPipe()
	if e != nil {
		return e
	}
	if e = command.Start(); e != nil {
		return e
	}
	transcript, e := os.Create(filepath.Join(s.Out, "transcript.jsonl"))
	if e != nil {
		return e
	}
	defer transcript.Close()
	scan := bufio.NewScanner(out)
	scan.Buffer(make([]byte, 4096), 32<<20)
	b := &bridge{in: in, out: scan, command: command, transcript: transcript}
	defer func() { in.Close(); command.Wait() }()
	resourceParameters := map[string][]tool.ToolParameter{}
	for _, definition := range defs {
		for _, parameter := range definition.Spec.Parameters {
			if parameter.Type == "resource" {
				resourceParameters[definition.Metadata.Name] = append(resourceParameters[definition.Metadata.Name], parameter)
			}
		}
	}
	if _, e = b.call(map[string]any{"op": "init", "plan": plan, "resource_parameters": resourceParameters}); e != nil {
		return e
	}
	eventfile, e := os.Create(filepath.Join(s.Out, "events.jsonl"))
	if e != nil {
		return e
	}
	defer eventfile.Close()
	controller, e := flowrun.NewController(flowrun.Options{Compiler: compiler, Runner: b, Tools: b, Store: flowrun.FileRunStore{}, Events: &events{f: eventfile}})
	if e != nil {
		return e
	}
	state, e := controller.Start(ctx, flowrun.StartRequest{SessionID: s.ID, SessionDir: s.Out, ProjectRoot: s.Root, FlowID: s.FlowID, Scheme: "acceptance", Inputs: s.Inputs, OriginalSource: "请使用本次明确提供的数据制作验收地图。"})
	if e != nil {
		return e
	}
	if e = writeJSON(filepath.Join(s.Out, "controller-result.json"), state); e != nil {
		return e
	}
	if state.Status != "completed" {
		return fmt.Errorf("流程未完成：%s，%+v", state.Status, state.Result.Issues)
	}
	for _, step := range state.Steps {
		for _, attempt := range step.Attempts {
			if !attempt.Verification.OK {
				return fmt.Errorf("步骤校验未通过：%s %+v", step.ID, attempt.Verification)
			}
		}
	}
	if _, e = b.call(map[string]any{"op": "finalize", "state": state}); e != nil {
		return e
	}
	fmt.Println(state.RunJSONPath)
	return nil
}
