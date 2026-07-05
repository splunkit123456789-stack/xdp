package json

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
func (c testProcessContext) StageID() string                    { return "parse-json" }
func (c testProcessContext) Attempt() int                       { return 1 }
func (c testProcessContext) Now() time.Time                     { return time.Now().UTC() }
func (c testProcessContext) AddTag(tag string)                  { c.e.Tags = append(c.e.Tags, tag) }
func (c testProcessContext) AddError(err event.ProcessingError) { c.e.Errors = append(c.e.Errors, err) }

func TestParserProcess(t *testing.T) {
	parser := New()
	if err := parser.Init(plugin.BasicInitContext{Code: "json-parser", Version: "1.0.0"}, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	e := event.New(`{"level":"info","msg":"ok","bytes":1024}`, event.Source{Type: "http", Name: "test"}, time.Now().UTC())
	out, err := parser.Process(testProcessContext{e: e}, e)
	if err != nil {
		t.Fatal(err)
	}
	if out.Fields["level"] != "info" {
		t.Fatalf("expected level field, got %#v", out.Fields["level"])
	}
	if out.Fields["msg"] != "ok" {
		t.Fatalf("expected msg field, got %#v", out.Fields["msg"])
	}
}

func TestParserRejectsInvalidJSON(t *testing.T) {
	parser := New()
	if err := parser.Init(plugin.BasicInitContext{}, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	e := event.New(`not-json`, event.Source{Type: "http", Name: "test"}, time.Now().UTC())
	_, err := parser.Process(testProcessContext{e: e}, e)
	if err == nil {
		t.Fatal("expected invalid json error")
	}
}
