package flow

import (
	"github.com/autocarto/carto_agent/internal/layers"
	"github.com/autocarto/carto_agent/internal/resource"
	"path/filepath"
)

// LoadGuideEnvironment reads only the local style/layout catalog.
func LoadGuideEnvironment(assets string) (GuideEnvironment, error) {
	absolute, err := filepath.Abs(assets)
	if err != nil {
		return GuideEnvironment{}, err
	}
	return GuideEnvironmentFromLibrary(resource.NewAssetLibrary(absolute))
}
func GuideEnvironmentFromLibrary(library *resource.Library) (GuideEnvironment, error) {
	env := GuideEnvironment{Resources: []resource.LibraryItem{}, Layers: []layers.Item{}}
	var items []resource.LibraryItem
	if library != nil {
		var err error
		items, err = library.List("all")
		if err != nil {
			return env, err
		}
	}
	if catalog, err := layers.NewDefaultLibrary(); err == nil {
		if library == nil || filepath.Clean(library.BuiltinDir) != resource.NewAssetLibrary(catalog.AssetsRoot).BuiltinDir {
			env.Layers, err = catalog.List()
		} else {
			env.Layers, err = catalog.ListWithResources(items)
		}
		if err != nil {
			return env, err
		}
	}
	for _, item := range items {
		if item.Type == "style" || item.Type == "layout" {
			env.Resources = append(env.Resources, item)
		}
	}
	return env, nil
}

func guideEnvironmentWithLock(env GuideEnvironment, lock *resource.Lock) GuideEnvironment {
	if lock == nil {
		return env
	}
	for _, e := range lock.Resources {
		if e.Type != "style" && e.Type != "layout" {
			continue
		}
		kept := []resource.LibraryItem{}
		for _, r := range env.Resources {
			if r.Type != e.Type || r.Name != e.Name {
				kept = append(kept, r)
			}
		}
		env.Resources = append(kept, resource.LibraryItem{ID: e.ID, Name: e.Name, Type: e.Type})
	}
	return env
}
func resourceDocumentWithLock(raw string, library *resource.Library, lock *resource.Lock) *FlowDoc {
	env, _ := GuideEnvironmentFromLibrary(library)
	g := ParseGuide(raw, guideEnvironmentWithLock(env, lock))
	return &FlowDoc{Meta: g.Meta, Resources: g.Resources, Layers: g.Layers, Diagnostics: g.Diagnostics}
}
