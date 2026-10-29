package sessionfiles

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
)

// ArtifactPaths accepts structured tool results, not arbitrary prose or input arguments.
// Optional declarations provide result-field paths; common output keys are the fallback.
func ArtifactPaths(value any, fields ...string) []string {
	if raw, ok := value.(string); ok {
		var decoded any
		if json.Unmarshal([]byte(raw), &decoded) != nil {
			return nil
		}
		value = decoded
	}
	if FailedResult(value) {
		return nil
	}
	found := map[string]bool{}
	var add func(any)
	add = func(v any) {
		switch x := v.(type) {
		case string:
			if strings.HasPrefix(x, "file://") {
				if u, e := url.Parse(x); e == nil && (u.Host == "" || u.Host == "localhost") {
					x = u.Path
				}
			}
			if filepath.IsAbs(x) {
				found[filepath.Clean(x)] = true
			}
		case []any:
			for _, entry := range x {
				add(entry)
			}
		case []string:
			for _, entry := range x {
				add(entry)
			}
		}
	}
	for _, field := range fields {
		current := value
		for _, part := range strings.Split(field, ".") {
			object, ok := current.(map[string]any)
			if !ok {
				current = nil
				break
			}
			current = object[part]
		}
		add(current)
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			// Command results use output for stdout/stderr, even when the
			// command prints an absolute path. Artifacts need explicit fields.
			_, commandResult := x["exit_code"]
			for key, entry := range x {
				// Source metadata describes inputs, not files produced by this call.
				if key == "source_record" || key == "source_path" {
					continue
				}
				switch key {
				case "output":
					if !commandResult {
						add(entry)
					}
				case "path", "output_path", "file_path", "image_path", "pdf_path", "svg_path", "dataset_dir", "artifact_paths":
					add(entry)
				}
				if object, ok := entry.(map[string]any); ok {
					walk(object)
				}
				if array, ok := entry.([]any); ok {
					for _, element := range array {
						walk(element)
					}
				}
			}
		case []any:
			for _, entry := range x {
				walk(entry)
			}
		}
	}
	if len(fields) == 0 {
		walk(value)
	}
	result := []string{}
	for path := range found {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

// ReadsInputPaths keeps lookup/load results from appearing as newly produced files.
func ReadsInputPaths(name string) bool {
	return strings.HasPrefix(name, "data_load_") || strings.HasPrefix(name, "read_") || strings.HasPrefix(name, "get_") || name == "file_read" || name == "file_list" || name == "geodata_inspect" || name == "output_get_info" || name == "resource_get" || name == "data_find"
}

// ResultPaths also handles bridge results whose display text has a trailing
// "[文件]" label: the text/file parts retain the original structured payload.
func ResultPaths(value any, parts []map[string]any, fields ...string) []string {
	if FailedResult(value) {
		return nil
	}
	for _, part := range parts {
		if part["type"] == "text" && FailedResult(part["text"]) {
			return nil
		}
	}
	paths := map[string]bool{}
	for _, path := range ArtifactPaths(value, fields...) {
		paths[path] = true
	}
	for _, part := range parts {
		if part["type"] == "text" {
			for _, path := range ArtifactPaths(part["text"], fields...) {
				paths[path] = true
			}
		}
		if file, ok := part["file"].(map[string]any); ok {
			if extra, ok := file["extra"].(map[string]any); ok {
				for _, path := range ArtifactPaths(map[string]any{"path": extra["path"]}) {
					paths[path] = true
				}
			}
		}
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

// ResultNames reads names paired with real file sources in tool receipts. Callers
// still register only ResultPaths; source metadata cannot introduce new files.
func ResultNames(value any, parts []map[string]any) map[string]string {
	names := map[string]string{}
	var walk func(any)
	walk = func(value any) {
		if raw, ok := value.(string); ok {
			if json.Unmarshal([]byte(raw), &value) != nil {
				return
			}
		}
		switch object := value.(type) {
		case map[string]any:
			name, _ := object["layer_name"].(string)
			if name == "" && object["reference_type"] == "project_layer" {
				name, _ = object["name"].(string)
			}
			if name != "" {
				for _, key := range []string{"source", "path", "output_path"} {
					path, _ := object[key].(string)
					path = strings.Split(path, "|layername=")[0]
					if filepath.IsAbs(path) {
						names[filepath.Clean(path)] = name
					}
				}
			}
			for key, child := range object {
				if key != "source_record" {
					switch child.(type) {
					case map[string]any, []any:
						walk(child)
					}
				}
			}
		case []any:
			for _, child := range object {
				walk(child)
			}
		}
	}
	walk(value)
	for _, part := range parts {
		if part["type"] == "text" {
			walk(part["text"])
		}
	}
	return names
}

// FailedResult examines tool/bridge envelopes, never arbitrary feature
// properties. A successful partial result with warnings is still an artifact.
func FailedResult(value any) bool {
	if raw, ok := value.(string); ok {
		if json.Unmarshal([]byte(raw), &value) != nil {
			return false
		}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	success, explicit := object["success"].(bool)
	if explicit && !success {
		return true
	}
	if !explicit {
		switch err := object["error"].(type) {
		case string:
			if strings.TrimSpace(err) != "" {
				return true
			}
		case map[string]any:
			if len(err) != 0 {
				return true
			}
		}
	}
	for _, key := range []string{"result", "data"} {
		if FailedResult(object[key]) {
			return true
		}
	}
	return false
}
