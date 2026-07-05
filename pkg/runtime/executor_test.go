package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"xdp/pkg/event"
	"xdp/pkg/pipeline"
	"xdp/pkg/plugin"
)

func TestExecuteSkipsStageWhenConditionIsFalse(t *testing.T) {
	reg := plugin.NewRegistry()
	mustRegister(t, reg.Register(testProcessorMeta(), func() any { return testProcessor{} }))
	mustRegister(t, reg.Register(testOutputMeta(), func() any { return &capturingOutput{} }))

	ev := event.New("raw", event.Source{Type: "test"}, time.Now().UTC())
	ev.Fields["action"] = "allow"
	_, err := NewExecutor(reg).Execute(context.Background(), testPipeline(pipeline.StageSpec{
		ID:      "mark",
		Type:    string(plugin.TypeTransform),
		Plugin:  "test-processor",
		Version: "1.0.0",
		When:    "fields.action == 'deny'",
	}), ev)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if _, ok := ev.Fields["processed"]; ok {
		t.Fatal("stage ran when condition was false")
	}
}

func TestExecuteRunsStageWhenConditionIsTrue(t *testing.T) {
	reg := plugin.NewRegistry()
	mustRegister(t, reg.Register(testProcessorMeta(), func() any { return testProcessor{} }))
	mustRegister(t, reg.Register(testOutputMeta(), func() any { return &capturingOutput{} }))

	ev := event.New("raw", event.Source{Type: "test"}, time.Now().UTC())
	ev.Fields["action"] = "deny"
	_, err := NewExecutor(reg).Execute(context.Background(), testPipeline(pipeline.StageSpec{
		ID:      "mark",
		Type:    string(plugin.TypeTransform),
		Plugin:  "test-processor",
		Version: "1.0.0",
		When:    "fields.action == 'deny'",
	}), ev)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if ev.Fields["processed"] != true {
		t.Fatalf("stage did not run when condition was true: %#v", ev.Fields)
	}
}

func TestExecuteSkipsOutputWhenConditionIsFalse(t *testing.T) {
	out := &capturingOutput{}
	reg := plugin.NewRegistry()
	mustRegister(t, reg.Register(testProcessorMeta(), func() any { return testProcessor{} }))
	mustRegister(t, reg.Register(testOutputMeta(), func() any { return out }))

	ev := event.New("raw", event.Source{Type: "test"}, time.Now().UTC())
	ev.Metadata["index"] = "app"
	pipe := testPipeline(pipeline.StageSpec{ID: "mark", Type: string(plugin.TypeTransform), Plugin: "test-processor", Version: "1.0.0"})
	pipe.Spec.Outputs[0].When = "metadata.index == 'firewall'"
	_, err := NewExecutor(reg).Execute(context.Background(), pipe, ev)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if out.writes != 0 {
		t.Fatalf("output writes = %d, want 0", out.writes)
	}
}

func TestExecuteRunsOutputWhenConditionIsTrue(t *testing.T) {
	out := &capturingOutput{}
	reg := plugin.NewRegistry()
	mustRegister(t, reg.Register(testProcessorMeta(), func() any { return testProcessor{} }))
	mustRegister(t, reg.Register(testOutputMeta(), func() any { return out }))

	ev := event.New("raw", event.Source{Type: "test"}, time.Now().UTC())
	ev.Metadata["index"] = "firewall"
	pipe := testPipeline(pipeline.StageSpec{ID: "mark", Type: string(plugin.TypeTransform), Plugin: "test-processor", Version: "1.0.0"})
	pipe.Spec.Outputs[0].When = "metadata.index != null"
	_, err := NewExecutor(reg).Execute(context.Background(), pipe, ev)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if out.writes != 1 {
		t.Fatalf("output writes = %d, want 1", out.writes)
	}
}

func TestExecuteAppliesPipelineSourceNameFromConfig(t *testing.T) {
	reg := plugin.NewRegistry()
	mustRegister(t, reg.Register(testProcessorMeta(), func() any { return testProcessor{} }))
	mustRegister(t, reg.Register(testOutputMeta(), func() any { return &capturingOutput{} }))

	ev := event.New("raw", event.Source{Type: "syslog", Name: "xdp-agent-syslog"}, time.Now().UTC())
	pipe := testPipeline(pipeline.StageSpec{ID: "mark", Type: string(plugin.TypeTransform), Plugin: "test-processor", Version: "1.0.0"})
	pipe.Spec.Source.Config = map[string]any{"source_name": "Firewall Syslog P0"}

	result, err := NewExecutor(reg).Execute(context.Background(), pipe, ev)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Event.Source.Name != "Firewall Syslog P0" {
		t.Fatalf("source name = %q, want Firewall Syslog P0", result.Event.Source.Name)
	}
}

