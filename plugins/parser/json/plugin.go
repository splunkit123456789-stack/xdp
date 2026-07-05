package json

import (
	"encoding/json"
	"fmt"

	"xdp/pkg/event"
	"xdp/pkg/plugin"
)

type Parser struct {
	cfg Config
}

type Config struct {
	Source  string
	Target  string
	Flatten bool
}

func New() *Parser {
	return &Parser{}
}

func (p *Parser) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Code:        "json-parser",
		Name:        "JSON Parser",
		Type:        plugin.TypeParser,
		Version:     "1.0.0",
		Description: "Parse raw JSON into event fields",
		Runtime:     "go",
		ConfigSchema: plugin.Schema{
			"type": "object",
			"properties": map[string]any{
				"source":  map[string]any{"type": "string", "default": "raw"},
				"target":  map[string]any{"type": "string", "default": "fields"},
				"flatten": map[string]any{"type": "boolean", "default": false},
			},
		},
	}
}

func (p *Parser) Validate(config map[string]any) error {
	_, err := parseConfig(config)
	return err
}

func (p *Parser) Init(ctx plugin.InitContext, config map[string]any) error {
	cfg, err := parseConfig(config)
	if err != nil {
		return err
	}
	p.cfg = cfg
	return nil
}

func (p *Parser) Process(ctx plugin.ProcessContext, e *event.Event) (*event.Event, error) {
	if p.cfg.Source != "raw" || p.cfg.Target != "fields" {
		return e, plugin.NewError(plugin.ErrInvalidConfig, "json-parser only supports source=raw and target=fields in MVP", false, nil)
	}

	var fields map[string]any
	if err := json.Unmarshal([]byte(e.Raw), &fields); err != nil {
		return e, plugin.NewError(plugin.ErrParseFailed, "invalid json", false, err)
	}
	if e.Fields == nil {
		e.Fields = map[string]any{}
	}
	for key, value := range fields {
		e.Fields[key] = value
	}
	return e, nil
}

func (p *Parser) Close() error {
	return nil
}

func Register(reg *plugin.Registry) error {
	parser := New()
	return reg.Register(parser.Metadata(), func() any { return New() })
}

func parseConfig(config map[string]any) (Config, error) {
	cfg := Config{Source: "raw", Target: "fields"}
	if value, ok := config["source"]; ok {
		source, ok := value.(string)
		if !ok {
			return cfg, fmt.Errorf("source must be a string")
		}
		cfg.Source = source
	}
	if value, ok := config["target"]; ok {
		target, ok := value.(string)
		if !ok {
			return cfg, fmt.Errorf("target must be a string")
		}
		cfg.Target = target
	}
	if value, ok := config["flatten"]; ok {
		flatten, ok := value.(bool)
		if !ok {
			return cfg, fmt.Errorf("flatten must be a boolean")
		}
		cfg.Flatten = flatten
	}
	return cfg, nil
}
