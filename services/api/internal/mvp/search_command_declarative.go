package mvp

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"xdp/pkg/plugin"
	"xdp/pkg/search/splquery"
)

func executeDeclarativeSearchCommand(input plugin.SearchCommandResult, item PluginImportResponse, command splquery.Command) (plugin.SearchCommandResult, error) {
	kind := strings.TrimSpace(textFromMap(item.RuntimeConfig, "operation"))
	if kind == "" {
		kind = strings.TrimSpace(textFromMap(item.RuntimeConfig, "kind"))
	}
	if kind == "" {
		return plugin.SearchCommandResult{}, fmt.Errorf("search command plugin %s missing runtime_config.operation", item.PluginCode)
	}
	switch kind {
	case "project":
		return executeProjectCommand(input, command)
	case "sort":
		return executeSortCommand(input, command)
	case "limit":
		return executeLimitCommand(input, command)
	case "dedup":
		return executeDedupCommand(input, command)
	default:
		return plugin.SearchCommandResult{}, fmt.Errorf("unsupported search command operation %s", kind)
	}
}

func executeProjectCommand(input plugin.SearchCommandResult, command splquery.Command) (plugin.SearchCommandResult, error) {
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

func executeSortCommand(input plugin.SearchCommandResult, command splquery.Command) (plugin.SearchCommandResult, error) {
	fields, err := declarativeSortFields(command.Args)
	if err != nil {
		return plugin.SearchCommandResult{}, err
	}
	rows := append([]map[string]any(nil), input.Rows...)
	sort.SliceStable(rows, func(i, j int) bool {
		for _, field := range fields {
			cmp := declarativeCompareValues(rows[i][field.Name], rows[j][field.Name])
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

func executeLimitCommand(input plugin.SearchCommandResult, command splquery.Command) (plugin.SearchCommandResult, error) {
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

func executeDedupCommand(input plugin.SearchCommandResult, command splquery.Command) (plugin.SearchCommandResult, error) {
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

type declarativeSortField struct {
	Name string
	Desc bool
}

func declarativeSortFields(args []string) ([]declarativeSortField, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("sort requires at least one field")
	}
	fields := make([]declarativeSortField, 0, len(args))
	for i := 0; i < len(args); i++ {
		token := args[i]
		switch {
		case token == "-":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("sort requires a field after -")
			}
			fields = append(fields, declarativeSortField{Name: args[i+1], Desc: true})
			i++
		case strings.HasPrefix(token, "-") && len(token) > 1:
			fields = append(fields, declarativeSortField{Name: strings.TrimPrefix(token, "-"), Desc: true})
		case strings.HasPrefix(token, "+") && len(token) > 1:
			fields = append(fields, declarativeSortField{Name: strings.TrimPrefix(token, "+")})
		default:
			fields = append(fields, declarativeSortField{Name: token})
		}
	}
	for _, field := range fields {
		if strings.TrimSpace(field.Name) == "" {
			return nil, fmt.Errorf("sort requires a field")
		}
	}
	return fields, nil
}

func declarativeCompareValues(left any, right any) int {
	if leftTime, ok := declarativeTimeValue(left); ok {
		if rightTime, ok := declarativeTimeValue(right); ok {
			if leftTime.Before(rightTime) {
				return -1
			}
			if leftTime.After(rightTime) {
				return 1
			}
			return 0
		}
	}
	if leftNum, ok := declarativeNumberValue(left); ok {
		if rightNum, ok := declarativeNumberValue(right); ok {
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

func declarativeTimeValue(value any) (time.Time, bool) {
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

func declarativeNumberValue(value any) (float64, bool) {
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

func textFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	if value, ok := values[key].(string); ok {
		return value
	}
	return ""
}
