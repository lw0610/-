package flow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"time"

	"github.com/autocarto/carto_agent/internal/resource"
)

type linkedMemory struct {
	fingerprint string
	compiled    string // 链接前计划内容的摘要
	plan        *FlowPlan
}

func resourceDeclaration(r *resource.ResolvedResource) ResourcePlan {
	return ResourcePlan{Key: r.Key, ID: r.ID, Label: r.Label, Name: r.Name, Type: r.Type, Version: r.Version, Owner: r.Owner, Origin: r.Origin, Location: r.Location, BaseDirFromInput: r.BaseDirFromInput, EntryCount: len(r.Entries)}
}

func (c *Compiler) linkFingerprint(ctx context.Context, source *compilationSource) (string, error) {
	return resourceLinkFingerprint(ctx, source.key, source.lock, source.resources)
}
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
		if r.Type == "data_list" || r.Type == "data_folder" {
			for _, entry := range r.Entries {
				if entry.ExistsUnknown {
					continue
				}
				path := resource.DataSourcePath(entry.Path)
				if err := statPath(path); err != nil {
					return "", err
				}
				for _, sidecar := range resource.ShapefileSidecarPaths(path) {
					if err := statPath(sidecar); err != nil {
						return "", err
					}
				}
			}
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

func (c *Compiler) linkSource(ctx context.Context, source *compilationSource, compiled *FlowPlan) (*FlowPlan, error) {
	return c.link(ctx, source, compiled, true)
}

// link 把资源快照写入计划。remember=false 用于未保存的草稿，避免覆盖已保存版本的链接记忆。
func (c *Compiler) link(ctx context.Context, source *compilationSource, compiled *FlowPlan, remember bool) (*FlowPlan, error) {
	fingerprint, err := c.linkFingerprint(ctx, source)
	if err != nil {
		return nil, err
	}
	key := source.stored.Meta.ID + "\x00" + source.env.Scheme
	digest := hashKey(compiled)
	c.linkMu.Lock()
	defer c.linkMu.Unlock()
	if remembered, ok := c.links[key]; remember && ok && remembered.fingerprint == fingerprint && remembered.compiled == digest {
		return cloneFlowPlan(remembered.plan), nil
	}
	plan := cloneFlowPlan(compiled)
	plan.FlowDir = source.stored.Dir
	plan.Lock = source.lock
	plan.Resources = []ResourcePlan{}
	plan.LinkDiagnostics = []Diagnostic{}
	for _, r := range source.resources {
		resolved := ResourcePlanFromResolved(r)
		resolved.EntryCount = len(r.Entries)
		plan.Resources = append(plan.Resources, resolved)
		plan.LinkDiagnostics = append(plan.LinkDiagnostics, r.Problems...)
	}
	plan.LinkDiagnostics = append(plan.LinkDiagnostics, executorResourceDiagnostics(plan)...)
	plan.LinkFingerprint = fingerprint
	plan.LinkedAt = time.Now().UTC()
	plan.Diagnostics = append(append([]Diagnostic{}, plan.CompileDiagnostics...), plan.LinkDiagnostics...)
	if remember {
		c.links[key] = linkedMemory{fingerprint: fingerprint, compiled: digest, plan: cloneFlowPlan(plan)}
	}
	return plan, nil
}

// Relink 在绑定等资源变化后刷新资源诊断，永不调用模型；旧返回布尔值固定为 false。
func (c *Compiler) Relink(ctx context.Context, id, scheme string) (*FlowPlan, bool, error) {
	plan, err := c.Plan(ctx, id, scheme)
	return plan, false, err
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
