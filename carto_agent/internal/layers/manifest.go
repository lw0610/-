// Package layers manages local, read-only geographic data registrations.
package layers

import (
	"fmt"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/resource"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Coverage struct {
	BBox    []float64 `json:"bbox,omitempty" yaml:"bbox,omitempty"`
	Regions []string  `json:"regions,omitempty" yaml:"regions,omitempty"`
}
type Scale struct {
	BestFrom int `json:"best_from,omitempty" yaml:"best_from,omitempty"`
	BestTo   int `json:"best_to,omitempty" yaml:"best_to,omitempty"`
}
type Source struct {
	Title       string `json:"title,omitempty" yaml:"title,omitempty"`
	License     string `json:"license,omitempty" yaml:"license,omitempty"`
	Attribution string `json:"attribution,omitempty" yaml:"attribution,omitempty"`
	URL         string `json:"url,omitempty" yaml:"url,omitempty"`
	Version     string `json:"version,omitempty" yaml:"version,omitempty"`
	RetrievedAt string `json:"retrieved_at,omitempty" yaml:"retrieved_at,omitempty"`
}
type Manifest struct {
	APIVersion   string            `json:"api_version" yaml:"apiVersion"`
	ID           string            `json:"id" yaml:"id"`
	Name         string            `json:"name" yaml:"name"`
	Aliases      []string          `json:"aliases,omitempty" yaml:"aliases,omitempty"`
	Pack         string            `json:"pack" yaml:"pack"`
	Category     string            `json:"category" yaml:"category"`
	Params       map[string]string `json:"params,omitempty" yaml:"params,omitempty"`
	Geometry     string            `json:"geometry" yaml:"geometry"`
	File         string            `json:"file" yaml:"file"`
	Sublayer     string            `json:"sublayer,omitempty" yaml:"sublayer,omitempty"`
	DeclaredCRS  string            `json:"declared_crs,omitempty" yaml:"declared_crs,omitempty"`
	CRS          string            `json:"crs" yaml:"crs"`
	Coverage     Coverage          `json:"coverage" yaml:"coverage"`
	Scale        Scale             `json:"scale" yaml:"scale"`
	Fields       map[string]string `json:"fields,omitempty" yaml:"fields,omitempty"`
	LabelField   string            `json:"label_field,omitempty" yaml:"label_field,omitempty"`
	DefaultStyle string            `json:"default_style,omitempty" yaml:"default_style,omitempty"`
	FeatureCount int64             `json:"feature_count" yaml:"feature_count"`
	Source       Source            `json:"source" yaml:"source"`
	Compliance   string            `json:"compliance,omitempty" yaml:"compliance,omitempty"`
	Digest       string            `json:"digest" yaml:"digest"`
	Preview      string            `json:"preview,omitempty" yaml:"preview,omitempty"`
	CreatedAt    string            `json:"created_at" yaml:"created_at"`
}
type Item struct {
	Manifest
	Scope      string   `json:"scope"`
	Status     string   `json:"status"`
	Issues     []string `json:"issues"`
	PreviewURL string   `json:"preview_url,omitempty"`
	Directory  string   `json:"-"`
}

func (i Item) Path() string {
	if filepath.IsAbs(i.File) {
		return i.File
	}
	return filepath.Clean(filepath.Join(i.Directory, i.File))
}
func (i Item) PreviewPath() string {
	if i.Preview == "" {
		return ""
	}
	return filepath.Join(i.Directory, i.Preview)
}

type Library struct{ SystemRoot, UserRoot, AssetsRoot string }

func NewLibrary(systemRoot, userRoot string) *Library {
	return &Library{SystemRoot: systemRoot, UserRoot: userRoot}
}
func NewDefaultLibrary() (*Library, error) {
	assets, e := config.ResolveAssetsRoot()
	if e != nil {
		return nil, e
	}
	home, e := config.ResolveAutoCartoHome()
	if e != nil {
		return nil, e
	}
	l := NewLibrary(filepath.Join(assets, "data", "layers", "system"), filepath.Join(home, "layers"))
	l.AssetsRoot = assets
	return l, nil
}
func validName(s string) bool {
	return strings.TrimSpace(s) != "" && !strings.ContainsAny(s, "/\\\x00\r\n") && s != "." && s != ".."
}

// manifestValidator belongs to one catalog read or import. Its lookups are loaded
// once for that operation, so edits remain visible on the next request.
type manifestValidator struct {
	assetsRoot string
	categories map[string]bool
	styles     map[string]bool
}

func (l *Library) Validate(m Manifest) error {
	return (&manifestValidator{assetsRoot: l.AssetsRoot}).validate(m)
}

func (v *manifestValidator) validate(m Manifest) error {
	if m.APIVersion != "layer/v1" || !strings.HasPrefix(m.ID, "layer.") || !validName(m.ID) || !validName(m.Name) || !validName(m.Pack) {
		return fmt.Errorf("图层清单版本、编号、名称或包名无效")
	}
	if m.File == "" || m.CRS == "" {
		return fmt.Errorf("%s：请选择数据文件并确认实际坐标系", m.Name)
	}
	switch m.Geometry {
	case "point", "line", "polygon", "raster":
	default:
		return fmt.Errorf("%s：几何类型无效", m.Name)
	}
	if len(m.Coverage.BBox) != 0 && (len(m.Coverage.BBox) != 4 || m.Coverage.BBox[0] > m.Coverage.BBox[2] || m.Coverage.BBox[1] > m.Coverage.BBox[3]) {
		return fmt.Errorf("%s：覆盖范围无效", m.Name)
	}
	if m.Scale.BestFrom < 0 || m.Scale.BestTo < 0 || (m.Scale.BestTo > 0 && m.Scale.BestFrom > m.Scale.BestTo) {
		return fmt.Errorf("%s：建议比例尺范围无效", m.Name)
	}
	if m.Preview != "" && (filepath.Base(m.Preview) != m.Preview || filepath.Ext(m.Preview) != ".png") {
		return fmt.Errorf("预览必须是清单目录内的 PNG")
	}
	if v.assetsRoot != "" {
		if v.categories == nil {
			raw, err := os.ReadFile(filepath.Join(v.assetsRoot, "tools", "system", "data-acquisition", "assets", "categories.yaml"))
			if err != nil {
				return err
			}
			var catalog struct {
				Categories []struct {
					ID string `yaml:"id"`
				} `yaml:"categories"`
			}
			if err = yaml.Unmarshal(raw, &catalog); err != nil {
				return err
			}
			v.categories = map[string]bool{}
			for _, category := range catalog.Categories {
				v.categories[category.ID] = true
			}
		}
		if !v.categories[m.Category] {
			return fmt.Errorf("%s：未知数据类别 %s", m.Name, m.Category)
		}
		if m.DefaultStyle != "" {
			if v.styles == nil {
				library, err := resource.NewDefaultLibrary()
				if err != nil {
					return err
				}
				items, err := library.List("all")
				if err != nil {
					return err
				}
				v.styles = styleNames(items)
			}
			if !v.styles[m.DefaultStyle] {
				return fmt.Errorf("%s：默认样式不存在：%s", m.Name, m.DefaultStyle)
			}
		}
	}
	if m.Category == "" {
		return fmt.Errorf("%s：请选择数据类别", m.Name)
	}
	return nil
}
func NewID() string { return fmt.Sprintf("layer.user.%x", time.Now().UnixNano()) }

func styleNames(items []resource.LibraryItem) map[string]bool {
	names := map[string]bool{}
	for _, item := range items {
		if item.Type == "style" {
			names[item.Name] = true
		}
	}
	return names
}
