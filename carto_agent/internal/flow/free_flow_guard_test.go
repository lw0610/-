package flow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFlowRuntimeHasNoStructuredExecutionContract(t *testing.T) {
	typ := reflect.TypeOf(GuideDoc{})
	fields := []string{}
	for i := 0; i < typ.NumField(); i++ {
		fields = append(fields, typ.Field(i).Name)
	}
	if !reflect.DeepEqual(fields, []string{"Meta", "Body", "Resources", "Diagnostics"}) {
		t.Fatalf("flow document acquired execution fields: %v", fields)
	}
	for _, file := range []string{"guide_parse.go", "guide_context.go", "guide_environment.go", "guide_store.go", "lock.go", "link.go", "repair.go", "export.go"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, retired := range []string{"guideTable(", "DataPreview", "ResolveInputResources", "\"data_list\"", "\"style_set\""} {
			if strings.Contains(string(raw), retired) {
				t.Fatalf("%s restored retired runtime mechanism %s", file, retired)
			}
		}
	}
}

func TestV5GuideMigrationRetainsDetailsAndRewritesMappedStyles(t *testing.T) {
	s := guideTestStore(t)
	root := t.TempDir()
	library := lockLibrary(t, root)
	s.Packages.SetLibrary(library)
	lockStyle(t, library.UserDir, "style.converted", "新河流样式", "1.0.0", "mapped")
	mapping := `[{"from":"old.qml","to":"new.qml","type":"style","name":"新河流样式","previous_name":"旧组合"}]`
	lockWrite(t, filepath.Join(library.UserDir, ".migration-v6-report.json"), mapping)
	dir := filepath.Join(s.Packages.UserDir, "v5-flow")
	original := "---\n编号: v5-flow\n名称: 旧指南\n说明: 业务要求\n版本: 4.0.0\n---\n## 适用任务\n制作河流地图。\n## 数据\n| 数据 | 内容 | 范围 | 字段 | 说明 |\n|---|---|---|---|---|\n| 河流 | 类别：河流线 | 用户区域 | name | 真实来源 |\n## 图层处理\n| 图层 | 数据 | 样式 | 标注 |\n|---|---|---|---|\n| 河流 | 河流 | 「旧组合」 | 名称 |\n## 需要明确的信息\n- 纸张（文本，默认 A1 横向）：可改\n## 制作要点\n### 符号化\n使用真实河流，不编造。\n## 产出与检查\n- 文件：PDF\n## 补充知识\n不能遗失的业务细节。\n"
	lockWrite(t, filepath.Join(dir, "GUIDE.md"), original)
	reports, err := s.MigrateLegacy()
	if err != nil || len(reports) != 1 {
		t.Fatal(reports, err)
	}
	got, err := s.Resolve(context.Background(), "v5-flow", library)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Doc.Resources) != 1 || got.Doc.Resources[0].Label != "新河流样式" || !strings.Contains(got.Doc.Body, "不能遗失的业务细节") || !strings.Contains(got.Doc.Body, "A1 横向") {
		t.Fatalf("migration lost business content: %+v", got.Doc)
	}
	backup, err := os.ReadFile(filepath.Join(dir, "GUIDE.md.bak"))
	if err != nil || string(backup) != original {
		t.Fatal("original GUIDE backup lost", err)
	}
	encoded, _ := json.Marshal(got.Doc)
	if strings.Contains(string(encoded), `"methods":`) || strings.Contains(string(encoded), `"data":`) {
		t.Fatal("converted document acquired executable structure")
	}
	if reports, err = s.MigrateLegacy(); err != nil || len(reports) != 0 {
		t.Fatal("migration is not idempotent", reports, err)
	}
}

func TestSavedFreeFlowWithLegacyLookingBodyNeverReconverted(t *testing.T) {
	s := guideTestStore(t)
	raw := "---\n名称: 自由流程\n说明: 新正文允许旧标题与字段\n---\n## 步骤\n### 第1步 我的描述\n- 做法：完整保留这句文字。\n- 完成标准：我的自然语言说明。\n## 适用任务\n自由正文。\n"
	created, err := s.Create(raw, "free-legacy-looking")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Save(created.Meta.ID, created.GuideMD+"\n编辑后的完整说明\n"); err != nil {
		t.Fatal(err)
	}
	copied, err := s.Copy(created.Meta.ID, "free-copy", "副本")
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "arbitrary-name.zip")
	if _, err = s.Packages.ExportWithOptions(copied.Meta.ID, archive, ExportOptions{}); err != nil {
		t.Fatal(err)
	}
	dest := guideTestStore(t)
	imported, err := dest.Import(archive, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if imported.Meta.ID != copied.Meta.ID || imported.Guide.Body != copied.Guide.Body || imported.ConversionReport != nil {
		t.Fatal("export/import interpreted free prose", imported)
	}
	for _, store := range []*GuideStore{s, dest} {
		before, _ := store.List()
		reports, err := store.MigrateLegacy()
		if err != nil || len(reports) != 0 {
			t.Fatal("new free flow converted", reports, err)
		}
		for _, row := range before {
			got, err := store.Get(row.ID)
			if err != nil || !strings.Contains(got.Guide.Body, "编辑后的完整说明") {
				t.Fatal("startup lost body", got, err)
			}
		}
	}
	lock, err := ReadLock(imported.Dir)
	if err != nil || lock.DocumentFormat != documentFormatV6 {
		t.Fatal("format marker lost", lock, err)
	}
	unmarked := "---\n名称: 新文档\n说明: 任意标题\n---\n## 步骤\n### 第一步\n这里没有旧的结构字段。\n## 适用任务\n自然语言。\n"
	converted, report, err := ConvertLegacy(unmarked)
	if err != nil || report.Converted || converted != unmarked {
		t.Fatal("headings alone triggered migration", report, err)
	}
}

