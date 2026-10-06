package skill

func effectiveSkillRuntime(def SkillDefinition) string {
	return NormalizeRuntime(firstNonEmpty(def.Metadata.Runtime, def.Metadata.Backend))
}
