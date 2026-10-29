package flow

import "github.com/autocarto/carto_agent/internal/datasource"

type Environment struct {
	PlanningInstructions string
	DataSources          *datasource.Catalog
	Integrations         map[string]bool
	Secrets              map[string]bool
	AutoAcquire          string
	WebSearchEnabled     bool
	MaxDownloadMB        int
	MainProvider         string
	VisionProviders      []string
	PackageReady         func(string) bool
	Scheme               string
	SchemeRevision       string
	CatalogRevision      string
	Roles                []RoleSpec
	Packages             []PackageSpec
	Capabilities         []CapabilitySpec
}

type CapabilitySpec struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords"`
	PackageIDs  []string `json:"package_ids"`
	Parameters  []string `json:"parameters,omitempty"`
}

type PackageSpec struct {
	Kind        string   `json:"kind,omitempty"`
	Hidden      bool     `json:"hidden,omitempty"`
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Tools       []string `json:"tools"`
	Enabled     bool     `json:"enabled"`
}

// searchTokens 对连续中文取字符二元组，其他字母/数字按完整词检索。
