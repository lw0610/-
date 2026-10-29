// Package datasource loads declarative acquisition catalogs without network or model calls.
package datasource

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"sort"
	"strings"
)

type Strings []string

func (s *Strings) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		if n.Tag != "!!str" {
			return fmt.Errorf("字段候选必须为字符串")
		}
		*s = []string{n.Value}
		return nil
	}
	var values []string
	if err := n.Decode(&values); err != nil {
		return err
	}
	*s = values
	return nil
}

type Parameter struct {
	Values   []string `yaml:"values,omitempty" json:"values,omitempty"`
	Required bool     `yaml:"required,omitempty" json:"required,omitempty"`
}

type ParameterIssue struct {
	Name          string
	Value         string
	Required      bool
	AllowedValues []string
}

// ParameterValidationError preserves the catalog constraints needed to correct
// a recognized category, without guessing a value on the caller's behalf.
type ParameterValidationError struct {
	Category string
	Issues   []ParameterIssue
}

func (e *ParameterValidationError) Error() string {
	messages := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		message := fmt.Sprintf("%s参数 %s 不支持 %s", e.Category, issue.Name, issue.Value)
		if issue.Value == "" {
			message = fmt.Sprintf("%s缺少参数 %s", e.Category, issue.Name)
		}
		if len(issue.AllowedValues) > 0 {
			message += "，允许值：" + strings.Join(issue.AllowedValues, "、")
		}
		messages = append(messages, message)
	}
	return strings.Join(messages, "；")
}

