package capability

import (
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/autocarto/carto_agent/internal/toolrouter"
)

type PackageRef struct {
	Key         string
	PackageID   string
	Scope       string
	OwnerConfig string
}

type ToolCapability struct {
	Name              string
	RawName           string
	DisplayName       string
	Description       string
	Category          string
	Backend           string
	ProviderID        string
	Source            string
	InputSchema       map[string]any
	Available         bool
	UnavailableReason string
	InstanceIDs       []string
	Package           PackageRef
	PackageKind       string
	PackageEnabled    bool
	PackagePath       string
}

type PackageCapability struct {
	Key         string
	PackageID   string
	Name        string
	Kind        string
	Backend     string
	Source      string
	Enabled     bool
	Scope       string
	OwnerConfig string
	Path        string
	HasTools    bool
	HasConfig   bool
	ToolNames   []string
	References  []config.SkillReference
}

type ToolRef struct {
	Name        string
	Description string
	Package     PackageRef
}

type AgentCapabilityView struct {
	AgentName     string
	DisplayName   string
	Description   string
	IsMain        bool
	Enabled       bool
	SkillPackages []string
	Tools         []ToolRef
}

type CapabilitySnapshot struct {
	ConfigName           string
	RuntimeRevision      string
	ToolCatalogRevision  string
	SchemaRevision       string
	AvailabilityRevision string
	Tools                map[string]ToolCapability
	ToolOrder            []string
	Packages             map[string]PackageCapability
	Agents               []AgentCapabilityView
	BuiltAt              time.Time
}

type BuildInput struct {
	ActiveConfigName       string
	ActiveConfig           *config.AgentConfigFile
	RuntimeSkillConfig     *subagents.RuntimeSkillConfig
	Registry               *skill.Registry
	ToolAggregatorSnapshot toolrouter.Snapshot
	RuntimeSnapshot        middlewareclient.RuntimeSnapshot
	ComputeReady           bool
	PackageSummaries       []settings.SkillPackageSummary
	PackageDetails         map[string]settings.SkillPackageDetail
}
