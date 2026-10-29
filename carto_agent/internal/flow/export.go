package flow

import (
	"archive/zip"
	"context"
	"fmt"
	"github.com/autocarto/carto_agent/internal/resource"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type ExportOptions struct {
	IncludeDataFiles   bool // Deprecated compatibility field, ignored: flow packages never contain data.
	IncludeLibraryDeps bool
	Library            *resource.Library
}

func (s *Store) Export(id, target string, includeDataFiles bool) (string, error) {
	return s.ExportWithOptions(id, target, ExportOptions{IncludeDataFiles: includeDataFiles, IncludeLibraryDeps: true})
}
func (s *Store) ExportWithOptions(id, target string, options ExportOptions) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.get(id)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) || !strings.EqualFold(filepath.Ext(target), ".zip") {
		return "", fmt.Errorf("导出路径必须是 ZIP 文件的绝对路径")
	}
	if err = rejectSymlinks(filepath.Dir(target)); err != nil {
		return "", err
	}
	if _, err = os.Lstat(target); err == nil {
		return "", fmt.Errorf("目标文件已存在，请选择新路径")
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	stage, err := os.MkdirTemp("", "autocarto-flow-export-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	if err = os.WriteFile(filepath.Join(stage, "FLOW.md"), []byte(rewriteMetadata(item.FlowMD, id, item.Meta.Name)), 0600); err != nil {
		return "", err
	}
	lock, err := ReadLock(item.Dir)
	if err != nil {
		return "", err
	}

	doc := resourceDocumentWithLock(item.FlowMD, options.Library, lock)
	lock = retainedLock(lock, doc)
	lock.DocumentFormat = documentFormatV6
	if options.Library != nil {
		_, lock = resolveVisualResources(context.Background(), id, item.Dir, doc, lock, options.Library)
	}
	// The exported package has only its document, lock and referenced visual
	// dependency snapshots. User data, backups and unreferenced files stay local.
	for i := range lock.Resources {
		e := &lock.Resources[i]
		if e.Source == "bundled" && e.Bundled != "" {
			if e.Type != "style" && e.Type != "layout" {
				continue
			}
			target := "deps/" + e.ID + "/"
			if err = resource.CopyPackage(filepath.Join(item.Dir, filepath.FromSlash(e.Bundled)), e.Type, filepath.Join(stage, filepath.FromSlash(target))); err != nil {
				return "", err
			}
			e.Bundled = target
		}
	}
	if options.IncludeLibraryDeps {
		packed := map[string]string{}
		for _, e := range lock.Resources {
			if e.Source == "bundled" {
				packed[e.ID] = e.Digest
			}
		}
		for i := range lock.Resources {
			e := &lock.Resources[i]
			if e.Source == "bundled" || !resource.IsPublicType(e.Type) {
				continue
			}
			if options.Library == nil {
				return "", fmt.Errorf("导出公共依赖需要资源库")
			}
			deps := filepath.Join(stage, "deps")
			if err = os.MkdirAll(deps, 0700); err != nil {
				return "", err
			}
			pending, err := os.MkdirTemp(deps, ".resource-bundle-")
			if err != nil {
				return "", err
			}
			r, err := options.Library.Bundle(context.Background(), resource.Ref{Type: e.Type, Name: e.Name, ID: e.ID, Location: "公共资源库"}, resource.Scope{FlowID: id, FlowDir: item.Dir, Lock: lock}, pending)
			if err != nil {
				return "", err
			}
			// Different labels may resolve to the same effective override. Publish one
			// snapshot per effective identity/digest rather than renaming it twice.
			if digest, exists := packed[r.ID]; exists {
				if digest != r.Digest {
					return "", fmt.Errorf("同一资源在导出期间出现不同内容，请重新导出：%s", r.ID)
				}
				if err = os.RemoveAll(pending); err != nil {
					return "", err
				}
			} else {
				dest := filepath.Join(deps, r.ID)
				if err = os.RemoveAll(dest); err != nil {
					return "", err
				}
				if err = os.Rename(pending, dest); err != nil {
					return "", err
				}
				packed[r.ID] = r.Digest
			}
			// Only the exported snapshot receives current effective version/digest.
			*e = resource.LockResource(e.Type, e.Name, r)
			e.Bundled = "deps/" + e.ID + "/"
		}
	}

	if err = writeLock(stage, lock); err != nil {
		return "", err
	}
	out, err := os.CreateTemp(filepath.Dir(target), ".flow-export-*.zip")
	if err != nil {
		return "", err
	}
	temp := out.Name()
	defer os.Remove(temp)
	w := zip.NewWriter(out)
	var total int64
	err = filepath.WalkDir(stage, func(path string, e fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(stage, path)
		if e.IsDir() {
			if rel != "." && strings.HasPrefix(e.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if e.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("不接受符号链接")
		}
		i, err := e.Info()
		if err != nil {
			return err
		}
		if !i.Mode().IsRegular() {
			return fmt.Errorf("不接受特殊文件")
		}

		total += i.Size()
		if i.Size() > MaxFileBytes || total > MaxPackageBytes {
			return fmt.Errorf("导出流程包超过大小上限")
		}
		h, err := zip.FileInfoHeader(i)
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(filepath.Join(id, rel))
		h.Method = zip.Deflate
		entry, err := w.CreateHeader(h)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(entry, in)
		closeErr := in.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	zipErr := w.Close()
	syncErr := out.Sync()
	closeErr := out.Close()
	if err != nil {
		return "", err
	}
	if zipErr != nil {
		return "", zipErr
	}
	if syncErr != nil {
		return "", syncErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	// Link publishes without replacing an existing user file created concurrently.
	if err = os.Link(temp, target); err != nil {
		return "", err
	}
	return target, nil
}
