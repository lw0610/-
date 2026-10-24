package compute

type ToolDefinition struct {
	Name              string                 `json:"name"`
	DisplayName       string                 `json:"display_name,omitempty"`
	Keywords          []string               `json:"keywords,omitempty"`
	Description       string                 `json:"description"`
	Category          string                 `json:"category"`
	InputSchema       map[string]interface{} `json:"inputSchema"`
	Backend           string                 `json:"backend,omitempty"`
	Available         bool                   `json:"available,omitempty"`
	UnavailableReason string                 `json:"unavailable_reason,omitempty"`
}

type ListToolsResponse struct {
	Tools []ToolDefinition `json:"tools"`
}

type InvokeRequest struct {
	ProjectID string                 `json:"project_id,omitempty"`
	Tool      string                 `json:"tool"`
	Params    map[string]interface{} `json:"params,omitempty"`
}

type OutputLayer struct {
	Path     string `json:"path"`
	AutoLoad bool   `json:"auto_load"`
}

type InvokeResponse struct {
	Success             bool           `json:"success"`
	Output              interface{}    `json:"output"`
	Error               string         `json:"error"`
	ErrorCode           string         `json:"error_code,omitempty"`
	OutputLayers        []OutputLayer  `json:"output_layers"`
	ProviderDiagnostics map[string]any `json:"provider_diagnostics,omitempty"`
}
