package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLinkChangesDoNotRecompileOrPersistDataPaths(t *testing.T) {
	env := fixtureEnvironment()
	calls := 0
	env.Model = modelFunc(func(_ context.Context, r ModelRequest) (string, error) {
		calls++
		if strings.Contains(r.Prompt, "secret.geojson") || strings.Contains(r.Prompt, "\"resolved_path\"") || strings.Contains(r.Prompt, "\"entries\"") {
			t.Fatal("model received expanded resource details")
		}
		return "invalid", nil
	})
	raw := strings.Replace(testFlow, "## 步骤", "## 使用的资源\n- 底图数据「基础数据」：resources/data/list.csv\n## 步骤", 1)
	compiler, store, library := compilerFixture(t, raw, &env)
	item, _ := store.Get("test-flow")
	list := filepath.Join(item.Dir, "resources", "data", "list.csv")
	if err := os.MkdirAll(filepath.Dir(list), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(list, []byte("名称,路径\n区域,secret.geojson\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := compiler.Compile(ctx, "test-flow", "", false)
	if err != nil || !hasCode(first, "FLOW_RESOURCE_UNBOUND") {
		t.Fatalf("initial diagnostics: %v %+v", err, first)
	}
	initialCalls := calls
	base := t.TempDir()
	data := filepath.Join(base, "secret.geojson")
	if err = os.WriteFile(data, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = library.Bindings.Set("flow:test-flow/"+first.Resources[0].Key, base); err != nil {
		t.Fatal(err)
	}
	bound, err := compiler.Compile(ctx, "test-flow", "", false)
	if err != nil || calls != initialCalls || bound.CompileKey != first.CompileKey || bound.LinkFingerprint == first.LinkFingerprint || hasCode(bound, "FLOW_RESOURCE_UNBOUND") {
		t.Fatalf("binding recompiled or stale: %v calls=%d/%d", err, calls, initialCalls)
	}
	if err = os.WriteFile(data, []byte("{\"changed\":true}"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := compiler.Compile(ctx, "test-flow", "", false)
	if err != nil || calls != initialCalls || changed.CompileKey != bound.CompileKey || changed.LinkFingerprint == bound.LinkFingerprint {
		t.Fatalf("data recompiled or unchanged link: %v", err)
	}
	// A caller must not be able to corrupt the remembered resource snapshot.
	changed.Resources[0].Entries[0].Path = "corrupted"
	cloned, err := compiler.Compile(ctx, "test-flow", "", false)
	if err != nil || cloned.Resources[0].Entries[0].Path != data {
		t.Fatalf("mutable link cache: %v %+v", err, cloned)
	}
	paths, _ := filepath.Glob(filepath.Join(compiler.enrichmentDir("test-flow"), "*.json"))
	if len(paths) != 2 {
		t.Fatalf("data changes created enrichment records: %v", paths)
	}
	for _, path := range paths {
		disk, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var saved map[string]any
		if err = json.Unmarshal(disk, &saved); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(disk), data) || strings.Contains(string(disk), base) || strings.Contains(string(disk), "secret.geojson") {
			t.Fatal("bound path or data entry persisted in enrichment cache")
		}
	}
	// 只改交付说明不重做增强；改步骤做法只重做这一步（无效输出各尝试两次）。
	if _, err = store.Save("test-flow", raw+"\n## 交付\n新增成果"); err != nil {
		t.Fatal(err)
	}
	if _, err = compiler.Compile(ctx, "test-flow", "", false); err != nil || calls != initialCalls {
		t.Fatalf("FLOW body change re-enriched unchanged steps: %v calls=%d", err, calls-initialCalls)
	}
	if _, err = store.Save("test-flow", strings.Replace(raw, "检查「文件夹」", "复核「文件夹」", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err = compiler.Compile(ctx, "test-flow", "", false); err != nil || calls != initialCalls+2 {
		t.Fatalf("step change did not re-enrich exactly that step: %v calls=%d", err, calls-initialCalls)
	}
}

func TestRelinkWithoutCompiledPlanIsModelFree(t *testing.T) {
	env := fixtureEnvironment()
	env.Model = modelFunc(func(context.Context, ModelRequest) (string, error) { t.Fatal("relink called model"); return "", nil })
	c, _, _ := compilerFixture(t, testFlow, &env)
	p, stale, err := c.Relink(context.Background(), "test-flow", "")
	if err != nil || stale || p == nil || hasCode(p, "FLOW_PLAN_STALE") || p.LinkFingerprint == "" || p.PlanStatus != PlanStatusBasic {
		t.Fatalf("%v %v %+v", err, stale, p)
	}
	if _, err = os.Stat(c.cacheRoot()); !os.IsNotExist(err) {
		t.Fatal("offline linking wrote the enrichment cache")
	}
}

func TestEnrichmentCacheRemovesLegacyPlansAndPrunes(t *testing.T) {
	env := fixtureEnvironment()
	env.Model = modelFunc(func(_ context.Context, r ModelRequest) (string, error) {
		if r.Kind == "flow" {
			return `{"inputs":[{"label":"原文","type":"conversation_text","options":[]},{"label":"文件夹","type":"folder","options":[]},{"label":"开关","type":"boolean","options":[]},{"label":"纸张","type":"choice","options":["A3","A4"]}],"input_constraints":[],"triggers":["测试"]}`, nil
		}
		return `{"suggested_tools":["data_load_list"],"missing_capabilities":[],"expected_outputs":[],"condition":null,"notes":""}`, nil
	})
	c, store, _ := compilerFixture(t, testFlow, &env)
	legacy := filepath.Join(c.cacheRoot(), "test-flow", env.Scheme)
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "0123.plan.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := c.enrichmentDir("test-flow")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	for i := 0; i < enrichmentKeepLimit+5; i++ {
		path := filepath.Join(dir, fmt.Sprintf("step-%016x.json", i))
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		at := old.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := c.Compile(context.Background(), "test-flow", "", false)
	if err != nil || plan.PlanStatus != PlanStatusReady {
		t.Fatalf("%v %+v", err, plan)
	}
	if _, err = os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy whole-plan cache was not removed")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(paths) != enrichmentKeepLimit+2 {
		t.Fatalf("enrichment files=%d", len(paths))
	}
	// 仍被当前计划引用的记录即使修改时间最早也保留；不再引用的旧记录按时间淘汰。
	records := map[string]string{}
	for _, kind := range []string{"flow", "step"} {
		matches, _ := filepath.Glob(filepath.Join(dir, kind+"-*.json"))
		for _, path := range matches {
			if raw, _ := os.ReadFile(path); strings.Contains(string(raw), enrichmentAPIVersion) {
				records[kind] = path
			}
		}
	}
	if len(records) != 2 {
		t.Fatalf("current records=%v", records)
	}
	ancient := old.Add(-24 * time.Hour)
	for _, path := range records {
		if err = os.Chtimes(path, ancient, ancient); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.Save("test-flow", strings.Replace(testFlow, "检查「文件夹」", "复核「文件夹」", 1)); err != nil {
		t.Fatal(err)
	}
	if plan, err = c.Compile(context.Background(), "test-flow", "", false); err != nil || plan.PlanStatus != PlanStatusReady {
		t.Fatalf("%v %+v", err, plan)
	}
	if _, err = os.Stat(records["flow"]); err != nil {
		t.Fatal("referenced flow record was pruned")
	}
	if _, err = os.Stat(records["step"]); !os.IsNotExist(err) {
		t.Fatal("unreferenced oldest step record was kept")
	}
	if paths, _ = filepath.Glob(filepath.Join(dir, "*.json")); len(paths) != enrichmentKeepLimit+2 {
		t.Fatalf("enrichment files after prune=%d", len(paths))
	}
	if plan, err = c.Plan(context.Background(), "test-flow", ""); err != nil || plan.PlanStatus != PlanStatusReady {
		t.Fatalf("pruning dropped current enrichment: %v %+v", err, plan)
	}
}

func TestEnsureRunnableRejectsResourceChangeDuringCompilation(t *testing.T) {
	env := fixtureEnvironment()
	raw := strings.Replace(testFlow, "## 步骤", "## 使用的资源\n- 参考「说明」：resources/readme.txt\n## 步骤", 1)
	c, store, _ := compilerFixture(t, raw, &env)
	item, _ := store.Get("test-flow")
	path := filepath.Join(item.Dir, "resources", "readme.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	loads := 0
	original := c.options.LoadEnvironment
	c.options.LoadEnvironment = func(ctx context.Context, scheme string) (Environment, error) {
		loads++
		if loads == 2 {
			if err := os.WriteFile(path, []byte("after-more"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return original(ctx, scheme)
	}
	env.Model = modelFunc(func(context.Context, ModelRequest) (string, error) {
		t.Fatal("run validation called model")
		return "", nil
	})
	if _, err := c.EnsureRunnable(context.Background(), "test-flow", ""); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("changed resource entered run: %v", err)
	}
}
