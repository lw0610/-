package flow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/autocarto/carto_agent/internal/resource"
)

func TestGuideSummaryDoesNotResolveUnselectedResources(t *testing.T) {
	store := guideTestStore(t)
	item, err := store.Packages.Create("---\n名称: 摘要测试\n说明: 列表不读取未选择流程的资源\n标签: 测试\n---\n样式「未知样式」\n", "summary-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SetEnabled(item.Meta.ID, false); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(item.Dir, "conversion-report.json"), []byte(`{"converted":false,"message":"原文保留"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// An unavailable resource directory must not make the skill menu disappear.
	unreadable := filepath.Join(t.TempDir(), "resource-directory")
	if err = os.WriteFile(unreadable, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	store.Packages.SetLibrary(resource.NewLibrary(unreadable, "", nil))
	if _, err = store.Get(item.Meta.ID); err == nil {
		t.Fatal("fixture did not fail resource resolution")
	}
	items, err := store.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("%+v %v", items, err)
	}
	if items[0].Name != "摘要测试" || items[0].Enabled || len(items[0].Tags) != 1 || items[0].ConversionReport == nil || items[0].ConversionReport.Message != "原文保留" {
		t.Fatalf("metadata or enable state lost: %+v", items[0])
	}
}
