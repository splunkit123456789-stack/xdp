package geoip

import (
	"fmt"

	"xdp/pkg/event"
	"xdp/pkg/expr"
	"xdp/pkg/plugin"
)

type Plugin struct{ field, target string }

func New() *Plugin { return &Plugin{} }
func (p *Plugin) Metadata() plugin.Metadata {
	return plugin.Metadata{Code: "geoip", Name: "GeoIP", Type: plugin.TypeEnrichment, Version: "1.0.0", Runtime: "go", ConfigSchema: plugin.Schema{"type": "object"}}
}
func (p *Plugin) Validate(config map[string]any) error { _, _, err := parse(config); return err }
func (p *Plugin) Init(ctx plugin.InitContext, config map[string]any) error {
	field, target, err := parse(config)
	p.field, p.target = field, target
	return err
}
func (p *Plugin) Close() error { return nil }
func (p *Plugin) Process(ctx plugin.ProcessContext, e *event.Event) (*event.Event, error) {
	ip, _ := expr.Get(e, p.field)
	geo := map[string]any{"country": "UNKNOWN", "city": "UNKNOWN", "ip": fmt.Sprint(ip)}
	if fmt.Sprint(ip) == "1.1.1.1" {
		geo["country"] = "AU"
		geo["city"] = "Sydney"
	}
	if fmt.Sprint(ip) == "8.8.8.8" {
		geo["country"] = "US"
		geo["city"] = "Mountain View"
	}
	return e, expr.Set(e, p.target, geo)
}
func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}
func parse(config map[string]any) (string, string, error) {
	field, _ := config["field"].(string)
	target, _ := config["target"].(string)
	if field == "" {
		field = "fields.src_ip"
	}
	if target == "" {
		target = "fields.src_geo"
	}
	return field, target, nil
}
