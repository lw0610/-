package flow

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
)

func migrationMemoryFixture(t *testing.T, association bool) (*Store, *InputMemory, *resource.Library, string, string) {
	t.Helper()
	workspace := t.TempDir()
	project := filepath.Join(workspace, "enio2")
	t.Setenv("AUTOCARTO_PROJECT_ROOT", project)
	t.Setenv("AUTOCARTO_HOME", filepath.Join(project, "runtime"))
	t.Setenv("AUTOCARTO_ASSETS_ROOT", filepath.Join(project, "autocarto"))
	file := filepath.Join(project, "runtime/config/resource_bindings.yaml")
	memory := NewInputMemory(filepath.Join(project, "runtime/config/flow_input_memory.yaml"))
	library := resource.NewLibrary(filepath.Join(project, "autocarto/resources/system"), filepath.Join(project, "runtime/resources"), bindings.New(file))
	store := NewStore(filepath.Join(project, "autocarto/flows/system"), filepath.Join(project, "runtime/flows"), library.Bindings)
	markdown := "---\n编号: marching-route\n名称: 路线\n---\n## 需要准备\n- 底图数据（必填，文件夹）：实际文件所在位置\n## 使用的资源\n- 数据清单「中国底图」：resources/data/中国底图.csv"
	if association {
		markdown += "（文件所在文件夹：「底图数据」）"
	}
	markdown += "\n## 步骤\n### 检查\n- 做法：加载底图\n"
	flowDir := filepath.Join(store.BuiltinDir, "marching-route")
	lockWrite(t, filepath.Join(flowDir, "FLOW.md"), markdown)
	lockWrite(t, filepath.Join(flowDir, "resources/data/中国底图.csv"), "name,path\n道路,road.geojson\n")
	return store, memory, library, file, filepath.Join(workspace, "数据/底图数据/China_map")
}

func TestInputMemoryMigrationV1HashAndEscapingRelativePath(t *testing.T) {
	store, memory, library, file, expectedPath := migrationMemoryFixture(t, true)
	before := "bindings:\n  flow:marching-route/resource_6d294f11b03f: ../数据/底图数据/China_map\n  library:legacy.data_list.0123456789: ../old-library\n  flow:unknown-flow/resource_deadbeef0000: ../unknown\n"
	lockWrite(t, file, before)
	if err := store.MigrateInputMemory(memory, library); err != nil {
		t.Fatal(err)
	}
	values, err := memory.Get("marching-route")
	if err != nil || !reflect.DeepEqual(values, map[string]string{"底图数据": expectedPath}) {
		t.Fatal(values, err)
	}
	backups, _ := filepath.Glob(filepath.Join(filepath.Dir(file), "backups/resource_bindings-*.yaml"))
	if len(backups) != 1 {
		t.Fatal(backups)
	}
	backup, _ := os.ReadFile(backups[0])
	if string(backup) != before {
		t.Fatal("backup did not preserve original bytes")
	}
	snapshot, _ := library.Bindings.Snapshot()
	if len(snapshot.Bindings) != 1 || snapshot.Bindings["library:legacy.data_list.0123456789"] == "" || len(snapshot.Orphaned) != 1 {
		t.Fatal(snapshot)
	}
	bindingsAfter, _ := os.ReadFile(file)
	memoryAfter, _ := os.ReadFile(memory.path)
	if err := store.MigrateInputMemory(memory, library); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(file)
	againMemory, _ := os.ReadFile(memory.path)
	backups, _ = filepath.Glob(filepath.Join(filepath.Dir(file), "backups/resource_bindings-*.yaml"))
	if string(again) != string(bindingsAfter) || string(againMemory) != string(memoryAfter) || len(backups) != 1 {
		t.Fatal("migration is not idempotent")
	}
}

func TestInputMemoryMigrationV2PathAndManifestIdentity(t *testing.T) {
	for _, key := range []string{"path:data/中国底图.csv", "data_list.china"} {
		t.Run(key, func(t *testing.T) {
			store, memory, library, file, expected := migrationMemoryFixture(t, true)
			lockWrite(t, filepath.Join(store.BuiltinDir, "marching-route/resources/data/中国底图.resource.yaml"), "apiVersion: resource/v1\nid: data_list.china\ntype: data_list\nname: 中国底图\nentry: 中国底图.csv\n")
			lockWrite(t, file, "apiVersion: resource-bindings/v2\nbindings:\n  flow:marching-route/"+key+": ../数据/底图数据/China_map\norphaned: {}\n")
			if err := store.MigrateInputMemory(memory, library); err != nil {
				t.Fatal(err)
			}
			values, _ := memory.Get("marching-route")
			if values["底图数据"] != expected {
				t.Fatal(values)
			}
			snapshot, _ := library.Bindings.Snapshot()
			if len(snapshot.Bindings) != 0 {
				t.Fatal(snapshot)
			}
			if _, err := os.Stat(file); err != nil {
				t.Fatal("empty binding file must remain", err)
			}
		})
	}
}

