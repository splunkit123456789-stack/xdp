package kafka

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	buskafka "xdp/pkg/bus/kafka"
	"xdp/pkg/event"
	"xdp/pkg/plugin"
)

func TestOutputWritesEventJSONToKafkaProducer(t *testing.T) {
	producer := buskafka.NewBus()
	out := NewWithProducer(producer)
	if err := out.Init(plugin.BasicInitContext{Ctx: context.Background(), Code: "kafka-output", Version: "1.0.0"}, map[string]any{
		"topic": "security-events",
		"key":   "${event_id}",
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	ev := event.New("raw", event.Source{Type: "test"}, time.Now().UTC())
	ev.PipelineID = "pipeline-a"
	if err := out.Write(context.Background(), &plugin.EventBatch{PipelineID: ev.PipelineID, PipelineVersion: "v1", OutputID: "kafka", Events: []*event.Event{ev}}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	messages, err := producer.Consume(context.Background(), "security-events", 1)
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	if messages[0].Key != ev.EventID {
		t.Fatalf("key = %q, want %q", messages[0].Key, ev.EventID)
	}
	var got event.Event
	if err := json.Unmarshal(messages[0].Value, &got); err != nil {
		t.Fatalf("message value is not event JSON: %v", err)
	}
	if got.EventID != ev.EventID {
		t.Fatalf("event_id = %q, want %q", got.EventID, ev.EventID)
	}
}

func TestParseConfigAcceptsCommaSeparatedBrokers(t *testing.T) {
	cfg, err := parseConfig(map[string]any{"topic": "out", "brokers": "kafka-a:9092,kafka-b:9092"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if len(cfg.Brokers) != 2 {
		t.Fatalf("brokers = %#v, want two brokers", cfg.Brokers)
	}
}

func TestParseConfigAcceptsTimeout(t *testing.T) {
	cfg, err := parseConfig(map[string]any{"topic": "out", "timeout": "2s"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.Timeout != 2*time.Second {
		t.Fatalf("timeout = %s, want 2s", cfg.Timeout)
	}
}

func TestParseConfigRequiresTopic(t *testing.T) {
	if _, err := parseConfig(map[string]any{}); err == nil {
		t.Fatal("parseConfig() expected topic error")
	}
}
