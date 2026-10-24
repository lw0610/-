package resource

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Bundle copies an immutable snapshot while holding the library's management
// lock. A folder's contents occupy the dependency root; single files keep their
// companion manifest alongside them.
func (l *Library) Bundle(ctx context.Context, ref Ref, scope Scope, target string) (*ResolvedResource, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.resolve(ctx, ref, scope)
	if r.ID == "" || r.Digest == "" {
		return nil, fmt.Errorf("无法打包未解析的资源：%s", ref.Name)
	}
	if err := CopyPackage(r.Root, r.Type, target); err != nil {
		return nil, err
	}
	return r, nil
}
func CopyPackage(source, kind, target string) error {
	if err := rejectResourcePath(source); err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyResource(source, target)
	}
	if err = os.MkdirAll(target, 0700); err != nil {
		return err
	}
	for _, p := range packagePaths(source, kind) {
		if err = copyResource(p, filepath.Join(target, filepath.Base(p))); err != nil {
			return err
		}
	}
	return nil
}

// BundledPath only locates the primary file. Callers must validate the enclosing
// lock and package before using it.
func BundledPath(root, kind string) (string, error) { return bundledResourcePath(root, kind) }

// UpgradeLocal is restricted to the resources directory of a writable flow.
// The flow store owns its transaction; library files use Library.Upgrade.
func UpgradeLocal(path, kind, flowDir string) (*ResolvedResource, error) {
	if !inside(path, filepath.Join(flowDir, "resources")) {
		return nil, fmt.Errorf("只能升级流程 resources 内的资源")
	}
	if err := rejectResourcePath(path); err != nil {
		return nil, err
	}
	item := inspectItem(path, kind, "flow", flowDir)
	if HasErrors(item.Problems) {
		return nil, fmt.Errorf("资源清单无效")
	}
	if !item.Legacy {
		return nil, fmt.Errorf("资源已有清单")
	}
	m := manifestForItem(item)
	var err error
	m.ID, err = newResourceID(kind)
	if err != nil {
		return nil, err
	}
	if info, _ := os.Stat(path); !info.IsDir() {
		m.Entry = filepath.Base(path)
	}
	if err = writeResourceManifest(path, m); err != nil {
		return nil, err
	}
	return NewLibrary("", "", nil).Resolve(context.Background(), Ref{Type: kind, Location: path}, Scope{FlowDir: flowDir}), nil
}

func (l *Library) InstallBundle(root, kind string) (*ResolvedResource, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	primary, err := bundledResourcePath(root, kind)
	if err != nil {
		return nil, err
	}
	if err = rejectResourcePath(primary); err != nil {
		return nil, err
	}
	item := inspectItem(primary, kind, "bundled", root)
	if HasErrors(item.Problems) {
		return nil, fmt.Errorf("随包资源清单无效")
	}
	name := item.Name
	for n := 1; n < 1000; n++ {
		if err = l.checkName(kind, name, ""); err == nil {
			return l.importItem(item, name, "")
		}
		if !errors.Is(err, ErrConflict) {
			return nil, err
		}
		prefix := []rune(item.Name)
		if len(prefix) > 48 {
			prefix = prefix[:48]
		}
		name = fmt.Sprintf("%s（流程副本%d）", string(prefix), n)
	}
	return nil, fmt.Errorf("无法为随包资源分配名称")
}
