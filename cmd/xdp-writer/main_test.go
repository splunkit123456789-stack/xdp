package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWriterMetricsRuntimeAndPrometheus(t *testing.T) {
	metrics := newWriterMetrics(writerConfig{
		OutputTopic:      "xdp.output.default",
		DeadletterTopic:  "xdp.deadletter.writer",
		BatchSize:        50,
		FlushInterval:    time.Second,
		RetryMax:         3,
		RetryBackoff:     500 * time.Millisecond,
		ClickHouseOutput: "http://127.0.0.1:8123",
	})
	metrics.observeSuccess(40, 2, 120*time.Millisecond, 1)
	metrics.observeFailure(10, 1, 800*time.Millisecond, 3, 10)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/writer/runtime", nil)
	metrics.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("runtime status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var runtime struct {
		Status              string  `json:"status"`
		OutputTopic         string  `json:"output_topic"`
		BatchSize           int     `json:"batch_size"`
		TotalEvents         uint64  `json:"total_events"`
		FailedEvents        uint64  `json:"failed_events"`
		DeadletterEvents    uint64  `json:"deadletter_events"`
		TotalBatches        uint64  `json:"total_batches"`
		FailedBatches       uint64  `json:"failed_batches"`
		LastBatchSize       int     `json:"last_batch_size"`
		LastDurationMS      int64   `json:"last_duration_ms"`
		LastRetryCount      int     `json:"last_retry_count"`
		LastDeadletterCount int     `json:"last_deadletter_count"`
		FailureRate         float64 `json:"failure_rate"`
		EPS                 float64 `json:"eps"`
		P95IngestLatencyMS  int64   `json:"p95_ingest_latency_ms"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&runtime); err != nil {
		t.Fatalf("decode runtime: %v", err)
	}
	if runtime.Status != "degraded" {
		t.Fatalf("status = %q, want degraded", runtime.Status)
	}
	if runtime.OutputTopic != "xdp.output.default" || runtime.BatchSize != 50 {
		t.Fatalf("runtime identity = %#v", runtime)
	}
	if runtime.TotalEvents != 50 || runtime.FailedEvents != 10 || runtime.DeadletterEvents != 10 || runtime.TotalBatches != 2 || runtime.FailedBatches != 1 {
		t.Fatalf("runtime counters = %#v", runtime)
	}
	if runtime.LastBatchSize != 10 || runtime.LastDurationMS != 800 || runtime.LastRetryCount != 3 || runtime.LastDeadletterCount != 10 {
		t.Fatalf("runtime last batch = %#v", runtime)
	}
	if runtime.FailureRate <= 0 {
		t.Fatalf("failure_rate = %f, want > 0", runtime.FailureRate)
	}
	if runtime.EPS <= 0 {
		t.Fatalf("eps = %f, want > 0", runtime.EPS)
	}
	if runtime.P95IngestLatencyMS != 800 {
		t.Fatalf("p95 latency = %d, want 800", runtime.P95IngestLatencyMS)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metrics.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"xdp_writer_events_total 50",
		"xdp_writer_failed_events_total 10",
		"xdp_writer_deadletter_events_total 10",
		"xdp_writer_batches_total 2",
		"xdp_writer_failed_batches_total 1",
		"xdp_writer_last_batch_size 10",
		"xdp_writer_last_duration_ms 800",
		"xdp_writer_p95_ingest_latency_ms 800",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q in:\n%s", want, body)
		}
	}
}
