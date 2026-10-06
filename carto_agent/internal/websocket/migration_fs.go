package websocket

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func movePathWithCrossDeviceFallback(srcPath, dstPath string) error {
	if err := os.Rename(srcPath, dstPath); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return err
	}

	info, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := copyDirRecursive(srcPath, dstPath, info.Mode()); err != nil {
			return err
		}
		return os.RemoveAll(srcPath)
	}
	if err := copyFileRecursive(srcPath, dstPath, info.Mode()); err != nil {
		return err
	}
	return os.Remove(srcPath)
}

func copyDirRecursive(srcDir, dstDir string, mode os.FileMode) error {
	if err := os.MkdirAll(dstDir, mode.Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcChild := filepath.Join(srcDir, entry.Name())
		dstChild := filepath.Join(dstDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := copyDirRecursive(srcChild, dstChild, info.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := copyFileRecursive(srcChild, dstChild, info.Mode()); err != nil {
			return err
		}
	}
	return nil
}

func copyFileRecursive(srcFile, dstFile string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dstFile), 0o755); err != nil {
		return err
	}
	src, err := os.Open(srcFile)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return nil
}