type Field struct {
	Meaning  string `yaml:"meaning" json:"meaning"`
	Required bool   `yaml:"required,omitempty" json:"required,omitempty"`
}
type Alias struct {
	Name   string            `yaml:"name" json:"name"`
	Params map[string]string `yaml:"params,omitempty" json:"params,omitempty"`
}
type Derivation struct {
	From    string            `yaml:"from" json:"from"`
	Method  string            `yaml:"method" json:"method"`
	Params  map[string]string `yaml:"params,omitempty" json:"params,omitempty"`
	Quality string            `yaml:"quality,omitempty" json:"quality,omitempty"`
	Note    string            `yaml:"note,omitempty" json:"note,omitempty"`
}
type Generalization struct {
	MaxDenominator int      `yaml:"max_denominator,omitempty" json:"max_denominator,omitempty"`
	KeepClass      []string `yaml:"keep_class" json:"keep_class"`
}
type Validation struct {
	MinFeatures   int     `yaml:"min_features" json:"min_features"`
	NameFillRatio float64 `yaml:"name_fill_ratio" json:"name_fill_ratio"`
}
type Category struct {
	ID          string               `yaml:"id" json:"id"`
	Name        string               `yaml:"name" json:"name"`
	Geometry    string               `yaml:"geometry" json:"geometry"`
	Params      map[string]Parameter `yaml:"params,omitempty" json:"params,omitempty"`
	Aliases     []Alias              `yaml:"aliases,omitempty" json:"aliases,omitempty"`
	Fields      map[string]Field     `yaml:"fields,omitempty" json:"fields,omitempty"`
	Derive      []Derivation         `yaml:"derive,omitempty" json:"derive,omitempty"`
	Generalize  []Generalization     `yaml:"generalize,omitempty" json:"generalize,omitempty"`
	SimplifyMM  float64              `yaml:"simplify_mm,omitempty" json:"simplify_mm,omitempty"`
	Sensitivity string               `yaml:"sensitivity,omitempty" json:"sensitivity,omitempty"`
	Validate    Validation           `yaml:"validate" json:"validate"`
}
type Coverage struct {
	Regions []string  `yaml:"regions,omitempty" json:"regions,omitempty"`
	BBox    []float64 `yaml:"bbox,omitempty" json:"bbox,omitempty"`
}
type Scale struct {
	MinDenominator int `yaml:"min_denominator,omitempty" json:"min_denominator,omitempty"`
	MaxDenominator int `yaml:"max_denominator,omitempty" json:"max_denominator,omitempty"`
}
type Limits struct {
	MinAreaKM2     float64 `yaml:"min_area_km2,omitempty" json:"min_area_km2,omitempty"`
	MaxAreaKM2     float64 `yaml:"max_area_km2,omitempty" json:"max_area_km2,omitempty"`
	MaxFeatures    int     `yaml:"max_features,omitempty" json:"max_features,omitempty"`
	MaxExtents     int     `yaml:"max_extents,omitempty" json:"max_extents,omitempty"`
	TimeoutSeconds int     `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	MaxMB          int     `yaml:"max_mb,omitempty" json:"max_mb,omitempty"`
}
type Provide struct {
	Category string             `yaml:"category" json:"category"`
	Tags     []string           `yaml:"tags,omitempty" json:"tags,omitempty"`
	Types    string             `yaml:"types,omitempty" json:"types,omitempty"`
	Fields   map[string]Strings `yaml:"fields,omitempty" json:"fields,omitempty"`
	URL      string             `yaml:"url,omitempty" json:"url,omitempty"`
	Path     string             `yaml:"path,omitempty" json:"path,omitempty"`
	Tool     string             `yaml:"tool,omitempty" json:"tool,omitempty"`
	Dataset  string             `yaml:"dataset,omitempty" json:"dataset,omitempty"`
	Layer    string             `yaml:"layer,omitempty" json:"layer,omitempty"`
	Params   map[string]string  `yaml:"params,omitempty" json:"params,omitempty"`
	Defaults map[string]string  `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Filter   string             `yaml:"filter,omitempty" json:"filter,omitempty"`
	Template string             `yaml:"template,omitempty" json:"template,omitempty"`
}
type Source struct {
	Handbook    Handbook  `yaml:"handbook,omitempty" json:"handbook"`
	APIVersion  string    `yaml:"apiVersion" json:"api_version"`
	ID          string    `yaml:"id" json:"id"`
	Name        string    `yaml:"name" json:"name"`
	Kind        string    `yaml:"kind" json:"kind"`
	Version     string    `yaml:"version,omitempty" json:"version,omitempty"`
	Root        string    `yaml:"root,omitempty" json:"root,omitempty"`
	Endpoints   string    `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
	Hosts       []string  `yaml:"hosts,omitempty" json:"hosts,omitempty"`
	Coverage    Coverage  `yaml:"coverage,omitempty" json:"coverage,omitempty"`
	Scale       Scale     `yaml:"scale,omitempty" json:"scale,omitempty"`
	Limits      Limits    `yaml:"limits,omitempty" json:"limits,omitempty"`
	License     string    `yaml:"license,omitempty" json:"license,omitempty"`
	Attribution string    `yaml:"attribution,omitempty" json:"attribution,omitempty"`
	Compliance  string    `yaml:"compliance,omitempty" json:"compliance,omitempty"`
	Requires    []string  `yaml:"requires,omitempty" json:"requires,omitempty"`
	RefreshDays int       `yaml:"refresh_days,omitempty" json:"refresh_days,omitempty"`
	Priority    int       `yaml:"priority,omitempty" json:"priority,omitempty"`
	Provides    []Provide `yaml:"provides" json:"provides"`
	SizeMB      float64   `yaml:"size_mb,omitempty" json:"size_mb,omitempty"`
	Status      string    `yaml:"status,omitempty" json:"status,omitempty"`
	Notes       []string  `yaml:"notes,omitempty" json:"notes,omitempty"`
	Builtin     bool      `yaml:"-" json:"builtin"`
	File        string    `yaml:"-" json:"-"`
}
type Handbook struct {
	GoodFor  string   `yaml:"good_for,omitempty" json:"good_for"`
	NotFor   string   `yaml:"not_for,omitempty" json:"not_for"`
	Coverage string   `yaml:"coverage,omitempty" json:"coverage"`
	Scale    string   `yaml:"scale,omitempty" json:"scale"`
	Fields   string   `yaml:"fields,omitempty" json:"fields"`
	Cautions string   `yaml:"cautions,omitempty" json:"cautions"`
	Examples []string `yaml:"examples,omitempty" json:"examples"`
}

func (h Handbook) Summary() string {
	values := []string{}
	for _, s := range []string{h.GoodFor, h.Coverage, h.Cautions} {
		if strings.TrimSpace(s) != "" {
			values = append(values, strings.TrimSpace(s))
		}
	}
	return strings.Join(values, "；")
}
func (c *Catalog) HandbookIndex() string {
	if c == nil {
		return ""
	}
	rows := []string{}
	for _, s := range c.Sources {
		rows = append(rows, s.ID+" · "+s.Name+"："+s.Handbook.Summary())
	}
	return strings.Join(rows, "\n")
}

type Diagnostic struct {
	Code     string `json:"code"`
	Level    string `json:"level"`
	Message  string `json:"message"`
	SourceID string `json:"source_id,omitempty"`
	File     string `json:"file,omitempty"`
}
type Catalog struct {
	Regions     []Region     `json:"-"`
	Categories  []Category   `json:"categories"`
	Sources     []Source     `json:"sources"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Revision    string       `json:"revision"`
}

