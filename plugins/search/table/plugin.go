package table

import (
	"context"
	"fmt"

	"xdp/pkg/plugin"
)

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Code:        "table",
		Name:        "Table Search Command",
		Type:        plugin.TypeSearchCommand,
		Version:     "1.0.0",
		Runtime:     "go_builtin",
		Description: "P1 SPL table command for projecting selected fields.",
		ConfigSchema: plugin.Schema{
			"type": "object",
		},
		InputSchema:  plugin.Schema{"mode": "rows"},
		OutputSchema: plugin.Schema{"mode": "table"},
		Labels: map[string]string{
			"phase":       "P1",
			"status":      "active",
			"output_mode": "table",
		},
	}
}

func (p *Plugin) Validate(config map[string]any) error { return nil }

func (p *Plugin) Init(ctx plugin.InitContext, config map[string]any) error {
	return p.Validate(config)
}

func (p *Plugin) Execute(ctx context.Context, input plugin.SearchCommandInput, command plugin.SearchCommand) (plugin.SearchCommandResult, error) {
	if len(command.Args) == 0 {
		return plugin.SearchCommandResult{}, fmt.Errorf("table requires at least one field")
	}
	fields := append([]string(nil), command.Args...)
	rows := make([]map[string]any, 0, len(input.Rows))
	for _, row := range input.Rows {
		out := make(map[string]any, len(fields))
		for _, field := range fields {
			if value, ok := row[field]; ok {
				out[field] = value
			} else {
				out[field] = ""
			}
		}
		rows = append(rows, out)
	}
	return plugin.SearchCommandResult{Rows: rows, Fields: fields, OutputMode: "table"}, nil
}

func (p *Plugin) Close() error { return nil }

func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}
