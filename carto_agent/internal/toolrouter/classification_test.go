package toolrouter

import "testing"

func TestDefaultClassificationRegistryTreatsPluginAuthorityFieldToolsAsInteractive(t *testing.T) {
	t.Parallel()

	registry := NewDefaultClassificationRegistry()
	for _, name := range []string{
		"add_field_to_attributes_table",
		"add_autoincremental_field",
		"order_by_expression",
		"join_attributes_by_location",
		"text_to_float",
		"export_layout_pdf",
		"output_get_info",
	} {
		if !registry.IsInteractive(name) {
			t.Fatalf("expected %s to be interactive", name)
		}
		if registry.IsComputeOverride(name) {
			t.Fatalf("expected %s not to be compute override", name)
		}
	}
}

func TestDefaultClassificationRegistryDoesNotFallbackToHardcodedPatternsWhenConfigMissing(t *testing.T) {
	original := classificationConfigYAML
	classificationConfigYAML = nil
	t.Cleanup(func() {
		classificationConfigYAML = original
	})

	registry := NewDefaultClassificationRegistry()
	if registry.IsInteractive("export_layout_pdf") {
		t.Fatal("expected missing config not to fallback to hardcoded interactive patterns")
	}
	if registry.IsComputeOverride("output_init_dir") {
		t.Fatal("expected missing config not to fallback to hardcoded compute override patterns")
	}
}
