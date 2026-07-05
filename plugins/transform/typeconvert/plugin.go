package typeconvert

import (
	"fmt"
	"strconv"

	"xdp/pkg/event"
	"xdp/pkg/plugin"
)

type Plugin struct{ fields map[string]string }

func New() *Plugin { return &Plugin{} }
func (p *Plugin) Metadata() plugin.Metadata {
	return plugin.Metadata{Code: "type-convert", Name: "Type Convert", Type: plugin.TypeTransform, Version: "1.0.0", Runtime: "go", ConfigSchema: plugin.Schema{"type": "object"}}
}
func (p *Plugin) Validate(config map[string]any) error { _, err := parse(config); return err }
func (p *Plugin) Init(ctx plugin.InitContext, config map[string]any) error {
	fields, err := parse(config)
	p.fields = fields
	return err
}
func (p *Plugin) Close() error { return nil }

func (p *Plugin) Process(ctx plugin.ProcessContext, e *event.Event) (*event.Event, error) {
	for name, typ := range p.fields {
		value, ok := e.Fields[name]
		if !ok {
			continue
		}
		text := fmt.Sprint(value)
		switch typ {
		case "int":
			v, err := strconv.Atoi(text)
			if err != nil {
				return e, plugin.NewError(plugin.ErrTransformFailed, "int conversion failed", false, err)
			}
			e.Fields[name] = v
		case "float":
			v, err := strconv.ParseFloat(text, 64)
			if err != nil {
				return e, plugin.NewError(plugin.ErrTransformFailed, "float conversion failed", false, err)
			}
			e.Fields[name] = v
		case "bool":
			v, err := strconv.ParseBool(text)
			if err != nil {
				return e, plugin.NewError(plugin.ErrTransformFailed, "bool conversion failed", false, err)
			}
			e.Fields[name] = v
		case "string":
			e.Fields[name] = text
		}
	}
	return e, nil
}

func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}

func parse(config map[string]any) (map[string]string, error) {
	raw, ok := config["fields"].(map[string]any)
	if !ok {
		return map[string]string{}, nil
	}
	out := map[string]string{}
	for k, v := range raw {
		text, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("fields.%s must be string", k)
		}
		out[k] = text
	}
	return out, nil
}
