package regex

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"xdp/pkg/event"
	"xdp/pkg/plugin"
)

type testInitContext struct{}

func (testInitContext) Context() context.Context { return context.Background() }
func (testInitContext) PluginCode() string       { return "regex" }
func (testInitContext) PluginVersion() string    { return "1.0.0" }

type testProcessContext struct{}

func (testProcessContext) Context() context.Context { return context.Background() }
func (testProcessContext) PipelineID() string       { return "pipe_test" }
func (testProcessContext) PipelineVersion() string  { return "v1" }
func (testProcessContext) StageID() string          { return "stage_regex" }
func (testProcessContext) Attempt() int             { return 1 }
func (testProcessContext) Now() time.Time           { return time.Unix(0, 0).UTC() }
func (testProcessContext) AddTag(tag string)        {}
func (testProcessContext) AddError(err event.ProcessingError) {
}

func TestMetadataUsesStandardRegexParserContract(t *testing.T) {
	metadata := New().Metadata()
	if metadata.Code != "regex" {
		t.Fatalf("Code = %q, want regex", metadata.Code)
	}
	if metadata.Type != plugin.TypeParser {
		t.Fatalf("Type = %q, want parser", metadata.Type)
	}
	if metadata.Version != "1.0.0" {
		t.Fatalf("Version = %q, want 1.0.0", metadata.Version)
	}
	if metadata.Runtime != "go_builtin" {
		t.Fatalf("Runtime = %q, want go_builtin", metadata.Runtime)
	}
	if metadata.ConfigSchema["type"] != "object" {
		t.Fatalf("ConfigSchema = %#v, want object schema", metadata.ConfigSchema)
	}
}

func TestValidateAcceptsStandardRegexConfigAndRejectsInvalidConfig(t *testing.T) {
	parser := New()
	valid := map[string]any{
		"source_field":  "raw",
		"regex_pattern": `src=(?<src_ip>\S+)\s+bytes=(?<bytes>\d+)`,
		"target":        "fields",
		"field_types":   map[string]any{"bytes": "int64"},
		"on_no_match":   "continue",
	}
	if err := parser.Validate(valid); err != nil {
		t.Fatalf("Validate(valid) error = %v", err)
	}

	cases := []struct {
		name    string
		config  map[string]any
		wantErr string
	}{
		{name: "missing regex_pattern", config: map[string]any{"source_field": "raw"}, wantErr: "regex_pattern is required"},
		{name: "unsupported source_field", config: map[string]any{"source_field": "message", "regex_pattern": `src=(?<src_ip>\S+)`}, wantErr: "source_field must be raw"},
		{name: "unsupported target", config: map[string]any{"regex_pattern": `src=(?<src_ip>\S+)`, "target": "metadata"}, wantErr: "target must be fields"},
		{name: "unsupported no match action", config: map[string]any{"regex_pattern": `src=(?<src_ip>\S+)`, "on_no_match": "fail"}, wantErr: "on_no_match must be continue"},
		{name: "missing named capture", config: map[string]any{"regex_pattern": `src=(\S+)`}, wantErr: "named capture"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := parser.Validate(tc.config)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestProcessExtractsNamedFieldsAndMarksParsed(t *testing.T) {
	parser := New()
	config := map[string]any{
		"source_field":  "raw",
		"regex_pattern": `src=(?<src_ip>\S+)\s+bytes=(?<bytes>\d+)`,
		"target":        "fields",
		"on_no_match":   "continue",
		"sourcetype":    "Firewall Regex",
		"rule_id":       "rule-1",
	}
	if err := parser.Init(testInitContext{}, config); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	ev := event.New("src=10.0.1.8 bytes=2048", event.Source{Type: "syslog"}, time.Now().UTC())
	result, err := parser.Process(testProcessContext{}, ev)
	if err != nil {
		t.Fatalf("Process(match) error = %v", err)
	}
	if result.Fields["src_ip"] != "10.0.1.8" || result.Fields["bytes"] != "2048" {
		t.Fatalf("Fields = %#v, want extracted src_ip and bytes", result.Fields)
	}
	if result.Metadata["parse_status"] != "parsed" || result.Metadata["parse_rule_name"] != "Firewall Regex" || result.Metadata["parse_rule_id"] != "rule-1" {
		t.Fatalf("Metadata = %#v, want parsed rule metadata", result.Metadata)
	}
	if result.Metadata["parse_error"] != "" {
		t.Fatalf("parse_error = %#v, want empty", result.Metadata["parse_error"])
	}
}

func TestProcessReturnsNoMatchWithoutFailureMetadata(t *testing.T) {
	parser := New()
	config := map[string]any{
		"source_field":  "raw",
		"regex_pattern": `src=(?<src_ip>\S+)\s+bytes=(?<bytes>\d+)`,
		"target":        "fields",
		"on_no_match":   "continue",
		"sourcetype":    "Firewall Regex",
		"rule_id":       "rule-1",
	}
	if err := parser.Init(testInitContext{}, config); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	missed := event.New("dst=172.16.0.4 action=deny", event.Source{Type: "syslog"}, time.Now().UTC())
	result, err := parser.Process(testProcessContext{}, missed)
	var pluginErr *plugin.PluginError
	if !errors.As(err, &pluginErr) || pluginErr.Code != plugin.ErrNoMatch {
		t.Fatalf("Process(no match) error = %v, want NO_MATCH", err)
	}
	if len(result.Fields) != 0 {
		t.Fatalf("Fields after no match = %#v, want empty", result.Fields)
	}
	if result.Metadata["parse_status"] != "unparsed" {
		t.Fatalf("parse_status = %#v, want unchanged unparsed state", result.Metadata["parse_status"])
	}
	if result.Metadata["parse_error"] != "" {
		t.Fatalf("parse_error = %#v, want empty for a rule miss", result.Metadata["parse_error"])
	}
}
