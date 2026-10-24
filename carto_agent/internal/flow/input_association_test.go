package flow

import (
	"strings"
	"testing"
)

func TestDataFolderInputDeclarationValidation(t *testing.T) {
	raw := "---\n名称: 数据输入\n编号: data-input\n---\n## 使用的资源\n- 数据清单「数据说明」：resources/data/list.csv（文件所在文件夹：「底图」）\n## 需要准备\n- 底图（必填，文件夹）\n## 步骤\n### 加载\n- 负责：用户\n- 做法：检查「数据说明」\n"
	doc := Parse(raw)
	if len(doc.Resources) != 1 || doc.Resources[0].Location != "resources/data/list.csv" || doc.Resources[0].FolderInput != "底图" {
		t.Fatal(doc.Resources)
	}
	for _, d := range doc.Diagnostics {
		if d.Level == "error" {
			t.Fatal(d)
		}
	}
	for _, bad := range []string{strings.Replace(raw, "（必填，文件夹）", "（必填，文件）", 1), strings.Replace(raw, "：「底图」", "：「不存在」", 1), strings.Replace(raw, "- 数据清单", "- 样式集", 1)} {
		parsed := Parse(bad)
		found := false
		for _, d := range parsed.Diagnostics {
			if d.Code == "FLOW_DATA_INPUT_INVALID" {
				found = true
			}
		}
		if !found {
			t.Fatal(parsed)
		}
	}
}
