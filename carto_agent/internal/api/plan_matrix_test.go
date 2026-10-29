package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/defaults"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/planexec"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type matrixRuntime struct{}

func (matrixRuntime) Run(_ context.Context, r flowrun.StepRequest) (flowrun.StepResult, error) {
	outputs := map[string]any{}
	for _, o := range r.Step.ExpectedOutputs {
		outputs[o.Label] = "fixture verified result"
	}
	b, _ := json.Marshal(map[string]any{"status": "completed", "summary": "fixed receipt", "outputs": outputs, "issues": []string{}})
	return flowrun.StepResult{LastContent: string(b)}, nil
}
func TestGuidePlanMatrixAcrossThreeSchemes(t *testing.T) {
	a, manager, root, home := realFlowAPI(t)
	library, err := a.resourceStore()
	if err != nil {
		t.Fatal(err)
	}
	guides, err := flow.NewDefaultGuideStore(library)
	if err != nil {
		t.Fatal(err)
	}
	builtin, err := defaults.FS.ReadFile("agent_configs/default/agent.yaml")
	if err != nil {
		t.Fatal(err)
	}
	profiles := map[string]string{"default": string(builtin), "zttzt": testutil.V4ZttztProfileYAML, "minimal": testutil.V4MinimalProfileYAML}
	for name, profile := range profiles {
		path := filepath.Join(home, "config/agent_configs", name, "agent.yaml")
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, []byte(profile), 0600); err != nil {
			t.Fatal(err)
		}
	}
	manager = config.NewAgentConfigManager(filepath.Join(home, "config/agent_configs"))
	assets := filepath.Join(root, "autocarto")
	service := settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{BuiltinSkillsDir: filepath.Join(assets, "skills/system"), UserSkillsDir: filepath.Join(home, "skills"), GlobalSkillsDir: filepath.Join(home, "skills"), BuiltinToolsDir: filepath.Join(assets, "tools/system"), UserToolsDir: filepath.Join(home, "tools"), RuntimeToolDirs: []string{filepath.Join(assets, "tools/system")}, RuntimeSkillDirs: []string{filepath.Join(assets, "skills/system")}}, manager)
	a = NewAPI(nil, manager, nil, service, filepath.Join(assets, "skills/system"), nil)
	for scheme, original := range profiles {
		t.Run(scheme, func(t *testing.T) {
			env, err := a.PlanEnvironment(context.Background(), scheme)
			if err != nil {
				t.Fatal(err)
			}
			env.MainProvider = "fixture"
			env.VisionProviders = []string{"fixture"}
			caps := map[string]bool{"路线知识抽取": true}
			for _, p := range env.Packages {
				caps[p.ID] = true
				caps[p.DisplayName] = true
				if i := strings.Index(p.DisplayName, "（"); i > 0 {
					caps[p.DisplayName[:i]] = true
				}
			}
			for _, id := range []string{"marching-route", "statistical-choropleth", "traffic-route", "data-to-map"} {
				t.Run(id, func(t *testing.T) {
					guide, err := guides.Resolve(context.Background(), id, library)
					if err != nil {
						t.Fatal(err)
					}
					if len(guide.Doc.Diagnostics) > 0 {
						t.Fatal(guide.Doc.Diagnostics)
					}
					for _, source := range []string{"user", "acquire"} {
						t.Run(source, func(t *testing.T) {
							p := &plan.Plan{APIVersion: plan.APIVersion, ID: "cp-" + id, Version: 1, Goal: guide.Doc.Meta.Name, Guide: &plan.GuideRef{ID: id, Version: guide.Doc.Meta.Version}}
							// Model-authored steps are independent of guide headings/tables.
							for i, capability := range []string{"data-loading", "print-layout"} {
								step := plan.Step{ID: fmt.Sprintf("s%d", i), Title: "执行制图任务", Capabilities: []string{capability}, Instructions: guide.Doc.Body, Outputs: []flow.OutputDeclaration{{Label: fmt.Sprintf("result%d", i), Type: "text"}}}
								if len(p.Steps) > 0 {
									step.DependsOn = []string{p.Steps[len(p.Steps)-1].ID}
								}
								p.Steps = append(p.Steps, step)
							}
							p.Data = []plan.Data{{ID: "d1", Name: "matrix fixture", From: source, Path: t.TempDir(), FindID: "fixture-find", Choice: "auto"}}
							p.Steps[0].Uses = []string{"d1"}
							validation := plan.Environment{ProjectRoot: root, Capabilities: caps, ValidateSource: func(id string, _ any) error {
								if id != "fixture-find" {
									return fmt.Errorf("bad find")
								}
								return nil
							}}
							if err = plan.RequireValid(p, validation); err != nil {
								t.Fatal(err)
							}
							executable, inputs, err := planexec.Build(p, planexec.BuildOptions{Environment: env, Guide: guide.Doc})
							if err != nil {
								t.Fatal(err)
							}
							c, err := flowrun.NewController(flowrun.Options{Runner: matrixRuntime{}, Store: flowrun.FileRunStore{}, DataGet: func(context.Context, *flowrun.RunState, plan.Data) (flowrun.DataResult, error) {
								return flowrun.DataResult{Value: "fixture data"}, nil
							}})
							if err != nil {
								t.Fatal(err)
							}
							state, err := c.StartFromPlan(context.Background(), flowrun.StartRequest{SessionID: "matrix", SessionDir: t.TempDir(), CartoPlan: p, Guide: guide.Doc, Inputs: inputs}, executable)
							if err != nil || state.Status != "completed" {
								t.Fatalf("status=%+v error=%v", state, err)
							}
							probe := &flow.StepPlan{ID: "knowledge", Title: "路线知识"}
							if err = flow.AssignPlanRole(executable, probe, []string{"路线知识抽取"}, "", env); err != nil || probe.Executor != "route_knowledge" {
								t.Fatal(probe, err)
							}
						})
					}
				})
			}
			saved, err := os.ReadFile(filepath.Join(home, "config/agent_configs", scheme, "agent.yaml"))
			if err != nil || string(saved) != original {
				t.Fatal("scheme was modified", err)
			}
		})
	}
}

