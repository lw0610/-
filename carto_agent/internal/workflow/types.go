package workflow

type Definition struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       Spec     `yaml:"spec"`
	SourcePath string   `yaml:"-"`
}

type Metadata struct {
	Name string `yaml:"name"`
}

type Spec struct {
	Inputs map[string]Input `yaml:"inputs,omitempty"`
	Steps  []Step           `yaml:"steps"`
}

type Input struct {
	Type     string `yaml:"type"`
	Required bool   `yaml:"required,omitempty"`
}

type Step struct {
	ID   string         `yaml:"id"`
	Type string         `yaml:"type"`
	Tool string         `yaml:"tool"`
	With map[string]any `yaml:"with,omitempty"`
}

const (
	APIVersionV1       = "workflow/v1"
	KindSerialWorkflow = "serial_workflow"
	StepTypeTool       = "tool"
)
