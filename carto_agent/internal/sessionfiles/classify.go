package sessionfiles

import (
	"path/filepath"
	"strings"
)

func classify(path, sessionDir string, dir bool) (kind, group string) {
	kind = "other"
	ext := strings.ToLower(filepath.Ext(path))
	name := filepath.Base(path)
	if dir {
		kind = "folder"
	} else {
		switch ext {
		case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".svg":
			kind = "image"
		case ".pdf":
			kind = "pdf"
		case ".shp", ".gpkg", ".geojson", ".kml", ".gml", ".tab", ".mif", ".fgb":
			kind = "vector"
		case ".tif", ".tiff", ".vrt", ".img", ".asc":
			kind = "raster"
		case ".csv", ".tsv", ".xlsx", ".xls", ".ods":
			kind = "table"
		case ".txt", ".md", ".yaml", ".yml", ".xml":
			kind = "text"
		case ".json":
			kind = "json"
		case ".qml":
			kind = "qgis_style"
		case ".qpt":
			kind = "qgis_layout"
		case ".zip":
			kind = "archive"
		}
	}
	group = "other"
	if name == "run.json" && within(filepath.Join(sessionDir, "flows"), path) {
		return "run", "flow"
	}
	if within(filepath.Join(sessionDir, "intermediate"), path) {
		return kind, "intermediate"
	}
	if within(filepath.Join(sessionDir, "temp"), path) || within(filepath.Join(sessionDir, "logs"), path) {
		return kind, "other"
	}
	if within(filepath.Join(sessionDir, "exports"), path) && (kind == "image" || kind == "pdf" || kind == "vector" || kind == "raster" || kind == "table") {
		group = "deliverable"
	} else if within(filepath.Join(sessionDir, "output", "osm"), path) && kind == "vector" {
		group = "source"
	} else if kind == "image" || kind == "pdf" {
		// Export tools may return a project path or output/ rather than exports/.
		// Only indexed external files are listed; temp/log previews stay hidden.
		group = "deliverable"
	} else if kind == "vector" || kind == "raster" || kind == "table" || kind == "folder" {
		group = "data"
	}
	return
}

// Dedicated output directories describe a file's role more reliably than older
// registrations that labelled every geospatial file simply as data.
func preferredGroup(classified, registered string) string {
	if classified == "deliverable" || classified == "source" || registered == "" {
		return classified
	}
	return registered
}
func mergeMembers(items []Item) []Item {
	byPath := map[string]int{}
	for i, item := range items {
		byPath[strings.ToLower(item.Path)] = i
	}
	hidden := map[int]bool{}
	attach := func(main int, path string) {
		if child, ok := byPath[strings.ToLower(path)]; ok && main != child {
			items[main].Members = append(items[main].Members, items[child].Path)
			items[main].Size += items[child].Size
			hidden[child] = true
		}
	}
	for i, item := range items {
		ext := strings.ToLower(filepath.Ext(item.Path))
		stem := strings.TrimSuffix(item.Path, filepath.Ext(item.Path))
		var suffixes []string
		switch ext {
		case ".shp":
			suffixes = []string{".shx", ".dbf", ".prj", ".cpg", ".qix", ".sbn", ".sbx", ".fix", ".shp.xml"}
		case ".tif", ".tiff", ".img":
			suffixes = []string{".tfw", ".hdr"}
			attach(i, item.Path+".ovr")
			attach(i, item.Path+".aux.xml")
		case ".tab":
			suffixes = []string{".dat", ".map", ".id"}
		case ".mif":
			suffixes = []string{".mid"}
		case ".gml":
			suffixes = []string{".gfs"}
		case ".gpkg":
			attach(i, item.Path+"-wal")
			attach(i, item.Path+"-shm")
		}
		if item.Kind == "vector" || item.Kind == "raster" {
			suffixes = append(suffixes, ".qml", ".qmd", ".source.json", ".provenance.json")
			attach(i, item.Path+".source.json")
		}
		for _, suffix := range suffixes {
			attach(i, stem+suffix)
		}
	}
	result := []Item{}
	for i, item := range items {
		if !hidden[i] {
			result = append(result, item)
		}
	}
	return result
}