func TestInputMemoryMigrationPreservesNewChoicesAndBacksUpBothExistingFiles(t *testing.T) {
	store, memory, library, file, _ := migrationMemoryFixture(t, true)
	newChoice := filepath.Join(t.TempDir(), "newer")
	if err := memory.Set("marching-route", "底图数据", newChoice); err != nil {
		t.Fatal(err)
	}
	if err := memory.Set("other-flow", "输入", filepath.Join(t.TempDir(), "other")); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(memory.path)
	lockWrite(t, file, "bindings:\n  flow:marching-route/resource_6d294f11b03f: ../old\n")
	if err := store.MigrateInputMemory(memory, library); err != nil {
		t.Fatal(err)
	}
	values, _ := memory.Get("marching-route")
	after, _ := os.ReadFile(memory.path)
	if values["底图数据"] != newChoice || string(after) != string(before) {
		t.Fatal("older binding overwrote a newer user choice")
	}
	// A second declaration with no remembered value modifies the memory file;
	// both previous files must have byte-for-byte backups before the writes.
	flowFile := filepath.Join(store.BuiltinDir, "marching-route/FLOW.md")
	raw, _ := os.ReadFile(flowFile)
	lockWrite(t, flowFile, strings.ReplaceAll(string(raw), "底图数据", "第二文件夹"))
	lockWrite(t, file, "bindings:\n  flow:marching-route/resource_6d294f11b03f: ../old\n")
	if err := store.MigrateInputMemory(memory, library); err != nil {
		t.Fatal(err)
	}
	backups, _ := filepath.Glob(filepath.Join(filepath.Dir(file), "backups/flow_input_memory-*.yaml"))
	if len(backups) != 1 {
		t.Fatal(backups)
	}
	backup, _ := os.ReadFile(backups[0])
	if string(backup) != string(before) {
		t.Fatal("memory backup differs from original")
	}
}

func TestInputMemoryMigrationLeavesUnassociatedOrConflictingBindingsUntouched(t *testing.T) {
	for _, associated := range []bool{false, true} {
		store, memory, library, file, _ := migrationMemoryFixture(t, associated)
		before := "apiVersion: resource-bindings/v2\nbindings:\n  flow:marching-route/path:data/中国底图.csv: ../one\n  flow:marching-route/resource_6d294f11b03f: ../two\norphaned: {}\n"
		lockWrite(t, file, before)
		if err := store.MigrateInputMemory(memory, library); err != nil {
			t.Fatal(err)
		}
		after, _ := os.ReadFile(file)
		if string(after) != before {
			t.Fatal("consumed unassociated or ambiguous binding")
		}
		if _, err := os.Stat(memory.path); !os.IsNotExist(err) {
			t.Fatal("invented an input choice")
		}
	}
}

func TestInputMemoryMigrationFailureRetainsBindingAndUnsupportedMemory(t *testing.T) {
	store, memory, library, file, _ := migrationMemoryFixture(t, true)
	before := "bindings:\n  flow:marching-route/resource_6d294f11b03f: ../old\n"
	lockWrite(t, file, before)
	bad := "apiVersion: unknown/v2\nflows: {}\n"
	lockWrite(t, memory.path, bad)
	if err := store.MigrateInputMemory(memory, library); err == nil {
		t.Fatal("accepted unsupported memory")
	}
	after, _ := os.ReadFile(file)
	afterMemory, _ := os.ReadFile(memory.path)
	if string(after) != before || string(afterMemory) != bad {
		t.Fatal("failure destroyed existing files")
	}
}

func TestInputMemoryMigrationRespectsDoNotRememberAttribute(t *testing.T) {
	store, memory, library, file, _ := migrationMemoryFixture(t, true)
	flowFile := filepath.Join(store.BuiltinDir, "marching-route/FLOW.md")
	raw, _ := os.ReadFile(flowFile)
	lockWrite(t, flowFile, strings.Replace(string(raw), "必填，文件夹", "必填，文件夹，不记住", 1))
	before := "bindings:\n  flow:marching-route/resource_6d294f11b03f: ../old\n"
	lockWrite(t, file, before)
	if err := store.MigrateInputMemory(memory, library); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(file)
	if string(after) != before {
		t.Fatal("opted-out input consumed a legacy binding")
	}
	if _, err := os.Stat(memory.path); !os.IsNotExist(err) {
		t.Fatal("opted-out input was remembered")
	}
}
