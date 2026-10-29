package resource

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestLayoutMapWidthUsesRealMarginsAndPaper(t *testing.T) {
	entry, err := filepath.Abs(filepath.Join("..", "..", "..", "autocarto", "resources", "system", "layouts", "四角对齐版式", "四角对齐版式.layout.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		paper, orientation string
		width              float64
	}{{"", "", 714.85}, {"A4", "landscape", 252.45}, {"A4", "portrait", 178.5}} {
		width, err := LayoutMapWidthMM(entry, tc.paper, tc.orientation)
		if err != nil || math.Abs(width-tc.width) > .0001 {
			t.Fatalf("%+v: width=%f err=%v", tc, width, err)
		}
	}
	// A different user layout must use its own margins, not the built-in ratio.
	custom := filepath.Join(t.TempDir(), "custom.layout.yaml")
	if err := os.WriteFile(custom, []byte("page:\n  default_paper: A4\n  reference_size_mm: {width: 297, height: 210}\n  margins_mm: {left: 50, right: 50}\nelements: {main_map: {fill: content_area}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if width, err := LayoutMapWidthMM(custom, "", ""); err != nil || width != 197 {
		t.Fatalf("custom width=%f err=%v", width, err)
	}
}

func TestLayoutMapWidthReadsQPTMainMapNotPage(t *testing.T) {
	entry := filepath.Join(t.TempDir(), "custom.qpt")
	if err := os.WriteFile(entry, []byte(`<Layout><PageCollection><LayoutItem size="297,210,mm"/></PageCollection><LayoutItem id="main_map" size="185,140,mm"/><LayoutItem id="inset" size="30,20,mm"/></Layout>`), 0600); err != nil {
		t.Fatal(err)
	}
	if width, err := LayoutMapWidthMM(entry, "", ""); err != nil || width != 185 {
		t.Fatalf("QPT main map width=%f err=%v", width, err)
	}
}