func TestExecuteParseRuleGroupContinuesOnMissAndStopsAfterFirstSuccess(t *testing.T) {
	out := &capturingOutput{}
	reg := plugin.NewRegistry()
	mustRegister(t, reg.Register(testSelectiveParserMeta(), func() any { return &testSelectiveParser{} }))
	mustRegister(t, reg.Register(testOutputMeta(), func() any { return out }))

	ev := event.New("traffic src=10.0.1.8 dst=172.16.0.4 bytes=2048", event.Source{Type: "syslog"}, time.Now().UTC())
	pipe := testPipeline(pipeline.StageSpec{
		ID:     "parse-rules",
		Type:   "parser_group",
		Config: map[string]any{"fallback_output_index": "audit_p0"},
		Stages: []pipeline.StageSpec{
			parseRuleTestStage("parse-rule-deny", "deny", "deny-rule", "audit_p0"),
			parseRuleTestStage("parse-rule-traffic", "traffic", "traffic-rule", "audit_alt"),
			parseRuleTestStage("parse-rule-catchall", "traffic", "catchall-rule", "audit_catchall"),
		},
	})
	pipe.Spec.Outputs[0].Config = map[string]any{"index": "${metadata.index}"}

	result, err := NewExecutor(reg).Execute(context.Background(), pipe, ev)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != "processed" {
		t.Fatalf("status = %q, want processed", result.Status)
	}
	if got := result.Event.Metadata["parse_status"]; got != "parsed" {
		t.Fatalf("parse_status = %#v, want parsed", got)
	}
	if got := result.Event.Metadata["parse_rule_name"]; got != "traffic-rule" {
		t.Fatalf("parse_rule_name = %#v, want traffic-rule", got)
	}
	if got := result.Event.Metadata["index"]; got != "audit_alt" {
		t.Fatalf("metadata.index = %#v, want audit_alt", got)
	}
	if result.Event.Fields["matched_pattern"] != "traffic" {
		t.Fatalf("matched_pattern = %#v, want traffic", result.Event.Fields["matched_pattern"])
	}
	if len(result.Event.Errors) != 0 {
		t.Fatalf("errors = %#v, want none for parser rule misses", result.Event.Errors)
	}
	if out.writes != 1 {
		t.Fatalf("output writes = %d, want 1", out.writes)
	}
}

func TestExecuteParseRuleGroupMarksUnparsedWhenNoRuleMatches(t *testing.T) {
	reg := plugin.NewRegistry()
	mustRegister(t, reg.Register(testSelectiveParserMeta(), func() any { return &testSelectiveParser{} }))
	mustRegister(t, reg.Register(testOutputMeta(), func() any { return &capturingOutput{} }))

	ev := event.New("unknown message", event.Source{Type: "syslog"}, time.Now().UTC())
	pipe := testPipeline(pipeline.StageSpec{
		ID:     "parse-rules",
		Type:   "parser_group",
		Config: map[string]any{"fallback_output_index": "_unparsed"},
		Stages: []pipeline.StageSpec{
			parseRuleTestStage("parse-rule-deny", "deny", "deny-rule", "audit_p0"),
			parseRuleTestStage("parse-rule-traffic", "traffic", "traffic-rule", "audit_alt"),
		},
	})

	result, err := NewExecutor(reg).Execute(context.Background(), pipe, ev)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := result.Event.Metadata["parse_status"]; got != "unparsed" {
		t.Fatalf("parse_status = %#v, want unparsed", got)
	}
	if got := result.Event.Metadata["parse_rule_id"]; got != "" {
		t.Fatalf("parse_rule_id = %#v, want empty", got)
	}
	if got := result.Event.Metadata["parse_rule_name"]; got != "" {
		t.Fatalf("parse_rule_name = %#v, want empty", got)
	}
	if got := result.Event.Metadata["parse_error"]; got != "" {
		t.Fatalf("parse_error = %#v, want empty", got)
	}
	if got := result.Event.Metadata["index"]; got != "_unparsed" {
		t.Fatalf("metadata.index = %#v, want _unparsed fallback", got)
	}
	if len(result.Event.Errors) != 0 {
		t.Fatalf("errors = %#v, want none for all parser rule misses", result.Event.Errors)
	}
}

func TestExecuteReturnsFailedOnInvalidWhenExpression(t *testing.T) {
	reg := plugin.NewRegistry()
	mustRegister(t, reg.Register(testProcessorMeta(), func() any { return testProcessor{} }))
	mustRegister(t, reg.Register(testOutputMeta(), func() any { return &capturingOutput{} }))

	ev := event.New("raw", event.Source{Type: "test"}, time.Now().UTC())
	result, err := NewExecutor(reg).Execute(context.Background(), testPipeline(pipeline.StageSpec{
		ID:      "mark",
		Type:    string(plugin.TypeTransform),
		Plugin:  "test-processor",
		Version: "1.0.0",
		When:    "fields.action =~ 'deny'",
	}), ev)
	if err == nil {
		t.Fatal("Execute() expected invalid expression error")
	}
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if len(result.Event.Errors) != 1 {
		t.Fatalf("errors = %d, want 1", len(result.Event.Errors))
	}
}

