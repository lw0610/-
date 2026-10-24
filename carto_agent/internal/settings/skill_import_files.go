package settings

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/skill"
)

// Resolve existing parents as well, so /tmp and /private/tmp (or a symlinked
// package root) cannot bypass the source/target overlap check.
func canonicalSkillImportPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := abs
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func skillImportContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func validateSkillImportPaths(sourceFile, sourceDir, targetDir string) error {
	if info, err := os.Lstat(targetDir); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("目标技能包必须是普通文件夹，不能是符号链接: %s", targetDir)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	source := sourceFile
	if sourceDir != "" {
		source = sourceDir
		info, err := os.Lstat(sourceDir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("源技能包必须是普通文件夹: %s", sourceDir)
		}
	}
	canonicalSource, err := canonicalSkillImportPath(source)
	if err != nil {
		return err
	}
	canonicalTarget, err := canonicalSkillImportPath(targetDir)
	if err != nil {
		return err
	}
	if skillImportContains(canonicalTarget, canonicalSource) || (sourceDir != "" && skillImportContains(canonicalSource, canonicalTarget)) {
		return fmt.Errorf("源技能包与目标目录不能相同或相互包含，请选择外部技能包副本")
	}
	return nil
}

// Prepare and validate an entire package before replacing the target. The
// temporary container itself has no SKILL.md, so catalog scans cannot expose
// an incomplete staged package or its rollback copy as an installed skill.
func installSkillImport(sourceDir, targetDir, markdown string) error {
	if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
		return err
	}
	container, err := os.MkdirTemp(filepath.Dir(targetDir), ".skill-import-")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(container)
		}
	}()
	staged := filepath.Join(container, "package")
	copyFrom := sourceDir
	// Replacing a standalone Markdown document should preserve the existing
	// package's references; replacing a whole folder replaces its full contents.
	if copyFrom == "" {
		if _, err := os.Stat(targetDir); err == nil {
			copyFrom = targetDir
		}
	}
	if copyFrom != "" {
		if err := copySkillImportDirectory(copyFrom, staged); err != nil {
			return err
		}
	} else if err := os.MkdirAll(staged, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staged, "SKILL.md"), []byte(markdown), 0o644); err != nil {
		return err
	}
	if err := skill.NewRegistry().LoadDirectories(staged); err != nil {
		return fmt.Errorf("技能包校验失败，原有技能未修改: %w", err)
	}
	backup := filepath.Join(container, "previous")
	hadPrevious := false
	if _, err := os.Lstat(targetDir); err == nil {
		if err := os.Rename(targetDir, backup); err != nil {
			return err
		}
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(staged, targetDir); err != nil {
		if hadPrevious {
			if restoreErr := os.Rename(backup, targetDir); restoreErr != nil {
				cleanup = false
				return fmt.Errorf("导入失败: %v；恢复失败: %v；原包保存在 %s", err, restoreErr, backup)
			}
		}
		return fmt.Errorf("导入失败，原包已保留: %w", err)
	}
	return nil
}

func copySkillImportDirectory(sourceDir, targetDir string) error {
	return filepath.WalkDir(sourceDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(targetDir, rel)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("技能包中不支持符号链接，请将引用文件放入包内: %s", rel)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("技能包中不支持非普通文件: %s", rel)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm()|0o600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}
