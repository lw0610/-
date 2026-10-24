package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/gorilla/mux"
)

func TestFlowEnvironmentCacheMetadataAndIsolation(t *testing.T) {
	a, manager, _, home := realFlowAPI(t)
	ctx := context.Background()
	now := time.Now()
	a.flowEnvironments.now = func() time.Time { return now }
	first, err := a.loadFlowEnvironment(ctx, "compile-scheme")
	if err != nil {
		t.Fatal(err)
	}
	first.Roles[0].DisplayName = "mutated"
	first.Packages[0].Tools = append(first.Packages[0].Tools, "mutated")
	next, err := a.loadFlowEnvironment(ctx, "compile-scheme")
	if err != nil || next.Roles[0].DisplayName == "mutated" {
		t.Fatal(next, err)
	}
	if a.flowEnvironments.loads != 1 || a.flowEnvironments.metadataScans != 1 {
		t.Fatalf("warm environment reloaded: loads=%d scans=%d", a.flowEnvironments.loads, a.flowEnvironments.metadataScans)
	}
	now = now.Add(2 * time.Second)
	if _, err = a.loadFlowEnvironment(ctx, "compile-scheme"); err != nil {
		t.Fatal(err)
	}
	if a.flowEnvironments.loads != 1 || a.flowEnvironments.metadataScans != 2 {
		t.Fatal("unchanged metadata reloaded YAML")
	}
	data, err := manager.GetConfigData("compile-scheme")
	if err != nil {
		t.Fatal(err)
	}
	data.Meta.Description = "方案元数据即时更新"
	if err = manager.SaveConfig("", "compile-scheme", data); err != nil {
		t.Fatal(err)
	}
	changed, err := a.loadFlowEnvironment(ctx, "compile-scheme")
	if err != nil || changed.SchemeRevision == next.SchemeRevision || a.flowEnvironments.loads != 2 {
		t.Fatal("scheme stat must bypass directory throttle", changed, err)
	}
	for _, dir := range []string{filepath.Join(home, "tools"), filepath.Join(home, "skills")} {
		if err = os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "metadata-check.md"), []byte("changed"), 0600); err != nil {
			t.Fatal(err)
		}
		before := a.flowEnvironments.loads
		now = now.Add(2 * time.Second)
		if _, err = a.loadFlowEnvironment(ctx, "compile-scheme"); err != nil {
			t.Fatal(err)
		}
		if a.flowEnvironments.loads != before+1 {
			t.Fatal("directory metadata did not invalidate", dir)
		}
	}
	before := a.flowEnvironments.loads
	a.InvalidateFlowEnvironment()
	if _, err = a.loadFlowEnvironment(ctx, "compile-scheme"); err != nil {
		t.Fatal(err)
	}
	if a.flowEnvironments.loads != before+1 {
		t.Fatal("explicit invalidation ignored")
	}
}
func TestAvailablePlansLoadsEnvironmentOnceAndHashesNoContent(t *testing.T) {
	a, _, project, home := realFlowAPI(t)
	a.flowEnvironments.now = func() time.Time { return time.Unix(100, 0) }
	library, err := a.resourceStore()
	if err != nil {
		t.Fatal(err)
	}
	a.SetFlowStore(flow.NewStore("", filepath.Join(home, "flows"), library.Bindings))
	store, err := a.flowStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Import(filepath.Join(project, "docs/refactor/examples/traffic-route"), flow.ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Flow  json.RawMessage     `json:"flow"`
		Tools map[string][]string `json:"tools"`
	}
	raw, err := os.ReadFile(filepath.Join(project, "carto_agent/internal/flow/testdata/compile-model-traffic-route.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	modelCalls := 0
	compiler := flow.NewCompiler(flow.CompilerOptions{Store: store, Resources: library, CacheDir: filepath.Join(home, "cache"), LoadEnvironment: func(ctx context.Context, scheme string) (flow.Environment, error) {
		env, err := a.loadFlowEnvironment(ctx, scheme)
		env.ModelName = "cache-count-model"
		env.Model = apiFlowModelFunc(func(_ context.Context, request flow.ModelRequest) (string, error) {
			modelCalls++
			if request.Kind == "flow" {
				return string(fixture.Flow), nil
			}
			raw, _ := json.Marshal(map[string]any{"suggested_tools": fixture.Tools[request.Step], "missing_capabilities": []string{}, "expected_outputs": []any{}, "condition": nil})
			return string(raw), nil
		})
		return env, err
	}})
	if _, err = compiler.Compile(context.Background(), "traffic-route", "compile-scheme", false); err != nil {
		t.Fatal(err)
	}
	hashes, calls := resource.ContentHashCount(), modelCalls
	for i := 0; i < 2; i++ {
		plans, err := compiler.AvailablePlans(context.Background(), "compile-scheme")
		if err != nil || len(plans) != 1 {
			t.Fatalf("available plans: %d %v", len(plans), err)
		}
	}
	if a.flowEnvironments.loads != 1 || modelCalls != calls || resource.ContentHashCount() != hashes {
		t.Fatalf("cache miss: environments=%d model=%d/%d content_hash=%d/%d", a.flowEnvironments.loads, modelCalls, calls, resource.ContentHashCount(), hashes)
	}
}
func TestFlowCompilerSharedAcrossConcurrentCallers(t *testing.T) {
	a, _, _, _ := realFlowAPI(t)
	var wg sync.WaitGroup
	results := make(chan *flow.Compiler, 8)
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			compiler, err := a.FlowCompiler()
			if err != nil {
				failures <- err
			} else {
				results <- compiler
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	var expected *flow.Compiler
	for compiler := range results {
		if expected == nil {
			expected = compiler
		} else if expected != compiler {
			t.Fatal("FlowCompiler created distinct instances")
		}
	}
}
func TestSuccessfulAssetMutationsInvalidateFlowEnvironment(t *testing.T) {
	a, manager, project, home := realFlowAPI(t)
	builtinSkills, builtinTools, userSkills := filepath.Join(project, "autocarto/skills/system"), filepath.Join(project, "autocarto/tools/system"), filepath.Join(home, "skills")
	service := settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{BuiltinSkillsDir: builtinSkills, UserSkillsDir: userSkills, GlobalSkillsDir: userSkills, BuiltinToolsDir: builtinTools, UserToolsDir: filepath.Join(home, "tools"), RuntimeToolDirs: []string{builtinTools}, RuntimeSkillDirs: []string{builtinSkills, userSkills}}, manager)
	a.configureDefaultControlServices(manager, nil, service, builtinSkills)
	a.flowEnvironments.now = func() time.Time { return time.Unix(100, 0) }
	router := mux.NewRouter()
	a.SetupRoutes(router)
	call := func(method, path string, body any) {
		t.Helper()
		raw, _ := json.Marshal(body)
		request := httptest.NewRequest(method, path, bytes.NewReader(raw))
		request.Header.Set("X-AutoCarto-Contract-Version", "v1")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("%s %s: %d %s", method, path, response.Code, response.Body.String())
		}
	}
	load := func() {
		t.Helper()
		if _, err := a.loadFlowEnvironment(context.Background(), "compile-scheme"); err != nil {
			t.Fatal(err)
		}
	}
	load()
	data, err := manager.GetConfigData("compile-scheme")
	if err != nil {
		t.Fatal(err)
	}
	data.Meta.Description = "through HTTP"
	call("PUT", "/api/agent-configs/compile-scheme", data)
	if a.flowEnvironments.entries != nil {
		t.Fatal("save did not actively clear cache")
	}
	load()
	source := filepath.Join(home, "source/cache-test-package")
	if err = os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	raw := "---\nname: cache-test-package\ndisplay_name: 缓存测试工具包\ndescription: test\nkind: category\nenabled: true\ntools: [data_load_vector]\n---\n# 缓存测试\n"
	if err = os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/skills/import", map[string]any{"source_path": source, "overwrite": false})
	if a.flowEnvironments.entries != nil {
		t.Fatal("import did not actively clear cache")
	}
	load()
	call("DELETE", "/api/skills/cache-test-package?scope=global", nil)
	if a.flowEnvironments.entries != nil {
		t.Fatal("delete did not actively clear cache")
	}
	load()
	if a.flowEnvironments.loads != 4 {
		t.Fatal(a.flowEnvironments.loads)
	}
}
