package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"github.com/gorilla/mux"
)

type requestOnlyKey struct{}

func TestV50AllFlowRoutesStayNonblocking(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_HOME", filepath.Join(root, "runtime"))
	t.Setenv("AUTOCARTO_ASSETS_ROOT", filepath.Join(root, "assets"))
	t.Setenv("AUTOCARTO_DISABLE_KEYRING", "1")
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	binding := bindings.New(filepath.Join(root, "runtime/config/resource_bindings.yaml"))
	library := resource.NewLibrary(filepath.Join(root, "builtin"), filepath.Join(root, "resources"), binding)
	write(filepath.Join(library.UserDir, "docs/旧说明.md"), "旧的本机参考材料")
	write(filepath.Join(library.UserDir, "docs/旧说明.resource.yaml"), "apiVersion: resource/v1\nid: doc.old-user\ntype: doc\nname: 旧说明\nversion: 1.0.0\nentry: 旧说明.md\n")
	store := flow.NewStore("", filepath.Join(root, "flows"), binding)
	raw := "---\n编号: base-flow\n名称: 本地操作验收\n---\n## 需要准备\n- 数据目录（可选，文件夹）：选择输入数据。\n## 使用的资源\n- 参考「旧说明」：公共资源库\n- 参考「附加说明」：resources/info.md\n## 步骤\n### 确认\n- 负责：用户\n- 做法：确认执行结果\n- 完成标准：「确认」（确认结果）\n"
	if _, err := store.Create(raw, ""); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "input/import.md")
	write(source, strings.Replace(raw, "base-flow", "imported-flow", 1))
	attachment := filepath.Join(root, "input/info.md")
	write(attachment, "附加材料")
	var synchronous, total atomic.Int32
	model := apiFlowModelFunc(func(ctx context.Context, _ flow.ModelRequest) (string, error) {
		total.Add(1)
		if ctx.Value(requestOnlyKey{}) != nil {
			synchronous.Add(1)
		}
		<-ctx.Done()
		return "", ctx.Err()
	})
	compiler := flow.NewCompiler(flow.CompilerOptions{Store: store, Resources: library, CacheDir: filepath.Join(root, "cache"), LoadEnvironment: func(context.Context, string) (flow.Environment, error) {
		return flow.Environment{Scheme: "test", Model: model, ModelName: "blocked-model"}, nil
	}})
	lifecycle, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobs := flow.NewEnrichmentManager(compiler, flow.EnrichmentOptions{})
	jobs.Start(lifecycle)
	configDir := filepath.Join(root, "runtime/config/agent_configs")
	write(filepath.Join(configDir, "test/agent.yaml"), "version: '2.0'\nruntime:\n  agents:\n    main:\n      kind: agent\n      display_name: 主角色\n      enabled: true\n      is_main: true\n")
	api := NewAPI(nil, config.NewAgentConfigManager(configDir), nil, nil, "", nil)
	api.SetFlowStore(store)
	api.SetResourceLibrary(library)
	api.SetFlowCompiler(compiler)
	router := mux.NewRouter()
	api.SetupRoutes(router)
	type routeCase struct {
		method, template, path string
		body                   any
		want                   int
	}
	cases := []routeCase{
		{"GET", "/api/flows", "/api/flows", nil, 200},
		{"GET", "/api/flows/{id}", "/api/flows/base-flow", nil, 200},
		{"GET", "/api/flows/{id}/roles", "/api/flows/base-flow/roles", nil, 200},
		{"PUT", "/api/flows/{id}/roles", "/api/flows/base-flow/roles", map[string]any{"mapping": map[string]string{}}, 200},
		{"GET", "/api/flows/bindings/status", "/api/flows/bindings/status", nil, 200},
		{"PUT", "/api/flows/{id}", "/api/flows/base-flow", map[string]any{"flow_md": raw}, 200},
		{"POST", "/api/flows", "/api/flows", map[string]any{"flow_md": strings.Replace(raw, "base-flow", "created-flow", 1)}, 200},
		{"POST", "/api/flows/{id}/copy", "/api/flows/base-flow/copy", map[string]any{"new_id": "copied-flow"}, 200},
		{"POST", "/api/flows/import", "/api/flows/import", map[string]any{"source_path": source}, 200},
		{"POST", "/api/flows/{id}/resources/import", "/api/flows/base-flow/resources/import", map[string]any{"source_path": attachment}, 200},
		{"POST", "/api/flows/{id}/resources/repair", "/api/flows/base-flow/resources/repair", map[string]any{"action": "upgrade_resource", "resource_key": "path:info.md"}, 200},
		{"POST", "/api/flows/{id}/resources/adopt", "/api/flows/base-flow/resources/adopt", map[string]any{"resource_id": "doc.old-user", "label": "旧说明"}, 200},
		{"POST", "/api/flows/{id}/inputs/check", "/api/flows/base-flow/inputs/check", map[string]any{"values": map[string]any{"数据目录": root}}, 200},
		{"GET", "/api/flows/{id}/inputs/memory", "/api/flows/base-flow/inputs/memory", nil, 200},
		{"PUT", "/api/flows/{id}/inputs/memory/{label}", "/api/flows/base-flow/inputs/memory/数据目录", map[string]any{"value": root}, 200},
		{"DELETE", "/api/flows/{id}/inputs/memory/{label}", "/api/flows/base-flow/inputs/memory/数据目录", nil, 200},
		{"PUT", "/api/flows/{id}/bindings", "/api/flows/base-flow/bindings", map[string]any{"resource_key": "old-key", "base_dir": root}, 410},
		{"POST", "/api/flows/{id}/export", "/api/flows/base-flow/export", map[string]any{"target_path": filepath.Join(root, "export.zip")}, 200},
		{"DELETE", "/api/flows/{id}/resources", "/api/flows/base-flow/resources?path=resources/info.md", nil, 200},
		{"DELETE", "/api/flows/{id}", "/api/flows/copied-flow", nil, 200},
	}
	covered := map[string]bool{}
	for _, sample := range cases {
		covered[sample.method+" "+sample.template] = true
	}
	if err := router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		path, err := route.GetPathTemplate()
		if err != nil || !strings.HasPrefix(path, "/api/flows") {
			return nil
		}
		methods, _ := route.GetMethods()
		for _, method := range methods {
			if method != "OPTIONS" && !covered[method+" "+path] {
				t.Errorf("新增流程路由必须补充非阻塞验收样例：%s %s", method, path)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, sample := range cases {
		t.Run(sample.method+" "+sample.template, func(t *testing.T) {
			payload, _ := json.Marshal(sample.body)
			ctx, done := context.WithTimeout(context.WithValue(context.Background(), requestOnlyKey{}, true), time.Second)
			defer done()
			request := httptest.NewRequest(sample.method, sample.path, bytes.NewReader(payload)).WithContext(ctx)
			request.Header.Set("X-AutoCarto-Contract-Version", "v1")
			response := httptest.NewRecorder()
			started := time.Now()
			router.ServeHTTP(response, request)
			elapsed := time.Since(started)
			if response.Code != sample.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, sample.want, response.Body.String())
			}
			if synchronous.Load() != 0 || elapsed >= time.Second {
				t.Fatalf("request blocked: calls=%d elapsed=%s", synchronous.Load(), elapsed)
			}
			t.Logf("model_in_request=0 elapsed=%s", elapsed)
		})
	}
	if total.Load() != 0 {
		t.Fatalf("flow requests started %d model calls", total.Load())
	}
}
