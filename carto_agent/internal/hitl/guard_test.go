package hitl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShouldInterruptHighRiskToolForExistingExportTarget(t *testing.T) {
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "layout.pdf")
	if err := os.WriteFile(targetPath, []byte("pdf"), 0644); err != nil {
		t.Fatalf("write target file failed: %v", err)
	}

	args := `{"OUTPUT":"` + targetPath + `"}`
	if !shouldInterruptHighRiskTool("export_layout_pdf", args) {
		t.Fatal("expected existing absolute export target to require confirmation")
	}
}

func TestShouldInterruptHighRiskToolSkipsSafeExportTargets(t *testing.T) {
	if shouldInterruptHighRiskTool("export_layout_pdf", `{}`) {
		t.Fatal("missing output should not require confirmation")
	}
	if shouldInterruptHighRiskTool("export_layout_pdf", `{"OUTPUT":"layout.pdf"}`) {
		t.Fatal("relative output should not require confirmation")
	}
	if shouldInterruptHighRiskTool("export_print_layout", `{"OUTPUT":"/tmp/non-existent-layout.png"}`) {
		t.Fatal("non-existent absolute output should not require confirmation")
	}
}

func TestShouldInterruptHighRiskToolKeepsNonExportGuard(t *testing.T) {
	if !shouldInterruptHighRiskTool("layout_delete", `{}`) {
		t.Fatal("layout_delete should still require confirmation")
	}
}
