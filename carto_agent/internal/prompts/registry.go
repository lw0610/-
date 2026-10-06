package prompts

import (
	"bytes"
	"fmt"
	"text/template"
)

type Registry struct {
	templates map[string]*template.Template
}

func NewRegistry() *Registry {
	r := &Registry{templates: make(map[string]*template.Template)}
	r.mustRegister(TemplateModePlan, modePlanPrompt)
	r.mustRegister(TemplateModeBuild, modeBuildPrompt)
	return r
}

func (r *Registry) mustRegister(name string, tpl string) {
	r.templates[name] = template.Must(template.New(name).Parse(tpl))
}

func (r *Registry) Render(name string, data any) (string, error) {
	tpl, ok := r.templates[name]
	if !ok {
		return "", fmt.Errorf("prompt template not found: %s", name)
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (r *Registry) MustRender(name string, data any) string {
	text, err := r.Render(name, data)
	if err != nil {
		return ""
	}
	return text
}
