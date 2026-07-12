#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

COMPOSE_FILE="${COMPOSE_FILE:-deployments/docker-compose/docker-compose.yaml}"
CLICKHOUSE_URL="${CLICKHOUSE_URL:-http://127.0.0.1:8123}"
CLICKHOUSE_USER="${CLICKHOUSE_USER:-xdp}"
CLICKHOUSE_PASSWORD="${CLICKHOUSE_PASSWORD:-xdp}"
KAFKA_TOPIC="${KAFKA_TOPIC:-xdp.output.default}"
RECOVERY_INDEX="${RECOVERY_INDEX:-writer_recovery}"
TOTAL_EVENTS="${TOTAL_EVENTS:-20}"
POLL_TIMEOUT_SECONDS="${POLL_TIMEOUT_SECONDS:-90}"

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
    req.add_header("Authorization", "Basic " + base64.b64encode(f"{user}:{password}".encode()).decode())
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
print(opener.open(req, timeout=20).read().decode().strip())
PY
}

wait_ready() {
  docker compose -f "$COMPOSE_FILE" up -d --no-build clickhouse kafka xdp-writer >/dev/null
  for _ in $(seq 1 90); do
    if curl --noproxy '*' -fsS "$CLICKHOUSE_URL/ping" >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done
  for _ in $(seq 1 90); do
    if docker compose -f "$COMPOSE_FILE" exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server 127.0.0.1:9092 --list >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  printf 'kafka not ready\n' >&2
  exit 1
}

produce_events() {
  local started_at
  started_at="$(date '+%Y-%m-%dT%H:%M:%S%z')"
  python3 - "$TOTAL_EVENTS" "$RECOVERY_INDEX" "$started_at" <<'PY' | \
    docker compose -f "$COMPOSE_FILE" exec -T kafka /opt/kafka/bin/kafka-console-producer.sh \
      --bootstrap-server 127.0.0.1:9092 \
      --topic "$KAFKA_TOPIC" >/dev/null
import json
import sys
from datetime import datetime, timezone

total = int(sys.argv[1])
index = sys.argv[2]
started_at = sys.argv[3]
now = datetime.now(timezone.utc).isoformat()

for i in range(total):
    raw = f"writer recovery seq={i} phase=recovery bytes={256 + i}"
    event = {
        "event_id": f"writer-recovery-{started_at}-{i}",
        "event_time": now,
        "ingest_time": now,
        "pipeline_id": "writer-recovery",
        "pipeline_version": "v1",
        "source": {"type": "benchmark", "name": "writer-recovery", "host": "local", "ip": "127.0.0.1"},
        "metadata": {
            "index": index,
            "sourcetype": "writer-recovery",
            "parse_status": "parsed",
            "parse_rule_id": "writer-recovery",
            "parse_rule_name": "writer-recovery",
            "parse_error": "",
        },
        "raw": raw,
        "fields": {"seq": i, "bytes": 256 + i, "phase": "recovery", "service": "writer"},
        "labels": {},
        "tags": ["writer-recovery"],
        "errors": [],
    }
    print(json.dumps(event, separators=(",", ":")))
PY
}

poll_rows() {
  local expected="$1"
  local start count
  start="$(date +%s)"
  while true; do
    count="$(clickhouse_query "SELECT count() FROM xdp.events_${RECOVERY_INDEX}" 2>/dev/null || printf '0')"
    count="${count:-0}"
    if [ "$count" -ge "$expected" ]; then
      printf 'writer recovery rows: %s\n' "$count"
      return 0
    fi
    if [ $(($(date +%s) - start)) -ge "$POLL_TIMEOUT_SECONDS" ]; then
      printf 'timeout waiting writer recovery rows: got %s want %s\n' "$count" "$expected" >&2
      return 1
    fi
    sleep 1
  done
}

export CLICKHOUSE_URL CLICKHOUSE_USER CLICKHOUSE_PASSWORD
wait_ready
clickhouse_query "DROP TABLE IF EXISTS xdp.events_${RECOVERY_INDEX}" >/dev/null
produce_events
poll_rows "$TOTAL_EVENTS"
