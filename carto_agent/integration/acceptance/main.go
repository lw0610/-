// 离线验收适配器：固定动作夹具生成 CartoPlan，使用真实角色选择、控制器、存储和 QGIS 工具；无模型或网络请求。
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
	"strconv"
	"strings"
	"sync"

	"github.com/autocarto/carto_agent/internal/api"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	cartoplan "github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/planexec"
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
	Steps    map[string]struct {
		Outputs map[string]any `json:"outputs"`
		Actions []struct {
			Tool string `json:"tool"`
		} `json:"actions"`
	} `json:"steps"`
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
	store, e := flow.NewDefaultGuideStore(library)
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
	a.SetGuideStore(store)
	a.SetResourceLibrary(library)
	env, e := a.PlanEnvironment(ctx, "acceptance")
	if e != nil {
		return e
	}
	guide, e := store.Resolve(ctx, s.FlowID, library)
	if e != nil {
		return e
	}
	defs, e := tool.LoadAuthorityDefinitions(filepath.Join(assets, "tools/system"))
	if e != nil {
		return e
	}
	manifest := tool.BuildExecutionManifestEntries(defs)
	if e = writeJSON(filepath.Join(s.Out, "authority-manifest.json"), manifest); e != nil {
		return e
	}
	// The offline bridge supplies fixed image-review observations. It makes no
	// real LLM calls; this fixture provider is explicitly recorded as such.
	env.MainProvider = "offline-fixture"
	env.VisionProviders = []string{"offline-fixture"}
	visible, e := fixturePlan(s, env)
	if e != nil {
		return e
	}
	visible.Guide = &cartoplan.GuideRef{ID: s.FlowID, Version: guide.Doc.Meta.Version}
	caps := map[string]bool{}
	for _, p := range env.Packages {
		if p.Enabled {
			caps[p.ID] = true
		}
	}
	if e = cartoplan.RequireValid(visible, cartoplan.Environment{ProjectRoot: s.Root, Capabilities: caps}); e != nil {
		return e
	}
	plan, inputs, e := planexec.Build(visible, planexec.BuildOptions{Environment: env, Guide: guide.Doc, Scope: guide.Scope, OriginalSource: "请使用本次明确提供的数据制作验收地图。"})
	if e != nil {
		return e
	}
	if e = writeJSON(filepath.Join(s.Out, "plan.json"), plan); e != nil {
		return e
	}
	if e = writeJSON(filepath.Join(s.Out, "carto-plan.json"), visible); e != nil {
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
			if parameter.Type == "resource" || len(parameter.Schema) > 0 {
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
	controller, e := flowrun.NewController(flowrun.Options{Runner: b, Tools: b, Store: flowrun.FileRunStore{}, Events: &events{f: eventfile}})
	if e != nil {
		return e
	}
	state, e := controller.StartFromPlan(ctx, flowrun.StartRequest{SessionID: s.ID, SessionDir: s.Out, ProjectRoot: s.Root, FlowID: s.FlowID, Scheme: "acceptance", Inputs: inputs, CartoPlan: visible, Guide: guide.Doc, OriginalSource: "请使用本次明确提供的数据制作验收地图。"}, plan)
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

// fixturePlan adapts deterministic acceptance actions, not GUIDE prose, into a
// concrete visible plan. This verifies execution and never claims LLM quality.
func fixturePlan(s scenario, env flow.Environment) (*cartoplan.Plan, error) {
	p := &cartoplan.Plan{APIVersion: cartoplan.APIVersion, ID: "cp-" + s.ID, Version: 1, Goal: "离线真实QGIS验收：" + s.ID}
	keys := []string{}
	for k := range s.Inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	uses := []string{}
	for _, name := range keys {
		value := s.Inputs[name]
		path, ok := value.(string)
		if ok {
			if _, err := os.Stat(path); err == nil {
				id := fmt.Sprintf("d%d", len(p.Data)+1)
				label := name
				if label == "底图数据" {
					label = "中国底图"
				}
				p.Data = append(p.Data, cartoplan.Data{ID: id, Name: label, From: "user", Path: path})
				uses = append(uses, id)
				continue
			}
		}
		p.Facts = append(p.Facts, cartoplan.Fact{Name: name, Value: value, From: "固定验收夹具"})
	}
	owners := map[string]string{}
	for _, pkg := range env.Packages {
		if !pkg.Enabled || pkg.Hidden {
			continue
		}
		for _, tool := range pkg.Tools {
			if owners[tool] == "" {
				owners[tool] = pkg.ID
			}
		}
	}
	steps := []string{}
	for id := range s.Steps {
		steps = append(steps, id)
	}
	sort.Slice(steps, func(i, j int) bool {
		a, _ := strconv.Atoi(strings.TrimPrefix(steps[i], "s"))
		b, _ := strconv.Atoi(strings.TrimPrefix(steps[j], "s"))
		return a < b
	})
	previous := ""
	for _, id := range steps {
		if s.FlowID == "marching-route" {
			review, _ := s.Inputs["质量审查"].(bool)
			if !review && (id == "s7" || id == "s8" || id == "s9" || id == "s10") {
				continue
			}
			if review && id == "s11" {
				continue
			}
		}
		fixture := s.Steps[id]
		st := cartoplan.Step{ID: id, Title: "验收步骤 " + id, Uses: uses, Instructions: "执行固定动作配方并返回实际核验产出"}
		if previous != "" {
			st.DependsOn = []string{previous}
		}
		previous = id
		seen := map[string]bool{}
		for _, action := range fixture.Actions {
			pkg := owners[action.Tool]
			if pkg != "" && !seen[pkg] {
				seen[pkg] = true
				st.Capabilities = append(st.Capabilities, pkg)
			}
		}
		if len(st.Capabilities) == 0 {
			st.Capabilities = []string{"data-loading"}
		}
		sort.Strings(st.Capabilities)
		labels := []string{}
		for label := range fixture.Outputs {
			labels = append(labels, label)
		}
		sort.Strings(labels)
		for _, label := range labels {
			o := flow.OutputDeclaration{Label: label, Type: "text"}
			switch {
			case label == "版面":
				o.Type = "layout"
			case label == "初审结果":
				o.Type = "review_initial"
			case label == "终审结果":
				o.Type = "review_final"
			case strings.Contains(label, "图层") || label == "参考底图":
				o.Type = "layers"
			case strings.Contains(label, "PDF"):
				o.Type = "file"
				o.Format = "pdf"
			case strings.Contains(label, "PNG") || label == "成果图" || strings.Contains(label, "图片"):
				o.Type = "file"
				o.Format = "png"
			}
			st.Outputs = append(st.Outputs, o)
		}
		p.Steps = append(p.Steps, st)
	}
	if len(p.Steps) > 0 {
		p.Deliverables = p.Steps[len(p.Steps)-1].Outputs
	}
	return p, nil
}
