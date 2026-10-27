// Package resource is the single authority for resolving declared resources.
package resource

import (
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/shared/diagnostic"
)

type Diagnostic = diagnostic.Diagnostic

type ResolvedResource struct {
	Key              string          `json:"key"`
	ID               string          `json:"id"`
	Label            string          `json:"label"`
	Name             string          `json:"name"`
	Type             string          `json:"type"`
	Version          string          `json:"version"`
	Owner            string          `json:"owner"`
	Origin           string          `json:"origin"`
	Location         string          `json:"location"`
	Root             string          `json:"root"`
	Entry            string          `json:"entry"`
	Format           string          `json:"format"`
	Geometry         string          `json:"geometry,omitempty"`
	ResolvedPath     string          `json:"-"`
	BaseDirFromInput string          `json:"base_dir_from_input,omitempty"`
	BaseDir          string          `json:"base_dir,omitempty"`
	BindingKey       string          `json:"binding_key,omitempty"`
	Digest           string          `json:"digest"`
	Overrides        string          `json:"overrides,omitempty"`
	OverriddenBy     string          `json:"overridden_by,omitempty"`
	Description      string          `json:"description,omitempty"`
	Source           ResourceSource  `json:"source,omitempty"`
	Legacy           bool            `json:"legacy"`
	Entries          []ResourceEntry `json:"entries"`
	Problems         []Diagnostic    `json:"problems"`
}
type ResourceEntry struct {
	LayerID       string            `json:"layer_id,omitempty"`
	Name          string            `json:"name"`
	Path          string            `json:"path"`
	Kind          string            `json:"kind"`
	Exists        bool              `json:"exists"`
	ExistsUnknown bool              `json:"exists_unknown,omitempty"`
	Extra         map[string]string `json:"extra"`
	Fields        map[string]any    `json:"fields"`
}

// Request is private parser input; all public lookups use Resolver.
type Request struct {
	Key, ResourceID, Label, Type, Location, FlowID, FlowDir, Entry, BaseDir, BaseDirFromInput, BaseDirOverride string
	EntryOverrides                                                                                             map[string]string
	EntryOrigins                                                                                               map[string]string
	EntryLayerIDs                                                                                              map[string]string
}
type LibraryItem struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Type         string         `json:"type"`
	Version      string         `json:"version"`
	Owner        string         `json:"owner"`
	Origin       string         `json:"origin"`
	Path         string         `json:"path"`
	Root         string         `json:"root"`
	Entry        string         `json:"entry"`
	Size         int64          `json:"size"`
	Digest       string         `json:"digest"`
	Overrides    string         `json:"overrides,omitempty"`
	OverriddenBy string         `json:"overridden_by,omitempty"`
	Description  string         `json:"description,omitempty"`
	Source       ResourceSource `json:"source,omitempty"`
	Legacy       bool           `json:"legacy"`
	Problems     []Diagnostic   `json:"problems"`
	Manifest     *Manifest      `json:"-"`
	ManifestPath string         `json:"-"`
}

// Unknown existence is distinct from a missing file before runtime inputs exist.
func (e ResourceEntry) MarshalJSON() ([]byte, error) {
	type alias ResourceEntry
	if e.ExistsUnknown {
		return json.Marshal(struct {
			alias
			Exists any `json:"exists"`
		}{alias(e), nil})
	}
	return json.Marshal(alias(e))
}
