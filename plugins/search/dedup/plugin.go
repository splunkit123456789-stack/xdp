package dedup

import (
	"context"
	"fmt"
	"strings"

	"xdp/pkg/plugin"
)

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Code:        "dedup",
		Name:        "Dedup Search Command",
		Type:        plugin.TypeSearchCommand,
		Version:     "1.0.0",
		Runtime:     "go_builtin",
		Description: "P1 SPL dedup command for keeping the first row per field key.",
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
	if len(command.Args) == 0 {
		return plugin.SearchCommandResult{}, fmt.Errorf("dedup requires at least one field")
	}
	seen := map[string]struct{}{}
	rows := make([]map[string]any, 0, len(input.Rows))
	for _, row := range input.Rows {
		keyParts := make([]string, 0, len(command.Args))
		for _, field := range command.Args {
			keyParts = append(keyParts, fmt.Sprint(row[field]))
		}
		key := strings.Join(keyParts, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		rows = append(rows, row)
	}
	return plugin.SearchCommandResult{Rows: rows, Fields: input.Fields, OutputMode: "rows"}, nil
}

func (p *Plugin) Close() error { return nil }

func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}
