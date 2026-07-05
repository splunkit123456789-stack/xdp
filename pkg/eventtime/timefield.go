package eventtime

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

func FromRaw(raw string, fieldPath string) (time.Time, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return time.Time{}, err
	}
	value, ok := valueAtPath(payload, fieldPath)
	if !ok {
		return time.Time{}, strconv.ErrSyntax
	}
	return Parse(value)
}

func Parse(value any) (time.Time, error) {
	switch v := value.(type) {
	case string:
		return ParseString(v)
	case json.Number:
		return parseNumeric(v.String())
	case float64:
		return unixFromFloat(v), nil
	case int64:
		return unixFromInt(v), nil
	case int:
		return unixFromInt(int64(v)), nil
	default:
		return time.Time{}, strconv.ErrSyntax
	}
}

func ParseOptional(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	return ParseString(value)
}

func ParseString(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, strconv.ErrSyntax
	}
	if parsed, err := parseNumeric(value); err == nil {
		return parsed, nil
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05.000",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range formats {
		if strings.Contains(layout, "Z07") {
			if t, err := time.Parse(layout, value); err == nil {
				return t.UTC(), nil
			}
			continue
		}
		if t, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, strconv.ErrSyntax
}

func valueAtPath(payload map[string]any, fieldPath string) (any, bool) {
	parts := strings.Split(fieldPath, ".")
	var current any = payload
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = obj[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func parseNumeric(value string) (time.Time, error) {
	if strings.ContainsAny(value, ".eE") {
		n, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return time.Time{}, err
		}
		return unixFromFloat(n), nil
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return unixFromInt(n), nil
}

func unixFromFloat(value float64) time.Time {
	seconds := int64(value)
	nanos := int64((value - float64(seconds)) * 1e9)
	return time.Unix(seconds, nanos).UTC()
}

func unixFromInt(value int64) time.Time {
	abs := value
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs >= 100000000000000000:
		return time.Unix(value/1e9, (value % 1e9)).UTC()
	case abs >= 100000000000000:
		return time.Unix(value/1e6, (value%1e6)*1e3).UTC()
	case abs >= 100000000000:
		return time.Unix(value/1e3, (value%1e3)*1e6).UTC()
	default:
		return time.Unix(value, 0).UTC()
	}
}
