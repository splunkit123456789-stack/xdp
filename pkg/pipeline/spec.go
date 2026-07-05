package pipeline

type Pipeline struct {
	APIVersion string   `json:"api_version" yaml:"api_version"`
	Kind       string   `json:"kind" yaml:"kind"`
	Metadata   Metadata `json:"metadata" yaml:"metadata"`
	Spec       Spec     `json:"spec" yaml:"spec"`
}

type Metadata struct {
	ID          string            `json:"id" yaml:"id"`
	Name        string            `json:"name" yaml:"name"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Tags        []string          `json:"tags,omitempty" yaml:"tags,omitempty"`
}

type Spec struct {
	Source   SourceSpec   `json:"source" yaml:"source"`
	Stages   []StageSpec  `json:"stages" yaml:"stages"`
	Outputs  []OutputSpec `json:"outputs" yaml:"outputs"`
	Settings Settings     `json:"settings,omitempty" yaml:"settings,omitempty"`
}

type SourceSpec struct {
	ID      string         `json:"id" yaml:"id"`
	Type    string         `json:"type" yaml:"type"`
	Plugin  string         `json:"plugin" yaml:"plugin"`
	Version string         `json:"version" yaml:"version"`
	Enabled *bool          `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Config  map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
	OnError *ErrorPolicy   `json:"on_error,omitempty" yaml:"on_error,omitempty"`
}

type StageSpec struct {
	ID      string         `json:"id" yaml:"id"`
	Type    string         `json:"type" yaml:"type"`
	Plugin  string         `json:"plugin,omitempty" yaml:"plugin,omitempty"`
	Version string         `json:"version,omitempty" yaml:"version,omitempty"`
	Enabled *bool          `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	When    string         `json:"when,omitempty" yaml:"when,omitempty"`
	Config  map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
	OnError *ErrorPolicy   `json:"on_error,omitempty" yaml:"on_error,omitempty"`
	Stages  []StageSpec    `json:"stages,omitempty" yaml:"stages,omitempty"`
}

type OutputSpec struct {
	ID      string         `json:"id" yaml:"id"`
	Plugin  string         `json:"plugin" yaml:"plugin"`
	Version string         `json:"version" yaml:"version"`
	Enabled *bool          `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	When    string         `json:"when,omitempty" yaml:"when,omitempty"`
	Config  map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
	OnError *ErrorPolicy   `json:"on_error,omitempty" yaml:"on_error,omitempty"`
}

type Settings struct {
	Execution     map[string]any `json:"execution,omitempty" yaml:"execution,omitempty"`
	ErrorPolicy   *ErrorPolicy   `json:"error_policy,omitempty" yaml:"error_policy,omitempty"`
	Observability map[string]any `json:"observability,omitempty" yaml:"observability,omitempty"`
	Delivery      map[string]any `json:"delivery,omitempty" yaml:"delivery,omitempty"`
}

type ErrorPolicy struct {
	Action string         `json:"action" yaml:"action"`
	Retry  map[string]any `json:"retry,omitempty" yaml:"retry,omitempty"`
}
