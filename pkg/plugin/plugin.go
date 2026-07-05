package plugin

import (
	"context"
	"time"

	"xdp/pkg/event"
)

type ProcessorPlugin interface {
	Metadata() Metadata
	Validate(config map[string]any) error
	Init(ctx InitContext, config map[string]any) error
	Process(ctx ProcessContext, event *event.Event) (*event.Event, error)
	Close() error
}

type InputPlugin interface {
	Metadata() Metadata
	Validate(config map[string]any) error
	Init(ctx InitContext, config map[string]any) error
	Start(ctx context.Context, emit EmitFunc) error
	Close() error
}

type OutputPlugin interface {
	Metadata() Metadata
	Validate(config map[string]any) error
	Init(ctx InitContext, config map[string]any) error
	Write(ctx context.Context, batch *EventBatch) error
	Close() error
}

type EmitFunc func(ctx context.Context, event *event.Event) error

type EventBatch struct {
	PipelineID      string
	PipelineVersion string
	OutputID        string
	Events          []*event.Event
}

type InitContext interface {
	Context() context.Context
	PluginCode() string
	PluginVersion() string
}

type ProcessContext interface {
	Context() context.Context
	PipelineID() string
	PipelineVersion() string
	StageID() string
	Attempt() int
	Now() time.Time
	AddTag(tag string)
	AddError(err event.ProcessingError)
}
