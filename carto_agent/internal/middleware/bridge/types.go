package bridge

type ToolDefinition struct {
	Name              string         `json:"name"`
	Description       string         `json:"description"`
	Category          string         `json:"category,omitempty"`
	InputSchema       map[string]any `json:"inputSchema"`
	Backend           string         `json:"backend,omitempty"`
	Available         bool           `json:"available,omitempty"`
	UnavailableReason string         `json:"unavailable_reason,omitempty"`
	InstanceIDs       []string       `json:"instance_ids,omitempty"`
}

type OutputLayer struct {
	Path     string `json:"path"`
	AutoLoad bool   `json:"auto_load"`
}

type InvokeResult struct {
	Success             bool           `json:"success"`
	Output              any            `json:"output,omitempty"`
	Error               string         `json:"error,omitempty"`
	ErrorCode           string         `json:"error_code,omitempty"`
	OutputLayers        []OutputLayer  `json:"output_layers,omitempty"`
	Status              string         `json:"status,omitempty"`
	TerminalState       string         `json:"terminal_state,omitempty"`
	Progress            float64        `json:"progress,omitempty"`
	Message             string         `json:"message,omitempty"`
	ProviderDiagnostics map[string]any `json:"provider_diagnostics,omitempty"`
}

type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type PromptDefinition struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

type PromptMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type PromptResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}