func TestLegacyPublicDataAndDocumentContentMoveIntoFreeFlow(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	library := lockLibrary(t, root)
	csv := "名称,路径,类型,类别,字段,说明,年代,精度,用途\n海岸线数据,private/coast.shp,矢量,海岸线,NAME,真实海岸边界,2020年,1:5万,背景定位\n"
	doc := "必须说明比例尺与统计口径，不能遗漏数据年代。\n"
	lockWrite(t, filepath.Join(library.UserDir, "data/shared.csv"), csv)
	lockWrite(t, filepath.Join(library.UserDir, "data/shared.resource.yaml"), "apiVersion: resource/v1\nid: data_list.shared\ntype: data_list\nname: 共享底图\nentry: shared.csv\n")
	lockWrite(t, filepath.Join(library.UserDir, "docs/rules.md"), doc)
	lockWrite(t, filepath.Join(library.UserDir, "docs/rules.resource.yaml"), "apiVersion: resource/v1\nid: doc.rules\ntype: doc\nname: 业务规则\nentry: rules.md\n")
	if _, err := library.MigrateV6(); err != nil {
		t.Fatal(err)
	}
	store := NewStore("", filepath.Join(root, "flows"), library.Bindings)
	store.SetLibrary(library)
	guides := NewGuideStore(store, filepath.Join(root, "guides.yaml"))
	raw := "---\n编号: shared-legacy\n名称: 原公共资源流程\n说明: 迁移测试\n---\n## 使用的资源\n- 底图数据「共享底图」：公共资源库\n- 参考文档「流程中的别名」：公共资源库/doc.rules\n## 步骤\n### 第1步 制作\n- 做法：准备真实数据后制图。\n"
	dir := filepath.Join(store.UserDir, "shared-legacy")
	lockWrite(t, filepath.Join(dir, "FLOW.md"), raw)
	lockWrite(t, filepath.Join(dir, "flow.lock"), "apiVersion: flow-lock/v1\nresources: []\n")
	reports, err := guides.MigrateLegacy()
	if err != nil || len(reports) != 1 {
		t.Fatal(reports, err)
	}
	got, err := guides.Get("shared-legacy")
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"海岸线数据", "海岸线", "NAME", "真实海岸边界", "2020年", "1:5万", "背景定位", strings.TrimSpace(doc)} {
		if !strings.Contains(got.Guide.Body, text) {
			t.Fatalf("public content missing %q: %s", text, got.Guide.Body)
		}
	}
	if strings.Contains(got.Guide.Body, "private/coast.shp") || len(got.Guide.Resources) != 0 {
		t.Fatal("old data resource still an execution binding", got.Guide)
	}
	original, err := os.ReadFile(filepath.Join(dir, "FLOW.md.bak"))
	if err != nil || string(original) != raw {
		t.Fatal("flow source backup lost", err)
	}
	saved, err := os.ReadFile(filepath.Join(root, ".backups/resources-v5/data/shared.csv"))
	if err != nil || string(saved) != csv {
		t.Fatal("library backup changed", err)
	}
	reports, err = guides.MigrateLegacy()
	if err != nil || len(reports) != 0 {
		t.Fatal("public migration repeated", reports, err)
	}
}

func TestStandaloneMarkdownWithLegacyLookingProseIsPreserved(t *testing.T) {
	raw := "---\n名称: 用户自由文档\n说明: 普通操作说明\n版本: 1.0.0\n---\n## 步骤\n### 第一步\n- 做法：这句话必须原样保留。\n- 完成标准：用户自己的自然语言说明。\n"
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "markdown", true: "unmarked-folder"}[directory], func(t *testing.T) {
			s := guideTestStore(t)
			source := filepath.Join(t.TempDir(), "user-flow.md")
			if directory {
				source = t.TempDir()
				lockWrite(t, filepath.Join(source, "FLOW.md"), raw)
			} else {
				lockWrite(t, source, raw)
			}
			got, err := s.Import(source, ImportOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if got.Guide.Body != ParseGuide(raw).Body || got.Meta.Version != "1.0.0" {
				t.Fatal("ambiguous free prose was rewritten", got.Guide)
			}
			if got.ConversionReport == nil || got.ConversionReport.Converted || !strings.Contains(got.ConversionReport.Message, "保留原文") {
				t.Fatal("ambiguity was not reported", got.ConversionReport)
			}
			reports, err := s.MigrateLegacy()
			if err != nil || len(reports) != 0 {
				t.Fatal("preserved import later converted", reports, err)
			}
		})
	}
}
