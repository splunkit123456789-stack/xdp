package splquery

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"xdp/pkg/search/splstats"
)

const MaxQueryLength = 512

type Query struct {
	Raw      string
	Filters  Filters
	Stats    *splstats.Query
	Commands []Command
}

type Command struct {
	Name string
	Args []string
	Raw  string
}

type Filters struct {
	Index        string
	Keyword      string
	Field        string
	Value        string
	FieldFilters []FieldFilter
}

type FieldFilter struct {
	Field string
	Value string
}

func Parse(input string) (Query, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return Query{}, fmt.Errorf("query is required")
	}
	if len(raw) > MaxQueryLength {
		return Query{}, fmt.Errorf("query is too long")
	}
	parts := splitPipeline(raw)
	if len(parts) == 0 {
		return Query{}, fmt.Errorf("query is required")
	}
	first := strings.TrimSpace(parts[0])
	if len(parts) == 1 && strings.HasPrefix(strings.ToLower(first), "stats ") {
		stats, err := splstats.Parse(first)
		if err != nil {
			return Query{}, err
		}
		return Query{Raw: raw, Stats: &stats}, nil
	}

	query := Query{Raw: raw}
	commandParts := parts[1:]
	if first == "" {
		commandParts = parts[1:]
	} else if strings.HasPrefix(strings.ToLower(first), "stats ") {
		stats, err := splstats.Parse(first)
		if err != nil {
			return Query{}, err
		}
		query.Stats = &stats
	} else {
		filters, err := parseFilters(first)
		if err != nil {
			return Query{}, err
		}
		query.Filters = filters
	}

	if len(commandParts) > 0 {
		if query.Stats == nil {
			command := strings.TrimSpace(commandParts[0])
			if strings.HasPrefix(strings.ToLower(command), "stats ") {
				stats, err := splstats.Parse(command)
				if err != nil {
					return Query{}, err
				}
				query.Stats = &stats
				commandParts = commandParts[1:]
			}
		}
		commands, err := parseCommands(commandParts)
		if err != nil {
			return Query{}, err
		}
		query.Commands = commands
	}
	return query, nil
}

func splitPipeline(input string) []string {
	rawParts := strings.Split(input, "|")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		parts = append(parts, strings.TrimSpace(part))
	}
	return parts
}

func parseCommands(parts []string) ([]Command, error) {
	commands := make([]Command, 0, len(parts))
	for _, part := range parts {
		command, err := parseCommand(part)
		if err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}
	return commands, nil
}

func parseCommand(input string) (Command, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return Command{}, fmt.Errorf("empty pipe command")
	}
	tokens := splitFilterTerms(raw)
	if len(tokens) == 0 {
		return Command{}, fmt.Errorf("empty pipe command")
	}
	name := strings.ToLower(tokens[0])
	args := tokens[1:]
	command := Command{Name: name, Args: args, Raw: raw}
	switch name {
	case "table":
		if len(args) == 0 {
			return Command{}, fmt.Errorf("table requires at least one field")
		}
		return command, validateFields(args)
	case "sort":
		fields, err := sortCommandFields(args)
		if err != nil {
			return Command{}, err
		}
		return command, validateFields(fields)
	case "head":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("head requires one positive integer")
		}
		limit, err := strconv.Atoi(args[0])
		if err != nil || limit <= 0 {
			return Command{}, fmt.Errorf("head requires one positive integer")
		}
		return command, nil
	case "dedup":
		if len(args) == 0 {
			return Command{}, fmt.Errorf("dedup requires at least one field")
		}
		return command, validateFields(args)
	default:
		return Command{}, fmt.Errorf("unsupported search command: %s", name)
	}
}

func sortCommandFields(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("sort requires at least one field")
	}
	fields := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		token := args[i]
		switch {
		case token == "-":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("sort requires a field after -")
			}
			fields = append(fields, args[i+1])
			i++
		case strings.HasPrefix(token, "-") && len(token) > 1:
			fields = append(fields, strings.TrimPrefix(token, "-"))
		case strings.HasPrefix(token, "+") && len(token) > 1:
			fields = append(fields, strings.TrimPrefix(token, "+"))
		default:
			fields = append(fields, token)
		}
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("sort requires at least one field")
	}
	return fields, nil
}

