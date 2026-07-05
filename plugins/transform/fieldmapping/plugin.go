package fieldmapping

import (
	"fmt"

	"xdp/pkg/event"
	"xdp/pkg/plugin"
)

type Plugin struct{ mapping map[string]string }

func New() *Plugin { return &Plugin{} }
func (p *Plugin) Metadata() plugin.Metadata {
	return plugin.Metadata{Code: "field-mapping", Name: "Field Mapping", Type: plugin.TypeTransform, Version: "1.0.0", Runtime: "go", ConfigSchema: plugin.Schema{"type": "object"}}
}
func (p *Plugin) Validate(config map[string]any) error { _, err := mapping(config); return err }
func (p *Plugin) Init(ctx plugin.InitContext, config map[string]any) error {
	m, err := mapping(config)
	p.mapping = m
	return err
}
func (p *Plugin) Close() error { return nil }

func (p *Plugin) Process(ctx plugin.ProcessContext, e *event.Event) (*event.Event, error) {
	for from, to := range p.mapping {
		value, ok := e.Fields[from]
		if !ok {
			continue
		}
		e.Fields[to] = value
		delete(e.Fields, from)
	}
	return e, nil
}

func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}

func mapping(config map[string]any) (map[string]string, error) {
	raw, ok := config["mapping"].(map[string]any)
	if !ok {
		return map[string]string{}, nil
	}
	out := map[string]string{}
	for k, v := range raw {
		text, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("mapping.%s must be string", k)
		}
		out[k] = text
	}
	return out, nil
}
