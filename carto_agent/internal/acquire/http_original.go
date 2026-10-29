package acquire

import (
	"context"
	"encoding/json"
	"path/filepath"
)

// Different categories from the same regional archive share a single download.
// Store.Materialize already owns the keyed cross-process lock and quota policy.
func (e *Engine) fetchHTTPOriginal(ctx context.Context, adapter Adapter, request FetchRequest) (FetchResult, error) {
	spec := CacheSpec{SourceID: request.Source.ID, SourceVersion: request.Source.Version, Category: "raw_download", Query: RedactURL(request.Provide.URL), AdapterVersion: "v7-raw"}
	record, reused, err := e.Store.Materialize(ctx, spec, request.Source.RefreshDays, func(dir string) (Manifest, error) {
		request.WorkDir = filepath.Join(dir, "raw")
		fetched, err := adapter.Fetch(ctx, request)
		if err != nil {
			return Manifest{}, err
		}
		files := make([]string, 0, len(fetched.Files))
		for _, file := range fetched.Files {
			relative, err := filepath.Rel(dir, file)
			if err != nil {
				return Manifest{}, err
			}
			files = append(files, filepath.ToSlash(relative))
		}
		return Manifest{RawFiles: files, OutputFiles: files, License: request.Source.License, Attribution: request.Source.Attribution, Notes: fetched.Notes, Metadata: map[string]any{"fetch": fetched, "raw_download": true}}, nil
	})
	if err != nil {
		return FetchResult{}, err
	}
	raw, err := json.Marshal(record.Metadata["fetch"])
	if err != nil {
		return FetchResult{}, err
	}
	var fetched FetchResult
	if err = json.Unmarshal(raw, &fetched); err != nil {
		return FetchResult{}, err
	}
	fetched.Files = record.Raw()
	fetched.SHA256 = map[string]string{}
	for _, file := range record.Files {
		fetched.SHA256[filepath.Join(record.Dir, filepath.FromSlash(file.Path))] = file.SHA256
	}
	if reused {
		fetched.Notes = append(fetched.Notes, "复用同一来源与版本的已下载原始包，无需再次联网")
	}
	return fetched, nil
}
