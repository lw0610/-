package toolcontract

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadFSExportsPublicCatalogAndExecutionManifest(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"system/project/open_project.yaml":       {Data: []byte(contractFixture("open_project", "public", "project", "handler", "handlers.project.open", "", []string{"catalog.read", "tool.invoke"}))},
		"system/project/open_project_debug.yaml": {Data: []byte(contractFixture("open_project_debug", "hidden", "project", "handler", "handlers.project.debug", "", []string{"tool.invoke.internal"}))},
		"system/runtime/internal_probe.yaml":     {Data: []byte(contractFixture("internal_probe", "internal", "runtime", "backend_tool", "", "internal:probe", []string{"runtime.internal"}))},
	}

	contracts, err := LoadFS(fsys, "system")
	if err != nil {
		t.Fatalf("LoadFS() error = %v", err)
	}
	if len(contracts) != 3 {
		t.Fatalf("expected 3 contracts, got %d", len(contracts))
	}

	tools, err := Materialize(contracts)
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if len(tools) != 3 {
		t.Fatalf("expected 3 materialized tools, got %d", len(tools))
	}

	byName := make(map[string]MaterializedTool, len(tools))
	for _, item := range tools {
		byName[item.Name] = item
	}

	openProject := byName["open_project"]
	openProject.Execution.Output = &Output{AutoLoad: false}
	if policy := openProject.ExecutionManifestEntry().AutoLoad; policy == nil || *policy {
		t.Fatal("explicit disabled output loading was lost")
	}
	if openProject.Visibility != VisibilityPublic {
		t.Fatalf("expected open_project visibility=%q, got %q", VisibilityPublic, openProject.Visibility)
	}
	if got := strings.Join(openProject.AuthorizationScopes, ","); got != "catalog.read,tool.invoke" {
		t.Fatalf("unexpected authorization scopes: %s", got)
	}
	if openProject.Execution.Binding != ExecutionBindingHandler {
		t.Fatalf("expected handler binding, got %q", openProject.Execution.Binding)
	}
	if openProject.Execution.Handler != "handlers.project.open" {
		t.Fatalf("unexpected handler: %s", openProject.Execution.Handler)
	}

	publicCatalog := make([]CatalogEntry, 0, len(tools))
	executionManifest := make([]ExecutionManifestEntry, 0, len(tools))
	for _, item := range tools {
		if entry, ok := item.PublicCatalogEntry(RuntimeState{Available: true}); ok {
			publicCatalog = append(publicCatalog, entry)
		}
		executionManifest = append(executionManifest, item.ExecutionManifestEntry())
	}

	if len(publicCatalog) != 1 {
		t.Fatalf("expected 1 public catalog entry, got %d", len(publicCatalog))
	}
	if publicCatalog[0].Name != "open_project" {
		t.Fatalf("unexpected public catalog tool: %s", publicCatalog[0].Name)
	}
	if len(executionManifest) != 3 {
		t.Fatalf("expected 3 execution manifest entries, got %d", len(executionManifest))
	}
	if executionManifest[0].Name == "" {
		t.Fatal("expected execution manifest name to be populated")
	}
	if executionManifest[0].Binding == "" {
		t.Fatal("expected execution manifest binding to be populated")
	}
	if len(executionManifest[0].AuthorizationScopes) == 0 {
		t.Fatal("expected execution manifest authorization scopes")
	}
}

func TestRejectsInvalidVisibility(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"system/project/broken.yaml": {Data: []byte(contractFixture("broken_visibility", "external", "project", "handler", "handlers.project.open", "", nil))},
	}

	_, err := LoadFS(fsys, "system")
	if err == nil {
		t.Fatal("expected invalid visibility error")
	}
	if !strings.Contains(err.Error(), "policy.visibility") {
		t.Fatalf("expected visibility validation error, got %v", err)
	}
}

func TestRejectsDuplicateToolName(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"system/project/first.yaml":  {Data: []byte(contractFixture("duplicate_name", "public", "project", "handler", "handlers.project.first", "", nil))},
		"system/project/second.yaml": {Data: []byte(contractFixture("duplicate_name", "public", "project", "handler", "handlers.project.second", "", nil))},
	}

	_, err := LoadFS(fsys, "system")
	if err == nil {
		t.Fatal("expected duplicate tool name error")
	}
	if !strings.Contains(err.Error(), "duplicate tool name") {
		t.Fatalf("expected duplicate tool name error, got %v", err)
	}
}

