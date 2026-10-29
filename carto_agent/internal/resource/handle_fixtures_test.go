package resource

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/resource/bindings"
)

func TestResourceHandleFixtures(t *testing.T) {
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOCARTO_PROJECT_ROOT", repo)
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	library := NewLibrary(filepath.Join(repo, "autocarto/resources/system"), t.TempDir(), bindings.New(filepath.Join(t.TempDir(), "bindings.yaml")))
	type sample struct{ name, typ, id, location, flowDir string }
	samples := []sample{
		{name: "gb-basemap", typ: "style_set", location: "resources/styles/国标底图样式"},
		{name: "mono", typ: "route_style", location: "styles/路线样式-单色.csv", flowDir: "autocarto/tools/system/route-cartography/assets/resources"},
		{name: "four-corner", typ: "layout", id: "layout.four-corner"},
		{name: "statistical", typ: "layout", id: "layout.statistical"},
		{name: "red-blue", typ: "route_style", location: "styles/路线样式-红蓝对比.csv", flowDir: "autocarto/tools/system/route-cartography/assets/resources"},
		{name: "military", typ: "symbols", location: "symbols/军标", flowDir: "autocarto/tools/system/route-cartography/assets/resources"},
		{name: "route-arrow", typ: "symbols", location: "symbols/图例", flowDir: "autocarto/tools/system/route-cartography/assets/resources"},
		{name: "survey-route", typ: "route_style", location: "resources/styles/考察单色路线.csv", flowDir: "docs/refactor/acceptance-flows/survey-route"},
		{name: "data-list", typ: "data_list", location: "resources/data/数据清单.csv"},
		{name: "data-optional", typ: "data_list", location: "resources/data/可选数据.csv"},
		{name: "data-folder", typ: "data_folder", location: "resources/data/图层"},
		{name: "style", typ: "style", location: "resources/styles/面样式.qml"},
		{name: "style-set-modes", typ: "style_set", location: "resources/styles/标注样式"},
		{name: "symbols", typ: "symbols", location: "resources/symbols/通用符号"},
		{name: "layout-template", typ: "layout:qpt", location: "resources/layouts/模板.qpt"},
		{name: "doc", typ: "doc", location: "resources/docs/说明.md"},
		{name: "contract-data-xlsx", typ: "data_list", location: "resources/data/契约清单.xlsx"},
		{name: "contract-style-xlsx", typ: "style_set", location: "resources/styles/契约样式"},
		{name: "contract-symbols-xlsx", typ: "symbols", location: "resources/symbols/契约符号"},
		{name: "contract-route-xlsx", typ: "route_style", location: "resources/styles/契约路线.xlsx"},
	}
	for _, sample := range samples {
		t.Run(sample.name, func(t *testing.T) {
			flowDir := sample.flowDir
			if flowDir == "" {
				flowDir = "autocarto/tests/fixtures/resource_samples"
			}
			r := library.Resolve(context.Background(), Ref{Type: sample.typ, ID: sample.id, Location: sample.location}, Scope{FlowID: "fixture-flow", FlowDir: filepath.Join(repo, flowDir)})
			handle := r.Handle()
			if handle == nil {
				t.Fatalf("invalid fixture: %+v", r.Problems)
			}
			raw, err := json.MarshalIndent(handle, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			normalized := strings.ReplaceAll(string(raw), filepath.ToSlash(repo), "${REPO}") + "\n"
			path := filepath.Join(repo, "autocarto/tests/fixtures/resource_handles", sample.name+".json")
			if os.Getenv("AUTOCARTO_UPDATE_FIXTURES") == "1" {
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(normalized), 0644); err != nil {
					t.Fatal(err)
				}
				return
			}
			expected, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(expected) != normalized {
				t.Fatalf("fixture %s differs from Go resolver; regenerate with AUTOCARTO_UPDATE_FIXTURES=1", sample.name)
			}
		})
	}
}
