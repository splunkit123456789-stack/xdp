package plugin

type Schema map[string]any

type Metadata struct {
	Code             string            `json:"code"`
	Name             string            `json:"name"`
	Type             Type              `json:"type"`
	Version          string            `json:"version"`
	Description      string            `json:"description,omitempty"`
	Runtime          string            `json:"runtime"`
	Labels           map[string]string `json:"labels,omitempty"`
	ConfigSchema     Schema            `json:"config_schema"`
	InputSchema      Schema            `json:"input_schema,omitempty"`
	OutputSchema     Schema            `json:"output_schema,omitempty"`
	PermissionSchema Schema            `json:"permission_schema,omitempty"`
}
