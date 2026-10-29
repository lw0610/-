package layers

import (
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
)

func (l *Library) Suggest(m *Manifest, fields []string) error {
	raw, e := os.ReadFile(filepath.Join(l.AssetsRoot, "data/layers/mappings/standard.yaml"))
	if e != nil {
		return e
	}
	var rules struct {
		Entries []struct {
			Names    []string
			Category string
			Params   map[string]string
			Style    string
			Labels   []string
		}
	}
	if e = yaml.Unmarshal(raw, &rules); e != nil {
		return e
	}
	m.Fields = map[string]string{}
	for _, key := range []string{"name", "code", "class", "ref", "kind"} {
		for _, f := range fields {
			if strings.EqualFold(key, f) || (key == "code" && f == "adcode") {
				m.Fields[key] = f
				break
			}
		}
	}
	for _, r := range rules.Entries {
		for _, name := range r.Names {
			if name == m.Name {
				m.Category = r.Category
				m.Params = r.Params
				m.DefaultStyle = r.Style
				for _, f := range r.Labels {
					for _, actual := range fields {
						if f == actual {
							m.LabelField = f
							return nil
						}
					}
				}
				return nil
			}
		}
	}
	raw, e = os.ReadFile(filepath.Join(l.AssetsRoot, "data/layers/mappings/webmap-1m.yaml"))
	if e != nil {
		return e
	}
	var web struct {
		Entries []struct {
			Codes       []string
			Category    string
			Suggestions []string
		}
	}
	if e = yaml.Unmarshal(raw, &web); e != nil {
		return e
	}
	for _, rule := range web.Entries {
		for _, code := range rule.Codes {
			if strings.Contains(strings.ToUpper(m.Name), code) {
				m.Category = rule.Category
				if len(rule.Suggestions) == 1 {
					m.Name = rule.Suggestions[0]
					m.DefaultStyle = m.Name
				}
				return nil
			}
		}
	}
	return nil
}
