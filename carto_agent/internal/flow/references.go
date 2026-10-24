package flow

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/autocarto/carto_agent/internal/resource"
	"gopkg.in/yaml.v3"
)

type ResourceReference struct {
	Label    string `json:"label"`
	FlowID   string `json:"flow_id"`
	FlowName string `json:"flow_name"`
	Source   string `json:"source"`
	Via      string `json:"via"`
}

// ResourceReferences scans declarations without compiling or mutating flows.
// A name ambiguity counts as a reference to every candidate, so deletion cannot
// silently remove an option that a flow still declares.
func (s *Store) ResourceReferences(library *resource.Library, id string) ([]ResourceReference, error) {
	target, err := library.GetByID(id)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{id: true}
	names := map[string]bool{target.Name: true}
	if target.Overrides != "" {
		ids[target.Overrides] = true
		if base, e := library.GetByID(target.Overrides); e == nil {
			names[base.Name] = true
		}
	}
	result := []ResourceReference{}
	// Read each root directly: even a shadowed/invalid package can contain a
	// reference that the user must see before deleting a shared dependency.
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, root := range []struct{ path, source string }{{s.BuiltinDir, "builtin"}, {s.UserDir, "user"}} {
		if root.path == "" {
			continue
		}
		entries, e := os.ReadDir(root.path)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		for _, entry := range entries {
			if !entry.IsDir() || ValidateID(entry.Name()) != nil {
				continue
			}
			dir := filepath.Join(root.path, entry.Name())
			if e = rejectSymlinks(dir); e != nil {
				return nil, e
			}
			raw, e := readBounded(filepath.Join(dir, "FLOW.md"), MaxFileBytes)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return nil, e
			}
			doc := Parse(string(raw))
			lock := &resource.Lock{}
			lockPath := filepath.Join(dir, "flow.lock")
			if e = rejectSymlinks(lockPath); e != nil && !os.IsNotExist(e) {
				return nil, e
			}
			lockRaw, e := readBounded(lockPath, MaxFileBytes)
			if e == nil {
				if e = yaml.Unmarshal(lockRaw, lock); e != nil {
					return nil, e
				}
			} else if !os.IsNotExist(e) {
				return nil, e
			}
			via, label := "", ""
			for _, locked := range lock.Resources {
				if ids[locked.ID] && locked.Source != "bundled" {
					via = "lock"
					label = locked.Name
					break
				}
			}
			if via == "" {
				for _, declaration := range doc.Resources {
					if declaration.Type != target.Type {
						continue
					}
					location := strings.TrimSpace(declaration.Location)
					name := declaration.Label
					if declaration.Type == "layout" && location != "" && location != "公共资源库" && filepath.Ext(location) == "" && !strings.ContainsAny(location, "/\\") {
						name = location
						location = "公共资源库"
					}
					if lock.Find(declaration.Type, declaration.Label) != nil {
						continue
					}
					if strings.HasPrefix(location, "公共资源库/") {
						if ids[strings.TrimPrefix(location, "公共资源库/")] {
							via = "name"
							label = declaration.Label
							break
						}
						continue
					}
					if location != "" && location != "公共资源库" {
						continue
					}
					// A lock for this declaration suppresses the broad name fallback.
					if locked := lock.Find(declaration.Type, declaration.Label); locked != nil {
						continue
					}
					if names[name] {
						via = "name"
						label = declaration.Label
						break
					}
				}
			}
			if via != "" {
				result = append(result, ResourceReference{FlowID: entry.Name(), FlowName: doc.Meta.Name, Source: root.source, Via: via, Label: label})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].FlowID == result[j].FlowID {
			return result[i].Source < result[j].Source
		}
		return result[i].FlowID < result[j].FlowID
	})
	return result, nil
}
