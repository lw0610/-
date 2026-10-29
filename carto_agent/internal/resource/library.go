package resource

import (
	"context"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
)

var (
	ErrNotFound  = errors.New("资源不存在")
	ErrReadOnly  = errors.New("内置资源只读")
	ErrConflict  = errors.New("同名用户资源已存在")
	libraryLocks sync.Map
)

const MaxFileBytes int64 = 200 << 20
const MaxPackageBytes int64 = 2 << 30

var typeDirectories = func() map[string]string {
	result := map[string]string{}
	for name, typ := range legacyFormatSet.Types {
		result[name] = typ.Group
	}
	return result
}()
var groupNames = func() []string {
	seen := map[string]bool{}
	result := []string{}
	for _, typ := range TypeInfos() {
		if !seen[typ.Group] {
			result = append(result, typ.Group)
			seen[typ.Group] = true
		}
	}
	return result
}()

type Library struct {
	BuiltinDir     string
	UserDir        string
	ToolDirs       []string
	Bindings       *bindings.Store
	mu             *sync.Mutex
	cacheMu        sync.Mutex
	indexSignature string
	indexItems     []LibraryItem
}

// NewAssetLibrary reads only shipped public resources; it never discovers or
// migrates the user's runtime directory. Guide environment projection uses it.
func NewAssetLibrary(assets string) *Library {
	return NewLibrary(filepath.Join(assets, "resources", "system"), "", nil)
}

func NewLibrary(builtinDir, userDir string, bindingStore *bindings.Store) *Library {
	if builtinDir != "" {
		if absolute, err := config.ResolveProjectPath(builtinDir); err == nil {
			builtinDir = absolute
		}
	}
	if absolute, err := config.ResolveProjectPath(userDir); err == nil {
		userDir = absolute
	}
	lock, _ := libraryLocks.LoadOrStore(filepath.Clean(userDir), &sync.Mutex{})
	toolDirs := []string{}
	if filepath.Base(builtinDir) == "system" && filepath.Base(filepath.Dir(builtinDir)) == "resources" {
		toolDirs, _ = filepath.Glob(filepath.Join(filepath.Dir(filepath.Dir(builtinDir)), "tools", "system", "*", "assets", "resources"))
	}
	return &Library{BuiltinDir: builtinDir, UserDir: userDir, ToolDirs: toolDirs, Bindings: bindingStore, mu: lock.(*sync.Mutex)}
}

func NewDefaultLibrary() (*Library, error) {
	assets, err := config.ResolveAssetsRoot()
	if err != nil {
		return nil, err
	}
	home, err := config.ResolveAutoCartoHome()
	if err != nil {
		return nil, err
	}
	file, err := config.ResolveResourceBindingsFile()
	if err != nil {
		return nil, err
	}
	library := NewLibrary(filepath.Join(assets, "resources", "system"), filepath.Join(home, "resources"), bindings.New(file))
	if _, err := library.MigrateV6(); err != nil {
		return nil, err
	}
	return library, nil
}

func validName(name string) bool {
	return strings.TrimSpace(name) != "" && name != "." && name != ".." && !strings.HasPrefix(name, ".") && !strings.ContainsAny(name, "/\\:\x00\r\n")
}

func classify(group, path string, dir bool) (kind, name string) {
	base := filepath.Base(path)
	if dir {
		raw, err := os.ReadFile(filepath.Join(path, "resource.yaml"))
		var m Manifest
		if err == nil && yaml.Unmarshal(raw, &m) == nil && IsPublicType(m.Type) && m.Entry != "" {
			return m.Type, m.Name
		}
	}
	for _, typ := range TypeInfos() {
		if typ.Group != group || typ.Folder != dir {
			continue
		}
		if dir {
			return typ.Type, base
		}
		for _, ext := range typ.EntryExtensions {
			if strings.HasSuffix(strings.ToLower(base), ext) {
				return typ.Type, base[:len(base)-len(ext)]
			}
		}
	}
	return "", ""
}

