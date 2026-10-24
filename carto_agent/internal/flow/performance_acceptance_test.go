package flow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autocarto/carto_agent/internal/resource"
)

// R73 measures the real Compile/link path with a valid, deterministic model.
// The shapefile bytes are metadata fixtures; opening them in QGIS is covered
// separately by the resource contract tests.
func TestR73CompileAndLinkCounters(t *testing.T) {
	const raw = `---
名称: 链接计数验收
编号: test-flow
版本: 2.0.0
---
## 使用的资源
- 底图数据「测试数据」：resources/data/list.csv
## 步骤
### 第1步 检查
- 负责：数据工程专家
- 做法：读取「测试数据」，检查图层
- 完成标准：「结果」（文本）
`
	env := fixtureEnvironment()
	modelCalls := 0
	env.Model = modelFunc(func(_ context.Context, request ModelRequest) (string, error) {
		modelCalls++
		if request.Kind == "flow" {
			return `{"inputs":[],"input_constraints":[],"triggers":[]}`, nil
		}
		return `{"suggested_tools":["data_load_list"],"missing_capabilities":[],"expected_outputs":[],"condition":null,"notes":"计数夹具"}`, nil
	})
	compiler, store, library := compilerFixture(t, raw, &env)
	item, err := store.Get("test-flow")
	if err != nil {
		t.Fatal(err)
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(item.Dir, "resources/data/list.csv"), "名称,路径\n测试区域,region.shp\n")
	ctx := context.Background()
	first, err := compiler.Compile(ctx, "test-flow", "", false)
	if err != nil || first == nil {
		t.Fatalf("initial compile: %v", err)
	}
	if modelCalls != 2 || hasCode(first, "FLOW_COMPILE_LLM_FAILED") || !hasCode(first, "FLOW_RESOURCE_UNBOUND") {
		t.Fatalf("initial model=%d diagnostics=%+v", modelCalls, first.Diagnostics)
	}
	t.Logf("cold compile: model=%d; unbound=true", modelCalls)
	previous := first
	check := func(stage string, wantModel int, wantCompileChange bool, diagnostic string, wantDiagnostic bool) *FlowPlan {
		t.Helper()
		beforeCalls, beforeHashes := modelCalls, resource.ContentHashCount()
		started := time.Now()
		plan, err := compiler.Compile(ctx, "test-flow", "", false)
		if err != nil || plan == nil {
			t.Fatalf("%s: %v", stage, err)
		}
		modelDelta, hashDelta := modelCalls-beforeCalls, resource.ContentHashCount()-beforeHashes
		compileChanged := plan.CompileKey != previous.CompileKey
		linkChanged := plan.LinkFingerprint != previous.LinkFingerprint
		// Save may rewrite copied resource files while replacing the flow
		// directory; the zero-content-read requirement applies to link-only
		// changes, not that structural save operation.
		if modelDelta != wantModel || (!wantCompileChange && hashDelta != 0) || compileChanged != wantCompileChange || !linkChanged {
			t.Fatalf("%s: model_delta=%d content_hash_delta=%d compile_changed=%t link_changed=%t", stage, modelDelta, hashDelta, compileChanged, linkChanged)
		}
		if diagnostic != "" && hasCode(plan, diagnostic) != wantDiagnostic {
			t.Fatalf("%s: %s present=%t, diagnostics=%+v", stage, diagnostic, wantDiagnostic, plan.Diagnostics)
		}
		if hasCode(plan, "FLOW_COMPILE_LLM_FAILED") {
			t.Fatalf("%s fell back after invalid model JSON", stage)
		}
		t.Logf("%s: model_delta=%d content_hash_delta=%d compile_changed=%t link_changed=%t %s=%t elapsed=%s", stage, modelDelta, hashDelta, compileChanged, linkChanged, diagnostic, wantDiagnostic, time.Since(started))
		previous = plan
		return plan
	}
	base := t.TempDir()
	shp, dbf := filepath.Join(base, "region.shp"), filepath.Join(base, "region.dbf")
	for _, ext := range []string{".shp", ".shx", ".dbf", ".prj"} {
		write(filepath.Join(base, "region"+ext), "initial")
	}
	bindingKey := "flow:test-flow/" + first.Resources[0].Key
	if err = library.Bindings.Set(bindingKey, base); err != nil {
		t.Fatal(err)
	}
	check("bind valid directory", 0, false, "FLOW_RESOURCE_UNBOUND", false)
	write(shp, "size changed, without reading these bytes for link hashing")
	check("data size changed", 0, false, "FLOW_DATA_FILE_MISSING", false)
	info, err := os.Stat(shp)
	if err != nil {
		t.Fatal(err)
	}
	modified := info.ModTime().Add(2 * time.Second)
	if err = os.Chtimes(shp, modified, modified); err != nil {
		t.Fatal(err)
	}
	check("data mtime only changed", 0, false, "FLOW_DATA_FILE_MISSING", false)
	write(dbf, "dbf changed to a different size")
	check("dbf content changed", 0, false, "FLOW_DATA_SIDECAR_MISSING", false)
	if err = os.Remove(dbf); err != nil {
		t.Fatal(err)
	}
	check("dbf removed", 0, false, "FLOW_DATA_SIDECAR_MISSING", true)
	write(dbf, "dbf restored")
	check("dbf restored", 0, false, "FLOW_DATA_SIDECAR_MISSING", false)
	if err = os.Remove(shp); err != nil {
		t.Fatal(err)
	}
	check("data removed", 0, false, "FLOW_DATA_FILE_MISSING", true)
	write(shp, "data restored")
	check("data restored", 0, false, "FLOW_DATA_FILE_MISSING", false)
	if err = library.Bindings.Set(bindingKey, ""); err != nil {
		t.Fatal(err)
	}
	check("binding removed", 0, false, "FLOW_RESOURCE_UNBOUND", true)
	if err = library.Bindings.Set(bindingKey, base); err != nil {
		t.Fatal(err)
	}
	check("binding restored", 0, false, "FLOW_RESOURCE_UNBOUND", false)
	if _, err = store.Save("test-flow", raw+"\n## 交付\n新增正文说明\n"); err != nil {
		t.Fatal(err)
	}
	// 正文中与步骤无关的部分不进入任何增强内容键；只改一步的做法只重做这一步。
	check("FLOW body changed", 0, true, "FLOW_RESOURCE_UNBOUND", false)
	changed := strings.Replace(raw, "检查图层", "逐一检查图层", 1) + "\n## 交付\n新增正文说明\n"
	if _, err = store.Save("test-flow", changed); err != nil {
		t.Fatal(err)
	}
	check("step instructions changed", 1, true, "FLOW_RESOURCE_UNBOUND", false)
}
