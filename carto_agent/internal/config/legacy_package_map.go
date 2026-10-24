package config

// legacyPackageTarget is the versioned migration table from refactor/05 §1/§4.3.
// Empty targets deliberately retire a package; unknown IDs are kept as warnings.
type legacyPackageTarget struct {
	Tools []string
	Flow  string
}

var legacyPackageMap = func() map[string]legacyPackageTarget {
	m := map[string]legacyPackageTarget{}
	for _, id := range []string{
		"browser-data-acquisition", "canvas-navigation", "cartography-styling", "creator-tools",
		"crs-projection", "data-conversion", "data-loading", "file-operations", "gdal-processing",
		"gee-remote-sensing", "geocoding", "gps-data", "grass-analysis", "marching-route-knowledge-tools",
		"network-analysis", "point-cloud", "print-layout", "pyqgis-scripting", "quality-feedback",
		"raster-analysis", "spatial-analysis", "statistical-data-tools", "vector-creation", "vector-fields",
		"vector-geometry", "vector-selection", "visibility-analysis", "route-cartography", "charts", "mesh-analysis", "admin-boundaries",
	} {
		m[id] = legacyPackageTarget{Tools: []string{id}}
	}
	for old, target := range map[string]string{
		"accessibility-service-area": "network-analysis", "attribute-cleaning": "vector-fields",
		"buffer-proximity-analysis": "vector-geometry", "cartography-styling-standard": "cartography-styling",
		"chart-visualization": "charts", "china-admin-boundaries": "admin-boundaries",
		"conditional-feature-extraction": "vector-selection", "crs-diagnosis-transform": "crs-projection",
		"data-format-conversion": "data-conversion", "data-loading-inspection": "data-loading",
		"dem-terrain-derivatives": "raster-analysis", "gee-scripting": "gee-remote-sensing",
		"geometry-repair-validation": "vector-geometry", "gps-field-data-import": "gps-data",
		"interpolation-surface": "raster-analysis", "land-use-change-detection": "raster-analysis",
		"map-series-batch-export": "print-layout", "mesh-3d-analysis": "mesh-analysis",
		"network-routing-analysis": "network-analysis", "overlay-analysis": "vector-geometry",
		"point-cloud-processing": "point-cloud", "print-layout-standard": "print-layout",
		"raster-algebra": "raster-analysis", "raster-reclassify-stats": "raster-analysis",
		"sampling-random-selection": "vector-selection", "site-suitability-analysis": "raster-analysis",
		"spatial-clustering": "spatial-analysis", "thematic-map-styling": "cartography-styling",
		"viewshed-visibility-analysis": "visibility-analysis", "watershed-hydrology-analysis": "grass-analysis",
	} {
		m[old] = legacyPackageTarget{Tools: []string{target}}
	}
	for _, suffix := range []string{"attribute-ops", "batch-processing", "crs-transform", "data-loading", "geometry-ops", "interpolation", "layout", "raster-analysis", "spatial-query", "styling", "terrain-ops", "vector-analysis", "workflow"} {
		m["pyqgis-"+suffix] = legacyPackageTarget{Tools: []string{"pyqgis-scripting"}}
	}
	m["misc-tools"] = legacyPackageTarget{Tools: []string{"charts", "mesh-analysis", "vector-geometry"}}
	for _, id := range []string{"project-state", "runtime-control", "build-capability-awareness", "data-loading-inspection-vector", "layout-image-export", "prompt-refinement", "marching-route-knowledge-extraction"} {
		m[id] = legacyPackageTarget{}
	}
	for old, target := range map[string]legacyPackageTarget{
		"marching-route-map-orchestration":  {Flow: "marching-route"},
		"marching-route-data-visualization": {Flow: "marching-route", Tools: []string{"data-loading"}},
		"data-loading-scv":                  {Flow: "marching-route", Tools: []string{"data-loading"}},
		"print-layout-marching-route":       {Flow: "marching-route", Tools: []string{"cartography-styling", "route-cartography", "print-layout"}},
		"statistical-map-orchestration":     {Flow: "statistical-choropleth"},
		"statistical-data-preparation":      {Flow: "statistical-choropleth", Tools: []string{"statistical-data-tools", "data-loading"}},
		"print-layout-statistical":          {Flow: "statistical-choropleth", Tools: []string{"cartography-styling", "print-layout"}},
		"traffic-map-orchestration":         {Flow: "traffic-route"},
		"print-layout-thematic":             {Flow: "traffic-route", Tools: []string{"cartography-styling", "print-layout"}},
		"data-to-map-full-pipeline":         {Flow: "data-to-map"},
	} {
		m[old] = target
	}
	return m
}()
