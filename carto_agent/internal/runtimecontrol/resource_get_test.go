package runtimecontrol

import (
	"context"
	"fmt"
	"github.com/autocarto/carto_agent/internal/skill"
	tooldef "github.com/autocarto/carto_agent/internal/tool"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
)

func TestResourceGetScopeAndPagination(t *testing.T) {
	library := resource.NewLibrary(t.TempDir(), t.TempDir(), bindings.New(filepath.Join(t.TempDir(), "bindings.yaml")))
	linked := &resource.ResolvedResource{ID: "data_list.flow", Label: "本步骤底图", Name: "列表文件", Type: "data_list", Version: "1.0.0"}
	for i := 0; i < 250; i++ {
		linked.Entries = append(linked.Entries, resource.ResourceEntry{Name: fmt.Sprint(i), Path: fmt.Sprintf("/data/%d.geojson", i), Kind: "vector", Exists: true})
	}
	ctx := resource.WithScope(resource.WithResolver(context.Background(), library), resource.Scope{FlowID: "flow", Linked: []*resource.ResolvedResource{linked}})
	result, err := Invoke(ctx, "resource_get", map[string]any{"name": "本步骤底图"})
	if err != nil {
		t.Fatal(err)
	}
	h := result.(map[string]any)
	if h["total_entries"] != 250 || h["truncated"] != true || len(h["entries"].([]map[string]any)) != 200 {
		t.Fatalf("wrong page: %#v", h)
	}
	result, err = Invoke(ctx, "resource_get", map[string]any{"name": "data_list.flow", "offset": 200., "limit": 100.})
	if err != nil {
		t.Fatal(err)
	}
	h = result.(map[string]any)
	entries := h["entries"].([]map[string]any)
	if len(entries) != 50 || entries[0]["name"] != "200" || len(linked.Entries) != 250 {
		t.Fatalf("page changed snapshot: %#v", h)
	}
	for _, args := range []map[string]any{{"name": "本步骤底图", "offset": -1}, {"name": "本步骤底图", "limit": 1001}, {"name": "本步骤底图", "offset": 1.2}, {"name": "本步骤底图", "type": "made_up"}, {"name": "不存在"}, {}} {
		if _, err = Invoke(ctx, "resource_get", args); err == nil {
			t.Fatalf("accepted invalid args: %#v", args)
		}
	}
	if _, err = Invoke(context.Background(), "resource_get", map[string]any{"name": "本步骤底图"}); err == nil {
		t.Fatal("missing resolver accepted")
	}
	if !Handles("resource_get") {
		t.Fatal("not registered")
	}
}

func TestResourceGetRealYAMLThroughSkillEngine(t *testing.T) {
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	tools := tooldef.NewRegistry()
	if err = tools.LoadDirectories(filepath.Join(repo, "autocarto/tools/system")); err != nil {
		t.Fatal(err)
	}
	skills := skill.NewRegistry(tools)
	if err = skills.LoadDirectories(filepath.Join(repo, "autocarto/skills/system")); err != nil {
		t.Fatal(err)
	}
	if _, ok := skills.Get("resource_get"); !ok {
		t.Fatal("system foundation package lacks resource_get")
	}
	library := resource.NewLibrary(t.TempDir(), t.TempDir(), bindings.New(filepath.Join(t.TempDir(), "bindings.yaml")))
	ctx := resource.WithResolver(context.Background(), library)
	ctx = resource.WithScope(ctx, resource.Scope{Linked: []*resource.ResolvedResource{{ID: "doc.flow", Label: "说明", Name: "流程说明", Type: "doc", Entry: "/flow/resources/说明.md"}}})
	engine := skill.NewEngine(skills, skill.NewInternalBackend(Invoke))
	result, err := engine.Execute(ctx, "resource_get", map[string]any{"name": "说明"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output.(map[string]any)["entry"] != "/flow/resources/说明.md" {
		t.Fatal("real YAML mapping lost scope")
	}
}
