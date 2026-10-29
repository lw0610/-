package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/defaults"
	"github.com/autocarto/carto_agent/internal/config"
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
		path := filepath.Join(r.SessionDir, r.Step.ID+"-"+o.Label+".csv")
		if err := os.WriteFile(path, []byte("name,value\nfixture,1\n"), 0600); err != nil {
			return flowrun.StepResult{}, err
		}
		outputs[o.Label] = path
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
			caps := map[string]bool{"route_knowledge": true}
			for _, p := range env.Packages {
				caps[p.ID] = true
				caps[p.DisplayName] = true
				if i := strings.Index(p.DisplayName, "（"); i > 0 {
					caps[p.DisplayName[:i]] = true
				}
			}
			probe := &flow.StepPlan{ID: "knowledge", Title: "路线知识"}
			knowledgeRole := ""
			if scheme == "default" {
				knowledgeRole = "knowledge_engineer"
			}
			err = flow.AssignPlanRole(&flow.FlowPlan{}, probe, []string{"route_knowledge"}, knowledgeRole, env)
			if knowledgeRole == "" {
				if err == nil {
					t.Fatalf("scheme without knowledge role received an invented executor: %+v", probe)
				}
			} else if err != nil || probe.Role != knowledgeRole || probe.RoleSource != "scheme" || probe.Executor != "route_knowledge" {
				t.Fatal(probe, err)
			}
			for _, id := range []string{"marching-route", "statistical-choropleth"} {
				t.Run(id, func(t *testing.T) {
					guide, err := guides.Resolve(context.Background(), id, library)
					if err != nil {
						t.Fatal(err)
					}
					if flow.HasErrors(guide.Doc.Diagnostics) {
						t.Fatal(guide.Doc.Diagnostics)
					}
					for _, source := range []string{"user", "retrieve"} {
						t.Run(source, func(t *testing.T) {
							p := &plan.Plan{APIVersion: plan.APIVersion, ID: "cp-" + id, Version: 1, Goal: guide.Doc.Meta.Name, Guide: &plan.GuideRef{ID: id, Version: guide.Doc.Meta.Version}}
							// Model-authored steps are independent of guide headings/tables.
							for i, role := range []string{"data_engineer", "cartographer"} {
								step := plan.Step{ID: fmt.Sprintf("s%d", i), Title: "执行制图任务", RoleHint: role, Instructions: guide.Doc.Body, Produces: []plan.Product{{ID: fmt.Sprintf("result%d", i), Kind: "table", Fields: []string{"name", "value"}}}}
								if len(p.Steps) > 0 {
									step.Inputs = []string{fmt.Sprintf("result%d", i-1)}
								}
								p.Steps = append(p.Steps, step)
							}
							input := filepath.Join(t.TempDir(), "input.csv")
							if err := os.WriteFile(input, []byte("name,value\nfixture,1\n"), 0600); err != nil {
								t.Fatal(err)
							}
							p.Data = []plan.Data{{ID: "d1", Name: "matrix fixture", Source: source, Kind: "table"}}
							bindings := map[string]any{}
							if source == "user" {
								p.Data[0].Ref = input
								bindings["d1"] = input
							} else {
								p.Data[0].Need = &plan.Need{Category: "stats.table"}
								p.Data[0].Extent = &plan.Extent{BBox: []float64{110, 25, 116, 30}}
							}
							p.Steps[0].Inputs = []string{"d1"}
							p.Deliverables = []flow.OutputDeclaration{{Label: "result1"}}
							plan.NormalizeV2(p, caps)
							validation := plan.Environment{ProjectRoot: root, Capabilities: caps}
							if err = plan.RequireValid(p, validation); err != nil {
								t.Fatal(err)
							}
							executable, inputs, err := planexec.Build(p, planexec.BuildOptions{Environment: env, Guide: guide.Doc, Bindings: bindings})
							if scheme == "minimal" {
								if err == nil {
									t.Fatalf("main-only scheme received execution roles: %+v", executable)
								}
								return
							}
							if err != nil {
								t.Fatal(err)
							}
							c, err := flowrun.NewController(flowrun.Options{Runner: matrixRuntime{}, Store: flowrun.FileRunStore{}, DataGet: func(context.Context, *flowrun.RunState, plan.Data) (flowrun.DataResult, error) {
								return flowrun.DataResult{Value: input}, nil
							}})
							if err != nil {
								t.Fatal(err)
							}
							state, err := c.StartFromPlan(context.Background(), flowrun.StartRequest{SessionID: "matrix", SessionDir: t.TempDir(), CartoPlan: p, Guide: guide.Doc, Inputs: inputs}, executable)
							if err != nil || state == nil || state.Status != "completed" {
								t.Fatalf("status=%+v error=%v", state, err)
							}
							if len(executable.Steps) != 2 || len(p.Data) != 1 || len(executable.Steps[1].Refs.Outputs) != 1 || executable.Steps[1].Refs.Outputs[0].Label != "result0" {
								t.Fatalf("v2 inserted acquisition/duplicate output data: %+v", executable.Steps)
							}
							for i, step := range executable.Steps {
								if step.Role != p.Steps[i].RoleHint || step.RoleSource != "scheme" {
									t.Fatalf("configured child was replaced: %+v", step)
								}
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
	raw, err := os.ReadFile(filepath.Join(root, "docs/refactor-v7/03-计划与执行规范.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Split(strings.Split(string(raw), "```json\n")[1], "\n```")[0]
	p, err := plan.Decode([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	// Bind the older example to this fixture's real scheme roles and add its abbreviated export.
	p.Steps[0].RoleHint = "reader-renamed"
	p.Steps[1].RoleHint = "map-renamed"
	p.Steps = append(p.Steps, plan.Step{ID: "export", Title: "导出 PDF", RoleHint: "map-renamed", Inputs: []string{"basemap_layers"}, Produces: []plan.Product{{ID: "export.pdf", Kind: "file", Format: "pdf"}}})
	caps := map[string]bool{"route_knowledge": true}
	for _, pkg := range env.Packages {
		caps[pkg.ID] = true
	}
	plan.NormalizeV2(p, caps)
	if err := plan.RequireValid(p, plan.Environment{Capabilities: caps}); err != nil {
		t.Fatal(err)
	}
	executable, _, err := planexec.Build(p, planexec.BuildOptions{Environment: env, Guide: guide.Doc, Scope: guide.Scope, OriginalSource: "验证史料"})
	if err != nil {
		t.Fatal(err)
	}
	if len(executable.Steps) != 3 || executable.Steps[0].Executor != "route_knowledge" || len(executable.Steps[2].Refs.Outputs) != 1 {
		t.Fatalf("unexpected v2 execution: %+v", executable.Steps)
	}
	for _, step := range executable.Steps {
		if step.Kind == "acquire" {
			t.Fatal("data preparation became a planned step")
		}
		if step.RoleSource != "scheme" {
			t.Fatalf("documented plan created a temporary role: %+v", step)
		}
	}
	if _, exists := p.DataByID("route"); exists {
		t.Fatal("step output was duplicated in data")
	}
}

func TestV2PlanRejectsForwardProductReferenceBeforeBuilding(t *testing.T) {
	p := &plan.Plan{APIVersion: plan.APIVersion, Goal: "检查依赖", Steps: []plan.Step{
		{ID: "consume", Title: "提前使用", Inputs: []string{"later"}},
		{ID: "produce", Title: "稍后产生", Produces: []plan.Product{{ID: "later", Kind: "file"}}},
	}}
	plan.NormalizeV2(p, nil)
	diagnostics := plan.ValidateV2(p)
	found := false
	for _, diagnostic := range diagnostics {
		found = found || diagnostic.Code == "PLAN_REF_INVALID"
	}
	if !found {
		t.Fatalf("accepted forward reference: %+v", diagnostics)
	}
}
