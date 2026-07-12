package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	batchSize := envInt("XDP_WRITER_BATCH_SIZE", 100)
	flushInterval := time.Duration(envInt("XDP_WRITER_FLUSH_INTERVAL_MS", 1000)) * time.Millisecond
	retryMax := envInt("XDP_WRITER_RETRY_MAX", 3)
	retryBackoff := time.Duration(envInt("XDP_WRITER_RETRY_BACKOFF_MS", 500)) * time.Millisecond
	writerEndpoint := env("XDP_WRITER_ADDR", ":8082")
	clickhouseEndpoint := env("XDP_CLICKHOUSE_ENDPOINT", "http://127.0.0.1:8123")
	metrics := newWriterMetrics(writerConfig{
		OutputTopic:      outputTopic,
		DeadletterTopic:  deadletterTopic,
		BatchSize:        batchSize,
		FlushInterval:    flushInterval,
		RetryMax:         retryMax,
		RetryBackoff:     retryBackoff,
		ClickHouseOutput: clickhouseEndpoint,
	})
	go func() {
		slog.Info("xdp-writer runtime endpoint started", "addr", writerEndpoint)
		if err := http.ListenAndServe(writerEndpoint, metrics.handler()); err != nil && err != http.ErrServerClosed {
			slog.Warn("xdp-writer runtime endpoint failed", "addr", writerEndpoint, "error", err)
		}
	}()
	writer := clickhouseoutput.New()
	if err := writer.Init(plugin.BasicInitContext{Ctx: ctx, Code: "clickhouse-output", Version: "1.0.0"}, map[string]any{"endpoint": clickhouseEndpoint, "database": env("XDP_CLICKHOUSE_DATABASE", "xdp"), "username": env("XDP_CLICKHOUSE_USERNAME", ""), "password": env("XDP_CLICKHOUSE_PASSWORD", "")}); err != nil {
		panic(err)
	}
	slog.Info("xdp-writer started", "output_topic", outputTopic, "batch_size", batchSize, "flush_interval_ms", flushInterval.Milliseconds(), "retry_max", retryMax, "retry_backoff_ms", retryBackoff.Milliseconds(), "runtime_addr", writerEndpoint)
	for {
		messages, err := bus.Consume(ctx, outputTopic, batchSize)
		if err != nil {
			slog.Warn("consume failed", "error", err)
			metrics.observeConsumeError()
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
				metrics.observeInvalidPayload()
			}
		}
		if len(events) == 0 {
			time.Sleep(flushInterval)
			continue
		}
		started := time.Now()
		retryCount, err := writeWithRetry(ctx, writer, events, retryMax, retryBackoff)
		duration := time.Since(started)
		if err != nil {
			deadletters := produceDeadletters(ctx, bus, deadletterTopic, events)
			metrics.observeFailure(len(events), len(groupIndexes(events)), duration, retryCount, deadletters)
			slog.Warn("clickhouse write failed", "events", len(events), "batch_size", len(events), "duration_ms", duration.Milliseconds(), "retry_count", retryCount, "deadletter_count", deadletters, "error", err)
			continue
		}
		metrics.observeSuccess(len(events), len(groupIndexes(events)), duration, retryCount)
		slog.Info("events written", "output_id", "clickhouse", "events", len(events), "batch_size", len(events), "duration_ms", duration.Milliseconds(), "retry_count", retryCount, "deadletter_count", 0)
	}
}

type writerConfig struct {
	OutputTopic      string
	DeadletterTopic  string
	BatchSize        int
	FlushInterval    time.Duration
	RetryMax         int
	RetryBackoff     time.Duration
	ClickHouseOutput string
}