func (c *Catalog) Category(id string) *Category {
	if c != nil {
		for i := range c.Categories {
			if c.Categories[i].ID == id {
				return &c.Categories[i]
			}
		}
	}
	return nil
}
func (c *Catalog) Source(id string) *Source {
	if c != nil {
		for i := range c.Sources {
			if c.Sources[i].ID == id {
				return &c.Sources[i]
			}
		}
	}
	return nil
}
func (c *Catalog) ResolveCategory(name string, params map[string]string) (*Category, map[string]string, error) {
	name = strings.TrimSpace(name)
	for i := range c.Categories {
		category := &c.Categories[i]
		aliases := append([]Alias{{Name: category.ID}, {Name: category.Name}}, category.Aliases...)
		for _, alias := range aliases {
			if !strings.EqualFold(alias.Name, name) {
				continue
			}
			merged := map[string]string{}
			for key, value := range alias.Params {
				merged[key] = value
			}
			for key, value := range params {
				merged[key] = value
			}
			keys := make([]string, 0, len(category.Params))
			for key := range category.Params {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			issues := []ParameterIssue{}
			for _, key := range keys {
				spec := category.Params[key]
				value := merged[key]
				if (spec.Required && value == "") || (value != "" && len(spec.Values) > 0 && !contains(spec.Values, value)) {
					issues = append(issues, ParameterIssue{Name: key, Value: value, Required: spec.Required, AllowedValues: append([]string(nil), spec.Values...)})
				}
			}
			if len(issues) > 0 {
				return category, merged, &ParameterValidationError{Category: category.Name, Issues: issues}
			}
			return category, merged, nil
		}
	}
	return nil, nil, fmt.Errorf("未知数据类别：%s", name)
}
func (c *Catalog) SourcesFor(category string) []Source {
	result := []Source{}
	if c != nil {
		for _, source := range c.Sources {
			for _, provide := range source.Provides {
				if provide.Category == category {
					result = append(result, source)
					break
				}
			}
		}
	}
	return result
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// TerritoryReason expresses catalog policy, not a certification of any data.
func TerritoryReason(category *Category, source *Source, intersectsChina bool) string {
	if category == nil || source == nil || !intersectsChina || category.Sensitivity != "cn_territory" {
		return ""
	}
	if !source.Builtin && source.Kind == "local_folder" {
		return ""
	}
	if source.Compliance != "cn_official" && source.Compliance != "cn_reference" {
		return "疆域类数据不能使用境外开放数据或来源尚未核实的数据"
	}
	return ""
}
