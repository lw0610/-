package fileops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/sessionfiles"
	"github.com/autocarto/carto_agent/internal/skill"
)

type workspace struct {
	root      string
	output    string
	readRoots []string
	readFiles []string
	files     *sessionfiles.Store
}

// Roots are derived from the trusted session, never session_dir supplied in a
// model tool call. Shell processes are local processes, not filesystem sandboxes.
func sessionWorkspace(ctx context.Context) (workspace, error) {
	session := skill.SessionContextFromContext(ctx)
	if session.SessionID == "" {
		return workspace{}, fmt.Errorf("文件工具需要当前会话，不能用工具参数提供 session_id 替代")
	}
	root, err := config.ResolveSessionDir(session.ProjectPath, session.SessionID)
	if err != nil {
		return workspace{}, err
	}
	w := workspace{root: root, output: filepath.Join(root, "output"), readRoots: []string{root}}
	// data_get/data_register and confirmed plan inputs already register exact
	// files here, including acquired files outside the session directory.
	w.files, err = sessionfiles.New(session.ProjectPath, session.SessionID)
	if err != nil {
		return workspace{}, err
	}
	if p := session.ProjectPath; filepath.IsAbs(p) {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			w.readRoots = append(w.readRoots, p)
		} else {
			w.readRoots = append(w.readRoots, filepath.Dir(p))
		}
	}
	for _, layer := range session.Layers {
		p := strings.SplitN(layer.Path, "|", 2)[0]
		if filepath.IsAbs(p) {
			w.readFiles = append(w.readFiles, p)
		}
	}
	return w, nil
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

func sensitivePath(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		name := strings.ToLower(part)
		if name == ".env" || strings.HasPrefix(name, ".env.") || name == ".ssh" || name == ".aws" || name == ".gnupg" || name == ".master_key" {
			return true
		}
	}
	return false
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
	if sensitivePath(p) {
		return "", fmt.Errorf("文件工具不能读取或覆盖凭据文件")
	}
	roots := w.readRoots
	if write {
		root, rootErr := canonical(w.root)
		output, outputErr := canonical(w.output)
		if rootErr != nil || outputErr != nil || !within(root, output) {
			return "", fmt.Errorf("会话 output 目录不能通过符号链接移到会话之外")
		}
		roots = []string{w.output}
	}
	for _, root := range roots {
		r, err := canonical(root)
		if err == nil && within(r, p) {
			return p, nil
		}
	}
	if !write {
		for _, file := range w.readFiles {
			if f, err := canonical(file); err == nil && f == p {
				return p, nil
			}
		}
		if w.files != nil {
			if path, err := w.files.ResolveProvidedPath(p); err == nil {
				return path, nil
			}
			if item, err := w.files.GetPath(p); err == nil && item.Exists {
				return item.Path, nil
			}
		}
	}
	if write {
		return "", fmt.Errorf("写入路径必须在当前会话 output 目录内: %s", w.output)
	}
	return "", fmt.Errorf("读取路径不在当前会话或已授权的工程范围内")
}
