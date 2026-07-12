#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
export BUILDX_CONFIG="${BUILDX_CONFIG:-$ROOT_DIR/.cache/writer-benchmark/docker-buildx}"
export NO_PROXY="${NO_PROXY:-127.0.0.1,localhost}"
export no_proxy="${no_proxy:-127.0.0.1,localhost}"

COMPOSE_FILE="${COMPOSE_FILE:-deployments/docker-compose/docker-compose.yaml}"
CLICKHOUSE_URL="${CLICKHOUSE_URL:-http://127.0.0.1:8123}"
CLICKHOUSE_USER="${CLICKHOUSE_USER:-xdp}"
CLICKHOUSE_PASSWORD="${CLICKHOUSE_PASSWORD:-xdp}"
WRITER_RUNTIME_URL="${WRITER_RUNTIME_URL:-http://127.0.0.1:8082/api/v1/writer/runtime}"
WRITER_RUNTIME_TOKEN="${WRITER_RUNTIME_TOKEN:-}"
KAFKA_TOPIC="${KAFKA_TOPIC:-xdp.output.default}"
BENCH_INDEX="${BENCH_INDEX:-writer_bench}"
TOTAL_EVENTS="${TOTAL_EVENTS:-2000}"
BATCH_SIZES="${BATCH_SIZES:-50 100 500 1000}"
POLL_TIMEOUT_SECONDS="${POLL_TIMEOUT_SECONDS:-90}"
OUTPUT_CSV="${OUTPUT_CSV:-}"
REBUILD_WRITER="${XDP_WRITER_BENCHMARK_REBUILD:-0}"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'required command not found: %s\n' "$1" >&2
    exit 1
  fi
}

clickhouse_query() {
  local sql="$1"
  python3 - "$sql" <<'PY'
import base64
import os
import sys
import urllib.request

url = os.environ.get("CLICKHOUSE_URL", "http://127.0.0.1:8123") + "/"
user = os.environ.get("CLICKHOUSE_USER", "")
password = os.environ.get("CLICKHOUSE_PASSWORD", "")
sql = sys.argv[1]
req = urllib.request.Request(url, data=sql.encode(), method="POST")
if user or password:
    token = base64.b64encode(f"{user}:{password}".encode()).decode()
    req.add_header("Authorization", "Basic " + token)
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
print(opener.open(req, timeout=20).read().decode().strip())
PY
}

runtime_json() {
  python3 - <<'PY'
import os
import urllib.request

url = os.environ.get("WRITER_RUNTIME_URL", "http://127.0.0.1:8082/api/v1/writer/runtime")
token = os.environ.get("WRITER_RUNTIME_TOKEN", "")
req = urllib.request.Request(url)
if token:
    req.add_header("Authorization", "Bearer " + token)
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
with opener.open(req, timeout=10) as resp:
    print(resp.read().decode())
PY
}

json_field() {
  local expr="$1"
  local payload
  payload="$(cat)"
  JSON_PAYLOAD="$payload" python3 - "$expr" <<'PY'
import json
import os
import sys

data = json.loads(os.environ.get("JSON_PAYLOAD") or "{}")
value = data
for part in sys.argv[1].split("."):
    if not part:
        continue
    value = value.get(part) if isinstance(value, dict) else None
print("" if value is None else value)
PY
}

write_compose_override() {
  local batch_size="$1"
  local override_file="$ROOT_DIR/.cache/writer-benchmark/docker-compose.writer-benchmark.yaml"
  mkdir -p "$(dirname "$override_file")"
  cat >"$override_file" <<EOF
services:
  xdp-writer:
    environment:
      XDP_WRITER_BATCH_SIZE: "$batch_size"
      XDP_WRITER_FLUSH_INTERVAL_MS: "1000"
      XDP_WRITER_RETRY_MAX: "3"
      XDP_WRITER_RETRY_BACKOFF_MS: "200"
EOF
  printf '%s' "$override_file"
}

ensure_dependencies() {
  mkdir -p "$BUILDX_CONFIG"
  docker compose -f "$COMPOSE_FILE" up -d --no-build clickhouse kafka >/dev/null
  for _ in $(seq 1 90); do
    if curl --noproxy '*' -fsS "$CLICKHOUSE_URL/ping" >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done
  if ! curl --noproxy '*' -fsS "$CLICKHOUSE_URL/ping" >/dev/null 2>&1; then
    printf 'clickhouse not ready: %s/ping\n' "$CLICKHOUSE_URL" >&2
    docker compose -f "$COMPOSE_FILE" logs --tail=80 clickhouse >&2 || true
    exit 1
  fi
  for _ in $(seq 1 90); do
    if docker compose -f "$COMPOSE_FILE" exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server 127.0.0.1:9092 --list >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  printf 'kafka not ready: 127.0.0.1:9092\n' >&2
  docker compose -f "$COMPOSE_FILE" logs --tail=80 kafka >&2 || true
  exit 1
}

restart_writer() {
  local batch_size="$1"
  local override_file
  override_file="$(write_compose_override "$batch_size")"
  mkdir -p "$BUILDX_CONFIG"
  if [ "$REBUILD_WRITER" = "1" ]; then
    docker compose -f "$COMPOSE_FILE" -f "$override_file" up -d --build xdp-writer >/dev/null
  else
    docker compose -f "$COMPOSE_FILE" -f "$override_file" up -d --no-build xdp-writer >/dev/null
  fi
  for _ in $(seq 1 90); do
    if curl --noproxy '*' -fsS "http://127.0.0.1:8082/healthz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  printf 'xdp-writer not ready after restart\n' >&2
  docker compose -f "$COMPOSE_FILE" logs --tail=80 xdp-writer >&2 || true
  exit 1
}