func parseRuleTestStage(id string, pattern string, name string, index string) pipeline.StageSpec {
	return pipeline.StageSpec{
		ID:      id,
		Type:    string(plugin.TypeParser),
		Plugin:  "test-selective-parser",
		Version: "1.0.0",
		Config: map[string]any{
			"pattern":         pattern,
			"parse_rule_id":   id,
			"parse_rule_name": name,
			"output_index":    index,
		},
		OnError: &pipeline.ErrorPolicy{Action: "continue"},
	}
}

func testPipeline(stage pipeline.StageSpec) pipeline.Pipeline {
	return pipeline.Pipeline{
		APIVersion: pipeline.APIVersionV1Alpha1,
		Kind:       pipeline.KindPipeline,
		Metadata:   pipeline.Metadata{ID: "test-pipeline", Name: "Test Pipeline"},
		Spec: pipeline.Spec{
			Source:  pipeline.SourceSpec{ID: "test-source", Type: "input", Plugin: "test-input", Version: "1.0.0"},
			Stages:  []pipeline.StageSpec{stage},
			Outputs: []pipeline.OutputSpec{{ID: "capture", Plugin: "test-output", Version: "1.0.0"}},
		},
	}
}

func testProcessorMeta() plugin.Metadata {
	return plugin.Metadata{Code: "test-processor", Name: "Test Processor", Type: plugin.TypeTransform, Version: "1.0.0", Runtime: "go"}
}

func testOutputMeta() plugin.Metadata {
	return plugin.Metadata{Code: "test-output", Name: "Test Output", Type: plugin.TypeOutput, Version: "1.0.0", Runtime: "go"}
}

type testProcessor struct{}

func (testProcessor) Metadata() plugin.Metadata                                { return testProcessorMeta() }
func (testProcessor) Validate(config map[string]any) error                     { return nil }
func (testProcessor) Init(ctx plugin.InitContext, config map[string]any) error { return nil }
func (testProcessor) Process(ctx plugin.ProcessContext, ev *event.Event) (*event.Event, error) {
	ev.Fields["processed"] = true
	return ev, nil
}
func (testProcessor) Close() error { return nil }

func testSelectiveParserMeta() plugin.Metadata {
	return plugin.Metadata{Code: "test-selective-parser", Name: "Test Selective Parser", Type: plugin.TypeParser, Version: "1.0.0", Runtime: "go"}
}

type testSelectiveParser struct {
	pattern string
	ruleID  string
	name    string
	index   string
}

func (p *testSelectiveParser) Metadata() plugin.Metadata { return testSelectiveParserMeta() }
func (p *testSelectiveParser) Validate(config map[string]any) error {
	if _, ok := config["pattern"].(string); !ok {
		return errors.New("pattern is required")
	}
	return nil
}
func (p *testSelectiveParser) Init(ctx plugin.InitContext, config map[string]any) error {
	pattern, _ := config["pattern"].(string)
	p.pattern = pattern
	p.ruleID, _ = config["parse_rule_id"].(string)
	p.name, _ = config["parse_rule_name"].(string)
	p.index, _ = config["output_index"].(string)
	return nil
}
func (p *testSelectiveParser) Process(ctx plugin.ProcessContext, ev *event.Event) (*event.Event, error) {
	if !strings.Contains(ev.Raw, p.pattern) {
		return ev, plugin.NewError(plugin.ErrParseFailed, "rule did not match", false, nil)
	}
	ev.Metadata["parse_status"] = "parsed"
	ev.Metadata["parse_rule_id"] = p.ruleID
	ev.Metadata["parse_rule_name"] = p.name
	ev.Metadata["sourcetype"] = p.name
	ev.Metadata["index"] = p.index
	ev.Fields["matched_pattern"] = p.pattern
	return ev, nil
}
func (p *testSelectiveParser) Close() error { return nil }

type capturingOutput struct {
	writes int
}

func (o *capturingOutput) Metadata() plugin.Metadata                                { return testOutputMeta() }
func (o *capturingOutput) Validate(config map[string]any) error                     { return nil }
func (o *capturingOutput) Init(ctx plugin.InitContext, config map[string]any) error { return nil }
func (o *capturingOutput) Write(ctx context.Context, batch *plugin.EventBatch) error {
	if len(batch.Events) == 0 {
		return errors.New("empty batch")
	}
	o.writes++
	return nil
}
func (o *capturingOutput) Close() error { return nil }

func mustRegister(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
