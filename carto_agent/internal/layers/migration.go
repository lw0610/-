package layers

import (
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"sort"
)

type MigrationSuggestion struct {
	Path    string `json:"path"`
	Pack    string `json:"pack"`
	Source  string `json:"source"`
	Message string `json:"message"`
}

func (l *Library) MigrationSuggestions(home string) ([]MigrationSuggestion, error) {
	items, e := l.List()
	if e != nil {
		return nil, e
	}
	registered := map[string]bool{}
	for _, i := range items {
		registered[filepath.Dir(i.Path())] = true
	}
	out := []MigrationSuggestion{}
	seen := map[string]bool{}
	for _, file := range []string{filepath.Join(home, "config", "flow_input_memory.yaml"), filepath.Join(home, "config", "resource_bindings.yaml"), filepath.Join(home, "resource-bindings.yaml")} {
		raw, e := os.ReadFile(file)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		var v any
		if e = yaml.Unmarshal(raw, &v); e != nil {
			return nil, e
		}
		var walk func(any)
		walk = func(v any) {
			switch value := v.(type) {
			case map[string]any:
				for _, child := range value {
					walk(child)
				}
			case []any:
				for _, child := range value {
					walk(child)
				}
			case string:
				if filepath.IsAbs(value) && !registered[value] && !seen[value] {
					if info, e := os.Stat(value); e == nil && info.IsDir() {
						seen[value] = true
						out = append(out, MigrationSuggestion{value, filepath.Base(value), filepath.Base(file), "发现以前使用的底图文件夹，可导入为内置图层"})
					}
				}
			}
		}
		walk(v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
