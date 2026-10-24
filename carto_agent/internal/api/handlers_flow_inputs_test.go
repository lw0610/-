package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"github.com/gorilla/mux"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFlowInputChecksAreReadOnlyAndMemoryIsLocal(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", root)
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	bindingStore := bindings.New(filepath.Join(root, "config/resource_bindings.yaml"))
	store := flow.NewStore("", filepath.Join(root, "flows"), bindingStore)
	library := resource.NewLibrary("", filepath.Join(root, "resources"), bindingStore)
	raw := "---\n编号: input-flow\n名称: 数据输入\n---\n## 需要准备\n- 数据目录（必填，文件夹）\n- 标题（可选，文本）\n## 使用的资源\n- 数据清单「数据」：resources/data/list.csv（文件所在文件夹：「数据目录」）\n## 步骤\n### 检查\n- 负责：用户\n- 做法：检查「数据」\n"
	item, err := store.Create(raw, "input-flow")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(item.Dir, "resources/data"), 0700)
	_ = os.WriteFile(filepath.Join(item.Dir, "resources/data/list.csv"), []byte("name,path\n区域,area.geojson\n"), 0600)
	data := filepath.Join(root, "datasets")
	_ = os.MkdirAll(data, 0700)
	a := &API{}
	a.SetFlowStore(store)
	a.SetResourceLibrary(library)
	a.SetFlowCompiler(flow.NewCompiler(flow.CompilerOptions{Store: store, Resources: library, CacheDir: filepath.Join(root, "cache"), LoadEnvironment: func(context.Context, string) (flow.Environment, error) {
		return flow.Environment{Scheme: "default", Model: apiFlowModelFunc(func(context.Context, flow.ModelRequest) (string, error) {
			t.Error("input API invoked model")
			return "", nil
		})}, nil
	}}))
	router := mux.NewRouter()
	a.SetupRoutes(router)
	call := func(method, path string, payload any, want int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(payload)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewReader(raw)))
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		result := map[string]any{}
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		return result
	}
	_ = os.Remove(filepath.Join(item.Dir, "flow.lock"))
	before := inputAPISnapshot(t, root)
	problems := call("POST", "/api/flows/input-flow/inputs/check", map[string]any{"scheme": "default", "values": map[string]any{"数据目录": data}}, 200)["data"].([]any)
	if len(problems) != 1 || problems[0].(map[string]any)["code"] != "FLOW_DATA_FILE_MISSING" {
		t.Fatal(problems)
	}
	call("GET", "/api/flows/input-flow/inputs/memory", nil, 200)
	after := inputAPISnapshot(t, root)
	if before != after {
		t.Fatal("read-only input APIs wrote files")
	}
	_ = os.WriteFile(filepath.Join(data, "area.geojson"), []byte("{}"), 0600)
	problems = call("POST", "/api/flows/input-flow/inputs/check", map[string]any{"values": map[string]any{"数据目录": data}}, 200)["data"].([]any)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	call("PUT", "/api/flows/input-flow/inputs/memory/数据目录", map[string]string{"value": data}, 200)
	remembered := call("GET", "/api/flows/input-flow/inputs/memory", nil, 200)["data"].(map[string]any)
	if remembered["数据目录"] != data {
		t.Fatal(remembered)
	}
	call("PUT", "/api/flows/input-flow/inputs/memory/标题", map[string]string{"value": data}, 400)
	call("PUT", "/api/flows/input-flow/inputs/memory/数据目录", map[string]string{"value": filepath.Join(data, "area.geojson")}, 400)
	copied, err := store.Copy("input-flow", "copied-input", "副本")
	if err != nil {
		t.Fatal(err)
	}
	memory, _ := flow.DefaultInputMemory()
	values, _ := memory.Get(copied.Meta.ID)
	if len(values) != 0 {
		t.Fatal("copy inherited memory")
	}
	call("DELETE", "/api/flows/input-flow/inputs/memory/数据目录", nil, 200)
	values, _ = memory.Get("input-flow")
	if len(values) != 0 {
		t.Fatal(values)
	}
	call("PUT", "/api/flows/input-flow/inputs/memory/数据目录", map[string]string{"value": data}, 200)
	call("DELETE", "/api/flows/input-flow", nil, 200)
	values, _ = memory.Get("input-flow")
	if len(values) != 0 {
		t.Fatal("delete retained memory", values)
	}
}
func inputAPISnapshot(t *testing.T, root string) string {
	t.Helper()
	var out strings.Builder
	err := filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		out.WriteString(path)
		if !e.IsDir() {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out.Write(raw)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestManualInputMemoryUsesDeclaredTypeAndRememberPolicy(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	file := filepath.Join(root, "source.txt")
	_ = os.WriteFile(file, []byte("source"), 0600)
	for _, tc := range []struct {
		name, declaration, value string
		allow                    bool
	}{
		{"explicit folder", "数据（可选，文件夹）", root, true},
		{"legacy folder", "数据（可选）：输入文件夹", root, true},
		{"legacy file", "数据（可选）：输入文件", file, true},
		{"do not remember", "数据（可选，文件夹，不记住）", root, false},
		{"explicit text wins", "数据（可选，文本）：文件夹名称", root, false},
		{"choice wins", "数据（可选值：甲、乙）：文件夹名称", root, false},
		{"source text wins", "数据（可选）：文件中的对话原文", file, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			memory := flow.NewInputMemory(filepath.Join(t.TempDir(), "memory.yaml"))
			item := &flow.StoredFlow{Meta: flow.FlowMetadata{ID: "memory-policy"}, FlowMD: "## 需要准备\n- " + tc.declaration + "\n"}
			err := setInputMemoryValue(memory, item, "数据", tc.value)
			if (err == nil) != tc.allow {
				t.Fatalf("allow=%v, err=%v", tc.allow, err)
			}
			values, _ := memory.Get(item.Meta.ID)
			if !tc.allow && len(values) != 0 {
				t.Fatal("rejected memory changed storage", values)
			}
		})
	}
}

func TestLegacyBindingConvertActionUsesInputMemory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_HOME", root)
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	b := bindings.New(filepath.Join(root, "bindings.yaml"))
	library := resource.NewLibrary("", filepath.Join(root, "resources"), b)
	store := flow.NewStore("", filepath.Join(root, "flows"), b)
	item, err := store.Create("---\n编号: convert-input\n名称: 转换数据\n---\n## 使用的资源\n- 数据清单「底图」：resources/data/list.csv\n## 步骤\n### 检查\n- 负责：用户\n- 做法：检查「底图」\n", "convert-input")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(item.Dir, "resources/data"), 0700)
	_ = os.WriteFile(filepath.Join(item.Dir, "resources/data/list.csv"), []byte("name,path\n区域,area.geojson\n"), 0600)
	data := filepath.Join(root, "data")
	_ = os.MkdirAll(data, 0700)
	_ = os.WriteFile(filepath.Join(data, "area.geojson"), []byte("{}"), 0600)
	if err = b.Set("flow:convert-input/path:data/list.csv", data); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(root, "blocked-memory-parent")
	_ = os.WriteFile(blocked, []byte("blocked"), 0600)
	if _, err = store.ConvertResourceToInput("convert-input", "path:data/list.csv", library, flow.NewInputMemory(filepath.Join(blocked, "memory.yaml"))); err == nil {
		t.Fatal("unwritable memory did not fail")
	}
	if _, found, _ := b.Get("flow:convert-input/path:data/list.csv"); !found {
		t.Fatal("failed conversion lost source binding")
	}
	memory, _ := flow.DefaultInputMemory()
	updated, err := store.ConvertResourceToInput("convert-input", "path:data/list.csv", library, memory)
	if err != nil {
		t.Fatal(err)
	}
	doc := flow.Parse(updated.FlowMD)
	if doc.Resources[0].FolderInput != "底图所在文件夹" || len(doc.Inputs) != 1 || doc.Inputs[0].Type != "folder" {
		t.Fatal(doc)
	}
	values, _ := memory.Get("convert-input")
	if values["底图所在文件夹"] != data {
		t.Fatal(values)
	}
	if _, found, _ := b.Get("flow:convert-input/path:data/list.csv"); found {
		t.Fatal("converted binding retained")
	}
	// A retry after the target was persisted but source deletion failed must
	// retain a newer explicit user choice, then consume the old source safely.
	newer := filepath.Join(root, "newer-data")
	_ = os.MkdirAll(newer, 0700)
	_ = memory.Set("convert-input", "底图所在文件夹", newer)
	_ = b.Set("flow:convert-input/path:data/list.csv", data)
	if _, err = store.ConvertResourceToInput("convert-input", "path:data/list.csv", library, memory); err != nil {
		t.Fatal(err)
	}
	values, _ = memory.Get("convert-input")
	if values["底图所在文件夹"] != newer {
		t.Fatal("conversion retry overwrote newer input choice", values)
	}
}
