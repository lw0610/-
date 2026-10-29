package flow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"

	"github.com/autocarto/carto_agent/internal/resource"
)

func resourceLinkFingerprint(ctx context.Context, key string, lock *resource.Lock, resources []*resource.ResolvedResource) (string, error) {
	digest := sha256.New()
	writeHashJSON(digest, key)
	writeHashJSON(digest, lock)
	seen := map[string]bool{}
	statPath := func(path string) error {
		if path == "" || seen[path] {
			return nil
		}
		seen[path] = true
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		writeHashJSON(digest, path)
		if os.IsNotExist(err) {
			writeHashJSON(digest, "missing")
			return nil
		}
		if err != nil {
			return err
		}
		writeHashJSON(digest, struct {
			Size, Modified int64
			Mode           string
		}{info.Size(), info.ModTime().UnixNano(), info.Mode().String()})
		return nil
	}
	for _, r := range resources {
		writeHashJSON(digest, struct {
			Root, Entry, Digest, BaseDir, BindingKey string
			Problems                                 []Diagnostic
			Entries                                  []resource.ResourceEntry
		}{r.Root, r.Entry, r.Digest, r.BaseDir, r.BindingKey, r.Problems, r.Entries})
		if r.Root != "" {
			if err := statPath(resource.ManifestPath(r.Root, r.Type)); err != nil {
				return "", err
			}
		}
		if err := statPath(resource.DataSourcePath(r.Entry)); err != nil {
			return "", err
		}

	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), nil
}

func cloneFlowPlan(plan *FlowPlan) *FlowPlan {
	if plan == nil {
		return nil
	}
	raw, _ := json.Marshal(plan)
	var cloned FlowPlan
	_ = json.Unmarshal(raw, &cloned)
	// Keep normalized resource field types, including []string and integer values.
	cloned.Resources = make([]ResourcePlan, len(plan.Resources))
	for i, r := range plan.Resources {
		cloned.Resources[i] = ResourcePlanFromResolved(r.ResolvedResource())
		cloned.Resources[i].EntryCount = r.EntryCount
	}
	return &cloned
}

// ResourceSnapshotFingerprint re-stats a persisted snapshot without rewriting
// its compiler key, identifiers, diagnostics or input-name conventions.
func ResourceSnapshotFingerprint(plan *FlowPlan) (string, error) {
	resources := []*resource.ResolvedResource{}
	for _, r := range plan.Resources {
		resources = append(resources, r.ResolvedResource())
	}
	return resourceLinkFingerprint(context.Background(), plan.CompileKey, plan.Lock, resources)
}
