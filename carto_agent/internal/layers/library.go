package layers

import (
	"errors"
	"fmt"
	"github.com/autocarto/carto_agent/internal/resource"
	"gopkg.in/yaml.v3"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrNotFound = errors.New("内置图层不存在")
var ErrAmbiguous = errors.New("图层别名对应多项，请选择明确名称")
var ErrReadOnly = errors.New("系统图层只读")

func (l *Library) List() ([]Item, error) {
	return l.list(&manifestValidator{assetsRoot: l.AssetsRoot})
}

// ListWithResources shares the resource catalog already loaded by the caller.
func (l *Library) ListWithResources(resources []resource.LibraryItem) ([]Item, error) {
	return l.list(&manifestValidator{assetsRoot: l.AssetsRoot, styles: styleNames(resources)})
}

func (l *Library) list(validator *manifestValidator) ([]Item, error) {
	items := []Item{}
	names := map[string]bool{}
	ids := map[string]bool{}
	for _, root := range []struct{ path, scope string }{{l.SystemRoot, "system"}, {l.UserRoot, "user"}} {
		if root.path == "" {
			continue
		}
		if _, e := os.Stat(root.path); os.IsNotExist(e) {
			continue
		}
		err := filepath.WalkDir(root.path, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || d.Name() != "layer.yaml" {
				return nil
			}
			raw, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			var m Manifest
			if e = yaml.Unmarshal(raw, &m); e != nil {
				return fmt.Errorf("%s: %w", path, e)
			}
			if err := validator.validate(m); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if names[m.Name] || ids[m.ID] {
				return fmt.Errorf("图层名称或编号重复：%s", m.Name)
			}
			names[m.Name] = true
			ids[m.ID] = true
			i := Item{Manifest: m, Scope: root.scope, Status: "ready", Issues: []string{}, Directory: filepath.Dir(path)}
			digest, e := Digest(i.Path())
			if e != nil {
				i.Status = "missing"
				i.Issues = append(i.Issues, "原数据不可读取："+e.Error())
			} else if digest != m.Digest {
				i.Status = "changed"
				i.Issues = append(i.Issues, "原数据已变化，请重新确认")
			}
			if m.CRS == "" {
				i.Status = "needs_confirmation"
				i.Issues = append(i.Issues, "请确认实际坐标系")
			}
			if m.Preview != "" {
				if _, e := os.Stat(i.PreviewPath()); e == nil {
					i.PreviewURL = "/api/layers/" + m.ID + "/preview"
				}
			}
			items = append(items, i)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Pack != items[j].Pack {
			return items[i].Pack < items[j].Pack
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}
func (l *Library) Get(id string) (Item, error) {
	items, e := l.List()
	if e != nil {
		return Item{}, e
	}
	for _, i := range items {
		if i.ID == id {
			return i, nil
		}
	}
	return Item{}, ErrNotFound
}
func ResolveName(items []Item, name string) (Item, error) {
	for _, i := range items {
		if i.Name == name {
			return i, nil
		}
	}
	var found *Item
	for _, i := range items {
		for _, alias := range i.Aliases {
			if alias == name {
				if found != nil && found.ID != i.ID {
					return Item{}, ErrAmbiguous
				}
				copy := i
				found = &copy
				break
			}
		}
	}
	if found != nil {
		return *found, nil
	}
	return Item{}, ErrNotFound
}
func (l *Library) ResolveName(name string) (Item, error) {
	items, e := l.List()
	if e != nil {
		return Item{}, e
	}
	return ResolveName(items, name)
}
func (l *Library) Save(m Manifest) (Item, error) {
	items, e := l.SaveBatch([]Manifest{m})
	if e != nil {
		return Item{}, e
	}
	return items[0], nil
}
func (l *Library) SaveBatch(manifests []Manifest) ([]Item, error) {
	validator := &manifestValidator{assetsRoot: l.AssetsRoot}
	existing, e := l.list(validator)
	if e != nil {
		return nil, e
	}
	names := map[string]string{}
	ids := map[string]Item{}
	for _, i := range existing {
		names[i.Name] = i.ID
		ids[i.ID] = i
	}
	for n := range manifests {
		m := &manifests[n]
		m.APIVersion = "layer/v1"
		if m.ID == "" {
			m.ID = NewID()
		}
		if prior, ok := ids[m.ID]; ok {
			if prior.Scope == "system" {
				return nil, ErrReadOnly
			}
			delete(names, prior.Name)
		}
		if other, ok := names[m.Name]; ok && other != m.ID {
			return nil, fmt.Errorf("名称已存在：%s", m.Name)
		}
		names[m.Name] = m.ID
		if !strings.HasPrefix(m.ID, "layer.user.") || !filepath.IsAbs(m.File) {
			return nil, fmt.Errorf("用户图层必须引用本机绝对路径")
		}
		if e = validator.validate(*m); e != nil {
			return nil, e
		}
		m.Digest, e = Digest(m.File)
		if e != nil {
			return nil, e
		}
		m.Preview = "preview.png"
		if m.CreatedAt == "" {
			m.CreatedAt = time.Now().Format(time.RFC3339)
		}
	}
	out := []Item{}
	for _, m := range manifests {
		dir := filepath.Join(l.UserRoot, m.Pack, m.ID)
		if old, ok := ids[m.ID]; ok {
			dir = old.Directory
		}
		if e = os.MkdirAll(dir, 0700); e != nil {
			return nil, e
		}
		raw, e := yaml.Marshal(m)
		if e != nil {
			return nil, e
		}
		if e = os.WriteFile(filepath.Join(dir, "layer.yaml"), raw, 0600); e != nil {
			return nil, e
		}
		out = append(out, Item{Manifest: m, Directory: dir, Scope: "user", Status: "ready", Issues: []string{}})
	}
	return out, nil
}
func (l *Library) Delete(id string) error {
	i, e := l.Get(id)
	if e != nil {
		return e
	}
	if i.Scope == "system" {
		return ErrReadOnly
	}
	return os.RemoveAll(i.Directory)
}
func (l *Library) ReadableFiles() ([]string, error) {
	items, e := l.List()
	if e != nil {
		return nil, e
	}
	out := []string{}
	for _, i := range items {
		files, e := DataFiles(i.Path())
		if e == nil {
			out = append(out, files...)
		}
	}
	return out, nil
}
