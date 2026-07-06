package plugin

type Type string

const (
	TypeInput         Type = "input"
	TypeParser        Type = "parser"
	TypeTransform     Type = "transform"
	TypeEnrichment    Type = "enrichment"
	TypeRouter        Type = "router"
	TypeOutput        Type = "output"
	TypeSearch        Type = "search"
	TypeSearchCommand Type = "search_command"
	TypeSPLFunction   Type = "spl_function"
)
