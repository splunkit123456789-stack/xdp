package propsconf

import (
	"context"
	"testing"
	"time"

	"xdp/pkg/event"
	"xdp/pkg/plugin"
)

type testProcessContext struct {
	e *event.Event
}

func (c testProcessContext) Context() context.Context           { return context.Background() }
func (c testProcessContext) PipelineID() string                 { return c.e.PipelineID }
func (c testProcessContext) PipelineVersion() string            { return c.e.PipelineVersion }
func (c testProcessContext) StageID() string                    { return "parse-rule-firewall" }
func (c testProcessContext) Attempt() int                       { return 1 }
func (c testProcessContext) Now() time.Time                     { return time.Now().UTC() }
func (c testProcessContext) AddTag(tag string)                  { c.e.Tags = append(c.e.Tags, tag) }
func (c testProcessContext) AddError(err event.ProcessingError) { c.e.Errors = append(c.e.Errors, err) }

func TestPropsConfParserProcessesRegexRule(t *testing.T) {
	parser := New()
	config := map[string]any{
		"parser_plugin": "regex",
		"sourcetype":    "Firewall Regex",
		"plugin_config": map[string]any{
			"regex_pattern": `src=(?<src_ip>\S+) bytes=(?<bytes>\d+)`,
		},
		"props_conf": "[source::firewall]\nEXTRACT-custom = src=(?<src_ip>\\S+) bytes=(?<bytes>\\d+)",
	}
	if err := parser.Init(plugin.BasicInitContext{Code: "props-conf-parser", Version: "1.0.0"}, config); err != nil {
		t.Fatal(err)
	}
	e := event.New("src=1.1.1.1 bytes=1024", event.Source{Type: "syslog", Name: "firewall"}, time.Now().UTC())
	out, err := parser.Process(testProcessContext{e: e}, e)
	if err != nil {
		t.Fatal(err)
	}
	if out.Fields["src_ip"] != "1.1.1.1" || out.Fields["bytes"] != "1024" {
		t.Fatalf("unexpected fields: %#v", out.Fields)
	}
	if out.Metadata["sourcetype"] != "Firewall Regex" {
		t.Fatalf("sourcetype = %#v, want Firewall Regex", out.Metadata["sourcetype"])
	}
	if out.Metadata["parse_status"] != "parsed" {
		t.Fatalf("parse_status = %#v, want parsed", out.Metadata["parse_status"])
	}
	if out.Metadata["parse_rule_name"] != "Firewall Regex" {
		t.Fatalf("parse_rule_name = %#v, want Firewall Regex", out.Metadata["parse_rule_name"])
	}
	if out.Metadata["parse_error"] != "" {
		t.Fatalf("parse_error = %#v, want empty", out.Metadata["parse_error"])
	}
}

func TestPropsConfParserMarksParseFailedOnRuleMatchFailure(t *testing.T) {
	parser := New()
	config := map[string]any{
		"parser_plugin": "regex",
		"sourcetype":    "Firewall Regex",
		"rule_id":       "pr_firewall_regex",
		"plugin_config": map[string]any{
			"regex_pattern": `src=(?<src_ip>\S+) bytes=(?<bytes>\d+)`,
		},
	}
	if err := parser.Init(plugin.BasicInitContext{Code: "props-conf-parser", Version: "1.0.0"}, config); err != nil {
		t.Fatal(err)
	}
	e := event.New("this log does not match", event.Source{Type: "syslog", Name: "firewall"}, time.Now().UTC())
	out, err := parser.Process(testProcessContext{e: e}, e)
	if err == nil {
		t.Fatal("Process() expected parse failure")
	}
	if out.Metadata["parse_status"] != "parse_failed" {
		t.Fatalf("parse_status = %#v, want parse_failed", out.Metadata["parse_status"])
	}
	if out.Metadata["parse_rule_id"] != "pr_firewall_regex" {
		t.Fatalf("parse_rule_id = %#v, want pr_firewall_regex", out.Metadata["parse_rule_id"])
	}
	if out.Metadata["parse_rule_name"] != "Firewall Regex" {
		t.Fatalf("parse_rule_name = %#v, want Firewall Regex", out.Metadata["parse_rule_name"])
	}
	if out.Metadata["parse_error"] == "" {
		t.Fatal("parse_error is empty, want failure reason")
	}
}

func TestPropsConfParserProcessesKVAndDelimitedRules(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config map[string]any
		raw    string
		field  string
		value  string
	}{
		{
			name: "kv",
			config: map[string]any{
				"parser_plugin": "kv",
				"plugin_config": map[string]any{"field_delimiter": " ", "kv_delimiter": "=", "field_quote": `"`},
				"props_conf":    "[sourcetype::kv]\nKV_MODE = auto",
			},
			raw:   "service=api level=info bytes=1024",
			field: "service",
			value: "api",
		},
		{
			name: "delimited",
			config: map[string]any{
				"parser_plugin": "delimited",
				"plugin_config": map[string]any{"field_delimiter": ",", "field_names": []any{"src_ip", "dst_ip", "action"}, "field_quote": `"`},
				"props_conf":    "[sourcetype::csv]\nINDEXED_EXTRACTIONS = csv",
			},
			raw:   "1.1.1.1,8.8.8.8,deny",
			field: "action",
			value: "deny",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parser := New()
			if err := parser.Init(plugin.BasicInitContext{}, tc.config); err != nil {
				t.Fatal(err)
			}
			e := event.New(tc.raw, event.Source{Type: "test", Name: tc.name}, time.Now().UTC())
			out, err := parser.Process(testProcessContext{e: e}, e)
			if err != nil {
				t.Fatal(err)
			}
			if out.Fields[tc.field] != tc.value {
				t.Fatalf("field %s = %#v, want %q; all fields %#v", tc.field, out.Fields[tc.field], tc.value, out.Fields)
			}
		})
	}
}
