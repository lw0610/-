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
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/layers"
	cartoplan "github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/planexec"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/tool"
	"gopkg.in/yaml.v3"
)

type scenario struct {
	LayerImportDir string   `json:"layer_import_dir"`
	BuiltinLayers  []string `json:"builtin_layers"`
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
	builtinStyles map[string]any
	plan          *cartoplan.Plan
	mu            sync.Mutex
	in            io.WriteCloser
	out           *bufio.Scanner
	command       *exec.Cmd
	transcript    *os.File
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
	outputNames := map[string]string{}
	step, _ := b.plan.StepByID(r.Step.ID)
	for _, product := range step.Produces {
		outputNames[product.ID] = product.Name
	}
	response, e := b.call(map[string]any{"op": "step", "step": r.Step, "role": r.Role, "messages": r.Messages, "resources": resources, "output_names": outputNames})
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
	b := &bridge{in: in, out: scan, command: command, transcript: transcript, builtinStyles: map[string]any{}}
	defer func() { in.Close(); command.Wait() }()
	if e = importLayers(ctx, s, b, library); e != nil {
		return e
	}
	guide, e := store.Resolve(ctx, s.FlowID, library)
	if e != nil {
		return e
	}
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
	b.plan = visible
	precheck := cartoplan.CheckData(visible, func(d *cartoplan.Data) cartoplan.Precheck {
		if d.Source == "builtin" {
			catalog, e := layers.NewDefaultLibrary()
			if e != nil {
				return cartoplan.Precheck{Status: "unavailable", Reason: e.Error()}
			}
			matches, e := catalog.Match(layers.Requirement{ExplicitName: d.Ref, Name: d.Name})
			if e != nil || len(matches) == 0 {
				return cartoplan.Precheck{Status: "unavailable", Reason: "内置图层未匹配：" + d.Ref}
			}
			return cartoplan.Precheck{Status: "available", Reason: strings.Join(matches[0].Reasons, "；"), Ref: matches[0].Layer.Name}
		}
		if _, e := os.Stat(d.Ref); e != nil {
			return cartoplan.Precheck{Status: "unavailable", Reason: e.Error()}
		}
		return cartoplan.Precheck{Status: "available", Reason: "已读取用户夹具路径"}
	})
	if e = writeJSON(filepath.Join(s.Out, "precheck.json"), precheck); e != nil {
		return e
	}
	for _, check := range precheck {
		if check.Status == "unavailable" {
			return fmt.Errorf("预检失败：%s", check.Reason)
		}
	}
	resourceParameters := map[string][]tool.ToolParameter{}
	for _, definition := range defs {
		for _, parameter := range definition.Spec.Parameters {
			if parameter.Type == "resource" || len(parameter.Schema) > 0 {
				resourceParameters[definition.Metadata.Name] = append(resourceParameters[definition.Metadata.Name], parameter)
			}
		}
	}
	if _, e = b.call(map[string]any{"op": "init", "plan": plan, "resource_parameters": resourceParameters, "builtin_styles": b.builtinStyles}); e != nil {
		return e
	}
	eventfile, e := os.Create(filepath.Join(s.Out, "events.jsonl"))
	if e != nil {
		return e
	}
	defer eventfile.Close()
	controller, e := flowrun.NewController(flowrun.Options{Runner: b, Tools: b, DataGet: acceptanceData, Store: flowrun.FileRunStore{}, Events: &events{f: eventfile}})
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
				p.Data = append(p.Data, cartoplan.Data{ID: id, Name: label, Source: "user", Ref: path})
				uses = append(uses, id)
				continue
			}
		}
		p.Facts = append(p.Facts, cartoplan.Fact{Name: name, Value: value, From: "固定验收夹具"})
	}
	for _, name := range s.BuiltinLayers {
		id := fmt.Sprintf("d%d", len(p.Data)+1)
		p.Data = append(p.Data, cartoplan.Data{ID: id, Name: name, Source: "builtin", Ref: name, Kind: "layer"})
		uses = append(uses, id)
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
		st := cartoplan.Step{ID: id, Title: "验收步骤 " + id, Inputs: append([]string{}, uses...), Instructions: "执行固定动作配方并返回实际核验产出"}
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
			product := cartoplan.Product{ID: fmt.Sprintf("%s_out%d", id, len(st.Produces)+1), Name: label, Kind: o.Type, Format: o.Format}
			if product.Kind == "layers" {
				product.Kind = "layer"
			}
			st.Produces = append(st.Produces, product)
		}
		p.Steps = append(p.Steps, st)
		for _, product := range st.Produces {
			uses = append(uses, product.ID)
		}
	}
	if len(p.Steps) > 0 {
		for _, product := range p.Steps[len(p.Steps)-1].Produces {
			p.Deliverables = append(p.Deliverables, product.Declaration())
		}
	}
	return p, nil
}

