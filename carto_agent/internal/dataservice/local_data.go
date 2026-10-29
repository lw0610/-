package dataservice

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/autocarto/carto_agent/internal/shared/xlsx"
	_ "github.com/mattn/go-sqlite3"
)

func NormalizeGeometry(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	switch {
	case v == "点" || strings.Contains(v, "point"):
		return "point"
	case v == "线" || strings.Contains(v, "line"):
		return "line"
	case v == "面" || strings.Contains(v, "polygon"):
		return "polygon"
	case v == "栅格" || v == "tiles" || v == "raster":
		return "raster"
	case v == "表" || v == "table":
		return "table"
	}
	return v
}

// InspectProvided reads local headers only. No model, network or caller-file writes.
// A GeoPackage can yield several independently selectable layers.
func InspectProvided(ctx context.Context, path string) ([]Binding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	physical, selector, _ := strings.Cut(path, "|layername=")
	info, err := os.Stat(physical)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("文件不存在或不可读取：%s", physical)
	}
	base := Binding{Name: strings.TrimSuffix(filepath.Base(physical), filepath.Ext(physical)), Path: physical}
	switch strings.ToLower(filepath.Ext(physical)) {
	case ".geojson", ".json":
		f, e := os.Open(physical)
		if e != nil {
			return nil, e
		}
		defer f.Close()
		dec := json.NewDecoder(io.LimitReader(f, 16<<20))
		token, e := dec.Token()
		if e != nil || token != json.Delim('{') {
			return nil, fmt.Errorf("不是有效GeoJSON")
		}
		for dec.More() {
			key, e := dec.Token()
			if e != nil {
				return nil, e
			}
			switch key {
			case "features":
				token, e = dec.Token()
				if e != nil || token != json.Delim('[') {
					return nil, fmt.Errorf("GeoJSON features须为数组")
				}
				for i := 0; dec.More() && i < 16; i++ {
					var feat struct {
						Geometry struct {
							Type string `json:"type"`
						} `json:"geometry"`
						Properties map[string]any `json:"properties"`
					}
					if e = dec.Decode(&feat); e != nil {
						return nil, e
					}
					g := NormalizeGeometry(feat.Geometry.Type)
					if base.Geometry != "" && g != "" && base.Geometry != g {
						return nil, fmt.Errorf("GeoJSON包含混合几何类型")
					}
					if g != "" {
						base.Geometry = g
					}
					for k := range feat.Properties {
						base.Fields = append(base.Fields, k)
					}
				}
				if base.Geometry == "" {
					return nil, fmt.Errorf("GeoJSON没有可核验的几何")
				}
				sort.Strings(base.Fields)
				return []Binding{base}, nil
			case "geometry":
				var geometry struct {
					Type string `json:"type"`
				}
				if e = dec.Decode(&geometry); e != nil {
					return nil, e
				}
				base.Geometry = NormalizeGeometry(geometry.Type)
			case "properties":
				var props map[string]any
				if e = dec.Decode(&props); e != nil {
					return nil, e
				}
				for k := range props {
					base.Fields = append(base.Fields, k)
				}
			default:
				var discard json.RawMessage
				if e = dec.Decode(&discard); e != nil {
					return nil, e
				}
			}
		}
		if base.Geometry != "" {
			sort.Strings(base.Fields)
			return []Binding{base}, nil
		}
		return nil, fmt.Errorf("GeoJSON没有可核验的几何")
	case ".shp":
		f, e := os.Open(physical)
		if e != nil {
			return nil, e
		}
		defer f.Close()
		header := make([]byte, 100)
		if _, e = io.ReadFull(f, header); e != nil {
			return nil, e
		}
		if binary.BigEndian.Uint32(header[:4]) != 9994 || binary.LittleEndian.Uint32(header[28:32]) != 1000 {
			return nil, fmt.Errorf("Shapefile头部无效")
		}
		switch binary.LittleEndian.Uint32(header[32:36]) {
		case 1, 8, 11, 18, 21, 28:
			base.Geometry = "point"
		case 3, 13, 23:
			base.Geometry = "line"
		case 5, 15, 25:
			base.Geometry = "polygon"
		default:
			return nil, fmt.Errorf("不支持的Shapefile几何")
		}
		stem := strings.TrimSuffix(physical, filepath.Ext(physical))
		for _, ext := range []string{".shx", ".dbf"} {
			if _, e = os.Stat(stem + ext); e != nil {
				return nil, fmt.Errorf("Shapefile缺少%s", ext)
			}
		}
		dbf, e := os.Open(stem + ".dbf")
		if e != nil {
			return nil, e
		}
		defer dbf.Close()
		h := make([]byte, 32)
		if _, e = io.ReadFull(dbf, h); e != nil {
			return nil, e
		}
		n := (int(binary.LittleEndian.Uint16(h[8:10])) - 33) / 32
		if n < 0 || n > 2048 {
			return nil, fmt.Errorf("DBF字段头部无效")
		}
		for i := 0; i < n; i++ {
			if _, e = io.ReadFull(dbf, h); e != nil {
				return nil, e
			}
			base.Fields = append(base.Fields, strings.TrimRight(string(h[:11]), "\x00"))
		}
		return []Binding{base}, nil
	case ".gpkg":
		uri := url.URL{Scheme: "file", Path: physical}
		q := uri.Query()
		q.Set("mode", "ro")
		q.Set("_query_only", "1")
		uri.RawQuery = q.Encode()
		db, e := sql.Open("sqlite3", uri.String())
		if e != nil {
			return nil, e
		}
		defer db.Close()
		db.SetMaxOpenConns(1)
		rows, e := db.QueryContext(ctx, "SELECT table_name, geometry_type_name FROM gpkg_geometry_columns ORDER BY table_name")
		if e != nil {
			return nil, e
		}
		var out []Binding
		for rows.Next() {
			var name, g string
			if e = rows.Scan(&name, &g); e != nil {
				rows.Close()
				return nil, e
			}
			if selector != "" && name != selector {
				continue
			}
			b := base
			b.Name = name
			b.Sublayer = name
			b.Geometry = NormalizeGeometry(g)
			out = append(out, b)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		for i := range out {
			fields, e := db.QueryContext(ctx, `PRAGMA table_info("`+strings.ReplaceAll(out[i].Sublayer, `"`, `""`)+`")`)
			if e != nil {
				return nil, e
			}
			for fields.Next() {
				var cid, notnull, pk int
				var name, typ string
				var def any
				if e = fields.Scan(&cid, &name, &typ, &notnull, &def, &pk); e != nil {
					fields.Close()
					return nil, e
				}
				out[i].Fields = append(out[i].Fields, name)
			}
			fields.Close()
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("GeoPackage没有匹配的矢量图层")
		}
		return out, nil
	case ".xlsx":
		book, e := xlsx.Open(physical)
		if e != nil {
			return nil, e
		}
		for _, sheet := range book.Sheets {
			_, cells, e := book.ReadSheet(sheet)
			if e != nil {
				return nil, e
			}
			numbers := []int{}
			for n, row := range cells {
				for _, value := range row {
					if strings.TrimSpace(value) != "" {
						numbers = append(numbers, n)
						break
					}
				}
			}
			sort.Ints(numbers)
			if len(numbers) == 0 {
				continue
			}
			columns := []int{}
			for col := range cells[numbers[0]] {
				columns = append(columns, col)
			}
			sort.Ints(columns)
			for _, col := range columns {
				base.Fields = append(base.Fields, cells[numbers[0]][col])
			}
			base.Geometry = "table"
			return []Binding{base}, nil
		}
		return nil, fmt.Errorf("Excel没有可核验的表头")
	case ".csv", ".tsv":
		f, e := os.Open(physical)
		if e != nil {
			return nil, e
		}
		defer f.Close()
		r := csv.NewReader(io.LimitReader(f, 1<<20))
		if strings.EqualFold(filepath.Ext(physical), ".tsv") {
			r.Comma = '\t'
		}
		r.FieldsPerRecord = -1
		row, e := r.Read()
		if e != nil {
			return nil, e
		}
		base.Fields = row
		base.Geometry = "table"
		return []Binding{base}, nil
	case ".tif", ".tiff":
		f, e := os.Open(physical)
		if e != nil {
			return nil, e
		}
		defer f.Close()
		h := make([]byte, 4)
		if _, e = io.ReadFull(f, h); e != nil {
			return nil, e
		}
		if string(h[:2]) != "II" && string(h[:2]) != "MM" {
			return nil, fmt.Errorf("TIFF头部无效")
		}
		base.Geometry = "raster"
		return []Binding{base}, nil
	default:
		return nil, fmt.Errorf("无法在本地核验此数据格式：%s", filepath.Ext(physical))
	}
}

