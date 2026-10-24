package runtimecontrol

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/resource"
)

const maxReferenceBytes = 200 << 10

type referenceRoot struct{ path, kind, anchor string }

type referencePackageDirsKey struct{}

// WithReferencePackageDirs supplies the same trusted, low-to-high priority
// package roots used by this runtime's registry and skill middleware.
func WithReferencePackageDirs(ctx context.Context, dirs []string) context.Context {
	return context.WithValue(ctx, referencePackageDirsKey{}, append([]string(nil), dirs...))
}

func referenceRoots(ctx context.Context) ([]referenceRoot, error) {
	assets, err := config.ResolveAssetsRoot()
	if err != nil {
		return nil, err
	}
	home, err := config.ResolveAutoCartoHome()
	if err != nil {
		return nil, err
	}
	configDir, err := config.ResolveAutoCartoConfigDir()
	if err != nil {
		return nil, err
	}
	libraryRoot, err := resource.BuiltinDirectory()
	if err != nil {
		return nil, err
	}
	roots := []referenceRoot{
		{filepath.Join(assets, "skills", "system"), "package", assets},
		{filepath.Join(assets, "skills", "user"), "package", assets},
		{config.ResolveGlobalSkillsDir(configDir), "package", home},
		{filepath.Join(assets, "flows", "system"), "flow", assets},
		{filepath.Join(home, "flows"), "flow", home},
		{libraryRoot, "library", assets},
		{filepath.Join(home, "resources"), "library", home},
	}
	if dirs, supplied := ctx.Value(referencePackageDirsKey{}).([]string); supplied {
		// The runtime provides roots, never the model's arguments. Replace the
		// default package roots so inactive exclusive packages cannot win.
		roots = roots[3:]
		for _, dir := range dirs {
			if strings.TrimSpace(dir) == "" {
				continue
			}
			absolute, err := config.ResolveProjectPath(dir)
			if err != nil {
				return nil, err
			}
			anchor := absolute
			if rel, err := filepath.Rel(assets, absolute); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				anchor = assets
			}
			if rel, err := filepath.Rel(home, absolute); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				anchor = home
			}
			roots = append(roots, referenceRoot{absolute, "package", anchor})
		}
	}
	return roots, nil
}

func referenceRelative(root referenceRoot, path string) (string, bool) {
	rel, err := filepath.Rel(root.path, path)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	switch root.kind {
	case "package":
		if len(parts) < 2 {
			return "", false
		}
		marker, err := os.Lstat(filepath.Join(root.path, parts[0], "SKILL.md"))
		return rel, err == nil && marker.Mode().IsRegular()
	case "flow":
		if len(parts) < 3 || parts[1] != "resources" {
			return "", false
		}
		if parts[2] == "docs" && len(parts) >= 4 {
			return rel, true
		}
		ext := strings.ToLower(filepath.Ext(path))
		return rel, ext == ".csv" || ext == ".json" || ext == ".yaml"
	case "library":
		return rel, true
	}
	return "", false
}

func rejectReferenceLinks(root, relative string) error {
	current := root
	// Check the approved root itself and every child component. Ancestors of
	// an approved root may use platform aliases (for example macOS temp roots).
	for _, part := range append([]string{""}, strings.Split(relative, string(filepath.Separator))...) {
		if part != "" {
			current = filepath.Join(current, part)
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("参考资料不允许符号链接")
		}
	}
	return nil
}

func readReference(ctx context.Context, rawPath string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := strings.TrimSpace(rawPath)
	if path == "" || strings.ContainsRune(path, '\x00') {
		return nil, fmt.Errorf("参考资料路径为空或无效")
	}
	for _, part := range strings.Split(strings.ReplaceAll(filepath.ToSlash(path), `\`, "/"), "/") {
		if part == ".." {
			return nil, fmt.Errorf("参考资料路径不允许越界")
		}
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".txt", ".csv", ".json", ".yaml":
	default:
		return nil, fmt.Errorf("参考资料仅支持 .md、.txt、.csv、.json、.yaml")
	}
	roots, err := referenceRoots(ctx)
	if err != nil {
		return nil, err
	}
	var selected, selectedRoot, relative string
	if !filepath.IsAbs(path) {
		// Choose the whole highest-priority package first. A missing reference
		// in that package must not silently fall back to stale bundled content.
		for i := len(roots) - 1; i >= 0; i-- {
			root := roots[i]
			if root.kind != "package" {
				continue
			}
			candidate := filepath.Join(root.path, filepath.FromSlash(path))
			if _, allowed := referenceRelative(root, candidate); allowed {
				roots = []referenceRoot{root}
				break
			}
		}
	}
	for _, root := range roots {
		candidate := path
		if !filepath.IsAbs(path) {
			if root.kind != "package" {
				continue
			}
			candidate = filepath.Join(root.path, filepath.FromSlash(path))
		}
		_, allowed := referenceRelative(root, candidate)
		if !allowed {
			continue
		}
		rel, relErr := filepath.Rel(root.anchor, candidate)
		if relErr != nil {
			return nil, relErr
		}
		if err := rejectReferenceLinks(root.anchor, rel); err != nil {
			if os.IsNotExist(err) && !filepath.IsAbs(path) {
				continue
			}
			return nil, err
		}
		selected, selectedRoot, relative = candidate, root.anchor, rel
	}
	if selected == "" {
		return nil, fmt.Errorf("参考资料不在允许目录内或文件不存在")
	}
	if err := rejectReferenceLinks(selectedRoot, relative); err != nil {
		return nil, err
	}
	info, err := os.Lstat(selected)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("参考资料必须是普通文件")
	}
	file, err := os.Open(selected)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("参考资料必须是普通文件")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxReferenceBytes+1))
	if err != nil {
		return nil, err
	}
	truncated := len(data) > maxReferenceBytes
	if truncated {
		data = data[:maxReferenceBytes]
		for n := 0; n < utf8.UTFMax-1 && !utf8.Valid(data); n++ {
			data = data[:len(data)-1]
		}
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), '\x00') {
		return nil, fmt.Errorf("参考资料必须是 UTF-8 文本")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	warning := ""
	if truncated {
		warning = "内容超过 200 KiB，已截断；请按章节拆分参考资料后读取。"
	}
	return map[string]any{"success": true, "path": selected, "text": string(data), "truncated": truncated, "warning": warning}, nil
}
