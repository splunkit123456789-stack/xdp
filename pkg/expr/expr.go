package expr

import (
	"fmt"
	"strings"

	"xdp/pkg/event"
)

func Eval(expression string, e *event.Event) (bool, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return true, nil
	}
	if strings.HasPrefix(expression, "exists(") && strings.HasSuffix(expression, ")") {
		_, ok := Get(e, strings.TrimSuffix(strings.TrimPrefix(expression, "exists("), ")"))
		return ok, nil
	}
	if strings.Contains(expression, " != ") {
		parts := strings.SplitN(expression, " != ", 2)
		value, ok := Get(e, strings.TrimSpace(parts[0]))
		literal := trimLiteral(parts[1])
		if literal == "null" {
			return ok && value != nil, nil
		}
		return ok && fmt.Sprint(value) != literal, nil
	}
	if strings.Contains(expression, " == ") {
		parts := strings.SplitN(expression, " == ", 2)
		value, ok := Get(e, strings.TrimSpace(parts[0]))
		literal := trimLiteral(parts[1])
		if literal == "null" {
			return !ok || value == nil, nil
		}
		return ok && fmt.Sprint(value) == literal, nil
	}
	if strings.Contains(expression, " contains ") {
		parts := strings.SplitN(expression, " contains ", 2)
		value, _ := Get(e, strings.TrimSpace(parts[0]))
		literal := trimLiteral(parts[1])
		switch values := value.(type) {
		case []string:
			for _, item := range values {
				if item == literal {
					return true, nil
				}
			}
			return false, nil
		default:
			return strings.Contains(fmt.Sprint(value), literal), nil
		}
	}
	return false, fmt.Errorf("unsupported expression: %s", expression)
}

func Get(e *event.Event, path string) (any, bool) {
	path = strings.TrimSpace(path)
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil, false
	}
	switch parts[0] {
	case "raw":
		return e.Raw, true
	case "tags":
		return e.Tags, true
	case "errors":
		if len(parts) == 2 && parts[1] == "length" {
			return len(e.Errors), true
		}
		return e.Errors, true
	case "metadata":
		if len(parts) != 2 {
			return nil, false
		}
		v, ok := e.Metadata[parts[1]]
		return v, ok
	case "fields":
		if len(parts) != 2 {
			return nil, false
		}
		v, ok := e.Fields[parts[1]]
		return v, ok
	case "source":
		if len(parts) != 2 {
			return nil, false
		}
		switch parts[1] {
		case "type":
			return e.Source.Type, true
		case "name":
			return e.Source.Name, true
		case "host":
			return e.Source.Host, true
		case "ip":
			return e.Source.IP, true
		}
	}
	return nil, false
}

func Set(e *event.Event, path string, value any) error {
	parts := strings.Split(path, ".")
	if len(parts) != 2 {
		return fmt.Errorf("unsupported path: %s", path)
	}
	switch parts[0] {
	case "metadata":
		if e.Metadata == nil {
			e.Metadata = map[string]any{}
		}
		e.Metadata[parts[1]] = value
		return nil
	case "fields":
		if e.Fields == nil {
			e.Fields = map[string]any{}
		}
		e.Fields[parts[1]] = value
		return nil
	}
	return fmt.Errorf("unsupported path: %s", path)
}

func trimLiteral(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "'")
	value = strings.Trim(value, "\"")
	return value
}