type writerMetrics struct {
	cfg writerConfig

	startedAt          time.Time
	totalEvents        atomic.Uint64
	failedEvents       atomic.Uint64
	deadletterEvents   atomic.Uint64
	totalBatches       atomic.Uint64
	failedBatches      atomic.Uint64
	consumeErrors      atomic.Uint64
	invalidPayloads    atomic.Uint64
	totalDurationNanos atomic.Int64

	lastBatchSize       atomic.Int64
	lastIndexCount      atomic.Int64
	lastDurationMS      atomic.Int64
	lastRetryCount      atomic.Int64
	lastDeadletterCount atomic.Int64
	lastWriteUnix       atomic.Int64
	lastErrorUnix       atomic.Int64

	latencyMu sync.Mutex
	latencies []int64
}

type writerRuntimeSnapshot struct {
	Status              string    `json:"status"`
	StartedAt           string    `json:"started_at"`
	OutputTopic         string    `json:"output_topic"`
	DeadletterTopic     string    `json:"deadletter_topic"`
	BatchSize           int       `json:"batch_size"`
	FlushIntervalMS     int64     `json:"flush_interval_ms"`
	RetryMax            int       `json:"retry_max"`
	RetryBackoffMS      int64     `json:"retry_backoff_ms"`
	ClickHouseOutput    string    `json:"clickhouse_output"`
	TotalEvents         uint64    `json:"total_events"`
	FailedEvents        uint64    `json:"failed_events"`
	DeadletterEvents    uint64    `json:"deadletter_events"`
	TotalBatches        uint64    `json:"total_batches"`
	FailedBatches       uint64    `json:"failed_batches"`
	ConsumeErrors       uint64    `json:"consume_errors"`
	InvalidPayloads     uint64    `json:"invalid_payloads"`
	LastBatchSize       int       `json:"last_batch_size"`
	LastIndexCount      int       `json:"last_index_count"`
	LastDurationMS      int64     `json:"last_duration_ms"`
	LastRetryCount      int       `json:"last_retry_count"`
	LastDeadletterCount int       `json:"last_deadletter_count"`
	LastWriteAt         string    `json:"last_write_at,omitempty"`
	LastErrorAt         string    `json:"last_error_at,omitempty"`
	FailureRate         float64   `json:"failure_rate"`
	EPS                 float64   `json:"eps"`
	AverageDurationMS   float64   `json:"avg_duration_ms"`
	P95IngestLatencyMS  int64     `json:"p95_ingest_latency_ms"`
	GeneratedAt         time.Time `json:"generated_at"`
}

func newWriterMetrics(cfg writerConfig) *writerMetrics {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	return &writerMetrics{cfg: cfg, startedAt: time.Now()}
}

func (m *writerMetrics) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("GET /api/v1/writer/runtime", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, m.snapshot()) })
	mux.HandleFunc("GET /metrics", m.prometheus)
	return mux
}

func (m *writerMetrics) observeSuccess(batchSize int, indexCount int, duration time.Duration, retryCount int) {
	m.totalEvents.Add(uint64(batchSize))
	m.totalBatches.Add(1)
	m.observeBatch(batchSize, indexCount, duration, retryCount, 0, false)
}

func (m *writerMetrics) observeFailure(batchSize int, indexCount int, duration time.Duration, retryCount int, deadletterCount int) {
	m.totalEvents.Add(uint64(batchSize))
	m.failedEvents.Add(uint64(batchSize))
	m.deadletterEvents.Add(uint64(deadletterCount))
	m.totalBatches.Add(1)
	m.failedBatches.Add(1)
	m.observeBatch(batchSize, indexCount, duration, retryCount, deadletterCount, true)
}

func (m *writerMetrics) observeBatch(batchSize int, indexCount int, duration time.Duration, retryCount int, deadletterCount int, failed bool) {
	m.lastBatchSize.Store(int64(batchSize))
	m.lastIndexCount.Store(int64(indexCount))
	m.lastDurationMS.Store(duration.Milliseconds())
	m.lastRetryCount.Store(int64(retryCount))
	m.lastDeadletterCount.Store(int64(deadletterCount))
	m.totalDurationNanos.Add(duration.Nanoseconds())
	now := time.Now().Unix()
	m.lastWriteUnix.Store(now)
	if failed {
		m.lastErrorUnix.Store(now)
	}
	m.latencyMu.Lock()
	m.latencies = append(m.latencies, duration.Milliseconds())
	if len(m.latencies) > 512 {
		m.latencies = append([]int64(nil), m.latencies[len(m.latencies)-512:]...)
	}
	m.latencyMu.Unlock()
}

