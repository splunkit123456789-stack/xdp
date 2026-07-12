package jsonparser

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"xdp/pkg/event"
	"xdp/pkg/plugin"
)

type Parser struct {
	cfg Config
}

type Config struct {
	SourceField      string
	Target           string
	FlattenNested    bool
	FlattenSeparator string
	ArrayMode        string
	OnInvalidJSON    string
	Sourcetype       string
	RuleID           string
}

func New() *Parser {
	return &Parser{}
}

func (p *Parser) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Code:        "json-parser",
		Name:        "JSON Parser",
		Type:        plugin.TypeParser,
		Version:     "1.0.0",
		Runtime:     "go_builtin",
		Description: "Parse JSON events and flatten fields for search and aggregation.",
		ConfigSchema: plugin.Schema{
			"type":     "object",
			"required": []string{"source_field", "target", "array_mode", "on_invalid_json"},
			"properties": map[string]any{
				"source_field":      map[string]any{"type": "string", "enum": []string{"raw"}, "default": "raw"},
				"target":            map[string]any{"type": "string", "enum": []string{"fields"}, "default": "fields"},
				"flatten_nested":    map[string]any{"type": "boolean", "default": true},
				"flatten_separator": map[string]any{"type": "string", "default": "."},
				"array_mode":        map[string]any{"type": "string", "enum": []string{"json_string", "expand_index"}, "default": "json_string"},
				"on_invalid_json":   map[string]any{"type": "string", "enum": []string{"continue", "fail"}, "default": "continue"},
			},
		},
	}
}

func (p *Parser) Validate(config map[string]any) error {
	_, err := parseConfig(config)
	return err
}

func (p *Parser) Init(ctx plugin.InitContext, config map[string]any) error {
	cfg, err := parseConfig(config)
	if err != nil {
		return err
	}
	p.cfg = cfg
	return nil
}

func (p *Parser) Process(ctx plugin.ProcessContext, e *event.Event) (*event.Event, error) {
	if e.Fields == nil {
		e.Fields = map[string]any{}
	}
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}

	raw := strings.TrimSpace(e.Raw)
	if raw == "" || (raw[0] != '{' && raw[0] != '[') {
		return e, plugin.NewError(plugin.ErrNoMatch, "json parser did not match", false, nil)
	}

	var value any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		parseErr := plugin.NewError(plugin.ErrParseFailed, "invalid json", false, err)
		markParseFailed(e, ctx, parseErr)
		return e, parseErr
	}

	setParseRuleMetadata(e, p.cfg)
	for key, fieldValue := range flattenValue(value, "", p.cfg) {
		e.Fields[key] = fieldValue
	}
	markParsed(e, ctx)
	return e, nil
}

func (p *Parser) Close() error {
	return nil
}

func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}

func parseConfig(config map[string]any) (Config, error) {
	cfg := Config{
		SourceField:      stringConfig(config, "source_field", "raw"),
		Target:           stringConfig(config, "target", "fields"),
		FlattenNested:    boolConfig(config, "flatten_nested", true),
		FlattenSeparator: stringConfig(config, "flatten_separator", "."),
		ArrayMode:        stringConfig(config, "array_mode", "json_string"),
		OnInvalidJSON:    stringConfig(config, "on_invalid_json", "continue"),
		Sourcetype:       stringConfig(config, "sourcetype", ""),
		RuleID:           stringConfig(config, "rule_id", ""),
	}
	if cfg.SourceField != "raw" {
		return Config{}, fmt.Errorf("source_field must be raw")
	}
	if cfg.Target != "fields" {
		return Config{}, fmt.Errorf("target must be fields")
	}
	if cfg.FlattenSeparator == "" {
		return Config{}, fmt.Errorf("flatten_separator is required")
	}
	if cfg.ArrayMode != "json_string" && cfg.ArrayMode != "expand_index" {
		return Config{}, fmt.Errorf("array_mode must be json_string or expand_index")
	}
	if cfg.OnInvalidJSON != "continue" && cfg.OnInvalidJSON != "fail" {
		return Config{}, fmt.Errorf("on_invalid_json must be continue or fail")
	}
	return cfg, nil
}

func flattenValue(value any, prefix string, cfg Config) map[string]any {
	out := map[string]any{}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			name := key
			if prefix != "" {
				name = prefix + cfg.FlattenSeparator + key
			}
			if !cfg.FlattenNested {
				if encoded, err := json.Marshal(child); err == nil {
					out[name] = string(encoded)
				}
				continue
			}
			for childKey, childValue := range flattenValue(child, name, cfg) {
				out[childKey] = childValue
			}
		}
	case []any:
		if cfg.ArrayMode == "expand_index" {
			for index, child := range typed {
				name := fmt.Sprintf("%s%s%d", prefix, cfg.FlattenSeparator, index)
				if prefix == "" {
					name = fmt.Sprintf("%d", index)
				}
				for childKey, childValue := range flattenValue(child, name, cfg) {
					out[childKey] = childValue
				}
			}
			return out
		}
		encoded, _ := json.Marshal(typed)
		out[firstNonEmpty(prefix, "root")] = string(encoded)
	case json.Number:
		out[firstNonEmpty(prefix, "root")] = typed.String()
	default:
		out[firstNonEmpty(prefix, "root")] = typed
	}
	return out
}

func setParseRuleMetadata(e *event.Event, cfg Config) {
	if cfg.Sourcetype != "" {
		e.Metadata["sourcetype"] = cfg.Sourcetype
		e.Metadata["parse_rule_name"] = cfg.Sourcetype
	}
	if cfg.RuleID != "" {
		e.Metadata["parse_rule_id"] = cfg.RuleID
	}
}

func markParsed(e *event.Event, ctx plugin.ProcessContext) {
	e.Metadata["parse_status"] = "parsed"
	e.Metadata["parse_error"] = ""
	if _, ok := e.Metadata["parsed_at"]; !ok {
		e.Metadata["parsed_at"] = processTime(ctx)
	}
}

func markParseFailed(e *event.Event, ctx plugin.ProcessContext, err error) {
	e.Metadata["parse_status"] = "parse_failed"
	e.Metadata["parse_error"] = err.Error()
	if _, ok := e.Metadata["parsed_at"]; !ok {
		e.Metadata["parsed_at"] = processTime(ctx)
	}
}

func processTime(ctx plugin.ProcessContext) time.Time {
	if ctx == nil {
		return time.Now().UTC()
	}
	return ctx.Now()
}

func stringConfig(config map[string]any, key string, fallback string) string {
	if config == nil {
		return fallback
	}
	value, ok := config[key]
	if !ok {
		return fallback
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" {
		return fallback
	}
	return text
}

func boolConfig(config map[string]any, key string, fallback bool) bool {
	if config == nil {
		return fallback
	}
	value, ok := config[key]
	if !ok {
		return fallback
	}
	typed, ok := value.(bool)
	if !ok {
		return fallback
	}
	return typed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