func TestCatalogSchemaRevisionStableAcrossOrdering(t *testing.T) {
	t.Parallel()

	baseline := []CatalogEntry{
		{Name: "buffer", Runtime: "cli", Backend: "cli", Domain: "vector", Description: "buffer", InputSchema: ParameterSchema{Parameters: []Parameter{{Name: "INPUT", Type: "layer", Required: true}}}, Available: true},
		{Name: "open_project", Runtime: "qgis", Backend: "qgis", Domain: "project", Description: "open", InputSchema: ParameterSchema{Parameters: []Parameter{{Name: "path", Type: "file", Required: true}}}, Available: true},
	}
	reordered := []CatalogEntry{baseline[1], baseline[0]}

	if got, want := CatalogSchemaRevision(reordered), CatalogSchemaRevision(baseline); got != want {
		t.Fatalf("expected stable schema revision across ordering, got=%q want=%q", got, want)
	}
	if got, want := CatalogAvailabilityRevision(reordered), CatalogAvailabilityRevision(baseline); got != want {
		t.Fatalf("expected stable availability revision across ordering, got=%q want=%q", got, want)
	}
}

func TestCatalogAvailabilityRevisionSeparatesAvailabilityOnlyDrift(t *testing.T) {
	t.Parallel()

	baseline := []CatalogEntry{{
		Name:        "open_project",
		Runtime:     "qgis",
		Backend:     "qgis",
		Domain:      "project",
		Description: "open",
		InputSchema: ParameterSchema{Parameters: []Parameter{{Name: "path", Type: "file", Required: true}}},
		Available:   true,
		InstanceIDs: []string{"inst-a"},
	}}
	candidate := []CatalogEntry{{
		Name:              "open_project",
		Runtime:           "qgis",
		Backend:           "qgis",
		Domain:            "project",
		Description:       "open",
		InputSchema:       ParameterSchema{Parameters: []Parameter{{Name: "path", Type: "file", Required: true}}},
		Available:         false,
		UnavailableReason: "interactive_instance_disconnected",
		InstanceIDs:       []string{"inst-a"},
	}}

	if got, want := CatalogSchemaRevision(candidate), CatalogSchemaRevision(baseline); got != want {
		t.Fatalf("expected availability-only drift to keep schema revision stable, got=%q want=%q", got, want)
	}
	if got, want := CatalogAvailabilityRevision(candidate), CatalogAvailabilityRevision(baseline); got == want {
		t.Fatalf("expected availability-only drift to change availability revision, got=%q want=%q", got, want)
	}
}

func contractFixture(name string, visibility Visibility, domain string, binding ExecutionBindingKind, handler string, backendTool string, scopes []string) string {
	var builder strings.Builder
	builder.WriteString("apiVersion: tool_contract/v1\n")
	builder.WriteString("kind: ToolContract\n")
	builder.WriteString("metadata:\n")
	builder.WriteString("  name: " + name + "\n")
	builder.WriteString("  domain: " + domain + "\n")
	builder.WriteString("  runtime: qgis\n")
	builder.WriteString("  backend: qgis\n")
	builder.WriteString("spec:\n")
	builder.WriteString("  description: Test contract.\n")
	builder.WriteString("  parameters:\n")
	builder.WriteString("    - name: INPUT\n")
	builder.WriteString("      type: layer\n")
	builder.WriteString("policy:\n")
	builder.WriteString("  visibility: " + string(visibility) + "\n")
	if len(scopes) > 0 {
		builder.WriteString("  authorization:\n")
		builder.WriteString("    scopes:\n")
		for _, scope := range scopes {
			builder.WriteString("      - " + scope + "\n")
		}
	}
	builder.WriteString("execution:\n")
	builder.WriteString("  binding: " + string(binding) + "\n")
	if handler != "" {
		builder.WriteString("  handler: " + handler + "\n")
	}
	if backendTool != "" {
		builder.WriteString("  backend_tool: " + backendTool + "\n")
	}
	return builder.String()
}

var _ fs.FS = fstest.MapFS{}
