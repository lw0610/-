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
	IncludeDataFiles   bool
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
	if err = copyTree(item.Dir, stage, false); err != nil {
		return "", err
	}
	lock, err := ReadLock(stage)
	if err != nil {
		return "", err
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
			deps := filepath.Join(stage, "resources", "_deps")
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
			e.Bundled = "resources/_deps/" + e.ID + "/"
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
		if !options.IncludeDataFiles && strings.HasPrefix(filepath.ToSlash(rel), "resources/data/") {
			switch strings.ToLower(filepath.Ext(rel)) {
			case ".csv", ".xlsx", ".txt", ".md", ".yaml":
			default:
				return nil
			}
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

// AdoptResource moves an existing declaration to a private package copy in the
// same atomic transaction used by flow saves. The library original is retained.
func (s *Store) AdoptResource(ctx context.Context, id, resourceID, label string, library *resource.Library) (*StoredFlow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if item.Source != "user" {
		return nil, ErrReadOnly
	}
	value, err := library.GetByID(resourceID)
	if err != nil {
		return nil, err
	}
	if value.Owner != "library_user" || resource.IsPublicType(value.Type) {
		return nil, fmt.Errorf("只能将旧类型的用户资源移入流程")
	}
	doc := Parse(item.FlowMD)
	var declaration *ResourceDoc
	for i := range doc.Resources {
		d := &doc.Resources[i]
		if d.Label == label && publicDeclaration(*d) {
			if declaration != nil {
				return nil, fmt.Errorf("资源标签重复")
			}
			declaration = d
		}
	}
	if declaration == nil {
		return nil, fmt.Errorf("流程中没有该公共资源声明")
	}
	lock, err := ReadLock(item.Dir)
	if err != nil {
		return nil, err
	}
	linked := library.Resolve(ctx, resource.Ref{Type: declaration.Type, Name: label, Location: declaration.Location}, resource.Scope{FlowID: id, FlowDir: item.Dir, Lock: lock})
	if linked.ID != resourceID {
		return nil, fmt.Errorf("所选资源与流程声明不一致")
	}
	stage, err := os.MkdirTemp(s.UserDir, ".flow-adopt-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	if err = copyTree(item.Dir, stage, false); err != nil {
		return nil, err
	}
	typ, _ := resource.TypeInfo(value.Type)
	destination := filepath.Join(stage, "resources", typ.Group, resourceID)
	if _, err = os.Lstat(destination); err == nil {
		return nil, fmt.Errorf("流程内已存在该资源副本")
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	if err = resource.CopyPackage(value.Root, value.Type, destination); err != nil {
		return nil, err
	}
	primary, err := resource.BundledPath(destination, value.Type)
	if err != nil {
		return nil, err
	}
	relative, _ := filepath.Rel(stage, primary)
	lines := strings.Split(strings.ReplaceAll(item.FlowMD, "\r\n", "\n"), "\n")
	if declaration.Line < 1 || declaration.Line > len(lines) {
		return nil, fmt.Errorf("资源声明位置无效")
	}
	lines[declaration.Line-1] = strings.Replace(lines[declaration.Line-1], declaration.Location, filepath.ToSlash(relative), 1)
	return s.write(strings.Join(lines, "\n"), id, stage, true)
}
