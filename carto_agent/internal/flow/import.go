package flow

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/resource"
	"gopkg.in/yaml.v3"
)

type ImportOptions struct {
	Overwrite bool   `json:"overwrite"`
	NewID     string `json:"new_id"`
}

func metadataYAML(meta FlowMetadata) string {
	b, _ := yaml.Marshal(map[string]string{"编号": meta.ID, "名称": meta.Name, "说明": meta.Description, "版本": meta.Version, "作者": meta.Author, "标签": strings.Join(meta.Tags, "、")})
	return "---\n" + string(b) + "---\n"
}

func (s *Store) Import(source string, options ImportOptions, libraries ...*resource.Library) (*StoredFlow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := rejectSymlinks(source); err != nil {
		return nil, err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return nil, err
	}
	root := source
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("只接受目录、ZIP 或 FLOW.md")
		}
		switch strings.ToLower(filepath.Ext(source)) {
		case ".md":
			raw, err := readBounded(source, MaxFileBytes)
			if err != nil {
				return nil, err
			}
			id, err := PackageID(string(raw), strings.TrimSuffix(filepath.Base(source), filepath.Ext(source)), options.NewID)
			if err != nil {
				return nil, err
			}
			return s.write(string(raw), id, "", options.Overwrite)
		case ".zip":
			temp, err := os.MkdirTemp("", "autocarto-flow-import-")
			if err != nil {
				return nil, err
			}
			defer os.RemoveAll(temp)
			if err = extractZIP(source, temp); err != nil {
				return nil, err
			}
			root, err = packageRoot(temp)
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("只接受目录、ZIP 或 FLOW.md")
		}
	}
	raw, err := readBounded(packageMainPath(root), MaxFileBytes)
	if err != nil {
		return nil, err
	}
	id, err := PackageID(string(raw), filepath.Base(root), options.NewID)
	if err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp("", "autocarto-flow-deps-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	if err = copyTree(root, stage, false); err != nil {
		return nil, err
	}
	var library *resource.Library
	if len(libraries) > 0 {
		library = libraries[0]
	}
	if err = importDependencies(stage, library); err != nil {
		return nil, err
	}
	return s.write(string(raw), id, stage, options.Overwrite)
}

func packageRoot(root string) (string, error) {
	if _, err := os.Stat(packageMainPath(root)); err == nil {
		return root, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	if len(entries) == 1 && entries[0].IsDir() {
		candidate := filepath.Join(root, entries[0].Name())
		if _, err = os.Stat(packageMainPath(candidate)); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("ZIP 顶层应包含 FLOW.md，或只包含一个流程包目录")
}

func extractZIP(source, target string) error {
	r, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer r.Close()
	var total int64
	for _, file := range r.File {
		name := strings.TrimSuffix(file.Name, "/")
		if name == "" {
			continue
		}
		rel, err := safeRelative(name)
		if err != nil {
			return err
		}
		if strings.Contains(rel, ":") {
			return fmt.Errorf("ZIP 包含非法路径")
		}
		if file.Mode()&os.ModeSymlink != 0 || (!file.Mode().IsRegular() && !file.FileInfo().IsDir()) {
			return fmt.Errorf("ZIP 不接受符号链接或特殊文件")
		}
		if file.UncompressedSize64 > uint64(MaxFileBytes) {
			return fmt.Errorf("ZIP 文件超过 200 MB")
		}
		dest := filepath.Join(target, rel)
		if file.FileInfo().IsDir() {
			if err = os.MkdirAll(dest, 0700); err != nil {
				return err
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			in.Close()
			return err
		}
		n, copyErr := io.Copy(out, io.LimitReader(in, MaxFileBytes+1))
		inErr := in.Close()
		outErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if inErr != nil {
			return inErr
		}
		if outErr != nil {
			return outErr
		}
		total += n
		if n > MaxFileBytes || total > MaxPackageBytes {
			return fmt.Errorf("流程包超过大小上限")
		}
	}
	return nil
}

func importDependencies(dir string, library *resource.Library) error {
	lock, err := ReadLock(dir)
	if err != nil {
		return err
	}
	processed := map[string]resource.LockEntry{}
	for i := range lock.Resources {
		e := &lock.Resources[i]
		if e.Bundled == "" {
			continue
		}
		key := e.Bundled + "\x00" + e.ID + "\x00" + e.Digest
		if prior, ok := processed[key]; ok {
			e.Source = prior.Source
			e.Bundled = prior.Bundled
			continue
		}
		root := filepath.Join(dir, filepath.FromSlash(e.Bundled))
		if err = rejectSymlinks(root); err != nil {
			return err
		}
		if _, err = resource.BundledPath(root, e.Type); err != nil {
			return err
		}
		if library != nil {
			current := library.Resolve(context.Background(), resource.Ref{Type: e.Type, ID: e.ID, Name: e.Name, Location: "公共资源库"}, resource.Scope{})
			if current.ID == e.ID && !resource.HasErrors(current.Problems) && current.Digest == e.Digest {
				if err = os.RemoveAll(root); err != nil {
					return err
				}
				e.Source = current.Owner
				e.Bundled = ""
				processed[key] = *e
				continue
			}
		}
		e.Source = "bundled"
		// Validate the actual bundle before publishing the imported flow. Its
		// manifest must identify the locked resource and its digest must match.
		resolver := library
		if resolver == nil {
			resolver = resource.NewLibrary("", "", nil)
		}
		r := resolver.Resolve(context.Background(), resource.Ref{Type: e.Type, Name: e.Name, Location: "公共资源库"}, resource.Scope{FlowDir: dir, Lock: lock})
		if r.ID != e.ID || r.Digest != e.Digest {
			return fmt.Errorf("随包依赖与锁定摘要不符：%s", e.Name)
		}
		processed[key] = *e
	}
	return writeLock(dir, lock)
}
