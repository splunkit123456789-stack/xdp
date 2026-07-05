package event

import "time"

type Event struct {
	EventID         string            `json:"event_id"`
	EventTime       time.Time         `json:"event_time"`
	IngestTime      time.Time         `json:"ingest_time"`
	PipelineID      string            `json:"pipeline_id,omitempty"`
	PipelineVersion string            `json:"pipeline_version,omitempty"`
	Source          Source            `json:"source"`
	Metadata        map[string]any    `json:"metadata"`
	Raw             string            `json:"raw"`
	Fields          map[string]any    `json:"fields"`
	Labels          map[string]string `json:"labels,omitempty"`
	Tags            []string          `json:"tags,omitempty"`
	Errors          []ProcessingError `json:"errors,omitempty"`
}

func New(raw string, source Source, now time.Time) *Event {
	return &Event{
		EventID:    NewID(),
		EventTime:  now,
		IngestTime: now,
		Source:     source,
		Metadata: map[string]any{
			"parse_status":    "unparsed",
			"parse_rule_id":   "",
			"parse_rule_name": "",
			"parse_error":     "",
		},
		Raw:    raw,
		Fields: map[string]any{},
		Labels: map[string]string{},
		Tags:   []string{},
		Errors: []ProcessingError{},
	}
}