func validateFields(fields []string) error {
	for _, field := range fields {
		if !isFieldKey(field) {
			return fmt.Errorf("invalid field: %s", field)
		}
	}
	return nil
}

func parseFilters(input string) (Filters, error) {
	filters := Filters{}
	explicitField := ""
	explicitValue := ""
	for _, token := range filterTokens(input) {
		key, value, ok := strings.Cut(token, "=")
		if !ok {
			if err := validateValue(token); err != nil {
				return Filters{}, err
			}
			if filters.Keyword != "" {
				return Filters{}, fmt.Errorf("only one keyword term is supported")
			}
			filters.Keyword = token
			continue
		}
		key = strings.TrimSpace(key)
		value = unquoteFilterValue(strings.TrimSpace(value))
		if err := validateKeyValue(key, value); err != nil {
			return Filters{}, err
		}
		switch strings.ToLower(key) {
		case "index", "metadata.index", "root.index":
			filters.Index = value
		case "keyword":
			filters.Keyword = value
		case "field":
			explicitField = strings.TrimPrefix(strings.ToLower(value), "fields.")
		case "value":
			explicitValue = value
		default:
			field := strings.TrimPrefix(strings.ToLower(key), "fields.")
			if err := addFieldFilter(&filters, field, value); err != nil {
				return Filters{}, err
			}
		}
	}
	if explicitField != "" {
		if err := addFieldFilter(&filters, explicitField, explicitValue); err != nil {
			return Filters{}, err
		}
	}
	if filters.Field != "" && filters.Value == "" {
		return Filters{}, fmt.Errorf("field filter value is required")
	}
	return filters, nil
}

func addFieldFilter(filters *Filters, field string, value string) error {
	field = strings.TrimSpace(field)
	if field == "" || !isFieldKey(field) {
		return fmt.Errorf("invalid field filter: %s", field)
	}
	if value == "" {
		return fmt.Errorf("field filter value is required")
	}
	filters.FieldFilters = append(filters.FieldFilters, FieldFilter{Field: field, Value: value})
	if filters.Field == "" {
		filters.Field = field
		filters.Value = value
	}
	return nil
}

func filterTokens(input string) []string {
	parts := splitFilterTerms(strings.TrimSpace(input))
	tokens := make([]string, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		token := parts[i]
		if token == "" {
			continue
		}
		if i+2 < len(parts) && parts[i+1] == "=" {
			token = token + "=" + parts[i+2]
			i += 2
		} else if strings.HasSuffix(token, "=") && i+1 < len(parts) && !strings.Contains(parts[i+1], "=") {
			token += parts[i+1]
			i++
		} else if !strings.Contains(token, "=") && i+1 < len(parts) && strings.HasPrefix(parts[i+1], "=") {
			token += parts[i+1]
			i++
		}
		tokens = append(tokens, token)
	}
	return tokens
}

func splitFilterTerms(input string) []string {
	var terms []string
	var current strings.Builder
	var quote rune
	for _, r := range input {
		if quote != 0 {
			current.WriteRune(r)
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '"' || r == '\'' {
			quote = r
			current.WriteRune(r)
			continue
		}
		if unicode.IsSpace(r) {
			if current.Len() > 0 {
				terms = append(terms, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		terms = append(terms, current.String())
	}
	return terms
}

func unquoteFilterValue(value string) string {
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func validateKeyValue(key string, value string) error {
	if key == "" || !isFieldKey(key) {
		return fmt.Errorf("invalid filter key: %s", key)
	}
	return validateValue(value)
}

func validateValue(value string) error {
	if value == "" || len(value) > 128 {
		return fmt.Errorf("invalid filter value")
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '_', '-', '.', ':', '/', '@':
			continue
		default:
			return fmt.Errorf("unsupported filter character %q", r)
		}
	}
	return nil
}

func isFieldKey(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for i, r := range value {
		if unicode.IsLetter(r) || r == '_' {
			continue
		}
		if i > 0 && unicode.IsDigit(r) {
			continue
		}
		if i > 0 && r == '.' {
			continue
		}
		return false
	}
	return true
}
