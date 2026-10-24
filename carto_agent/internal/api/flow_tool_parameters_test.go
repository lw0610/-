package api

import (
	"context"
	"reflect"
	"testing"

	"github.com/autocarto/carto_agent/internal/tool"
)

func TestFlowEnvironmentIncludesAuthorityParameterNamesAndIsolatesCache(t *testing.T) {
	a, _, _, _ := realFlowAPI(t)
	environment, err := a.loadFlowEnvironment(context.Background(), "compile-scheme")
	if err != nil {
		t.Fatal(err)
	}
	found := -1
	for i, capability := range environment.Capabilities {
		if capability.Name == "layout_compose" {
			found = i
			break
		}
	}
	if found < 0 {
		t.Fatal("missing authority layout_compose capability")
	}
	parameters := environment.Capabilities[found].Parameters
	hasLayout := false
	for _, name := range parameters {
		if name == "layout" {
			hasLayout = true
		}
	}
	if !hasLayout {
		t.Fatalf("resource parameter from authority missing: %v", parameters)
	}
	saved := append([]string{}, parameters...)
	parameters[0] = "mutated_parameter"
	next, err := a.loadFlowEnvironment(context.Background(), "compile-scheme")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next.Capabilities[found].Parameters, saved) {
		t.Fatal("cached parameter vocabulary mutated")
	}
	if got := flowParameterNames([]tool.ToolParameter{{Name: " dpi "}, {Name: ""}, {Name: "layout"}, {Name: "dpi"}}); !reflect.DeepEqual(got, []string{"dpi", "layout"}) {
		t.Fatalf("unexpected names: %v", got)
	}
}
