package flow

import (
	"path/filepath"
	"runtime"
	"testing"
)

func compilerProject(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	t.Setenv("AUTOCARTO_PROJECT_ROOT", root)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", filepath.Join(root, "autocarto"))
	return root
}

const testFlow = "---\n名称: 资源测试\n编号: test-flow\n---\n## 步骤\n### 检查\n- 做法：读取资源\n"
