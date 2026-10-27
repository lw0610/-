package integrations

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/runtimecontrol"
	"github.com/autocarto/carto_agent/internal/skill"
	toolpkg "github.com/autocarto/carto_agent/internal/tool"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"
)

// The statistics method is an attachment of the already assigned browser
// package. Exercise the shipped profile -> real Eino skill -> actual reference
// tool chain; a file being present on disk alone does not make it discoverable.
func TestChinaStatisticsMethodReachableThroughAssignedBrowserPackage(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../.."))
	assets := filepath.Join(root, "autocarto")
	skillsRoot := filepath.Join(assets, "skills", "system")
	t.Setenv("AUTOCARTO_ASSETS_ROOT", assets)
	t.Setenv("AUTOCARTO_HOME", t.TempDir())

	raw, err := os.ReadFile(filepath.Join(root, "carto_agent", "defaults", "agent_configs", "default", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var profile config.AgentConfigFile
	if err := yaml.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}
	runtimeConfig := config.ConvertToRuntimeSkillConfig(&profile, skillsRoot, "", t.TempDir(), "default")
	readable := runtimeConfig.GetAgentMiddlewareSkills("data_engineer")
	if !slices.Contains(readable, "browser-data-acquisition") {
		t.Fatal("default data role cannot discover its browser package methods")
	}
	if slices.Contains(readable, "china-statistics-data") {
		t.Fatal("statistics reference must not require an additional role assignment")
	}
	for _, name := range []string{"reference_read", "web_search", "browser_list_tools", "browser_call_tool"} {
		if !slices.Contains(runtimeConfig.GetAgentAllowedTools("data_engineer"), name) {
			t.Fatalf("default data role cannot complete statistics acquisition: missing %s", name)
		}
	}

	ctx := runtimecontrol.WithReferencePackageDirs(context.Background(), []string{skillsRoot})
	mw, err := subagents.LoadSkillMiddleware(ctx, "data_engineer", []string{skillsRoot}, nil, runtimeConfig)
	if err != nil || mw == nil {
		t.Fatalf("actual skill middleware unavailable: %v", err)
	}
	var skillBody string
	for _, candidate := range mw.AdditionalTools {
		info, err := candidate.Info(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if info.Name != "skill" {
			continue
		}
		if !strings.Contains(info.Desc, "china-statistics-data") {
			t.Fatal("statistics use case is absent from actual model-visible skill catalog")
		}
		invokable, ok := candidate.(tool.InvokableTool)
		if !ok {
			t.Fatal("actual skill reader is not invokable")
		}
		skillBody, err = invokable.InvokableRun(ctx, `{"skill":"browser-data-acquisition"}`)
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	// Resolve the path returned to the model, rather than independently using
	// a guessed filesystem path that could hide a broken document link.
	reference := regexp.MustCompile(`browser-data-acquisition/references/[^"\x60\s]+\.md`).FindString(skillBody)
	if reference == "" {
		t.Fatal("actual skill response does not point to its full statistics method")
	}

	registry := toolpkg.NewRegistry()
	if err := registry.LoadDirectories(filepath.Join(assets, "tools", "system")); err != nil {
		t.Fatal(err)
	}
	skills := skill.NewRegistry(registry)
	if err := skills.LoadDirectories(skillsRoot); err != nil {
		t.Fatal(err)
	}
	definition, ok := skills.GetTool("reference_read")
	if !ok {
		t.Fatal("shipped reference reader contract missing")
	}
	engine := skill.NewEngine(skills, skill.NewInternalBackend(runtimecontrol.Invoke))
	args, _ := json.Marshal(map[string]string{"path": reference})
	reader, ok := skill.NewSkillAdapter(definition, engine).(tool.EnhancedInvokableTool)
	if !ok {
		t.Fatal("actual reference adapter is not invokable")
	}
	result, err := reader.InvokableRun(ctx, &schema.ToolArgument{Text: string(args)})
	if err != nil {
		t.Fatalf("actual reference adapter could not read linked method: %v", err)
	}
	var payload map[string]any
	if result == nil || len(result.Parts) != 1 || result.Parts[0].Type != schema.ToolPartTypeText {
		t.Fatalf("reference tool did not return a readable text result: %#v", result)
	}
	if err := json.Unmarshal([]byte(result.Parts[0].Text), &payload); err != nil {
		t.Fatalf("reference tool result is not JSON: %v", err)
	}
	expected, err := os.ReadFile(filepath.Join(skillsRoot, filepath.FromSlash(reference)))
	if err != nil {
		t.Fatal(err)
	}
	if payload["text"] != string(expected) || payload["truncated"] != false {
		t.Fatalf("reference did not return the complete attached method: %#v", payload)
	}
	if payload["path"] != filepath.Join(skillsRoot, filepath.FromSlash(reference)) {
		t.Fatalf("reference resolved a different package: %v", payload["path"])
	}
}
