package deepagent

import (
	"context"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuideBackendLocalContentAndDisabledIndex(t *testing.T) {
	root := t.TempDir()
	s := flow.NewGuideStore(flow.NewStore("", filepath.Join(root, "flows"), nil), filepath.Join(root, "guides.yaml"))
	raw := "---\n名称: 地图知识\n编号: test-guide\n说明: 描述适用请求\n---\n## 适用任务\n数据说明\n## 数据\n无\n## 制作要点\n核验数据\n## 产出与检查\n- 「结果」（文本）\n"
	if _, err := s.Create(raw, "test-guide"); err != nil {
		t.Fatal(err)
	}
	b := &GuideBackend{Store: s, Resources: resource.NewLibrary("", "", nil)}
	items, err := b.List(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("%+v %v", items, err)
	}
	got, err := b.Get(context.Background(), GuideSkillName("test-guide"))
	if err != nil || !strings.Contains(got.Content, "## 适用任务\n数据说明\n## 数据\n无\n## 制作要点\n核验数据") || !strings.Contains(got.Content, "引用的样式与版面") {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err = s.SetEnabled("test-guide", false); err != nil {
		t.Fatal(err)
	}
	items, err = b.List(context.Background())
	if err != nil || len(items) != 0 {
		t.Fatal(items, err)
	}
	if _, err = b.Get(context.Background(), GuideSkillName("test-guide")); err == nil {
		t.Fatal("disabled guide readable")
	}
}
