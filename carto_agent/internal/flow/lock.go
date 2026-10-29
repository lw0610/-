package flow

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/resource"
	"gopkg.in/yaml.v3"
)

const documentFormatV6 = "flow/v6"

func ReadLock(dir string) (*resource.Lock, error) {
	lock := &resource.Lock{APIVersion: resource.LockAPIVersion, Resources: []resource.LockEntry{}}
	raw, err := readBounded(filepath.Join(dir, "flow.lock"), MaxFileBytes)
	if os.IsNotExist(err) {
		return lock, nil
	}
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err = decoder.Decode(lock); err != nil {
		return nil, fmt.Errorf("flow.lock 无法解析：%w", err)
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("flow.lock 只允许一个 YAML 文档")
	}
	if err = lock.Validate(); err != nil {
		return nil, err
	}
	return lock, nil
}
func writeLock(dir string, lock *resource.Lock) error {
	if err := lock.Validate(); err != nil {
		return err
	}
	raw, err := yaml.Marshal(lock)
	if err != nil {
		return err
	}
	target := filepath.Join(dir, "flow.lock")
	existing, err := readBounded(target, MaxFileBytes)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if bytes.Equal(existing, raw) {
		return nil
	}
	f, err := os.CreateTemp(dir, ".flow-lock-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), target)
}
func publicDeclaration(d ResourceDoc) bool {
	return d.Location == "公共资源库" || strings.HasPrefix(d.Location, "公共资源库/")
}
func retainedLock(lock *resource.Lock, doc *FlowDoc) *resource.Lock {
	out := &resource.Lock{APIVersion: resource.LockAPIVersion, Resources: []resource.LockEntry{}}
	if lock != nil {
		out.DocumentFormat = lock.DocumentFormat
	}
	seen := map[string]bool{}
	for _, d := range doc.Resources {
		if d.Type != "style" && d.Type != "layout" {
			continue
		}
		if publicDeclaration(d) && !seen[d.Type+"\x00"+d.Label] {
			seen[d.Type+"\x00"+d.Label] = true
			if e := lock.Find(d.Type, d.Label); e != nil {
				out.Resources = append(out.Resources, *e)
			}
		}
	}
	return out
}

// ResolveResources takes one package snapshot and only fills previously unlocked
// references. An existing identity/digest changes solely through a repair action.
func (s *Store) ResolveResources(ctx context.Context, id string, library *resource.Library) (*StoredFlow, *FlowDoc, *resource.Lock, []*resource.ResolvedResource, error) {
	return s.resolveResources(ctx, id, library, true)
}

// ResolveResourcesReadOnly 读取已保存流程的资源快照，不写锁定文件。
func (s *Store) ResolveResourcesReadOnly(ctx context.Context, id string, library *resource.Library) (*StoredFlow, *FlowDoc, *resource.Lock, []*resource.ResolvedResource, error) {
	return s.resolveResources(ctx, id, library, false)
}

func (s *Store) resolveResources(ctx context.Context, id string, library *resource.Library, persistLock bool) (*StoredFlow, *FlowDoc, *resource.Lock, []*resource.ResolvedResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.get(id)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	lock, err := ReadLock(item.Dir)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	doc := resourceDocumentWithLock(item.FlowMD, library, lock)
	lock = retainedLock(lock, doc)
	resolved, lock := resolveVisualResources(ctx, id, item.Dir, doc, lock, library)
	if item.Source == "user" && persistLock {
		if err = writeLock(item.Dir, lock); err != nil {
			return nil, nil, nil, nil, err
		}
	}
	return item, doc, lock, resolved, nil
}

func resolveVisualResources(ctx context.Context, id, dir string, doc *FlowDoc, lock *resource.Lock, library *resource.Library) ([]*resource.ResolvedResource, *resource.Lock) {
	resolved := []*resource.ResolvedResource{}
	for _, d := range doc.Resources {
		key := "unresolved:" + d.Type + ":" + d.Label
		r := &resource.ResolvedResource{Key: key, Label: d.Label, Type: d.Type, Location: d.Location, Entries: []resource.ResourceEntry{}, Problems: []Diagnostic{{Level: "error", Code: "FLOW_RESOURCE_NOT_FOUND", Message: "资源库尚未配置", Fix: map[string]any{"action": "import_resource", "resource_key": key}}}}
		if library != nil {
			r = library.Resolve(ctx, resource.Ref{Key: key, Name: d.Label, Type: d.Type, Location: d.Location, BaseDirFromInput: d.FolderInput}, resource.Scope{FlowID: id, FlowDir: dir, Lock: lock})
		}
		resolved = append(resolved, r)
		if publicDeclaration(d) && lock.Find(d.Type, d.Label) == nil && r.ID != "" && r.Digest != "" && strings.HasPrefix(r.Owner, "library_") {
			lock.Resources = append(lock.Resources, resource.LockResource(d.Type, d.Label, r))
		}
	}
	return resolved, lock
}