func TestDocumentedMarchingPlanUsesRealGuideAndCatalog(t *testing.T) {
	a, _, root, _ := realFlowAPI(t)
	env, err := a.PlanEnvironment(context.Background(), "compile-scheme")
	if err != nil {
		t.Fatal(err)
	}
	library, err := a.resourceStore()
	if err != nil {
		t.Fatal(err)
	}
	guides, err := flow.NewDefaultGuideStore(library)
	if err != nil {
		t.Fatal(err)
	}
	guide, err := guides.Resolve(context.Background(), "marching-route", library)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "docs/refactor-v5/07-行军路线图指南示例.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Split(strings.Split(string(raw), "```json\n")[1], "\n```")[0]
	p, err := plan.Decode([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	caps := map[string]bool{"路线知识抽取": true}
	for _, pkg := range env.Packages {
		caps[pkg.ID] = true
		caps[pkg.DisplayName] = true
		if at := strings.Index(pkg.DisplayName, "（"); at > 0 {
			caps[pkg.DisplayName[:at]] = true
		}
	}
	validation := plan.Environment{Capabilities: caps, ValidateSource: func(id string, c any) error {
		if id != "df-7c21" || c != "auto" {
			return fmt.Errorf("fixture find mismatch")
		}
		return nil
	}}
	if ds := plan.Validate(p, validation); flow.HasErrors(ds) {
		t.Fatal(ds)
	}
	executable, _, err := planexec.Build(p, planexec.BuildOptions{Environment: env, Guide: guide.Doc, Scope: guide.Scope, OriginalSource: "验证史料"})
	if err != nil {
		t.Fatal(err)
	}
	if len(executable.Steps) != 7 || executable.Steps[0].Executor != "route_knowledge" || executable.Steps[1].Kind != "acquire" {
		t.Fatal(executable.Steps)
	}
}

func TestPlanRejectsFolderWhereVerifiedGeometryIsRequired(t *testing.T) {
	p := &plan.Plan{APIVersion: plan.APIVersion, ID: "cp-incomplete", Version: 1, Goal: "load", Data: []plan.Data{{ID: "base", Name: "河流", From: "user", Path: t.TempDir(), Geometry: "line"}}, Steps: []plan.Step{{ID: "load", Title: "加载", Capabilities: []string{"data-loading"}, Uses: []string{"base"}, Instructions: "load", Outputs: []flow.OutputDeclaration{{Label: "layers", Type: "layers"}}}}}
	env := plan.Environment{Capabilities: map[string]bool{"data-loading": true}, ValidateData: func(d plan.Data) error {
		_, err := dataservice.ValidateLocalData(context.Background(), d.Path, d.Geometry, nil)
		return err
	}}
	if !flow.HasErrors(plan.Validate(p, env)) {
		t.Fatal("accepted directory as a verified line dataset")
	}
}
