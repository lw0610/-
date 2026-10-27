package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// RunIsolatedTests prevents tests from inheriting a developer's writable runtime
// or another checkout's assets. Individual fixtures may still override paths.
func RunIsolatedTests(m *testing.M) int {
	home, err := os.MkdirTemp("", "autocarto-tests-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(home)
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, "cannot locate test project")
		return 1
	}
	project := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	for name, value := range map[string]string{"AUTOCARTO_HOME": home, "AUTOCARTO_PROJECT_ROOT": project, "AUTOCARTO_ASSETS_ROOT": filepath.Join(project, "autocarto"), "AUTOCARTO_DISABLE_KEYRING": "1"} {
		if err := os.Setenv(name, value); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	return m.Run()
}
