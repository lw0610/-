package flow

import (
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/resource"
	"testing"
)

func TestV4RequirementsBindListAndInsertStableStep(t *testing.T) {
	catalog, err := datasource.Load("../../../autocarto/tools/system/data-acquisition/assets", "")
	if err != nil {
		t.Fatal(err)
	}
	doc := &FlowDoc{Requirements: []RequirementDoc{{Name: "底图", List: "「中国底图」", Extent: "「路线数据集」外扩 20%", Scale: "自动", Params: map[string]string{}}}}
	plan := &FlowPlan{Inputs: []InputPlan{{Key: "底图文件夹", Label: "底图文件夹", Type: "folder"}}, Steps: []StepPlan{{ID: "s1", Index: 1, ExpectedOutputs: []OutputDeclaration{{Label: "路线数据集"}}}, {ID: "s2", Index: 2, Refs: PlanRefs{Resources: []string{"data-list"}}}}}
	rows := []resource.ResourceEntry{}
	for _, row := range []struct{ name, category, fields string }{{"省域", "省域", "NAME"}, {"县级城市驻地", "县级驻地", "name"}, {"铁路", "铁路", ""}, {"公路", "公路", ""}, {"国道", "国道", ""}, {"地区界线", "地区界线", ""}, {"河流线", "河流线", "NAME"}, {"河流面", "河流面", ""}} {
		rows = append(rows, resource.ResourceEntry{Name: row.name, Kind: "vector", Path: row.name + ".shp", Extra: map[string]string{"path": row.name + ".shp"}, Fields: map[string]any{"category": row.category, "fields": row.fields, "required": true}})
	}
	resources := []*resource.ResolvedResource{{Key: "data-list", Label: "中国底图", Type: "data_list", BaseDirFromInput: "底图文件夹", Entries: rows}}
	buildRequirements(doc, plan, resources, Environment{DataSources: catalog, Integrations: map[string]bool{"osm": true, "web": true}, MaxDownloadMB: 2048})
	if HasErrors(plan.Diagnostics) {
		t.Fatalf("%+v", plan.Diagnostics)
	}
	if plan.Acquisition == nil || len(plan.Acquisition.Items) != 8 {
		t.Fatalf("%+v", plan.Acquisition)
	}
	if len(plan.Steps) != 3 || plan.Steps[0].ID != "s1" || plan.Steps[1].ID != "acquire-1" || plan.Steps[2].ID != "s2" {
		t.Fatalf("%+v", plan.Steps)
	}
	for _, item := range plan.Acquisition.Items {
		if item.Category == "" || !item.Available || item.Extent == nil || !item.Extent.Pending || item.Extent.DependencyStep != "s1" {
			t.Fatalf("%+v", item)
		}
	}
	if plan.Acquisition.Items[0].Fields["NAME"] != "name" {
		t.Fatal("case mapping lost")
	}
	if !plan.Inputs[0].Acquirable || len(plan.Inputs[0].Modes) != 3 {
		t.Fatal(plan.Inputs[0])
	}
}
func TestV4RequirementsRejectFolderWithoutList(t *testing.T) {
	catalog, err := datasource.Load("../../../autocarto/tools/system/data-acquisition/assets", "")
	if err != nil {
		t.Fatal(err)
	}
	doc := &FlowDoc{Requirements: []RequirementDoc{{Name: "数据", Category: "铁路", Extent: "全国"}}}
	plan := &FlowPlan{Inputs: []InputPlan{{Key: "数据", Label: "数据", Type: "folder"}}}
	buildRequirements(doc, plan, nil, Environment{DataSources: catalog})
	if len(plan.Diagnostics) != 1 || plan.Diagnostics[0].Code != "FLOW_REQ_FOLDER_NEEDS_LIST" {
		t.Fatalf("%+v", plan.Diagnostics)
	}
}
