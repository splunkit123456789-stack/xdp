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
