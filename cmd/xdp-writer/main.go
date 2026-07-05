package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"time"

	"xdp/pkg/bus/kafka"
	"xdp/pkg/event"
	"xdp/pkg/plugin"
	clickhouseoutput "xdp/plugins/output/clickhouse"
)

func main() {
	ctx := context.Background()
	brokers := strings.Split(env("XDP_KAFKA_BROKERS", "127.0.0.1:9092"), ",")
	bus := kafka.NewKafka(brokers, "xdp-writer")
	outputTopic := kafka.OutputTopic(env("XDP_TARGET", "default"))
	deadletterTopic := kafka.DeadletterTopic("writer")
	writer := clickhouseoutput.New()
	if err := writer.Init(plugin.BasicInitContext{Ctx: ctx, Code: "clickhouse-output", Version: "1.0.0"}, map[string]any{"endpoint": env("XDP_CLICKHOUSE_ENDPOINT", "http://127.0.0.1:8123"), "database": env("XDP_CLICKHOUSE_DATABASE", "xdp"), "username": env("XDP_CLICKHOUSE_USERNAME", ""), "password": env("XDP_CLICKHOUSE_PASSWORD", "")}); err != nil {
		panic(err)
	}
	slog.Info("xdp-writer started", "output_topic", outputTopic)
	for {
		messages, err := bus.Consume(ctx, outputTopic, 100)
		if err != nil {
			slog.Warn("consume failed", "error", err)
			time.Sleep(time.Second)
			continue
		}
		events := []*event.Event{}
		for _, msg := range messages {
			var e event.Event
			if err := json.Unmarshal(msg.Value, &e); err == nil {
				events = append(events, &e)
			} else {
				slog.Warn("drop invalid output payload", "topic", outputTopic, "error", err)
			}
		}
		if len(events) == 0 {
			continue
		}
		if err := writer.Write(ctx, &plugin.EventBatch{PipelineID: "kafka-writer", PipelineVersion: "v1", OutputID: "clickhouse", Events: events}); err != nil {
			slog.Warn("clickhouse write failed", "events", len(events), "error", err)
			for _, e := range events {
				payload, _ := json.Marshal(e)
				if produceErr := bus.Produce(ctx, kafka.Message{Topic: deadletterTopic, Key: e.EventID, Value: payload}); produceErr != nil {
					slog.Warn("produce writer deadletter failed", "topic", deadletterTopic, "event_id", e.EventID, "error", produceErr)
				}
			}
			continue
		}
		slog.Info("events written", "output_id", "clickhouse", "events", len(events))
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
