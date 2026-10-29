package resource

import (
	"encoding/hex"
	"fmt"
	"path"
	"strings"

	"github.com/autocarto/carto_agent/internal/resource/bindings"
)

const LockAPIVersion = "flow-lock/v1"

type LayerLockEntry struct {
	ID     string `json:"id" yaml:"id"`
	Name   string `json:"name" yaml:"name"`
	Digest string `json:"digest" yaml:"digest"`
}
type Lock struct {
	Layers         []LayerLockEntry `json:"layers,omitempty" yaml:"layers,omitempty"`
	DocumentFormat string           `json:"document_format,omitempty" yaml:"document_format,omitempty"`
	APIVersion     string           `json:"api_version" yaml:"apiVersion"`
	Resources      []LockEntry      `json:"resources" yaml:"resources"`
}
type LockEntry struct {
	Type    string `json:"type" yaml:"type"`
	Name    string `json:"name" yaml:"name"`
	ID      string `json:"id" yaml:"id"`
	Version string `json:"version" yaml:"version"`
	Digest  string `json:"digest" yaml:"digest"`
	Source  string `json:"source" yaml:"source"`
	Bundled string `json:"bundled,omitempty" yaml:"bundled,omitempty"`
}

func (l *Lock) Find(typ, name string) *LockEntry {
	if l != nil {
		for i := range l.Resources {
			item := &l.Resources[i]
			if item.Type == typ && item.Name == name {
				return item
			}
		}
	}
	return nil
}
func (l *Lock) Validate() error {
	if l == nil || l.APIVersion != LockAPIVersion {
		return fmt.Errorf("flow.lock apiVersion 必须是 %s", LockAPIVersion)
	}
	seen := map[string]bool{}
	for _, e := range l.Layers {
		if !strings.HasPrefix(e.ID, "layer.") || !validDisplayName(e.Name) || !strings.HasPrefix(e.Digest, "sha256:") || len(e.Digest) != 71 {
			return fmt.Errorf("flow.lock 图层引用无效：%s", e.Name)
		}
		if seen[e.Name] {
			return fmt.Errorf("flow.lock 图层重复：%s", e.Name)
		}
		seen[e.Name] = true
	}
	seen = map[string]bool{}
	for _, e := range l.Resources {
		key := e.Type + "\x00" + e.Name
		if _, ok := TypeInfo(e.Type); !ok || !validDisplayName(e.Name) || bindings.ValidateKey("library:"+e.ID) != nil {
			return fmt.Errorf("flow.lock 资源类型、名称或编号无效")
		}
		if seen[key] {
			return fmt.Errorf("flow.lock 重复资源：%s", e.Name)
		}
		seen[key] = true
		if !versionPattern.MatchString(e.Version) || !strings.HasPrefix(e.Digest, "sha256:") || len(e.Digest) != 71 {
			return fmt.Errorf("flow.lock 版本或摘要无效：%s", e.Name)
		}
		if _, err := hex.DecodeString(strings.TrimPrefix(e.Digest, "sha256:")); err != nil {
			return fmt.Errorf("flow.lock 摘要无效：%s", e.Name)
		}
		if e.Source != "library_builtin" && e.Source != "library_user" && e.Source != "bundled" {
			return fmt.Errorf("flow.lock 来源无效：%s", e.Source)
		}
		if e.Source == "bundled" && e.Bundled == "" {
			return fmt.Errorf("flow.lock 随包资源缺少路径")
		}
		if e.Bundled != "" && (strings.ContainsAny(e.Bundled, "\\:") || path.Clean(e.Bundled) != strings.TrimSuffix(e.Bundled, "/") || (strings.TrimSuffix(e.Bundled, "/") != "resources/_deps/"+e.ID && strings.TrimSuffix(e.Bundled, "/") != "deps/"+e.ID)) {
			return fmt.Errorf("flow.lock 随包资源路径必须是 deps/<编号>/")
		}
	}
	return nil
}
func LockResource(typ, name string, r *ResolvedResource) LockEntry {
	return LockEntry{Type: typ, Name: name, ID: r.ID, Version: r.Version, Digest: r.Digest, Source: r.Owner}
}
func (e *LockEntry) Drift(r *ResolvedResource) *Diagnostic {
	if e == nil || r == nil || e.ID != r.ID || e.Digest == r.Digest {
		return nil
	}
	level := "info"
	if strings.Split(e.Version, ".")[0] != strings.Split(r.Version, ".")[0] {
		level = "warning"
	}
	return &Diagnostic{Level: level, Code: "FLOW_RESOURCE_CHANGED", Message: "资源版本或内容已变化；确认后可更新锁定。", Fix: map[string]any{"action": "update_lock", "resource_key": r.Key, "resource_id": r.ID, "locked_version": e.Version, "current_version": r.Version}}
}
