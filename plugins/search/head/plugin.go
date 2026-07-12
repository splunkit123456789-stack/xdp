package head

import (
	"context"
	"fmt"
	"strconv"

	"xdp/pkg/plugin"
)

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Code:        "head",
		Name:        "Head Search Command",
		Type:        plugin.TypeSearchCommand,
		Version:     "1.0.0",
		Runtime:     "go_builtin",
		Description: "P1 SPL head command for limiting row count.",
		ConfigSchema: plugin.Schema{
			"type": "object",
		},
		InputSchema:  plugin.Schema{"mode": "rows"},
		OutputSchema: plugin.Schema{"mode": "rows"},
		Labels: map[string]string{
			"phase":       "P1",
			"status":      "active",
			"output_mode": "rows",
		},
	}
}

func (p *Plugin) Validate(config map[string]any) error { return nil }

func (p *Plugin) Init(ctx plugin.InitContext, config map[string]any) error {
	return p.Validate(config)
}

func (p *Plugin) Execute(ctx context.Context, input plugin.SearchCommandInput, command plugin.SearchCommand) (plugin.SearchCommandResult, error) {
	if len(command.Args) != 1 {
		return plugin.SearchCommandResult{}, fmt.Errorf("head requires one positive integer")
	}
	limit, err := strconv.Atoi(command.Args[0])
	if err != nil || limit <= 0 {
		return plugin.SearchCommandResult{}, fmt.Errorf("head requires one positive integer")
	}
	rows := input.Rows
	if limit < len(rows) {
		rows = rows[:limit]
	}
	return plugin.SearchCommandResult{Rows: rows, Fields: input.Fields, OutputMode: "rows"}, nil
}

func (p *Plugin) Close() error { return nil }

func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}
