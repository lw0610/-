package browserruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartupReportsMissingWorkingDirectory(t *testing.T) {
	opts := fakeOptions(t, "missing-working-directory")
	// Simulate a directory disappearing after prepareOutputDir, before exec.
	if err := os.Remove(opts.OutputDir); err != nil {
		t.Fatal(err)
	}
	if _, err := newCommand(context.Background(), opts.Command, opts.Args); err != nil {
		t.Fatalf("test command must exist: %v", err)
	}
	e := &browserSession{}
	defer e.close()
	err := e.connect(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "浏览器工作目录已不存在") || !strings.Contains(err.Error(), opts.OutputDir) {
		t.Fatalf("missing working directory was not identified: %v", err)
	}
	if strings.Contains(err.Error(), "找不到可执行的浏览器启动命令") {
		t.Fatalf("existing command was blamed for missing working directory: %v", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("original missing-path cause was lost: %v", err)
	}
}

func TestStartupReportsWorkingDirectoryReplacedByFile(t *testing.T) {
	opts := fakeOptions(t, "working-directory-is-file")
	if err := os.Remove(opts.OutputDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opts.OutputDir, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	e := &browserSession{}
	defer e.close()
	err := e.connect(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "浏览器工作目录不是目录") || !strings.Contains(err.Error(), opts.OutputDir) {
		t.Fatalf("invalid working directory was not identified: %v", err)
	}
	var cause *os.PathError
	if !errors.As(err, &cause) {
		t.Fatalf("original process error was lost: %v", err)
	}
}

func TestStartupReportsActualMissingCommand(t *testing.T) {
	opts := fakeOptions(t, "missing-command")
	opts.Command = filepath.Join(t.TempDir(), "missing-browser-command")
	e := &browserSession{}
	defer e.close()
	err := e.connect(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "找不到可执行的浏览器启动命令") || !strings.Contains(err.Error(), opts.Command) {
		t.Fatalf("missing command was not identified: %v", err)
	}
	if strings.Contains(err.Error(), "工作目录") {
		t.Fatalf("valid working directory was blamed for a missing command: %v", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("original command lookup cause was lost: %v", err)
	}
}

func TestStartupFailureHelper(t *testing.T) {
	for _, arg := range os.Args {
		if arg == "autocarto-startup-failure-helper" {
			fmt.Fprintln(os.Stderr, "fixture startup failure detail")
			os.Exit(9)
		}
	}
}

func TestStartupFailurePreservesStderrWithoutAssumingMissingDependency(t *testing.T) {
	opts := fakeOptions(t, "handshake-failure")
	opts.Args = []string{"-test.run=^TestStartupFailureHelper$", "--", "autocarto-startup-failure-helper"}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	e := &browserSession{}
	defer e.close()
	err := e.connect(ctx, opts)
	if err == nil || !strings.Contains(err.Error(), "浏览器服务启动失败") || !strings.Contains(err.Error(), "fixture startup failure detail") {
		t.Fatalf("startup failure or stderr was lost: %v", err)
	}
	for _, misleading := range []string{"找不到可执行", "需要 Node.js", "工作目录"} {
		if strings.Contains(err.Error(), misleading) {
			t.Fatalf("startup error assumed an unverified cause %q: %v", misleading, err)
		}
	}
	if errors.Unwrap(err) == nil {
		t.Fatalf("original handshake cause was lost: %v", err)
	}
}
