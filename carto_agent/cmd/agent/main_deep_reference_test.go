package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/runtimecontrol"
	"github.com/autocarto/carto_agent/internal/skill"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestBuildRuntimeToolsReferenceRootsStayWithConstructedRuntime(t *testing.T) {
	base := t.TempDir()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", filepath.Join(root, "autocarto"))
	t.Setenv("AUTOCARTO_HOME", t.TempDir())
	for _, name := range []string{"first", "second"} {
		root := config.ResolveExclusiveSkillsDir(base, name)
		writeRuntimeFile(t, filepath.Join(root, "same-package", "SKILL.md"), "---\nname: same-package\ndescription: 方法\nkind: category\n---\n方法\n")
		writeRuntimeFile(t, filepath.Join(root, "same-package", "references", "method.md"), "method-for-"+name)
	}
	build := func(name string) []einotool.BaseTool {
		t.Helper()
		tools, err := buildRuntimeTools(context.Background(), &config.Config{ConfigBaseDir: base}, skill.BuildOptions{
			ComputeClient:   newFakeCLIComputeClient(t, t.TempDir(), `{"tools":[]}`, `{"success":true}`),
			InternalInvoker: runtimecontrol.Invoke,
		}, "", "", "", name)
		if err != nil {
			t.Fatal(err)
		}
		return tools
	}
	first, second := build("first"), build("second")
	for name, tools := range map[string][]einotool.BaseTool{"first": first, "second": second} {
		found := false
		for _, candidate := range tools {
			info, err := candidate.Info(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if info.Name != runtimecontrol.ToolReferenceRead {
				continue
			}
			found = true
			result, err := candidate.(einotool.EnhancedInvokableTool).InvokableRun(context.Background(), &schema.ToolArgument{Text: `{"path":"same-package/references/method.md"}`})
			text := ""
			if result != nil {
				for _, part := range result.Parts {
					text += part.Text
				}
			}
			if err != nil || !strings.Contains(text, "method-for-"+name) {
				t.Fatalf("runtime %s reference mismatch: %s %v", name, text, err)
			}
		}
		if !found {
			t.Fatal("reference_read tool missing")
		}
	}
}