// Imports run through the same scanner, QGIS header probe and registration
// library as the UI. Original data are never copied into a flow package.
func importLayers(ctx context.Context, s scenario, b *bridge, library *resource.Library) error {
	if len(s.BuiltinLayers) == 0 {
		return nil
	}
	catalog, e := layers.NewDefaultLibrary()
	if e != nil {
		return e
	}
	sources, e := datasource.Load(filepath.Join(s.Root, "autocarto/tools/system/data-acquisition/assets"), "")
	if e != nil {
		return e
	}
	draft, e := sources.ScanLocalDraft(ctx, s.LayerImportDir)
	if e != nil {
		return e
	}
	wanted := map[string]bool{}
	for _, name := range s.BuiltinLayers {
		wanted[name] = true
	}
	manifests := []layers.Manifest{}
	inspection := []any{}
	for _, entry := range draft.Entries {
		name := strings.TrimSuffix(entry.Name, filepath.Ext(entry.Name))
		if !wanted[name] {
			continue
		}
		path := filepath.Join(draft.Source.Root, entry.Path)
		headers, e := dataservice.InspectProvided(ctx, path)
		if e != nil {
			return e
		}
		for _, header := range headers {
			response, e := b.Invoke(ctx, "layer_inspect", map[string]any{"path": path, "sublayer": header.Sublayer})
			if e != nil {
				return e
			}
			raw, _ := json.Marshal(response)
			var probe struct {
				Success bool   `json:"success"`
				Info    string `json:"info"`
				Data    struct {
					CRS          string          `json:"crs"`
					DeclaredCRS  string          `json:"declared_crs"`
					Geometry     string          `json:"geometry"`
					Fields       []string        `json:"available_fields"`
					FeatureCount int64           `json:"feature_count"`
					Coverage     layers.Coverage `json:"coverage"`
					Scale        layers.Scale    `json:"scale"`
				} `json:"data"`
			}
			if e = json.Unmarshal(raw, &probe); e != nil {
				return e
			}
			if !probe.Success {
				return fmt.Errorf("夹具图层探测失败：%s", probe.Info)
			}
			inspection = append(inspection, response)
			m := layers.Manifest{APIVersion: "layer/v1", ID: layers.NewID(), Name: name, Pack: "China_map 验收夹具", File: path, Sublayer: header.Sublayer, CRS: probe.Data.CRS, DeclaredCRS: probe.Data.DeclaredCRS, Geometry: probe.Data.Geometry, FeatureCount: probe.Data.FeatureCount, Coverage: probe.Data.Coverage, Scale: probe.Data.Scale, Source: layers.Source{Title: "显式选择的离线验收夹具", License: "用户提供"}, Compliance: "验收使用文件声明坐标系，不代表已核实真实坐标系或公开制图合规性"}
			if e = catalog.Suggest(&m, probe.Data.Fields); e != nil {
				return e
			}
			manifests = append(manifests, m)
		}
	}
	items, e := catalog.SaveBatch(manifests)
	if e != nil {
		return e
	}
	if len(items) != len(s.BuiltinLayers) {
		return fmt.Errorf("夹具缺少要求的内置图层：需要%d，导入%d", len(s.BuiltinLayers), len(items))
	}
	for _, item := range items {
		params := map[string]any{"resource_type": "layer", "resource_path": item.Path(), "output_path": item.PreviewPath(), "crs": item.CRS, "label_field": item.LabelField, "sublayer": item.Sublayer}
		if item.DefaultStyle != "" {
			resolved := library.Resolve(ctx, resource.Ref{Type: "style", Name: item.DefaultStyle}, resource.Scope{})
			handle := resolved.Handle()
			if handle == nil {
				return fmt.Errorf("验收图层默认样式无法使用：%s：%v", item.DefaultStyle, resolved.Problems)
			}
			params["style"] = handle
			b.builtinStyles[item.Name] = handle
		}
		if _, e = b.Invoke(ctx, "resource_preview", params); e != nil {
			return e
		}
	}
	return writeJSON(filepath.Join(s.Out, "imported-layers.json"), map[string]any{"items": items, "inspection": inspection})
}
func acceptanceData(ctx context.Context, run *flowrun.RunState, d cartoplan.Data) (flowrun.DataResult, error) {
	if d.Source == "user" {
		if _, e := os.Stat(d.Ref); e != nil {
			return flowrun.DataResult{}, e
		}
		return flowrun.DataResult{Value: d.Ref, Details: map[string]any{"source": "user", "path": d.Ref}}, nil
	}
	catalog, e := layers.NewDefaultLibrary()
	if e != nil {
		return flowrun.DataResult{}, e
	}
	requirement := layers.Requirement{ExplicitName: d.Ref, Name: d.Name}
	if d.Extent != nil {
		requirement.Extent = d.Extent.BBox
	}
	matches, e := catalog.Match(requirement)
	if e != nil {
		return flowrun.DataResult{}, e
	}
	if len(matches) == 0 {
		return flowrun.DataResult{}, fmt.Errorf("内置图层匹配失败：%s", d.Ref)
	}
	item := matches[0].Layer
	return flowrun.DataResult{Value: item.Path(), Details: map[string]any{"source": "builtin", "builtin_name": item.Name, "load_method": "layer_load_builtin", "path": item.Path(), "crs": item.CRS}, Notes: []string{item.Name + "：" + item.Source.Title}}, nil
}
