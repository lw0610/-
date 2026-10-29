package resource

import (
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// LayoutMapWidthMM reads the selected local layout's main map, without QGIS.
// YAML uses the same scaled margins as layout_compose.page_geometry; a QPT
// already contains its actual main-map rectangle in millimetres.
func LayoutMapWidthMM(entry, paper, orientation string) (float64, error) {
	raw, err := boundedFile(entry, maxTableBytes)
	if err != nil {
		return 0, err
	}
	if strings.HasSuffix(strings.ToLower(entry), ".qpt") {
		var doc struct {
			Items []struct {
				ID   string `xml:"id,attr"`
				Size string `xml:"size,attr"`
			} `xml:"LayoutItem"`
		}
		if err := xml.Unmarshal(raw, &doc); err != nil {
			return 0, err
		}
		for _, item := range doc.Items {
			if item.ID == "main_map" || item.ID == "主图" {
				parts := strings.Split(item.Size, ",")
				if len(parts) == 3 && parts[2] == "mm" {
					return strconv.ParseFloat(parts[0], 64)
				}
			}
		}
		return 0, nil
	}
	if !strings.HasSuffix(strings.ToLower(entry), ".layout.yaml") {
		return 0, nil
	}
	var spec struct {
		Page struct {
			Paper       string `yaml:"default_paper"`
			Orientation string `yaml:"default_orientation"`
			Reference   struct {
				Width float64 `yaml:"width"`
			} `yaml:"reference_size_mm"`
			Margins struct {
				Left  float64 `yaml:"left"`
				Right float64 `yaml:"right"`
			} `yaml:"margins_mm"`
		} `yaml:"page"`
		Elements map[string]any `yaml:"elements"`
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return 0, err
	}
	if spec.Elements["main_map"] == nil {
		return 0, nil
	}
	if paper == "" {
		paper = spec.Page.Paper
	}
	if orientation == "" {
		orientation = spec.Page.Orientation
	}
	if paper == "" { // Matches the layout composer's default.
		paper = "A4"
	}
	size, ok := map[string][2]float64{"A0": {841, 1189}, "A1": {594, 841}, "A2": {420, 594}, "A3": {297, 420}, "A4": {210, 297}}[strings.ToUpper(paper)]
	if !ok {
		parts := strings.Split(strings.ReplaceAll(strings.ToLower(paper), "×", "x"), "x")
		if len(parts) != 2 {
			return 0, fmt.Errorf("版面纸张无法解析：%s", paper)
		}
		for i, part := range parts {
			size[i], err = strconv.ParseFloat(strings.TrimSpace(part), 64)
			if err != nil {
				return 0, err
			}
		}
	}
	width := math.Max(size[0], size[1])
	if orientation == "portrait" {
		width = math.Min(size[0], size[1])
	}
	reference := spec.Page.Reference.Width
	if reference == 0 {
		reference = width
	}
	width -= (spec.Page.Margins.Left + spec.Page.Margins.Right) * width / reference
	if width <= 0 {
		return 0, fmt.Errorf("版面页边距没有留下有效的主图区域")
	}
	return width, nil
}
