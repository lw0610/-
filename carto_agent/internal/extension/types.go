package extension

type Manifest struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       Spec     `yaml:"spec"`
	SourcePath string   `yaml:"-"`
}

type Metadata struct {
	Name        string `yaml:"name"`
	DisplayName string `yaml:"display_name,omitempty"`
	Version     string `yaml:"version,omitempty"`
}

type Spec struct {
	SourceDir  string     `yaml:"source_dir,omitempty"`
	Resources  []Resource `yaml:"resources,omitempty"`
	Activation Activation `yaml:"activation"`
}

type Activation struct {
	Scope            string `yaml:"scope"`
	OwnerConfig      string `yaml:"owner_config,omitempty"`
	EnabledByDefault bool   `yaml:"enabled_by_default,omitempty"`
	RebuildPolicy    string `yaml:"rebuild_policy,omitempty"`
}

type Resource struct {
	Type      string `yaml:"type"`
	SourceDir string `yaml:"source_dir"`
}

const (
	APIVersionV1         = "extension/v1"
	APIVersionV2         = "extension/v2"
	KindSkillPackage     = "skill-package"
	KindPackageImport    = "PackageImport"
	ResourceToolPackage  = "tool_package"
	ResourceSkillPackage = "skill_package"
)
