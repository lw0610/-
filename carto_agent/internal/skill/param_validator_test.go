package skill

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyExecutionOutputDefaultsUsesMetadataRuntimeForCLIAutoOutput(t *testing.T) {
	t.Parallel()
	session := SessionContext{SessionID: "sess-1", ProjectPath: "/tmp/demo.qgz"}
	updated := applyExecutionOutputDefaults(SkillDefinition{
		Metadata:  SkillMetadata{Name: "buffer", Runtime: RuntimeIDCLI},
		Spec:      SkillSpec{Parameters: []SkillParameter{{Name: "OUTPUT", Type: "output_layer"}}},
		Execution: SkillExecution{Output: &SkillOutput{AutoLoad: true}},
	}, map[string]any{}, session)

	outputPath, _ := updated["OUTPUT"].(string)
	if strings.TrimSpace(outputPath) == "" {
		t.Fatal("expected cli runtime auto output path to be populated")
	}
	if filepath.Ext(outputPath) != ".gpkg" {
		t.Fatalf("expected gpkg output path, got %q", outputPath)
	}
}
