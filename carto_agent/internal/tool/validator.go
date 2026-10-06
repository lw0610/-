package tool

func ValidateDefinition(def ToolDefinition) error {
	return def.Validate()
}

func ValidateDefinitions(defs []ToolDefinition) error {
	for _, def := range defs {
		if err := def.Validate(); err != nil {
			return err
		}
	}
	return nil
}