reset_benchmark_table() {
  clickhouse_query "DROP TABLE IF EXISTS xdp.events_${BENCH_INDEX}" >/dev/null
}

produce_events() {
  local batch_size="$1"
  local started_at
  started_at="$(date '+%Y-%m-%dT%H:%M:%S%z')"
  python3 - "$TOTAL_EVENTS" "$batch_size" "$BENCH_INDEX" "$started_at" <<'PY' | \
    docker compose -f "$COMPOSE_FILE" exec -T kafka /opt/kafka/bin/kafka-console-producer.sh \
      --bootstrap-server 127.0.0.1:9092 \
      --topic "$KAFKA_TOPIC" >/dev/null
import json
import sys
from datetime import datetime, timezone

total = int(sys.argv[1])
batch_size = int(sys.argv[2])
index = sys.argv[3]
started_at = sys.argv[4]
now = datetime.now(timezone.utc).isoformat()

for i in range(total):
    raw = f"writer benchmark seq={i} bytes={128 + (i % 1024)} batch_size={batch_size}"
    event = {
        "event_id": f"writer-benchmark-{batch_size}-{started_at}-{i}",
        "event_time": now,
        "ingest_time": now,
        "pipeline_id": "writer-benchmark",
        "pipeline_version": "v1",
        "source": {
            "type": "benchmark",
            "name": "writer-benchmark",
            "host": "local",
            "ip": "127.0.0.1",
        },
        "metadata": {
            "index": index,
            "sourcetype": "writer-benchmark",
            "parse_status": "parsed",
            "parse_rule_id": "writer-benchmark",
            "parse_rule_name": "writer-benchmark",
            "parse_error": "",
        },
        "raw": raw,
        "fields": {
            "seq": i,
            "bytes": 128 + (i % 1024),
            "batch_size": batch_size,
            "service": "writer",
        },
        "labels": {},
        "tags": ["writer-benchmark"],
        "errors": [],
    }
    print(json.dumps(event, separators=(",", ":")))
PY
}

poll_rows() {
  local expected="$1"
  local start epoch_now count
  start="$(date +%s)"
  while true; do
    count="$(clickhouse_query "SELECT count() FROM xdp.events_${BENCH_INDEX}" 2>/dev/null || printf '0')"
    count="${count:-0}"
    if [ "$count" -ge "$expected" ]; then
      printf '%s' "$count"
      return 0
    fi
    epoch_now="$(date +%s)"
    if [ $((epoch_now - start)) -ge "$POLL_TIMEOUT_SECONDS" ]; then
      printf 'timeout waiting rows: got %s want %s\n' "$count" "$expected" >&2
      return 1
    fi
    sleep 1
  done
}

emit_header() {
  printf 'batch_size,total_events,rows,elapsed_ms,eps,p95_ingest_latency_ms,avg_duration_ms,total_batches,failed_events,deadletter_events,failure_rate\n'
}

run_one() {
  local batch_size="$1"
  local start_ms end_ms elapsed_ms rows runtime eps p95 avg batches failed deadletter failure_rate
  printf '== writer benchmark batch_size=%s total_events=%s ==\n' "$batch_size" "$TOTAL_EVENTS" >&2
  ensure_dependencies
  reset_benchmark_table
  restart_writer "$batch_size"
  start_ms="$(python3 - <<'PY'
import time
print(int(time.time() * 1000))
PY
)"
  produce_events "$batch_size"
  rows="$(poll_rows "$TOTAL_EVENTS")"
  end_ms="$(python3 - <<'PY'
import time
print(int(time.time() * 1000))
PY
)"
  elapsed_ms=$((end_ms - start_ms))
  runtime="$(runtime_json)"
  eps="$(printf '%s' "$runtime" | json_field eps)"
  p95="$(printf '%s' "$runtime" | json_field p95_ingest_latency_ms)"
  avg="$(printf '%s' "$runtime" | json_field avg_duration_ms)"
  batches="$(printf '%s' "$runtime" | json_field total_batches)"
  failed="$(printf '%s' "$runtime" | json_field failed_events)"
  deadletter="$(printf '%s' "$runtime" | json_field deadletter_events)"
  failure_rate="$(printf '%s' "$runtime" | json_field failure_rate)"
  printf '%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n' \
    "$batch_size" "$TOTAL_EVENTS" "$rows" "$elapsed_ms" "$eps" "$p95" "$avg" "$batches" "$failed" "$deadletter" "$failure_rate"
}

require_command docker
require_command curl
require_command python3

export CLICKHOUSE_URL CLICKHOUSE_USER CLICKHOUSE_PASSWORD WRITER_RUNTIME_URL WRITER_RUNTIME_TOKEN

if [ -n "$OUTPUT_CSV" ]; then
  mkdir -p "$(dirname "$OUTPUT_CSV")"
  emit_header >"$OUTPUT_CSV"
  for batch_size in $BATCH_SIZES; do
    run_one "$batch_size" >>"$OUTPUT_CSV"
  done
  printf 'writer benchmark saved: %s\n' "$OUTPUT_CSV"
else
  emit_header
  for batch_size in $BATCH_SIZES; do
    run_one "$batch_size"
  done
fi
