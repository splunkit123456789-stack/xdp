package kafka

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	buskafka "xdp/pkg/bus/kafka"
	"xdp/pkg/event"
	"xdp/pkg/plugin"
)

type closableTestConsumer struct {
	closed atomic.Bool
}

func (c *closableTestConsumer) Consume(ctx context.Context, topic string, max int) ([]buskafka.Message, error) {
	return nil, nil
}

func (c *closableTestConsumer) Close() error {
	c.closed.Store(true)
	return nil
}

func TestMetadataUsesStandardKafkaInputPluginContract(t *testing.T) {
	meta := New().Metadata()
	if meta.Code != "kafka" {
		t.Fatalf("plugin code = %q, want kafka", meta.Code)
	}
	if meta.Type != plugin.TypeInput {
		t.Fatalf("plugin type = %q, want input", meta.Type)
	}
	if meta.Runtime != "go_builtin" {
		t.Fatalf("runtime = %q, want go_builtin", meta.Runtime)
	}
	required, ok := meta.ConfigSchema["required"].([]string)
	if !ok {
		t.Fatalf("required schema = %#v, want []string", meta.ConfigSchema["required"])
	}
	for _, want := range []string{"brokers", "topic", "consumer_group", "start_offset", "security_protocol", "encoding", "log_filter_enabled"} {
		if !containsString(required, want) {
			t.Fatalf("required schema = %#v, missing %s", required, want)
		}
	}
	if meta.Labels["phase"] != "P1" || meta.Labels["runtime_ingest"] != "true" {
		t.Fatalf("labels = %#v, want P1 runtime_ingest=true", meta.Labels)
	}
}

func TestKafkaInputConsumesMessagesAndEmitsEvents(t *testing.T) {
	bus := buskafka.NewBus()
	input := NewWithConsumer(bus)
	if err := input.Init(plugin.BasicInitContext{Ctx: context.Background(), Code: "kafka", Version: "1.0.0"}, map[string]any{
		"brokers":            []any{"127.0.0.1:9092"},
		"topic":              "source-topic",
		"consumer_group":     "xdp-test",
		"start_offset":       "earliest",
		"security_protocol":  "PLAINTEXT",
		"encoding":           "UTF-8",
		"log_filter_enabled": true,
		"log_filter_regex":   "action=(allow|deny)",
		"source_name":        "Kafka Audit",
		"data_source_id":     "kafka-audit",
		"config_version":     3,
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if err := bus.Produce(context.Background(), buskafka.Message{Topic: "source-topic", Key: "1", Value: []byte("action=deny src=10.0.1.8")}); err != nil {
		t.Fatalf("produce matching message: %v", err)
	}
	if err := bus.Produce(context.Background(), buskafka.Message{Topic: "source-topic", Key: "2", Value: []byte("healthcheck ok")}); err != nil {
		t.Fatalf("produce filtered message: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan string, 1)
	errc := make(chan error, 1)
	go func() {
		errc <- input.Start(ctx, func(ctx context.Context, ev *event.Event) error {
			events <- ev.Raw
			cancel()
			return nil
		})
	}()

	select {
	case raw := <-events:
		if raw != "action=deny src=10.0.1.8" {
			t.Fatalf("emitted raw = %q", raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for emitted Kafka event")
	}
	if err := input.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("Start() returned error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for input stop")
	}
	status := input.Health(context.Background())
	if status.ReceivedEventsTotal != 1 || status.ReceivedBytesTotal == 0 || status.Endpoint != "kafka://127.0.0.1:9092/source-topic" {
		t.Fatalf("health = %#v", status)
	}
}

func TestKafkaInputStopClosesUnderlyingConsumer(t *testing.T) {
	consumer := &closableTestConsumer{}
	input := NewWithConsumer(consumer)
	if err := input.Init(plugin.BasicInitContext{Ctx: context.Background(), Code: "kafka", Version: "1.0.0"}, map[string]any{
		"brokers":            []any{"127.0.0.1:9092"},
		"topic":              "source-topic",
		"consumer_group":     "xdp-test",
		"start_offset":       "earliest",
		"security_protocol":  "PLAINTEXT",
		"encoding":           "UTF-8",
		"log_filter_enabled": false,
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if err := input.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if !consumer.closed.Load() {
		t.Fatal("Stop() did not close the underlying Kafka consumer")
	}
}

func TestKafkaInputReloadClosesPreviousConsumer(t *testing.T) {
	consumer := &closableTestConsumer{}
	input := NewWithConsumer(consumer)
	if err := input.Init(plugin.BasicInitContext{Ctx: context.Background(), Code: "kafka", Version: "1.0.0"}, map[string]any{
		"brokers":            []any{"127.0.0.1:9092"},
		"topic":              "source-topic",
		"consumer_group":     "xdp-test",
		"start_offset":       "earliest",
		"security_protocol":  "PLAINTEXT",
		"encoding":           "UTF-8",
		"log_filter_enabled": false,
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if err := input.Reload(context.Background(), map[string]any{
		"brokers":            []any{"127.0.0.1:9092"},
		"topic":              "next-topic",
		"consumer_group":     "xdp-test",
		"start_offset":       "earliest",
		"security_protocol":  "PLAINTEXT",
		"encoding":           "UTF-8",
		"log_filter_enabled": false,
	}); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	if !consumer.closed.Load() {
		t.Fatal("Reload() did not close the previous Kafka consumer")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
