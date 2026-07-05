package event

type Source struct {
	Type string `json:"type"`
	Name string `json:"name"`
	Host string `json:"host,omitempty"`
	IP   string `json:"ip,omitempty"`
}
