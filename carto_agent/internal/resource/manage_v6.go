package resource

import (
	"archive/zip"
	"context"
	"fmt"
	"github.com/autocarto/carto_agent/internal/config"
	"gopkg.in/yaml.v3"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type ImportResult struct {
	Items    []*ResolvedResource `json:"items"`
	Warnings []string            `json:"warnings"`
}

func (l *Library) ImportMany(source string) (*ImportResult, error) {
	path, err := config.ResolveProjectPath(source)
	if err != nil {
		return nil, err
	}
	if err = rejectResourcePath(path); err != nil {
		return nil, err
	}
	files := []string{}
	err = filepath.WalkDir(path, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = rejectResourcePath(p); err != nil {
			return err
		}
		if p != path && strings.HasPrefix(e.Name(), ".") {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if e.IsDir() {
			manifest := filepath.Join(p, "resource.yaml")
			if _, manifestErr := os.Stat(manifest); manifestErr == nil {
				if err := rejectResourcePath(manifest); err != nil {
					return err
				}
				if _, err := DetectPublicType(p); err != nil {
					return fmt.Errorf("资源包 %s 无效：%w", p, err)
				}
				// A managed package is indivisible: its QML/QPT may refer to SVG,
				// images, or other files relative to the package entry.
				files = append(files, p)
				return filepath.SkipDir
			} else if !os.IsNotExist(manifestErr) {
				return manifestErr
			}
		}
		if !e.IsDir() && (validEntryExtension("style", p) || validEntryExtension("layout", p)) {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("文件夹中没有 .qml、.qpt 或 .layout.yaml")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	result := &ImportResult{Items: []*ResolvedResource{}, Warnings: []string{}}
	complete := false
	defer func() {
		if !complete {
			for _, item := range result.Items {
				_ = os.RemoveAll(item.Root)
			}
			l.invalidate()
		}
	}()
	for _, file := range files {
		kind, err := DetectPublicType(file)
		if err != nil {
			return nil, err
		}
		source, err := sourceResource(file, kind)
		if err != nil {
			return nil, err
		}
		name, err := l.nextName(kind, source.Name)
		if err != nil {
			return nil, err
		}
		if name != source.Name {
			result.Warnings = append(result.Warnings, source.Name+" 已存在，已命名为 "+name)
		}
		item, err := l.importItem(source, name, "")
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, item)
	}
	complete = true
	return result, nil
}

type MetadataUpdate struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Tags        *[]string `json:"tags"`
	LabelField  *string   `json:"label_field"`
}

func (l *Library) UpdateMetadata(id string, update MetadataUpdate) (*ResolvedResource, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	item, err := l.itemByID(id)
	if err != nil {
		return nil, err
	}
	if err = writableItem(item); err != nil {
		return nil, err
	}
	m := manifestForItem(item)
	if update.Name != nil {
		if err = l.checkName(m.Type, *update.Name, id); err != nil {
			return nil, err
		}
		m.Name = *update.Name
	}
	if update.Description != nil {
		m.Description = *update.Description
	}
	if update.Tags != nil {
		m.Tags = *update.Tags
	}
	if update.LabelField != nil {
		m.LabelField = *update.LabelField
	}
	if err = ValidateManifest(&m, true); err != nil {
		return nil, err
	}
	raw, err := yaml.Marshal(m)
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(filepath.Dir(item.ManifestPath), ".metadata-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(raw); err != nil {
		file.Close()
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	if err = os.Rename(file.Name(), item.ManifestPath); err != nil {
		return nil, err
	}
	l.invalidate()
	updated, err := l.itemByID(id)
	if err != nil {
		return nil, err
	}
	return l.resolveItem(updated, Ref{Type: m.Type}, Scope{}), nil
}
func (l *Library) Export(ctx context.Context, id, target string) (string, error) {
	target, err := config.ResolveProjectPath(target)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(strings.ToLower(target), ".zip") {
		return "", fmt.Errorf("导出路径必须是 ZIP 文件路径")
	}
	if err = rejectResourcePath(target); err != nil {
		return "", err
	}
	stage, cleanup, err := l.NewStaging()
	if err != nil {
		return "", err
	}
	defer cleanup()
	pack := filepath.Join(stage, "package")
	if _, err = l.Bundle(ctx, Ref{ID: id}, Scope{}, pack); err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(target)
		}
	}()
	z := zip.NewWriter(f)
	err = filepath.WalkDir(pack, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		rel, _ := filepath.Rel(pack, p)
		dst, err := z.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(dst, src)
		return err
	})
	if err != nil {
		return "", err
	}
	if err = z.Close(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	ok = true
	return target, nil
}
