package resource

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// enrichMetadata reads only local metadata; it never invokes QGIS or a model.
func enrichMetadata(item *LibraryItem) {
	if !IsPublicType(item.Type) {
		return
	}
	raw, err := boundedFile(item.Entry, maxTableBytes)
	if err == nil && item.Type == "style" {
		if item.Geometry == "" {
			item.Geometry = qmlGeometry(item.Entry)
		}
		if bytes.Contains(raw, []byte("<pipe>")) || bytes.Contains(raw, []byte("<rasterrenderer")) {
			item.Geometry = "raster"
		}
		if item.LabelField == "" {
			dec := xml.NewDecoder(bytes.NewReader(raw))
			for {
				tok, e := dec.Token()
				if e == io.EOF || e != nil {
					break
				}
				start, ok := tok.(xml.StartElement)
				if !ok || start.Name.Local != "text-style" {
					continue
				}
				for _, a := range start.Attr {
					if a.Name.Local == "fieldName" {
						item.LabelField = a.Value
					}
				}
			}
			if item.LabelField == "" {
				item.LabelField = "无"
			}
		}
	}
	if err == nil && item.Type == "layout" && strings.HasSuffix(item.Entry, ".layout.yaml") {
		var d struct {
			Page struct {
				Size               string `yaml:"size"`
				Paper              string `yaml:"paper"`
				DefaultPaper       string `yaml:"default_paper"`
				DefaultOrientation string `yaml:"default_orientation"`
				Orientation        string `yaml:"orientation"`
			} `yaml:"page"`
			Elements map[string]any `yaml:"elements"`
		}
		if yaml.Unmarshal(raw, &d) == nil {
			if item.Paper == "" {
				item.Paper = d.Page.Size
				if item.Paper == "" {
					item.Paper = d.Page.Paper
					if item.Paper == "" {
						item.Paper = d.Page.DefaultPaper
					}
				}
			}
			if item.Orientation == "" {
				item.Orientation = d.Page.Orientation
				if item.Orientation == "" {
					item.Orientation = d.Page.DefaultOrientation
				}
			}
			if len(item.Elements) == 0 {
				for k := range d.Elements {
					item.Elements = append(item.Elements, k)
				}
				sort.Strings(item.Elements)
			}
		}
	}
	if err == nil && item.Type == "layout" && strings.HasSuffix(strings.ToLower(item.Entry), ".qpt") {
		var document struct {
			PageCollection struct {
				Pages []struct {
					Size string `xml:"size,attr"`
				} `xml:"LayoutItem"`
			} `xml:"PageCollection"`
			Items []struct {
				ID string `xml:"id,attr"`
			} `xml:"LayoutItem"`
		}
		if xml.Unmarshal(raw, &document) == nil {
			if len(document.PageCollection.Pages) > 0 {
				parts := strings.Split(document.PageCollection.Pages[0].Size, ",")
				if len(parts) == 3 && parts[2] == "mm" {
					w, we := strconv.ParseFloat(parts[0], 64)
					h, he := strconv.ParseFloat(parts[1], 64)
					if we == nil && he == nil && w > 0 && h > 0 {
						if item.Orientation == "" {
							item.Orientation = "portrait"
							if w > h {
								item.Orientation = "landscape"
							}
						}
						if item.Paper == "" {
							item.Paper = fmt.Sprintf("%g × %g mm", w, h)
							small, large := math.Min(w, h), math.Max(w, h)
							for name, size := range map[string][2]float64{"A0": {841, 1189}, "A1": {594, 841}, "A2": {420, 594}, "A3": {297, 420}, "A4": {210, 297}, "A5": {148, 210}} {
								if math.Abs(small-size[0]) < 0.6 && math.Abs(large-size[1]) < 0.6 {
									item.Paper = name
								}
							}
						}
					}
				}
			}
			if len(item.Elements) == 0 {
				seen := map[string]bool{}
				for _, element := range document.Items {
					if element.ID != "" && !seen[element.ID] {
						seen[element.ID] = true
						item.Elements = append(item.Elements, element.ID)
					}
				}
				sort.Strings(item.Elements)
			}
		}
	}
	item.PreviewURL = "/api/resources/by-id/" + url.PathEscape(item.ID) + "/preview?digest=" + url.QueryEscape(item.Digest)
	item.PreviewStatus = "pending"
	root := item.Root
	if info, e := os.Stat(root); e == nil && !info.IsDir() {
		root = filepath.Dir(root)
	}
	if previewCurrent(root, item.Digest) {
		item.PreviewStatus = "ready"
	}
}
