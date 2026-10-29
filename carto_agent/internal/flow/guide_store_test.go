package flow

import (
	"context"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func guideTestStore(t *testing.T) *GuideStore {
	t.Helper()
	root := t.TempDir()
	return NewGuideStore(NewStore(filepath.Join(root, "builtin"), filepath.Join(root, "flows"), bindings.New(filepath.Join(root, "bindings.yaml"))), filepath.Join(root, "config", "guides.yaml"))
}
func TestGuideStoreRoundTripAndGlobalEnable(t *testing.T) {
	s := guideTestStore(t)
	g, err := s.Create(guideExample(t), "route-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(g.Dir, "FLOW.md")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetEnabled(g.Meta.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Save(g.Meta.ID, g.GuideMD+"\n补充\n"); err != nil {
		t.Fatal(err)
	}
	again := NewGuideStore(s.Packages, s.ConfigPath)
	loaded, err := again.Get(g.Meta.ID)
	if err != nil || loaded.Enabled {
		t.Fatalf("disabled state lost %+v %v", loaded, err)
	}
	copy, err := s.Copy(g.Meta.ID, "copied-guide", "指南副本")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "guide.zip")
	if _, err = s.Packages.ExportWithOptions(copy.Meta.ID, target, ExportOptions{IncludeDataFiles: true}); err != nil {
		t.Fatal(err)
	}
	other := guideTestStore(t)
	imported, err := other.Import(target, ImportOptions{})
	if err != nil || imported.Meta.Name != "指南副本" {
		t.Fatalf("%+v %v", imported, err)
	}
}
func TestGuideMigrationPreservesOriginalAndIsIdempotent(t *testing.T) {
	s := guideTestStore(t)
	old := "---\n名称: 老流程\n编号: legacy-test\n版本: 3.1.0\n---\n## 适用场景\n制作地图\n## 参与角色\n### 制图人员\n- 工具包：符号与标注\n## 需要准备\n- 区域（必填，文本）\n- 底图（文件夹）：用户目录\n## 数据需求\n### 底图\n- 类别：行政区划面\n- 范围：「区域」\n## 步骤\n### 第1步 绘制\n- 负责：制图人员\n- 条件：用户需要时\n- 做法：核验数据后制图。\n- 完成标准：「成果」（文件，pdf）\n- 失败时：停止流程\n"
	item, err := s.Packages.Create(old, "legacy-test")
	if err != nil {
		t.Fatal(err)
	}
	legacyLock, _ := ReadLock(item.Dir)
	legacyLock.DocumentFormat = ""
	if err := writeLock(item.Dir, legacyLock); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(filepath.Join(item.Dir, "FLOW.md"))
	if err = os.WriteFile(filepath.Join(item.Dir, "payload.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	reports, err := s.MigrateLegacy()
	if err != nil || len(reports) != 1 {
		t.Fatalf("%+v %v", reports, err)
	}
	g, err := s.Get(item.Meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	backup, _ := os.ReadFile(filepath.Join(item.Dir, "FLOW.md.bak"))
	if string(backup) != string(original) || g.Meta.Version != "4.0.0" || g.ConversionReport == nil {
		t.Fatal("conversion lost original/report/version")
	}
	if !strings.Contains(g.Guide.Body, "行政区划面") {
		t.Fatal("converted data requirement lost")
	}
	before, _ := os.Stat(filepath.Join(item.Dir, "FLOW.md"))
	reports, err = s.MigrateLegacy()
	after, _ := os.Stat(filepath.Join(item.Dir, "FLOW.md"))
	if err != nil || len(reports) != 0 || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("migration repeated")
	}
	raw, _ := os.ReadFile(filepath.Join(item.Dir, "payload.txt"))
	if string(raw) != "keep" {
		t.Fatal("package payload changed")
	}
}
func TestGuideSaveAdvisoriesAndReadContext(t *testing.T) {
	s := guideTestStore(t)
	library := resource.NewLibrary("", filepath.Join(t.TempDir(), "library"), s.Packages.Bindings)
	input := filepath.Join(t.TempDir(), "river.qml")
	if err := os.WriteFile(input, []byte(`<qgis version="3.34"><renderer-v2 type="singleSymbol"/></qgis>`), 0600); err != nil {
		t.Fatal(err)
	}
	imported, err := library.Import(input, "style")
	if err != nil {
		t.Fatal(err)
	}
	s.Packages.SetLibrary(library)
	raw := "---\n名称: 自由流程\n说明: 测试\n---\n# 任意标题\n图层按样式「" + imported.Name + "」绘制。\n"
	g, err := s.Create(raw, "free-flow")
	if err != nil {
		t.Fatal(err)
	}
	lock, err := ReadLock(g.Dir)
	if err != nil || len(lock.Resources) != 1 || lock.Resources[0].ID != imported.ID {
		t.Fatalf("save did not lock references: %+v %v", lock, err)
	}
	text, err := s.ReadContext(context.Background(), g.Meta.ID, library)
	if err != nil || !strings.Contains(text, "# 任意标题") || !strings.Contains(text, "引用的样式与版面") || !strings.Contains(text, imported.Name) {
		t.Fatalf("full context missing: %s %v", text, err)
	}
	g, err = s.Save(g.Meta.ID, "没有头部也可以暂存。样式「未知」")
	if err != nil || len(g.Diagnostics) == 0 {
		t.Fatalf("warning blocked save: %+v %v", g, err)
	}
	lock, err = ReadLock(g.Dir)
	if err != nil || len(lock.Resources) != 0 {
		t.Fatalf("removed reference retained lock: %+v %v", lock, err)
	}
}
