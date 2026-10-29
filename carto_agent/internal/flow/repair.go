package flow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/resource"
)

func (s *Store) RepairResource(ctx context.Context, id, action, key, resourceID string, library *resource.Library) (*StoredFlow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if item.Source == "builtin" {
		return nil, ErrReadOnly
	}
	if library == nil {
		return nil, fmt.Errorf("资源库未配置")
	}
	doc := resourceDocument(item.FlowMD, library)
	lock, err := ReadLock(item.Dir)
	if err != nil {
		return nil, err
	}
	var declaration *ResourceDoc
	matches := []ResourceDoc{}
	var current *resource.ResolvedResource
	for i := range doc.Resources {
		d := &doc.Resources[i]
		r := library.Resolve(ctx, resource.Ref{Key: "unresolved:" + d.Type + ":" + d.Label, Type: d.Type, Name: d.Label, Location: d.Location}, resource.Scope{FlowID: id, FlowDir: item.Dir, Lock: lock})
		if r.Key == key {
			matches = append(matches, *d)
			if declaration == nil {
				declaration = d
				current = r
			}
		}
	}
	if declaration == nil {
		return nil, fmt.Errorf("未找到资源声明，请重新打开流程后重试")
	}
	d := *declaration
	e := lock.Find(d.Type, d.Label)
	switch action {
	case "choose_resource", "update_lock", "use_library", "install_resource":
		for _, alias := range matches {
			if !publicDeclaration(alias) {
				return nil, fmt.Errorf("该动作仅适用于公共资源或随包依赖")
			}
			if alias.Type != d.Type {
				return nil, fmt.Errorf("同一资源的声明类型不一致，请先修正类型")
			}
		}
		var chosen *resource.ResolvedResource
		switch action {
		case "choose_resource":
			if resourceID == "" {
				return nil, fmt.Errorf("请选择资源编号")
			}
			chosen = library.Resolve(ctx, resource.Ref{Type: d.Type, Name: d.Label, ID: resourceID, Location: "公共资源库"}, resource.Scope{})
		case "update_lock":
			chosen = current
		case "use_library":
			if e == nil || e.Source != "bundled" {
				return nil, fmt.Errorf("资源没有随包副本")
			}
			chosen = library.Resolve(ctx, resource.Ref{Type: d.Type, Name: d.Label, ID: e.ID, Location: "公共资源库"}, resource.Scope{})
		case "install_resource":
			if e == nil || e.Source != "bundled" {
				return nil, fmt.Errorf("资源没有随包副本")
			}
			chosen, err = library.InstallBundle(filepath.Join(item.Dir, filepath.FromSlash(e.Bundled)), d.Type)
			if err != nil {
				return nil, err
			}
		}
		if chosen == nil || chosen.ID == "" || chosen.Digest == "" || chosen.Type != d.Type {
			return nil, fmt.Errorf("所选资源无效或类型不符")
		}
		for _, alias := range matches {
			old := lock.Find(alias.Type, alias.Label)
			next := resource.LockResource(alias.Type, alias.Label, chosen)
			if chosen.Owner == "bundled" && old != nil {
				next.Bundled = old.Bundled
			}
			if old == nil {
				lock.Resources = append(lock.Resources, next)
			} else {
				*old = next
			}
		}
		// Persist selection before removing any no-longer-needed bundle; stale
		// copies are harmless and retained so a failed write never loses data.
		if err = writeLock(item.Dir, lock); err != nil {
			return nil, err
		}

	default:
		return nil, fmt.Errorf("未知资源修复动作：%s", action)
	}
	return s.get(id)
}

// MigrateResourceIdentity updates only editable packages. Builtin locks remain
// repository-controlled even when a user's legacy public resource is upgraded.
func (s *Store) MigrateResourceIdentity(oldID string, updated *resource.ResolvedResource) error {
	if oldID == updated.ID {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.migrateResourceIdentity(oldID, updated)
}
func (s *Store) migrateResourceIdentity(oldID string, updated *resource.ResolvedResource) error {
	dirs, err := os.ReadDir(s.UserDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range dirs {
		if !entry.IsDir() || ValidateID(entry.Name()) != nil {
			continue
		}
		dir := filepath.Join(s.UserDir, entry.Name())
		lock, err := ReadLock(dir)
		if err != nil {
			return err
		}
		changed := false
		for i := range lock.Resources {
			e := &lock.Resources[i]
			if e.ID == oldID && e.Source != "bundled" {
				*e = resource.LockResource(e.Type, e.Name, updated)
				changed = true
			}
		}
		raw, err := readBounded(packageMainPath(dir), MaxFileBytes)
		if err != nil {
			return err
		}
		rewritten := rewritePublicResourceID(string(raw), oldID, updated.ID)

		if changed || rewritten != string(raw) {
			stage, err := os.MkdirTemp("", "autocarto-flow-identity-")
			if err != nil {
				return err
			}
			// Keep each package's FLOW.md and lock in one staged transaction.
			transaction := func() error {
				defer os.RemoveAll(stage)
				if err := copyTree(dir, stage, false); err != nil {
					return err
				}
				if err := writeLock(stage, lock); err != nil {
					return err
				}
				_, err := s.write(rewritten, entry.Name(), stage, true)
				return err
			}
			if err = transaction(); err != nil {
				return err
			}
		}
	}
	if s.Bindings != nil {
		snapshot, err := s.Bindings.Snapshot()
		if err != nil {
			return err
		}
		mapping := map[string]string{}
		for key := range snapshot.Bindings {
			if key == "library:"+oldID || strings.HasSuffix(key, "/library:"+oldID) {
				mapping[key] = strings.TrimSuffix(key, oldID) + updated.ID
			}
		}
		return s.Bindings.RenameKeys(mapping)
	}
	return nil
}

// Identity migration only changes the location of exact parsed declarations;
// resource IDs in prose or in a longer neighbouring ID are ordinary user text.
func rewritePublicResourceID(raw, oldID, newID string) string {
	lines := strings.Split(raw, "\n")
	oldLocation := "公共资源库/" + oldID
	for _, d := range Parse(raw).Resources {
		if d.Location != oldLocation || d.Line < 1 || d.Line > len(lines) {
			continue
		}
		line := lines[d.Line-1]
		at := strings.LastIndex(line, oldLocation)
		if at >= 0 {
			lines[d.Line-1] = line[:at] + "公共资源库/" + newID + line[at+len(oldLocation):]
		}
	}
	return strings.Join(lines, "\n")
}
