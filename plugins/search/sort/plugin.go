package sortcmd

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"xdp/pkg/plugin"
)

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Code:        "sort",
		Name:        "Sort Search Command",
		Type:        plugin.TypeSearchCommand,
		Version:     "1.0.0",
		Runtime:     "go_builtin",
		Description: "P1 SPL sort command for ordering rows by one or more fields.",
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
	fields, err := sortFields(command.Args)
	if err != nil {
		return plugin.SearchCommandResult{}, err
	}
	rows := append([]map[string]any(nil), input.Rows...)
	sort.SliceStable(rows, func(i, j int) bool {
		for _, field := range fields {
			cmp := compareValues(rows[i][field.Name], rows[j][field.Name])
			if cmp == 0 {
				continue
			}
			if field.Desc {
				return cmp > 0
			}
			return cmp < 0
		}
		return false
	})
	return plugin.SearchCommandResult{Rows: rows, Fields: input.Fields, OutputMode: "rows"}, nil
}

type sortField struct {
	Name string
	Desc bool
}

func sortFields(args []string) ([]sortField, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("sort requires at least one field")
	}
	fields := make([]sortField, 0, len(args))
	for i := 0; i < len(args); i++ {
		token := args[i]
		switch {
		case token == "-":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("sort requires a field after -")
			}
			fields = append(fields, sortField{Name: args[i+1], Desc: true})
			i++
		case strings.HasPrefix(token, "-") && len(token) > 1:
			fields = append(fields, sortField{Name: strings.TrimPrefix(token, "-"), Desc: true})
		case strings.HasPrefix(token, "+") && len(token) > 1:
			fields = append(fields, sortField{Name: strings.TrimPrefix(token, "+")})
		default:
			fields = append(fields, sortField{Name: token})
		}
	}
	for _, field := range fields {
		if strings.TrimSpace(field.Name) == "" {
			return nil, fmt.Errorf("sort requires a field")
		}
	}
	return fields, nil
}

func compareValues(left any, right any) int {
	if leftTime, ok := timeValue(left); ok {
		if rightTime, ok := timeValue(right); ok {
			if leftTime.Before(rightTime) {
				return -1
			}
			if leftTime.After(rightTime) {
				return 1
			}
			return 0
		}
	}
	if leftNum, ok := numberValue(left); ok {
		if rightNum, ok := numberValue(right); ok {
			if leftNum < rightNum {
				return -1
			}
			if leftNum > rightNum {
				return 1
			}
			return 0
		}
	}
	return strings.Compare(fmt.Sprint(left), fmt.Sprint(right))
}

func timeValue(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed, true
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, typed); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func (p *Plugin) Close() error { return nil }

func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}
