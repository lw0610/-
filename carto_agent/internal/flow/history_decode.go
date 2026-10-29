package flow

import (
	"encoding/json"
	"path/filepath"
)

// Legacy names are accepted only while reading saved plans. New responses and
// run snapshots serialize the canonical fields declared on FlowPlan.
func (p *FlowPlan) UnmarshalJSON(raw []byte) error {
	type current FlowPlan
	decoded := struct {
		*current
		LegacyCacheKey string `json:"cache_key"`
	}{current: (*current)(p)}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	if p.CompileKey == "" {
		p.CompileKey = decoded.LegacyCacheKey
	}
	return nil
}

func (r *ResourcePlan) UnmarshalJSON(raw []byte) error {
	type current ResourcePlan
	decoded := struct {
		*current
		LegacyPath string `json:"resolved_path"`
	}{current: (*current)(r)}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	if decoded.LegacyPath == "" {
		return nil
	}
	// Old directory resources had one path for the whole set. Preserve that
	// root without probing today's files or replacing the saved entries.
	switch r.Type {
	case "data_folder", "style_set", "symbols":
		if r.Root == "" {
			r.Root = decoded.LegacyPath
		}
	default:
		if r.Entry == "" {
			r.Entry = decoded.LegacyPath
		}
		if r.Root == "" {
			r.Root = filepath.Dir(r.Entry)
		}
	}
	return nil
}
