package flow

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
)

// MigrateInputMemory runs only at process startup (or an explicit migration).
// Unassociated declarations, shared bindings, conflicts and unknown flows stay
// intact for the one-version read-only compatibility period.
func (s *Store) MigrateInputMemory(memory *InputMemory, library *resource.Library) error {
	if memory == nil || library == nil || s.Bindings == nil {
		return fmt.Errorf("输入记忆迁移服务未配置")
	}
	if err := s.PrepareBindingMigration(library); err != nil {
		return err
	}
	snapshot, err := s.Bindings.Snapshot()
	if err != nil {
		return err
	}
	observed := map[string]string{}
	for key, value := range snapshot.Orphaned {
		observed[key] = value
	}
	for key, value := range snapshot.Bindings {
		observed[key] = value
	}
	if len(observed) == 0 {
		return nil
	}
	flows, err := s.List()
	if err != nil {
		return err
	}
	identities := resource.NewLibrary(library.BuiltinDir, library.UserDir, nil)
	type candidate struct {
		flowID, label string
		keys          map[string]string
		values        map[string]bool
	}
	candidates := map[string]*candidate{}
	for _, summary := range flows {
		stored, err := s.Get(summary.ID)
		if err != nil {
			return err
		}
		doc := Parse(stored.FlowMD)
		inputs := map[string][]InputPlan{}
		for _, input := range baseInputs(doc.Inputs) {
			inputs[input.Key] = append(inputs[input.Key], input)
		}
		for _, declaration := range doc.Resources {
			matches := inputs[declaration.FolderInput]
			if declaration.Type != "data_list" || len(matches) != 1 || matches[0].Type != "folder" || !matches[0].Remember {
				continue
			}
			label := matches[0].Label
			if err := memoryLabel(label); err != nil {
				continue
			}
			resolved := identities.Resolve(context.Background(), resource.Ref{Type: declaration.Type, Name: declaration.Label, Location: declaration.Location}, resource.Scope{FlowID: summary.ID, FlowDir: stored.Dir})
			keys := []string{bindings.LegacyResourceKey(declaration.Type, declaration.Label), resolved.Key, resolved.ID}
			location := filepath.ToSlash(filepath.Clean(declaration.Location))
			if strings.HasPrefix(location, "resources/") {
				keys = append(keys, "path:"+strings.TrimPrefix(location, "resources/"))
			}
			target := summary.ID + "\x00" + label
			for _, key := range keys {
				if key == "" {
					continue
				}
				fullKey := "flow:" + summary.ID + "/" + key
				value, found := observed[fullKey]
				if !found || strings.TrimSpace(value) == "" {
					continue
				}
				absolute, err := config.ResolveProjectPath(value)
				if err != nil {
					return err
				}
				absolute, err = memoryValue(absolute)
				if err != nil {
					return err
				}
				if candidates[target] == nil {
					candidates[target] = &candidate{flowID: summary.ID, label: label, keys: map[string]string{}, values: map[string]bool{}}
				}
				candidates[target].keys[fullKey] = value
				candidates[target].values[absolute] = true
			}
		}
	}
	remove := map[string]string{}
	remember := map[string]map[string]string{}
	for _, item := range candidates {
		if len(item.values) != 1 {
			continue // Multiple conflicting historical choices require an explicit user choice.
		}
		if remember[item.flowID] == nil {
			remember[item.flowID] = map[string]string{}
		}
		for value := range item.values {
			remember[item.flowID][item.label] = value
		}
		for key, value := range item.keys {
			remove[key] = value
		}
	}
	return s.Bindings.Consume(remove, func() error { return memory.mergeMigrated(remember) })
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