// ValidateLocalData is used at plan submission and again before use.
func ValidateLocalData(ctx context.Context, path, geometry string, fields map[string]string) (Binding, error) {
	items, err := InspectProvided(ctx, path)
	if err != nil {
		return Binding{}, err
	}
	var matches []Binding
	for _, b := range items {
		if geometry == "" || NormalizeGeometry(geometry) == b.Geometry {
			matches = append(matches, b)
		}
	}
	if len(matches) != 1 {
		return Binding{}, fmt.Errorf("几何类型不符或存在多个匹配图层（需要%s），请明确文件或GeoPackage图层", geometry)
	}
	b := matches[0]
	for required := range fields {
		found := false
		for _, f := range b.Fields {
			if f == required {
				found = true
			}
		}
		if !found {
			return Binding{}, fmt.Errorf("数据缺少字段：%s", required)
		}
	}
	return b, nil
}

func normalizedName(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}
func (s *Service) matchName(name string, need Need, category string) bool {
	name = normalizedName(strings.TrimSuffix(name, filepath.Ext(name)))
	if name == "" {
		return false
	}
	terms := []string{need.Name, need.What.Category, need.What.Description}
	if s.options.Environment.Catalog != nil {
		if c := s.options.Environment.Catalog.Category(category); c != nil {
			terms = append(terms, c.Name, c.ID)
			for _, a := range c.Aliases {
				terms = append(terms, a.Name)
			}
		}
	}
	for _, term := range terms {
		t := normalizedName(term)
		if t != "" && (strings.Contains(name, t) || strings.Contains(t, name)) {
			return true
		}
	}
	return false
}

func localPaths(ctx context.Context, path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	var out []string
	count := 0
	err = filepath.WalkDir(path, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		count++
		if count > 2000 {
			return fmt.Errorf("文件夹超过2000项，请提供更具体的子文件夹")
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(path, p)
			if strings.Count(rel, string(filepath.Separator)) >= 2 {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".geojson", ".shp", ".gpkg", ".csv", ".tsv", ".xlsx", ".tif", ".tiff":
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

func (s *Service) relatedName(name, category string) bool {
	if s.options.Environment.Catalog == nil {
		return false
	}
	family, _, ok := strings.Cut(category, ".")
	if !ok {
		return false
	}
	for _, c := range s.options.Environment.Catalog.Categories {
		if strings.HasPrefix(c.ID, family+".") && s.matchName(name, Need{}, c.ID) {
			return true
		}
	}
	return false
}
