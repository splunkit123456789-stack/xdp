package stats

import (
	"context"
	"fmt"

	"xdp/pkg/plugin"
	"xdp/pkg/search/splstats"
)

type Plugin struct{}

func New() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Code:        "stats",
		Name:        "Stats Search Command",
		Type:        plugin.TypeSearch,
		Version:     "1.0.0",
		Runtime:     "go_builtin",
		Description: "P0 built-in SPL stats command for count, sum, avg, min and max aggregations.",
		ConfigSchema: plugin.Schema{
			"type": "object",
			"properties": map[string]any{
				"functions":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "default": []string{"count", "sum", "avg", "min", "max"}},
				"max_group_fields":    map[string]any{"type": "integer", "default": 5},
				"max_aggregations":    map[string]any{"type": "integer", "default": 5},
				"max_result_rows":     map[string]any{"type": "integer", "default": 1000},
				"allow_json_fallback": map[string]any{"type": "boolean", "default": true},
			},
		},
		InputSchema: plugin.Schema{
			"mode": "events",
		},
		OutputSchema: plugin.Schema{
			"mode": "stats",
		},
		Labels: map[string]string{
			"phase":       "P0",
			"status":      "active",
			"output_mode": "stats",
		},
	}
}

func (p *Plugin) Validate(config map[string]any) error {
	for key := range config {
		switch key {
		case "functions", "max_group_fields", "max_aggregations", "max_result_rows", "allow_json_fallback":
		default:
			return fmt.Errorf("unsupported stats config: %s", key)
		}
	}
	return nil
}

func (p *Plugin) Init(ctx plugin.InitContext, config map[string]any) error {
	return p.Validate(config)
}

func (p *Plugin) Execute(ctx context.Context, input plugin.SearchInput, query splstats.Query) (splstats.Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := query.Validate(); err != nil {
		return splstats.Result{}, err
	}
	if input.Backend == nil {
		return splstats.Result{}, fmt.Errorf("stats search backend is required")
	}
	result, err := input.Backend.Stats(ctx, plugin.SearchStatsQuery{
		Index:        input.Index,
		Keyword:      input.Keyword,
		Field:        input.Field,
		Value:        input.Value,
		FieldFilters: input.FieldFilters,
		StartTime:    input.StartTime,
		EndTime:      input.EndTime,
		Limit:        input.Limit,
		Offset:       input.Offset,
		Stats:        query,
		HotFields:    input.HotFields,
	})
	if err != nil {
		return splstats.Result{}, err
	}
	if result.Query == "" {
		result.Query = query.Raw
	}
	return result, nil
}

func (p *Plugin) Close() error {
	return nil
}

func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}
