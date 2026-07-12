package regex

import (
	"fmt"
	"regexp"
	"strings"

	"xdp/pkg/event"
	"xdp/pkg/plugin"
)

type Parser struct {
	pattern    *regexp.Regexp
	sourcetype string
	ruleID     string
}

type compiledConfig struct {
	pattern    *regexp.Regexp
	sourcetype string
	ruleID     string
}

func New() *Parser { return &Parser{} }
func (p *Parser) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Code:        "regex",
		Name:        "Regex Parser",
		Type:        plugin.TypeParser,
		Version:     "1.0.0",
		Runtime:     "go_builtin",
		Description: "Extract fields from raw events with named regex capture groups.",
		ConfigSchema: plugin.Schema{
			"type":     "object",
			"required": []string{"regex_pattern"},
			"properties": map[string]any{
				"source_field":  map[string]any{"type": "string", "default": "raw", "enum": []string{"raw"}},
				"regex_pattern": map[string]any{"type": "string"},
				"target":        map[string]any{"type": "string", "default": "fields", "enum": []string{"fields"}},
				"field_types":   map[string]any{"type": "object"},
				"on_no_match":   map[string]any{"type": "string", "default": "continue", "enum": []string{"continue"}},
			},
		},
	}
}
func (p *Parser) Validate(config map[string]any) error { _, err := compile(config); return err }
func (p *Parser) Init(ctx plugin.InitContext, config map[string]any) error {
	cfg, err := compile(config)
	if err != nil {
		return err
	}
	p.pattern = cfg.pattern
	p.sourcetype = cfg.sourcetype
	p.ruleID = cfg.ruleID
	return err
}
func (p *Parser) Close() error { return nil }

func (p *Parser) Process(ctx plugin.ProcessContext, e *event.Event) (*event.Event, error) {
	if e.Fields == nil {
		e.Fields = map[string]any{}
	}
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	matches := p.pattern.FindStringSubmatch(e.Raw)
	if matches == nil {
		return e, plugin.NewError(plugin.ErrNoMatch, "regex did not match", false, nil)
	}
	setParseRuleMetadata(e, p.sourcetype, p.ruleID)
	for i, name := range p.pattern.SubexpNames() {
		if i == 0 || name == "" {
			continue
		}
		e.Fields[name] = matches[i]
	}
	markParsed(e, ctx)
	return e, nil
}

func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}

func compile(config map[string]any) (compiledConfig, error) {
	sourceField := stringConfig(config, "source_field", "raw")
	if sourceField != "raw" {
		return compiledConfig{}, fmt.Errorf("source_field must be raw")
	}
	target := stringConfig(config, "target", "fields")
	if target != "fields" {
		return compiledConfig{}, fmt.Errorf("target must be fields")
	}
	onNoMatch := stringConfig(config, "on_no_match", "continue")
	if onNoMatch != "continue" {
		return compiledConfig{}, fmt.Errorf("on_no_match must be continue")
	}
	pattern := stringConfig(config, "regex_pattern", "")
	if pattern == "" {
		return compiledConfig{}, fmt.Errorf("regex_pattern is required")
	}
	re, err := regexp.Compile(goRegexPattern(pattern))
	if err != nil {
		return compiledConfig{}, err
	}
	hasNamedCapture := false
	for i, name := range re.SubexpNames() {
		if i > 0 && name != "" {
			hasNamedCapture = true
			break
		}
	}
	if !hasNamedCapture {
		return compiledConfig{}, fmt.Errorf("regex_pattern must include named capture groups")
	}
	return compiledConfig{pattern: re, sourcetype: stringConfig(config, "sourcetype", ""), ruleID: stringConfig(config, "rule_id", "")}, nil
}

func stringConfig(config map[string]any, key string, fallback string) string {
	value, ok := config[key]
	if !ok {
		return fallback
	}
	text, ok := value.(string)
	if !ok {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fallback
	}
	return text
}

func goRegexPattern(pattern string) string {
	return strings.ReplaceAll(pattern, "(?<", "(?P<")
}

func setParseRuleMetadata(e *event.Event, sourcetype string, ruleID string) {
	if sourcetype != "" {
		e.Metadata["sourcetype"] = sourcetype
		e.Metadata["parse_rule_name"] = sourcetype
	}
	if ruleID != "" {
		e.Metadata["parse_rule_id"] = ruleID
	}
}

func markParsed(e *event.Event, ctx plugin.ProcessContext) {
	e.Metadata["parse_status"] = "parsed"
	e.Metadata["parse_error"] = ""
	if _, ok := e.Metadata["parsed_at"]; !ok {
		e.Metadata["parsed_at"] = ctx.Now()
	}
}

func markParseFailed(e *event.Event, ctx plugin.ProcessContext, err error) {
	e.Metadata["parse_status"] = "parse_failed"
	e.Metadata["parse_error"] = err.Error()
	if _, ok := e.Metadata["parsed_at"]; !ok {
		e.Metadata["parsed_at"] = ctx.Now()
	}
}
