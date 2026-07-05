package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	buskafka "xdp/pkg/bus/kafka"
	"xdp/pkg/event"
	"xdp/pkg/plugin"
)

type Output struct {
	producer buskafka.Producer
	cfg      Config
}

type Config struct {
	Brokers []string
	Topic   string
	Key     string
	Timeout time.Duration
}

func New() *Output { return &Output{} }

func NewWithProducer(producer buskafka.Producer) *Output {
	return &Output{producer: producer}
}

func (o *Output) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Code:        "kafka-output",
		Name:        "Kafka Output",
		Type:        plugin.TypeOutput,
		Version:     "1.0.0",
		Description: "Forward events to a Kafka topic",
		Runtime:     "go",
		ConfigSchema: plugin.Schema{
			"type": "object",
			"properties": map[string]any{
				"brokers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"topic":   map[string]any{"type": "string"},
				"key":     map[string]any{"type": "string", "default": "${event_id}"},
				"timeout": map[string]any{"type": "string", "default": "5s"},
			},
			"required": []string{"topic"},
		},
	}
}

func (o *Output) Validate(config map[string]any) error {
	_, err := parseConfig(config)
	return err
}

func (o *Output) Init(ctx plugin.InitContext, config map[string]any) error {
	cfg, err := parseConfig(config)
	if err != nil {
		return err
	}
	o.cfg = cfg
	if o.producer == nil {
		o.producer = buskafka.NewKafka(cfg.Brokers, "xdp-kafka-output")
	}
	return nil
}

func (o *Output) Write(ctx context.Context, batch *plugin.EventBatch) error {
	if o.producer == nil {
		return plugin.NewError(plugin.ErrInvalidConfig, "kafka output is not initialized", false, nil)
	}
	writeCtx := ctx
	cancel := func() {}
	if o.cfg.Timeout > 0 {
		writeCtx, cancel = context.WithTimeout(ctx, o.cfg.Timeout)
	}
	defer cancel()
	for _, ev := range batch.Events {
		data, err := json.Marshal(ev)
		if err != nil {
			return plugin.NewError(plugin.ErrOutputFailed, "kafka output encode failed", false, err)
		}
		if err := o.producer.Produce(writeCtx, buskafka.Message{Topic: o.cfg.Topic, Key: keyForEvent(o.cfg.Key, ev), Value: data}); err != nil {
			return plugin.NewError(plugin.ErrOutputFailed, "kafka output produce failed", true, err)
		}
	}
	return nil
}

func (o *Output) Close() error { return nil }

func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}

func parseConfig(config map[string]any) (Config, error) {
	cfg := Config{Brokers: []string{"127.0.0.1:9092"}, Key: "${event_id}", Timeout: 5 * time.Second}
	for key, value := range config {
		switch key {
		case "brokers":
			brokers, err := parseBrokers(value)
			if err != nil {
				return cfg, err
			}
			cfg.Brokers = brokers
		case "topic":
			text, ok := value.(string)
			if !ok {
				return cfg, fmt.Errorf("topic must be a string")
			}
			cfg.Topic = text
		case "key":
			text, ok := value.(string)
			if !ok {
				return cfg, fmt.Errorf("key must be a string")
			}
			cfg.Key = text
		case "timeout":
			text, ok := value.(string)
			if !ok {
				return cfg, fmt.Errorf("timeout must be a string")
			}
			timeout, err := time.ParseDuration(text)
			if err != nil {
				return cfg, fmt.Errorf("timeout must be a duration: %w", err)
			}
			cfg.Timeout = timeout
		}
	}
	if strings.TrimSpace(cfg.Topic) == "" {
		return cfg, fmt.Errorf("topic is required")
	}
	if len(cfg.Brokers) == 0 {
		return cfg, fmt.Errorf("brokers is required")
	}
	return cfg, nil
}

func parseBrokers(value any) ([]string, error) {
	switch v := value.(type) {
	case []string:
		return compact(v), nil
	case []any:
		brokers := make([]string, 0, len(v))
		for _, item := range v {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("brokers must contain strings")
			}
			brokers = append(brokers, text)
		}
		return compact(brokers), nil
	case string:
		return compact(strings.Split(v, ",")), nil
	default:
		return nil, fmt.Errorf("brokers must be a string or array")
	}
}

func compact(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func keyForEvent(template string, ev *event.Event) string {
	switch template {
	case "", "${event_id}":
		return ev.EventID
	case "${pipeline_id}":
		return ev.PipelineID
	default:
		return template
	}
}
