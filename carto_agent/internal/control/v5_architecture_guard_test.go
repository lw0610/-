package control

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// This gate checks active source and definitions, not historical fixtures or
// compatibility decoders. The one token exception is registered in the ledger.
func TestV5RetiredExecutionMechanismsAbsent(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"carto_agent/internal/flow/compile.go", "carto_agent/internal/flow/base_plan.go",
		"carto_agent/internal/flow/compile_llm.go", "carto_agent/internal/flow/enrich_jobs.go",
		"carto_agent/internal/flow/enrich_cache.go", "carto_agent/internal/flow/bm25.go",
		"carto_agent/internal/flow/input_choice.go", "carto_agent/internal/flow/reachability.go",
		"carto_agent/internal/flowrun/conditions.go", "carto_agent/internal/config/flow_policy.go",
		"carto_agent/internal/runtimecontrol/flow_tools.go", "carto_agent/internal/api/handlers_flow_compile.go",
		"carto_agent/internal/api/handlers_flow_inputs.go", "carto_agent/internal/api/handlers_flow_roles.go",
		"autocarto-web/src/features/flow-run/FlowComposer.tsx", "autocarto-web/src/features/flow-run/useFlowComposer.ts",
		"autocarto-web/src/features/flows/RoleMappingPanel.tsx",
	} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Errorf("retired execution file must be absent: %s (%v)", path, err)
		}
	}
	forbidden := regexp.MustCompile(`\b(flow_start|flow_preview|role_fill|EnsureRunnable|EnsureCompiled|NewCompiler)\b|\$(acquire|fill)\b`)
	for _, dir := range []string{"carto_agent/internal", "carto_agent/defaults/agent_configs", "autocarto-web/src", "autocarto/skills/system", "autocarto/tools/system"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" || entry.Name() == "__pycache__" {
					return filepath.SkipDir
				}
				return nil
			}
			name := entry.Name()
			if strings.HasSuffix(name, "_test.go") || strings.Contains(name, ".test.") || strings.HasPrefix(name, "test_") {
				return nil
			}
			switch filepath.Ext(name) {
			case ".go", ".ts", ".tsx", ".yaml", ".md":
			default:
				return nil
			}
			relative, _ := filepath.Rel(root, path)
			if filepath.ToSlash(relative) == "carto_agent/internal/flow/legacy/input_choice.go" {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for index, line := range strings.Split(string(raw), "\n") {
				if match := forbidden.FindString(line); match != "" {
					t.Errorf("active source reintroduced %s at %s:%d", match, relative, index+1)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	ledger := readEmbeddedArchitecturePolicyForTest[testEmbeddedRemovalLedger](t, testRemovalLedgerPath)
	found := false
	for _, item := range ledger.CompatLayers {
		if item.ID == "guide_legacy_document_conversion_v5" {
			found = true
		}
	}
	if !found {
		t.Fatal("legacy input decoder must have explicit conversion-only removal criteria")
	}
}

func TestV5LocalRequestImplementationsExcludeModelAndTransportCalls(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{
		"carto_agent/internal/flow/guide*.go", "carto_agent/internal/flow/convert.go",
		"carto_agent/internal/plan/validate.go", "carto_agent/internal/plan/store.go",
		"carto_agent/internal/dataservice/find.go", "carto_agent/internal/api/handlers_guides.go",
		"carto_agent/internal/api/handlers_plans.go", "carto_agent/internal/api/flow_environment.go",
	} {
		paths, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil || len(paths) == 0 {
			t.Fatalf("guard target %s: %v", pattern, err)
		}
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range file.Imports {
				name, _ := strconv.Unquote(imp.Path.Value)
				if strings.Contains(name, "/components/model") || strings.Contains(name, "/ark") || strings.Contains(name, "/openai") || name == "net" {
					t.Errorf("local request path imports model/network transport: %s %s", path, name)
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch selector.Sel.Name {
				case "NewChatModel", "Generate", "Stream", "RoundTrip", "DialContext":
					t.Errorf("local request path starts model/network work: %s %s", path, selector.Sel.Name)
				}
				return true
			})
		}
	}
}
