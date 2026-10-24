package skill

import "fmt"

// ValidateToolPackageImport rejects new method-only packages while keeping the
// existing package reader available for previously installed legacy content.
func ValidateToolPackageImport(pkg SkillPackage) error {
	explicit := normalizeStringSlice(append(append([]string{}, pkg.FrontMatter.ToolRefs...), pkg.FrontMatter.Tools...))
	pkg.normalize()
	if pkg.Kind != SkillPackageKindCategory || len(explicit) == 0 {
		return fmt.Errorf("只能导入 kind: category 且含 tools/tool_refs 工具清单的工具包；方法说明请加入现有工具包的使用说明或 references，流程请导入 FLOW.md")
	}
	return nil
}
