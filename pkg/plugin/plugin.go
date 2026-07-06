package plugin

import (
	"context"
	"time"

	"xdp/pkg/event"
	"xdp/pkg/search/splstats"
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
	Stop(ctx context.Context) error
	Reload(ctx context.Context, config map[string]any) error
	Health(ctx context.Context) HealthStatus
	Close() error
}

type OutputPlugin interface {
	Metadata() Metadata
	Validate(config map[string]any) error
	Init(ctx InitContext, config map[string]any) error
	Write(ctx context.Context, batch *EventBatch) error
	Close() error
}

type SearchPlugin interface {
	Metadata() Metadata
	Validate(config map[string]any) error
	Init(ctx InitContext, config map[string]any) error
	Execute(ctx context.Context, input SearchInput, stats splstats.Query) (splstats.Result, error)
	Close() error
}

type SearchStatsBackend interface {
	Stats(ctx context.Context, query SearchStatsQuery) (splstats.Result, error)
}

type SearchInput struct {
	Index     string
	Keyword   string
	Field     string
	Value     string
	StartTime time.Time
	EndTime   time.Time
	Limit     int
	Offset    int
	HotFields []SearchHotField
	Backend   SearchStatsBackend
}

type SearchStatsQuery struct {
	Index     string
	Keyword   string
	Field     string
	Value     string
	StartTime time.Time
	EndTime   time.Time
	Limit     int
	Offset    int
	Stats     splstats.Query
	HotFields []SearchHotField
}

type SearchHotField struct {
	Name         string
	Type         string
	Searchable   bool
	Aggregatable bool
	Aliases      []string
}

type EmitFunc func(ctx context.Context, event *event.Event) error

type HealthStatus struct {
	Status              string         `json:"status"`
	ListenerStatus      string         `json:"listener_status,omitempty"`
	Endpoint            string         `json:"endpoint,omitempty"`
	ReceivedEventsTotal uint64         `json:"received_events_total,omitempty"`
	ReceivedBytesTotal  uint64         `json:"received_bytes_total,omitempty"`
	LastReceivedAt      time.Time      `json:"last_received_at,omitempty"`
	LastLoadedAt        time.Time      `json:"last_loaded_at,omitempty"`
	LastError           string         `json:"last_error,omitempty"`
	Metadata            map[string]any `json:"metadata,omitempty"`
}

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
