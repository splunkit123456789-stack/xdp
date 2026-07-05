package propsconf

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"xdp/pkg/event"
	"xdp/pkg/plugin"
)

type Parser struct {
	cfg Config
}

type Config struct {
	ParserPlugin string
	PluginConfig map[string]any
	PropsConf    string
	Sourcetype   string
	RuleID       string
}

func New() *Parser {
	return &Parser{}
}

func (p *Parser) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Code:        "props-conf-parser",
		Name:        "Props.conf Parser",
		Type:        plugin.TypeParser,
		Version:     "1.0.0",
		Description: "Run parser rules generated from props.conf-style parse configuration",
		Runtime:     "go",
		ConfigSchema: plugin.Schema{
			"type": "object",
			"properties": map[string]any{
				"parser_plugin": map[string]any{"type": "string", "enum": []string{"json", "regex", "kv", "delimited"}},
				"plugin_config": map[string]any{"type": "object"},
				"props_conf":    map[string]any{"type": "string"},
				"sourcetype":    map[string]any{"type": "string"},
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
	setParseRuleMetadata(e, p.cfg)
	var err error
	switch p.cfg.ParserPlugin {
	case "json":
		var fields map[string]any
		if parseErr := json.Unmarshal([]byte(e.Raw), &fields); parseErr != nil {
			err = plugin.NewError(plugin.ErrParseFailed, "invalid json", false, parseErr)
			markParseFailed(e, err)
			return e, err
		}
		for key, value := range flattenMap(fields, "") {
			e.Fields[key] = value
		}
	case "regex":
		pattern := stringConfig(p.cfg.PluginConfig, "regex_pattern", "")
		re, err := regexp.Compile(goRegexPattern(pattern))
		if err != nil {
			parseErr := plugin.NewError(plugin.ErrInvalidConfig, "invalid regex_pattern", false, err)
			markParseFailed(e, parseErr)
			return e, parseErr
		}
		matches := re.FindStringSubmatch(e.Raw)
		if matches == nil {
			parseErr := plugin.NewError(plugin.ErrParseFailed, "regex did not match", false, nil)
			markParseFailed(e, parseErr)
			return e, parseErr
		}
		for i, name := range re.SubexpNames() {
			if i == 0 || name == "" {
				continue
			}
			e.Fields[name] = matches[i]
		}
	case "kv":
		for key, value := range parseKV(e.Raw, p.cfg.PluginConfig) {
			e.Fields[key] = value
		}
	case "delimited":
		fields, err := parseDelimited(e.Raw, p.cfg.PluginConfig)
		if err != nil {
			parseErr := plugin.NewError(plugin.ErrParseFailed, "invalid delimited event", false, err)
			markParseFailed(e, parseErr)
			return e, parseErr
		}
		for key, value := range fields {
			e.Fields[key] = value
		}
	default:
		parseErr := plugin.NewError(plugin.ErrInvalidConfig, "unsupported parser_plugin", false, nil)
		markParseFailed(e, parseErr)
		return e, parseErr
	}
	markParsed(e)
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
	cfg := Config{PluginConfig: map[string]any{}}
	cfg.ParserPlugin = strings.ToLower(strings.TrimSpace(stringConfig(config, "parser_plugin", "")))
	if cfg.ParserPlugin == "" {
		return cfg, fmt.Errorf("parser_plugin is required")
	}
	if cfg.ParserPlugin != "json" && cfg.ParserPlugin != "regex" && cfg.ParserPlugin != "kv" && cfg.ParserPlugin != "delimited" {
		return cfg, fmt.Errorf("unsupported parser_plugin")
	}
	if raw, ok := config["plugin_config"]; ok {
		pluginConfig, ok := raw.(map[string]any)
		if !ok {
			return cfg, fmt.Errorf("plugin_config must be an object")
		}
		cfg.PluginConfig = pluginConfig
	}
	cfg.PropsConf = stringConfig(config, "props_conf", "")
	cfg.Sourcetype = strings.TrimSpace(stringConfig(config, "sourcetype", ""))
	cfg.RuleID = strings.TrimSpace(stringConfig(config, "rule_id", ""))
	switch cfg.ParserPlugin {
	case "regex":
		if _, err := regexp.Compile(goRegexPattern(stringConfig(cfg.PluginConfig, "regex_pattern", ""))); err != nil {
			return cfg, fmt.Errorf("regex_pattern is invalid")
		}
	case "kv":
		if strings.TrimSpace(stringConfig(cfg.PluginConfig, "kv_delimiter", "")) == "" {
			return cfg, fmt.Errorf("kv_delimiter is required")
		}
	case "delimited":
		if len(fieldNamesConfig(cfg.PluginConfig["field_names"])) == 0 {
			return cfg, fmt.Errorf("field_names is required")
		}
	}
	return cfg, nil
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

func markParsed(e *event.Event) {
	e.Metadata["parse_status"] = "parsed"
	if _, ok := e.Metadata["parse_error"]; !ok {
		e.Metadata["parse_error"] = ""
	}
	if _, ok := e.Metadata["parsed_at"]; !ok {
		e.Metadata["parsed_at"] = time.Now().UTC()
	}
}

func markParseFailed(e *event.Event, err error) {
	e.Metadata["parse_status"] = "parse_failed"
	e.Metadata["parse_error"] = err.Error()
	if _, ok := e.Metadata["parsed_at"]; !ok {
		e.Metadata["parsed_at"] = time.Now().UTC()
	}
}

func flattenMap(values map[string]any, prefix string) map[string]any {
	out := map[string]any{}
	for key, value := range values {
		name := key
		if prefix != "" {
			name = prefix + "." + key
		}
		if nested, ok := value.(map[string]any); ok {
			for nestedKey, nestedValue := range flattenMap(nested, name) {
				out[nestedKey] = nestedValue
			}
			continue
		}
		out[name] = value
	}
	return out
}

func parseKV(raw string, config map[string]any) map[string]any {
	kvDelimiter := stringConfig(config, "kv_delimiter", "=")
	re := regexp.MustCompile(`([\w.@-]+)\s*` + regexp.QuoteMeta(kvDelimiter) + `\s*("[^"]*"|'[^']*'|\S+)`)
	out := map[string]any{}
	for _, match := range re.FindAllStringSubmatch(raw, -1) {
		out[match[1]] = stripQuotes(match[2])
	}
	return out
}

func parseDelimited(raw string, config map[string]any) (map[string]any, error) {
	delimiter := resolveDelimiter(stringConfig(config, "field_delimiter", ","))
	runes := []rune(delimiter)
	if len(runes) == 0 {
		runes = []rune{','}
	}
	reader := csv.NewReader(strings.NewReader(raw))
	reader.Comma = runes[0]
	reader.FieldsPerRecord = -1
	record, err := reader.Read()
	if err != nil {
		return nil, err
	}
	names := fieldNamesConfig(config["field_names"])
	out := map[string]any{}
	for i, value := range record {
		name := fmt.Sprintf("field_%d", i+1)
		if i < len(names) {
			name = names[i]
		}
		out[name] = stripQuotes(value)
	}
	return out, nil
}

func stringConfig(config map[string]any, key string, fallback string) string {
	if config == nil {
		return fallback
	}
	value, ok := config[key]
	if !ok {
		return fallback
	}
	text, ok := value.(string)
	if !ok {
		return fallback
	}
	return text
}

func fieldNamesConfig(value any) []string {
	switch typed := value.(type) {
	case []string:
		return cleanFieldNames(typed)
	case []any:
		names := make([]string, 0, len(typed))
		for _, item := range typed {
			names = append(names, fmt.Sprint(item))
		}
		return cleanFieldNames(names)
	case string:
		return cleanFieldNames(strings.Split(typed, ","))
	default:
		return nil
	}
}

func cleanFieldNames(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func resolveDelimiter(value string) string {
	switch strings.TrimSpace(value) {
	case "", "逗号":
		return ","
	case "空格":
		return " "
	case "制表符", "\\t":
		return "\t"
	case "竖杠":
		return "|"
	case "分号":
		return ";"
	default:
		return value
	}
}

func stripQuotes(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func goRegexPattern(pattern string) string {
	return strings.ReplaceAll(pattern, "(?<", "(?P<")
}
