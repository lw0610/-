package flow

import (
	"context"
	"fmt"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
)

// NewDefaultStore is also used by the HTTP fallback and acceptance runner. The
// library and every flow share the one binding store created at application boot.
func NewDefaultStore(library *resource.Library) (*Store, error) {
	if library == nil || library.Bindings == nil {
		return nil, fmt.Errorf("资源绑定存储未配置")
	}
	user, err := config.ResolveUserFlowsDir()
	if err != nil {
		return nil, err
	}
	store := NewStore(config.ResolveBuiltinFlowsDir(), user, library.Bindings)
	if err = store.PrepareBindingMigration(library); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) SetBinding(id, key, baseDir string) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	if s.Bindings == nil {
		return fmt.Errorf("资源绑定存储未配置")
	}
	return s.Bindings.Set("flow:"+id+"/"+key, baseDir)
}

func (s *Store) OrphanedBindingCount() (int, error) {
	if s.Bindings == nil {
		return 0, nil
	}
	doc, err := s.Bindings.Snapshot()
	return len(doc.Orphaned), err
}

// PrepareBindingMigration resolves identities with bindings disabled, avoiding
// recursion and never depending on the machine's data folders being available.
func (s *Store) PrepareBindingMigration(library *resource.Library) error {
	if s.Bindings == nil || library == nil {
		return fmt.Errorf("资源绑定存储未配置")
	}
	identities := resource.NewLibrary(library.BuiltinDir, library.UserDir, nil)
	candidates := map[string][]string{}
	items, err := identities.List("all")
	if err != nil {
		return err
	}
	retired, err := identities.List("retired")
	if err != nil {
		return err
	}
	items = append(items, retired...)
	for _, item := range items {
		if item.Type == "data_list" {
			old := "library:" + item.Name
			candidates[old] = append(candidates[old], "library:"+item.ID)
		}
	}
	flows, err := s.List()
	if err != nil {
		return err
	}
	for _, summary := range flows {
		stored, err := s.Get(summary.ID)
		if err != nil {
			return err
		}
		for _, declaration := range Parse(stored.FlowMD).Resources {
			if declaration.Type != "data_list" {
				continue
			}
			resolved := identities.Resolve(context.Background(), resource.Ref{Type: declaration.Type, Name: declaration.Label, Location: declaration.Location}, resource.Scope{FlowID: summary.ID, FlowDir: stored.Dir})
			if resolved.Key == "" {
				continue
			}
			old := "flow:" + summary.ID + "/" + bindings.LegacyResourceKey(declaration.Type, declaration.Label)
			candidates[old] = append(candidates[old], "flow:"+summary.ID+"/"+resolved.Key)
		}
	}
	return s.Bindings.PrepareMigration(candidates)
}
