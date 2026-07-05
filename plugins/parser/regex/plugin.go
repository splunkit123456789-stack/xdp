package regex

import (
	"fmt"
	"regexp"

	"xdp/pkg/event"
	"xdp/pkg/plugin"
)

type Parser struct{ pattern *regexp.Regexp }

func New() *Parser { return &Parser{} }
func (p *Parser) Metadata() plugin.Metadata {
	return plugin.Metadata{Code: "regex-parser", Name: "Regex Parser", Type: plugin.TypeParser, Version: "1.0.0", Runtime: "go", ConfigSchema: plugin.Schema{"type": "object"}}
}
func (p *Parser) Validate(config map[string]any) error { _, err := compile(config); return err }
func (p *Parser) Init(ctx plugin.InitContext, config map[string]any) error {
	pattern, err := compile(config)
	p.pattern = pattern
	return err
}
func (p *Parser) Close() error { return nil }

func (p *Parser) Process(ctx plugin.ProcessContext, e *event.Event) (*event.Event, error) {
	matches := p.pattern.FindStringSubmatch(e.Raw)
	if matches == nil {
		return e, plugin.NewError(plugin.ErrParseFailed, "regex did not match", false, nil)
	}
	for i, name := range p.pattern.SubexpNames() {
		if i == 0 || name == "" {
			continue
		}
		e.Fields[name] = matches[i]
	}
	return e, nil
}

func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}

func compile(config map[string]any) (*regexp.Regexp, error) {
	pattern, ok := config["pattern"].(string)
	if !ok || pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	return regexp.Compile(pattern)
}
