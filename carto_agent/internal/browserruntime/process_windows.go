//go:build windows

package browserruntime

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func newCommand(ctx context.Context, command string, args []string) (*exec.Cmd, error) {
	resolved, err := exec.LookPath(command)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Base(resolved), "npx.cmd") {
		// Windows cannot CreateProcess a .cmd file. Invoke npm's bundled JS
		// entry directly with Node, avoiding cmd.exe quoting and injection.
		node, cli, err := resolveNpxShim(resolved)
		if err != nil {
			return nil, err
		}
		return exec.CommandContext(ctx, node, append([]string{cli}, args...)...), nil
	}
	if ext := strings.ToLower(filepath.Ext(resolved)); ext == ".cmd" || ext == ".bat" {
		return nil, fmt.Errorf("浏览器不通过命令解释器运行批处理文件，请配置 node.exe 绝对路径及 MCP cli.js 参数")
	}
	return exec.CommandContext(ctx, resolved, args...), nil
}

func isolateProcess(cmd *exec.Cmd) {}

func killProcessTree(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		_ = cmd.Process.Kill()
	}
}
