//go:build !windows

package browserruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentRejectsNonExecutableFile(t *testing.T) {
	command := filepath.Join(t.TempDir(), "node-without-execute-permission")
	if err := os.WriteFile(command, []byte("not executable"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newCommand(context.Background(), command, nil); err == nil {
		t.Fatal("non-executable file was marked available")
	}
}
