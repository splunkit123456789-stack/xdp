package splquery

import (
	"fmt"
	"strings"
	"unicode"

	"xdp/pkg/search/splstats"
)

const MaxQueryLength = 512

type Query struct {
	Raw     string
	Filters Filters
	Stats   *splstats.Query
}

type Filters struct {
	Index   string
	Keyword string
	Field   string
	Value   string
}

func Parse(input string) (Query, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return Query{}, fmt.Errorf("query is required")
	}
	if len(raw) > MaxQueryLength {
		return Query{}, fmt.Errorf("query is too long")
	}
	if strings.Count(raw, "|") > 1 {
		return Query{}, fmt.Errorf("only one pipe command is supported")
	}

	if strings.HasPrefix(strings.ToLower(raw), "stats ") || strings.HasPrefix(raw, "|") {
		stats, err := splstats.Parse(raw)
		if err != nil {
			return Query{}, err
		}
		return Query{Raw: raw, Stats: &stats}, nil
	}

	searchPart, commandPart, _ := strings.Cut(raw, "|")
	filters, err := parseFilters(searchPart)
	if err != nil {
		return Query{}, err
	}
	query := Query{Raw: raw, Filters: filters}
	if strings.TrimSpace(commandPart) != "" {
		stats, err := splstats.Parse(commandPart)
		if err != nil {
			return Query{}, err
		}
		query.Stats = &stats
	}
	return query, nil
}

func parseFilters(input string) (Filters, error) {
	filters := Filters{}
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
			filters.Field = value
		case "value":
			filters.Value = value
		default:
			field := strings.TrimPrefix(strings.ToLower(key), "fields.")
			if filters.Field != "" && filters.Field != field {
				return Filters{}, fmt.Errorf("only one field filter is supported")
			}
			filters.Field = field
			filters.Value = value
		}
	}
	if filters.Field != "" && filters.Value == "" {
		return Filters{}, fmt.Errorf("field filter value is required")
	}
	return filters, nil
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