func (m *writerMetrics) observeConsumeError() {
	m.consumeErrors.Add(1)
	m.lastErrorUnix.Store(time.Now().Unix())
}

func (m *writerMetrics) observeInvalidPayload() {
	m.invalidPayloads.Add(1)
	m.lastErrorUnix.Store(time.Now().Unix())
}

func (m *writerMetrics) snapshot() writerRuntimeSnapshot {
	totalEvents := m.totalEvents.Load()
	failedEvents := m.failedEvents.Load()
	totalBatches := m.totalBatches.Load()
	failedBatches := m.failedBatches.Load()
	status := "idle"
	if totalBatches > 0 {
		status = "running"
	}
	if failedEvents > 0 || failedBatches > 0 || m.consumeErrors.Load() > 0 || m.invalidPayloads.Load() > 0 {
		status = "degraded"
	}
	elapsed := time.Since(m.startedAt).Seconds()
	eps := 0.0
	if elapsed > 0 {
		eps = float64(totalEvents) / elapsed
	}
	avgDuration := 0.0
	if totalBatches > 0 {
		avgDuration = float64(m.totalDurationNanos.Load()) / 1e6 / float64(totalBatches)
	}
	failureRate := 0.0
	if totalEvents > 0 {
		failureRate = float64(failedEvents) / float64(totalEvents)
	}
	return writerRuntimeSnapshot{
		Status:              status,
		StartedAt:           m.startedAt.Format(time.RFC3339),
		OutputTopic:         m.cfg.OutputTopic,
		DeadletterTopic:     m.cfg.DeadletterTopic,
		BatchSize:           m.cfg.BatchSize,
		FlushIntervalMS:     m.cfg.FlushInterval.Milliseconds(),
		RetryMax:            m.cfg.RetryMax,
		RetryBackoffMS:      m.cfg.RetryBackoff.Milliseconds(),
		ClickHouseOutput:    m.cfg.ClickHouseOutput,
		TotalEvents:         totalEvents,
		FailedEvents:        failedEvents,
		DeadletterEvents:    m.deadletterEvents.Load(),
		TotalBatches:        totalBatches,
		FailedBatches:       failedBatches,
		ConsumeErrors:       m.consumeErrors.Load(),
		InvalidPayloads:     m.invalidPayloads.Load(),
		LastBatchSize:       int(m.lastBatchSize.Load()),
		LastIndexCount:      int(m.lastIndexCount.Load()),
		LastDurationMS:      m.lastDurationMS.Load(),
		LastRetryCount:      int(m.lastRetryCount.Load()),
		LastDeadletterCount: int(m.lastDeadletterCount.Load()),
		LastWriteAt:         unixString(m.lastWriteUnix.Load()),
		LastErrorAt:         unixString(m.lastErrorUnix.Load()),
		FailureRate:         failureRate,
		EPS:                 eps,
		AverageDurationMS:   avgDuration,
		P95IngestLatencyMS:  m.p95LatencyMS(),
		GeneratedAt:         time.Now(),
	}
}

func (m *writerMetrics) p95LatencyMS() int64 {
	m.latencyMu.Lock()
	defer m.latencyMu.Unlock()
	if len(m.latencies) == 0 {
		return 0
	}
	values := append([]int64(nil), m.latencies...)
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j-1] > values[j]; j-- {
			values[j-1], values[j] = values[j], values[j-1]
		}
	}
	idx := int(float64(len(values))*0.95 + 0.999999)
	if idx < 1 {
		idx = 1
	}
	if idx > len(values) {
		idx = len(values)
	}
	return values[idx-1]
}

