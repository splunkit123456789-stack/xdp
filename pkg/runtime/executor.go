package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"xdp/pkg/event"
	"xdp/pkg/expr"
	"xdp/pkg/pipeline"
	"xdp/pkg/plugin"
)

type Executor struct {
	Registry *plugin.Registry
}

type Result struct {
	Event  *event.Event `json:"event"`
	Status string       `json:"status"`
}

func NewExecutor(reg *plugin.Registry) *Executor {
	return &Executor{Registry: reg}
}

func (e *Executor) Execute(ctx context.Context, spec pipeline.Pipeline, ev *event.Event) (Result, error) {
	if e.Registry == nil {
		return Result{Event: ev, Status: "failed"}, fmt.Errorf("plugin registry is required")
	}
	if err := spec.Validate(); err != nil {
		return Result{Event: ev, Status: "failed"}, err
	}
	ev.PipelineID = spec.Metadata.ID
	ev.PipelineVersion = "v1"
	applySourceConfig(spec.Spec.Source, ev)
	status := "processed"
	for _, stage := range spec.Spec.Stages {
		if disabled(stage.Enabled) {
			continue
		}
		matched, err := expr.Eval(stage.When, ev)
		if err != nil {
			appendError(ev, stage, err, time.Now().UTC())
			return Result{Event: ev, Status: "failed"}, err
		}
		if !matched {
			continue
		}
		if stage.Type == "parser_group" {
			next, groupMatched, err := e.executeParserGroup(ctx, spec, stage, ev)
			if next != nil {
				ev = next
			}
			if err != nil {
				appendError(ev, stage, err, time.Now().UTC())
				if actionFor(stage.OnError, spec.Spec.Settings.ErrorPolicy) == "continue" {
					status = "processed_with_errors"
					continue
				}
				return Result{Event: ev, Status: "dead_letter"}, err
			}
			if !groupMatched {
				markUnparsed(ev, stage)
			}
			continue
		}
		processor, err := e.processor(stage)
		if err != nil {
			return Result{Event: ev, Status: "failed"}, err
		}
		if err := processor.Init(plugin.BasicInitContext{Ctx: ctx, Code: stage.Plugin, Version: stage.Version}, stage.Config); err != nil {
			appendError(ev, stage, err, time.Now().UTC())
			return Result{Event: ev, Status: "dead_letter"}, err
		}
		out, err := processor.Process(ProcessContext{ctx: ctx, event: ev, stageID: stage.ID}, ev)
		_ = processor.Close()
		if out != nil {
			ev = out
		}
		if err != nil {
			appendError(ev, stage, err, time.Now().UTC())
			if actionFor(stage.OnError, spec.Spec.Settings.ErrorPolicy) == "continue" {
				status = "processed_with_errors"
				continue
			}
			return Result{Event: ev, Status: "dead_letter"}, err
		}
	}
	for _, output := range spec.Spec.Outputs {
		if disabled(output.Enabled) {
			continue
		}
		matched, err := expr.Eval(output.When, ev)
		if err != nil {
			appendOutputError(ev, output, err, time.Now().UTC())
			return Result{Event: ev, Status: "failed"}, err
		}
		if !matched {
			continue
		}
		writer, err := e.output(output)
		if err != nil {
			return Result{Event: ev, Status: "failed"}, err
		}
		if err := writer.Init(plugin.BasicInitContext{Ctx: ctx, Code: output.Plugin, Version: output.Version}, output.Config); err != nil {
			return Result{Event: ev, Status: "failed"}, err
		}
		err = writer.Write(ctx, &plugin.EventBatch{PipelineID: ev.PipelineID, PipelineVersion: ev.PipelineVersion, OutputID: output.ID, Events: []*event.Event{ev}})
		_ = writer.Close()
		if err != nil {
			appendOutputError(ev, output, err, time.Now().UTC())
			return Result{Event: ev, Status: "dead_letter"}, err
		}
	}
	return Result{Event: ev, Status: status}, nil
}

func (e *Executor) executeParserGroup(ctx context.Context, spec pipeline.Pipeline, group pipeline.StageSpec, ev *event.Event) (*event.Event, bool, error) {
	for _, stage := range group.Stages {
		if disabled(stage.Enabled) {
			continue
		}
		matched, err := expr.Eval(stage.When, ev)
		if err != nil {
			return ev, false, err
		}
		if !matched {
			continue
		}
		processor, err := e.processor(stage)
		if err != nil {
			return ev, false, err
		}
		if err := processor.Init(plugin.BasicInitContext{Ctx: ctx, Code: stage.Plugin, Version: stage.Version}, stage.Config); err != nil {
			return ev, false, err
		}
		out, err := processor.Process(ProcessContext{ctx: ctx, event: ev, stageID: stage.ID}, ev)
		_ = processor.Close()
		if out != nil {
			ev = out
		}
		if err != nil {
			if isParseMiss(err) {
				clearParseMissMetadata(ev)
				continue
			}
			applyParserRuleOutputIndex(ev, stage)
			return ev, false, err
		}
		applyParserRuleOutputIndex(ev, stage)
		return ev, true, nil
	}
	return ev, false, nil
}

