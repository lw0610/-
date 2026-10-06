package websocket

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	runtimeHome, err := os.MkdirTemp("", "autocarto-websocket-runtime-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(runtimeHome)

	if err := os.Setenv("AUTOCARTO_HOME", runtimeHome); err != nil {
		panic(err)
	}
	defer os.Unsetenv("AUTOCARTO_HOME")

	if err := os.MkdirAll(filepath.Join(runtimeHome, "config"), 0o755); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(filepath.Join(runtimeHome, "data"), 0o755); err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}