func (m *writerMetrics) prometheus(w http.ResponseWriter, r *http.Request) {
	snapshot := m.snapshot()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	lines := []string{
		"xdp_writer_events_total " + strconv.FormatUint(snapshot.TotalEvents, 10),
		"xdp_writer_failed_events_total " + strconv.FormatUint(snapshot.FailedEvents, 10),
		"xdp_writer_deadletter_events_total " + strconv.FormatUint(snapshot.DeadletterEvents, 10),
		"xdp_writer_batches_total " + strconv.FormatUint(snapshot.TotalBatches, 10),
		"xdp_writer_failed_batches_total " + strconv.FormatUint(snapshot.FailedBatches, 10),
		"xdp_writer_consume_errors_total " + strconv.FormatUint(snapshot.ConsumeErrors, 10),
		"xdp_writer_invalid_payloads_total " + strconv.FormatUint(snapshot.InvalidPayloads, 10),
		"xdp_writer_last_batch_size " + strconv.Itoa(snapshot.LastBatchSize),
		"xdp_writer_last_duration_ms " + strconv.FormatInt(snapshot.LastDurationMS, 10),
		"xdp_writer_last_retry_count " + strconv.Itoa(snapshot.LastRetryCount),
		"xdp_writer_last_deadletter_count " + strconv.Itoa(snapshot.LastDeadletterCount),
		"xdp_writer_failure_rate " + strconv.FormatFloat(snapshot.FailureRate, 'f', 6, 64),
		"xdp_writer_eps " + strconv.FormatFloat(snapshot.EPS, 'f', 6, 64),
		"xdp_writer_avg_duration_ms " + strconv.FormatFloat(snapshot.AverageDurationMS, 'f', 6, 64),
		"xdp_writer_p95_ingest_latency_ms " + strconv.FormatInt(snapshot.P95IngestLatencyMS, 10),
	}
	_, _ = w.Write([]byte(strings.Join(lines, "\n") + "\n"))
}

func unixString(value int64) string {
	if value <= 0 {
		return ""
	}
	return time.Unix(value, 0).Format(time.RFC3339)
}

func groupIndexes(events []*event.Event) map[string]struct{} {
	indexes := map[string]struct{}{}
	for _, e := range events {
		index := "app"
		if e != nil {
			if value, ok := e.Metadata["index"].(string); ok && strings.TrimSpace(value) != "" {
				index = strings.TrimSpace(value)
			}
		}
		indexes[index] = struct{}{}
	}
	return indexes
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func writeWithRetry(ctx context.Context, writer plugin.OutputPlugin, events []*event.Event, retryMax int, retryBackoff time.Duration) (int, error) {
	var lastErr error
	for attempt := 0; attempt <= retryMax; attempt++ {
		if attempt > 0 && retryBackoff > 0 {
			time.Sleep(retryBackoff)
		}
		err := writer.Write(ctx, &plugin.EventBatch{PipelineID: "kafka-writer", PipelineVersion: "v1", OutputID: "clickhouse", Events: events})
		if err == nil {
			return attempt, nil
		}
		lastErr = err
		slog.Warn("clickhouse write retry", "attempt", attempt+1, "retry_max", retryMax, "events", len(events), "error", err)
	}
	return retryMax, lastErr
}

func produceDeadletters(ctx context.Context, bus *kafka.Kafka, topic string, events []*event.Event) int {
	count := 0
	for _, e := range events {
		payload, _ := json.Marshal(e)
		if produceErr := bus.Produce(ctx, kafka.Message{Topic: topic, Key: e.EventID, Value: payload}); produceErr != nil {
			slog.Warn("produce writer deadletter failed", "topic", topic, "event_id", e.EventID, "error", produceErr)
			continue
		}
		count++
	}
	return count
}
