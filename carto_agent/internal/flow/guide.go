package flow

import (
	"github.com/autocarto/carto_agent/internal/layers"
	"github.com/autocarto/carto_agent/internal/resource"
)

// GuideDoc is a free-form flow document. Only metadata and visual resource
// references are interpreted by the application; Body is supplied to the model.
type GuideDoc struct {
	Layers      []LayerReference `json:"layers"`
	Meta        FlowMetadata     `json:"meta"`
	Body        string           `json:"body"`
	Resources   []ResourceDoc    `json:"resources"`
	Diagnostics []Diagnostic     `json:"diagnostics"`
}

// A nil Resources slice requests syntax-only parsing. A non-nil snapshot enables
// exact resource-name checks without performing I/O inside ParseGuide.
type GuideEnvironment struct {
	Resources []resource.LibraryItem
	Layers    []layers.Item
}
type LayerReference struct {
	Name   string `json:"name"`
	ID     string `json:"id,omitempty"`
	Digest string `json:"digest,omitempty"`
	Line   int    `json:"line,omitempty"`
}
