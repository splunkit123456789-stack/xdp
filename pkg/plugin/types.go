package plugin

type Type string

const (
	TypeInput      Type = "input"
	TypeParser     Type = "parser"
	TypeTransform  Type = "transform"
	TypeEnrichment Type = "enrichment"
	TypeRouter     Type = "router"
	TypeOutput     Type = "output"
)
