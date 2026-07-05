package indexrouter

import (
	"fmt"

	"xdp/pkg/event"
	"xdp/pkg/expr"
	"xdp/pkg/plugin"
)

type Plugin struct{ rules []Rule }
type Rule struct {
	When    string
	Set     map[string]any
	AddTags []string
}

func New() *Plugin { return &Plugin{} }
func (p *Plugin) Metadata() plugin.Metadata {
	return plugin.Metadata{Code: "index-router", Name: "Index Router", Type: plugin.TypeRouter, Version: "1.0.0", Runtime: "go", ConfigSchema: plugin.Schema{"type": "object"}}
}
func (p *Plugin) Validate(config map[string]any) error { _, err := parse(config); return err }
func (p *Plugin) Init(ctx plugin.InitContext, config map[string]any) error {
	rules, err := parse(config)
	p.rules = rules
	return err
}
func (p *Plugin) Close() error { return nil }

func (p *Plugin) Process(ctx plugin.ProcessContext, e *event.Event) (*event.Event, error) {
	for _, rule := range p.rules {
		ok, err := expr.Eval(rule.When, e)
		if err != nil {
			return e, plugin.NewError(plugin.ErrRouteFailed, "route expression failed", false, err)
		}
		if !ok {
			continue
		}
		for path, value := range rule.Set {
			if err := expr.Set(e, path, value); err != nil {
				return e, plugin.NewError(plugin.ErrRouteFailed, "set route field failed", false, err)
			}
		}
		for _, tag := range rule.AddTags {
			ctx.AddTag(tag)
		}
	}
	return e, nil
}

func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}

func parse(config map[string]any) ([]Rule, error) {
	raw, ok := config["rules"].([]any)
	if !ok {
		return nil, nil
	}
	rules := []Rule{}
	for _, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("rule must be object")
		}
		rule := Rule{When: fmt.Sprint(obj["when"]), Set: map[string]any{}}
		if set, ok := obj["set"].(map[string]any); ok {
			rule.Set = set
		}
		if tags, ok := obj["add_tags"].([]any); ok {
			for _, tag := range tags {
				rule.AddTags = append(rule.AddTags, fmt.Sprint(tag))
			}
		}
		rules = append(rules, rule)
	}
	return rules, nil
}
