package fileops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/skill"
)

type workspace struct {
	root   string
	output string
}

// The session determines where generated outputs go, not which local inputs
// can be read. Shell processes are local processes, not filesystem sandboxes.
func sessionWorkspace(ctx context.Context) (workspace, error) {
	session := skill.SessionContextFromContext(ctx)
	if session.SessionID == "" {
		return workspace{}, fmt.Errorf("文件工具需要当前会话，不能用工具参数提供 session_id 替代")
	}
	root, err := config.ResolveSessionDir(session.ProjectPath, session.SessionID)
	if err != nil {
		return workspace{}, err
	}
	return workspace{root: root, output: filepath.Join(root, "output")}, nil
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Resolve existing ancestors too, so a symlink in a not-yet-created destination
// cannot move a write outside the session output directory.
func canonical(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		missing = append(missing, filepath.Base(path))
		path = parent
	}
}

func (w workspace) resolve(raw string, write bool) (string, error) {
	if strings.TrimSpace(raw) == "" || strings.ContainsRune(raw, '\x00') {
		return "", fmt.Errorf("path 不能为空或包含空字符")
	}
	p := raw
	if !filepath.IsAbs(p) {
		p = filepath.Join(w.output, p)
	}
	p, err := canonical(p)
	if err != nil {
		return "", err
	}
	if !write {
		return p, nil
	}
	root, rootErr := canonical(w.root)
	output, outputErr := canonical(w.output)
	if rootErr != nil || outputErr != nil || !within(root, output) {
		return "", fmt.Errorf("会话 output 目录不能通过符号链接移到会话之外")
	}
	if !within(output, p) {
		return "", fmt.Errorf("写入路径必须在当前会话 output 目录内: %s", w.output)
	}
	return p, nil
}
