package jsonparser

import (
	"testing"

	"xdp/pkg/event"
	"xdp/pkg/plugin"
)

func TestProcessFlattensNestedJSONAndExpandsArrays(t *testing.T) {
	parser := New()
	config := map[string]any{
		"source_field":      "raw",
		"target":            "fields",
		"flatten_nested":    true,
		"flatten_separator": ".",
		"array_mode":        "expand_index",
		"on_invalid_json":   "continue",
		"sourcetype":        "JSON Rule",
		"rule_id":           "rule-json",
	}
	if err := parser.Init(plugin.BasicInitContext{Code: "json-parser", Version: "1.0.0"}, config); err != nil {
		t.Fatalf("init json parser: %v", err)
	}

	result, err := parser.Process(nil, &event.Event{
		Raw:      `{"service":"checkout","latency":128,"user":{"id":"u-1"},"tags":["api","p1"]}`,
		Fields:   map[string]any{},
		Metadata: map[string]any{},
	})
	if err != nil {
		t.Fatalf("process json: %v", err)
	}
	if result.Fields["service"] != "checkout" || result.Fields["user.id"] != "u-1" {
		t.Fatalf("flattened fields = %#v", result.Fields)
	}
	if result.Fields["tags.0"] != "api" || result.Fields["tags.1"] != "p1" {
		t.Fatalf("expanded array fields = %#v", result.Fields)
	}
	if result.Metadata["parse_status"] != "parsed" || result.Metadata["parse_rule_name"] != "JSON Rule" {
		t.Fatalf("parse metadata = %#v", result.Metadata)
	}
}

func TestProcessReturnsParseMissForNonJSONInput(t *testing.T) {
	parser := New()
	if err := parser.Init(plugin.BasicInitContext{Code: "json-parser", Version: "1.0.0"}, map[string]any{
		"source_field":      "raw",
		"target":            "fields",
		"flatten_nested":    true,
		"flatten_separator": ".",
		"array_mode":        "json_string",
		"on_invalid_json":   "continue",
	}); err != nil {
		t.Fatalf("init json parser: %v", err)
	}

	result, err := parser.Process(nil, &event.Event{Raw: "src=10.0.0.1", Fields: map[string]any{}, Metadata: map[string]any{}})
	if err == nil {
		t.Fatal("expected parse miss")
	}
	pluginErr, ok := err.(*plugin.PluginError)
	if !ok || pluginErr.Code != plugin.ErrNoMatch {
		t.Fatalf("parse miss error = %#v", err)
	}
	if _, exists := result.Metadata["parse_status"]; exists {
		t.Fatalf("parse miss should not persist failure metadata: %#v", result.Metadata)
	}
}

func TestProcessMarksMalformedJSONAsFailed(t *testing.T) {
	parser := New()
	if err := parser.Init(plugin.BasicInitContext{Code: "json-parser", Version: "1.0.0"}, map[string]any{
		"source_field":      "raw",
		"target":            "fields",
		"flatten_nested":    true,
		"flatten_separator": ".",
		"array_mode":        "json_string",
		"on_invalid_json":   "fail",
	}); err != nil {
		t.Fatalf("init json parser: %v", err)
	}

	result, err := parser.Process(nil, &event.Event{Raw: `{"service":`, Fields: map[string]any{}, Metadata: map[string]any{}})
	if err == nil {
		t.Fatal("expected malformed json error")
	}
	if result.Metadata["parse_status"] != "parse_failed" || result.Metadata["parse_error"] == "" {
		t.Fatalf("malformed json metadata = %#v", result.Metadata)
	}
}
