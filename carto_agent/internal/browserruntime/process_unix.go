//go:build !windows

package browserruntime

import (
	"context"
	"os/exec"
	"syscall"
)

func newCommand(ctx context.Context, command string, args []string) (*exec.Cmd, error) {
	// CommandContext defers checking absolute paths until Start. LookPath also
	// checks explicit paths, so environment diagnostics cannot report a missing
	// or non-executable file as an available command.
	resolved, err := exec.LookPath(command)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, resolved, args...)
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	return cmd, nil
}

func isolateProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func killProcessTree(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