func (l *Library) List(scope string) ([]LibraryItem, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if scope == "retired" {
		items, err := l.list("user")
		result := []LibraryItem{}
		for _, item := range items {
			if !IsPublicType(item.Type) {
				result = append(result, item)
			}
		}
		return result, err
	}
	items, err := l.list(scope)
	result := []LibraryItem{}
	for _, item := range items {
		if IsPublicType(item.Type) && item.Owner != "tool" {
			result = append(result, item)
		}
	}
	return result, err
}
func (l *Library) list(scope string) ([]LibraryItem, error) {
	if scope == "" {
		scope = "all"
	}
	if scope != "all" && scope != "user" && scope != "builtin" {
		return nil, fmt.Errorf("资源范围须为 builtin、user 或 all")
	}
	signature, err := l.indexStamp()
	if err != nil {
		return nil, err
	}
	l.cacheMu.Lock()
	defer l.cacheMu.Unlock()
	if signature != l.indexSignature || l.indexItems == nil {
		items, err := l.scanItems()
		if err != nil {
			return nil, err
		}
		l.indexItems = items
		l.indexSignature = signature
	}
	result := []LibraryItem{}
	for _, item := range l.indexItems {
		if scope == "all" || item.Owner == "library_"+scope {
			copy := cloneItem(item)
			copy.PreviewStatus = "pending"
			if IsPublicType(item.Type) && previewCurrent(l.previewRoot(&ResolvedResource{Root: item.Root, Owner: item.Owner, ID: item.ID, Digest: item.Digest}), item.Digest) {
				copy.PreviewStatus = "ready"
			}
			result = append(result, copy)
		}
	}
	return result, nil
}
func (l *Library) invalidate() {
	l.cacheMu.Lock()
	l.indexSignature = ""
	l.indexItems = nil
	l.cacheMu.Unlock()
}
func (l *Library) scanItems() ([]LibraryItem, error) {
	items := []LibraryItem{}
	roots := []struct{ path, owner string }{{l.UserDir, "library_user"}, {l.BuiltinDir, "library_builtin"}}
	for _, path := range l.ToolDirs {
		roots = append(roots, struct{ path, owner string }{path, "tool"})
	}
	for _, root := range roots {
		if root.path == "" {
			continue
		}
		if err := bindings.RejectSymlinks(root.path); err != nil {
			return nil, err
		}
		for _, group := range groupNames {
			entries, err := os.ReadDir(filepath.Join(root.path, group))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".") {
					continue
				}
				path := filepath.Join(root.path, group, entry.Name())
				if err = bindings.RejectSymlinks(path); err != nil {
					return nil, err
				}
				kind, _ := classify(group, path, entry.IsDir())
				if kind == "" {
					continue
				}
				items = append(items, inspectItem(path, kind, root.owner, root.path))
			}
		}
	}
	applyIdentityRules(items)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Type != items[j].Type {
			return items[i].Type < items[j].Type
		}
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].Owner > items[j].Owner
	})
	return items, nil
}
func (l *Library) find(kind, name, scope string) (LibraryItem, error) {
	if typeDirectories[kind] == "" || !validName(name) {
		return LibraryItem{}, fmt.Errorf("资源类型或名称无效")
	}
	items, err := l.list(scope)
	if err != nil {
		return LibraryItem{}, err
	}
	candidates := []LibraryItem{}
	for _, item := range items {
		if item.Type == kind && item.Name == name && item.Owner != "tool" {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) == 0 {
		return LibraryItem{}, ErrNotFound
	}
	if len(candidates) > 1 {
		return LibraryItem{}, ErrConflict
	}
	return candidates[0], nil
}
func (l *Library) Get(kind, name string) (*ResolvedResource, error) {
	r := l.Resolve(context.Background(), Ref{Type: kind, Name: name}, Scope{})
	for _, problem := range r.Problems {
		switch problem.Code {
		case "FLOW_RESOURCE_NOT_FOUND":
			l.mu.Lock()
			defer l.mu.Unlock()
			if item, err := l.find(kind, name, "all"); err == nil {
				return l.resolveItem(item, Ref{Type: kind, Name: name}, Scope{}), nil
			}
			return nil, ErrNotFound
		case "FLOW_RESOURCE_AMBIGUOUS":
			return nil, ErrConflict
		}
	}
	return r, nil
}
func (l *Library) GetByID(id string) (*ResolvedResource, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	item, err := l.itemByID(id)
	if err != nil {
		return nil, err
	}
	return l.resolveItem(item, Ref{Type: item.Type, Name: item.Name, ID: id, Location: "公共资源库/" + id}, Scope{}), nil
}
func (l *Library) itemByID(id string) (LibraryItem, error) {
	items, err := l.list("all")
	if err != nil {
		return LibraryItem{}, err
	}
	found := []LibraryItem{}
	for _, item := range items {
		if item.ID == id {
			found = append(found, item)
		}
	}
	if len(found) == 0 {
		return LibraryItem{}, ErrNotFound
	}
	if len(found) > 1 {
		return LibraryItem{}, ErrConflict
	}
	return found[0], nil
}

func (l *Library) SetBinding(key, baseDir string) error { return l.Bindings.Set(key, baseDir) }

// Import is retained for callers that do not supply a display name.
func (l *Library) Import(source, kind string) (*ResolvedResource, error) {
	return l.ImportNamed(source, kind, "")
}
func (l *Library) Delete(kind, name string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	item, err := l.find(kind, name, "user")
	if errors.Is(err, ErrNotFound) {
		if _, e := l.find(kind, name, "builtin"); e == nil {
			return ErrReadOnly
		}
	}
	if err != nil {
		return err
	}
	return l.deleteItem(item)
}

func inside(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func copyResource(source, target string) error {
	var total int64
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := bindings.RejectSymlinks(path); err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := target
		if rel != "." {
			dest = filepath.Join(target, rel)
		}
		if entry.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > MaxFileBytes {
			return fmt.Errorf("资源包含特殊文件或单个文件超过 200 MiB")
		}
		total += info.Size()
		if total > MaxPackageBytes {
			return fmt.Errorf("资源总大小超过 2 GiB")
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(output, io.LimitReader(input, MaxFileBytes+1))
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if n > MaxFileBytes {
			return fmt.Errorf("资源文件超过 200 MiB")
		}
		total += n - info.Size()
		if total > MaxPackageBytes {
			return fmt.Errorf("资源总大小超过 2 GiB")
		}
		return nil
	})
}
