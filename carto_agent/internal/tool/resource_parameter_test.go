package tool

import (
	"context"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"path/filepath"
	"testing"
)

func TestResourceParameterValidation(t *testing.T) {
	for _, tc := range []struct {
		kind, typ, id string
		valid         bool
	}{
		{"resource", "style_set", "style_set.gb-basemap", true}, {"resource", "layout:qpt", "", true}, {"resource", "layout:layout_yaml", "layout.four-corner", true},
		{"resource", "", "", false}, {"resource", "unknown", "", false}, {"resource", "layout:wrong", "", false}, {"resource", "style", "layout.x", false}, {"string", "style", "", false},
	} {
		if err := ValidateResourceParameter(tc.kind, tc.typ, tc.id); (err == nil) != tc.valid {
			t.Errorf("%+v: %v", tc, err)
		}
	}
}
func TestToolDefaultResourcesExist(t *testing.T) {
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOCARTO_PROJECT_ROOT", repo)
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	library := resource.NewLibrary(filepath.Join(repo, "autocarto/resources/system"), t.TempDir(), bindings.New(filepath.Join(t.TempDir(), "bindings.yaml")))
	defs, err := LoadDefinitionsFromDirectories(filepath.Join(repo, "autocarto/tools/system"))
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range defs {
		for _, param := range def.Spec.Parameters {
			if param.DefaultResource != "" {
				r := library.Resolve(context.Background(), resource.Ref{Type: param.ResourceType, ID: param.DefaultResource}, resource.Scope{})
				if r.Handle() == nil {
					t.Errorf("%s.%s: %+v", def.Metadata.Name, param.Name, r.Problems)
				}
			}
		}
	}
}