func applySourceConfig(source pipeline.SourceSpec, ev *event.Event) {
	if ev == nil || source.Config == nil {
		return
	}
	if value, ok := source.Config["source_name"].(string); ok && value != "" {
		ev.Source.Name = value
	}
}

func applyParserRuleOutputIndex(ev *event.Event, stage pipeline.StageSpec) {
	if ev == nil || stage.Config == nil {
		return
	}
	index, _ := stage.Config["output_index"].(string)
	index = strings.TrimSpace(index)
	if index == "" {
		return
	}
	if ev.Metadata == nil {
		ev.Metadata = map[string]any{}
	}
	ev.Metadata["index"] = index
}

func isParseMiss(err error) bool {
	var pluginErr *plugin.PluginError
	return errors.As(err, &pluginErr) && pluginErr.Code == plugin.ErrNoMatch
}

func clearParseMissMetadata(ev *event.Event) {
	if ev == nil || ev.Metadata == nil {
		return
	}
	for _, key := range []string{"parse_status", "parse_rule_id", "parse_rule_name", "sourcetype", "parse_error"} {
		delete(ev.Metadata, key)
	}
}

func markUnparsed(ev *event.Event, group pipeline.StageSpec) {
	if ev == nil {
		return
	}
	if ev.Metadata == nil {
		ev.Metadata = map[string]any{}
	}
	ev.Metadata["parse_status"] = "unparsed"
	ev.Metadata["parse_rule_id"] = ""
	ev.Metadata["parse_rule_name"] = ""
	ev.Metadata["parse_error"] = ""
	if index := parserGroupFallbackOutputIndex(group); index != "" {
		ev.Metadata["index"] = index
	}
	delete(ev.Metadata, "sourcetype")
}

func parserGroupFallbackOutputIndex(group pipeline.StageSpec) string {
	if group.Config == nil {
		return ""
	}
	index, _ := group.Config["fallback_output_index"].(string)
	return strings.TrimSpace(index)
}

func (e *Executor) processor(stage pipeline.StageSpec) (plugin.ProcessorPlugin, error) {
	factory, _, err := e.Registry.Get(plugin.Type(stage.Type), stage.Plugin, stage.Version)
	if err != nil {
		return nil, err
	}
	processor, ok := factory().(plugin.ProcessorPlugin)
	if !ok {
		return nil, fmt.Errorf("plugin %s is not a processor", stage.Plugin)
	}
	return processor, nil
}

func (e *Executor) output(output pipeline.OutputSpec) (plugin.OutputPlugin, error) {
	factory, _, err := e.Registry.Get(plugin.TypeOutput, output.Plugin, output.Version)
	if err != nil {
		return nil, err
	}
	writer, ok := factory().(plugin.OutputPlugin)
	if !ok {
		return nil, fmt.Errorf("plugin %s is not an output", output.Plugin)
	}
	return writer, nil
}

func disabled(enabled *bool) bool {
	return enabled != nil && !*enabled
}

func actionFor(stagePolicy *pipeline.ErrorPolicy, defaultPolicy *pipeline.ErrorPolicy) string {
	if stagePolicy != nil && stagePolicy.Action != "" {
		return stagePolicy.Action
	}
	if defaultPolicy != nil && defaultPolicy.Action != "" {
		return defaultPolicy.Action
	}
	return "dead_letter"
}

func appendError(ev *event.Event, stage pipeline.StageSpec, err error, now time.Time) {
	pe := event.ProcessingError{PluginID: stage.Plugin, Stage: stage.ID, ErrorCode: "PLUGIN_FAILED", Message: err.Error(), Time: now}
	var pluginErr *plugin.PluginError
	if errors.As(err, &pluginErr) {
		pe.ErrorCode = string(pluginErr.Code)
		pe.Retryable = pluginErr.Retryable
	}
	ev.Errors = append(ev.Errors, pe)
}

func appendOutputError(ev *event.Event, output pipeline.OutputSpec, err error, now time.Time) {
	pe := event.ProcessingError{PluginID: output.Plugin, Stage: output.ID, ErrorCode: "OUTPUT_FAILED", Message: err.Error(), Time: now}
	var pluginErr *plugin.PluginError
	if errors.As(err, &pluginErr) {
		pe.ErrorCode = string(pluginErr.Code)
		pe.Retryable = pluginErr.Retryable
	}
	ev.Errors = append(ev.Errors, pe)
}
