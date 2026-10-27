package geeruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquisitionTemplateRejectsPathsAndEncodesParams(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tools/system/data-acquisition/assets/gee")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, "common.py"), []byte("# common"), 0600)
	os.WriteFile(filepath.Join(dir, "dem.py"), []byte("# dem"), 0600)
	attack := "\"; import os; os.remove('x'); #"
	code, e := BuildAcquisitionScript(root, "dem.py", map[string]any{"dataset": attack})
	if e != nil || strings.Contains(code, attack) || !strings.Contains(code, "base64.b64decode") {
		t.Fatal(code, e)
	}
	if _, e = BuildAcquisitionScript(root, "../dem.py", nil); e == nil {
		t.Fatal("template traversal")
	}
}
