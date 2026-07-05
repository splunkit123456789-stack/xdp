#!/usr/bin/env bash
set -euo pipefail
COMPOSE=${COMPOSE:-deployments/docker-compose/docker-compose.yaml}
BASE=${BASE:-http://127.0.0.1:8080}
AGENT=${AGENT:-http://127.0.0.1:8081}
CLICKHOUSE_URL=${CLICKHOUSE_URL:-http://127.0.0.1:8123}
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO_CACHE_DIR="${GOCACHE:-$ROOT_DIR/.cache/go-build}"
GO_MOD_CACHE_DIR="${GOMODCACHE:-$ROOT_DIR/.cache/go-mod}"
GO_PATH_DIR="${GOPATH:-$ROOT_DIR/.cache/go-path}"
HOST_AGENT_BIN="$ROOT_DIR/build/host-bin/xdp-agent"
HOST_AGENT_LOG="$ROOT_DIR/.cache/e2e/agent.log"
HOST_AGENT_PID=""
export BUILDX_CONFIG="${BUILDX_CONFIG:-$ROOT_DIR/.cache/docker-buildx}"
mkdir -p "$BUILDX_CONFIG"

cleanup() {
    if [ -n "$HOST_AGENT_PID" ] && kill -0 "$HOST_AGENT_PID" >/dev/null 2>&1; then
        kill "$HOST_AGENT_PID" >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

printf '== build linux binaries ==\n'
mkdir -p build/docker-bin build/host-bin "$GO_CACHE_DIR" "$GO_MOD_CACHE_DIR" "$GO_PATH_DIR"
rm -f build/docker-bin/xdp-agent
env GOCACHE="$GO_CACHE_DIR" GOMODCACHE="$GO_MOD_CACHE_DIR" GOPATH="$GO_PATH_DIR" CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o build/docker-bin/xdp-api ./cmd/xdp-api
env GOCACHE="$GO_CACHE_DIR" GOMODCACHE="$GO_MOD_CACHE_DIR" GOPATH="$GO_PATH_DIR" CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o build/docker-bin/xdp-worker ./cmd/xdp-worker
env GOCACHE="$GO_CACHE_DIR" GOMODCACHE="$GO_MOD_CACHE_DIR" GOPATH="$GO_PATH_DIR" CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o build/docker-bin/xdp-writer ./cmd/xdp-writer
env GOCACHE="$GO_CACHE_DIR" GOMODCACHE="$GO_MOD_CACHE_DIR" GOPATH="$GO_PATH_DIR" CGO_ENABLED=0 go build -o "$HOST_AGENT_BIN" ./cmd/xdp-agent

printf '== start compose ==\n'
docker compose -f "$COMPOSE" down -v --remove-orphans >/dev/null 2>&1 || true
docker compose -f "$COMPOSE" up -d --build mysql kafka clickhouse

printf '== prepare kafka topics ==\n'
for i in $(seq 1 60); do
    if docker compose -f "$COMPOSE" exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --list >/dev/null 2>&1; then
        break
    fi
    if [ "$i" = 60 ]; then
        docker compose -f "$COMPOSE" logs --tail=80 kafka
        exit 1
    fi
    sleep 1
done
for topic in xdp.raw.http xdp.raw.syslog xdp.output.default; do
    docker compose -f "$COMPOSE" exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --create --if-not-exists --topic "$topic" --partitions 1 --replication-factor 1 >/dev/null
done

docker compose -f "$COMPOSE" up -d --build xdp-api xdp-worker xdp-writer

printf '== wait api ==\n'
python3 - <<'PY'
import time, urllib.request
for _ in range(90):
    try:
        with urllib.request.urlopen('http://127.0.0.1:8080/healthz', timeout=2) as r:
            if r.status == 200:
                print('api ready')
                break
    except Exception:
        time.sleep(1)
else:
    raise SystemExit('api not ready')
PY

printf '== start host agent ==\n'
mkdir -p "$(dirname "$HOST_AGENT_LOG")"
env \
  XDP_AGENT_ADDR=127.0.0.1:8081 \
  XDP_KAFKA_BROKERS=127.0.0.1:9092 \
  XDP_CONFIG_API="$BASE" \
  XDP_CONFIG_RELOAD_INTERVAL=2s \
  "$HOST_AGENT_BIN" >"$HOST_AGENT_LOG" 2>&1 &
HOST_AGENT_PID="$!"

printf '== wait host agent ==\n'
python3 - <<'PY'
import time, urllib.request
for _ in range(90):
    try:
        with urllib.request.urlopen('http://127.0.0.1:8081/healthz', timeout=2) as r:
            if r.status == 200:
                print('host agent ready')
                break
    except Exception:
        time.sleep(1)
else:
    raise SystemExit('host agent not ready')
PY

printf '== migrate clickhouse ==\n'
CLICKHOUSE_USER=xdp CLICKHOUSE_PASSWORD=xdp CLICKHOUSE_URL="$CLICKHOUSE_URL" bash scripts/migrate-clickhouse.sh

printf '== verify mysql metadata ==\n'
python3 - <<'PY'
import urllib.request, json, time
for _ in range(30):
    plugins=json.load(urllib.request.urlopen('http://127.0.0.1:8080/api/v1/plugins'))
    if len(plugins) >= 9:
        print('mysql/plugin api ok', len(plugins))
        break
    time.sleep(1)
else:
    raise SystemExit('plugins not seeded')
PY

printf '== send http json through agent -> kafka -> worker -> writer -> clickhouse ==\n'
E2E_HTTP_TS=$(python3 - <<'PY'
from datetime import datetime, timezone
print(datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace('+00:00', 'Z'))
PY
)
curl -fsS -X POST "$AGENT/ingest" -H 'Content-Type: application/json' -d "{\"time_field\":\"@timestamp\",\"raw\":\"{\\\"@timestamp\\\":\\\"$E2E_HTTP_TS\\\",\\\"level\\\":\\\"info\\\",\\\"msg\\\":\\\"ok\\\",\\\"service\\\":\\\"e2e-http\\\",\\\"bytes\\\":2048}\"}" | python3 -m json.tool

printf '== send udp syslog through agent -> kafka -> worker(regex/geoip/router) -> writer -> clickhouse ==\n'
python3 - <<'PY'
import socket
msg=b'src=1.1.1.1 dst=8.8.8.8 action=deny bytes=4096'
s=socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.sendto(msg, ('127.0.0.1', 5514))
s.close()
print('syslog sent')
PY

printf '== wait clickhouse rows ==\n'
python3 - <<'PY'
import time, urllib.request, urllib.parse, base64
url='http://127.0.0.1:8123/'
def q(sql):
    req=urllib.request.Request(url, data=sql.encode(), method='POST')
    req.add_header('Authorization', 'Basic ' + base64.b64encode(b'xdp:xdp').decode())
    return urllib.request.urlopen(req, timeout=5).read().decode().strip()
for _ in range(60):
    http_count=q("SELECT count() FROM xdp.events_app WHERE event_time >= now() - INTERVAL 10 MINUTE AND event_time <= now() + INTERVAL 10 MINUTE AND position(raw, 'e2e-http') > 0")
    fw_count=q("SELECT count() FROM xdp.events_firewall WHERE position(raw, 'action=deny') > 0")
    if int(http_count or 0) >= 1 and int(fw_count or 0) >= 1:
        print('clickhouse e2e ok', 'http=', http_count, 'firewall=', fw_count)
        break
    time.sleep(1)
else:
    raise SystemExit('clickhouse rows not found')
PY

printf '== verify product api on clickhouse ==\n'
curl -fsS "$BASE/api/v1/indexes" | python3 -m json.tool >/tmp/xdp_e2e_indexes.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_e2e_indexes.json'))
indexes={item.get('index_name') for item in body.get('indexes', [])}
assert {'app','firewall'} <= indexes, body
print('indexes api ok', sorted(indexes))
PY
curl -fsS -X POST "$BASE/api/v1/indexes" -H 'Content-Type: application/json' -d '{"index_name":"audit_e2e","name":"Audit E2E","ttl_days":7,"status":"active"}' | python3 -m json.tool >/tmp/xdp_e2e_index_save.json
curl -fsS "$BASE/api/v1/indexes" | python3 -m json.tool >/tmp/xdp_e2e_indexes_after_save.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_e2e_indexes_after_save.json'))
indexes={item.get('index_name') for item in body.get('indexes', [])}
assert 'audit_e2e' in indexes, body
print('index save api ok', sorted(indexes))
PY
curl -fsS -X DELETE "$BASE/api/v1/indexes?index=audit_e2e&drop_storage=true" | python3 -m json.tool >/tmp/xdp_e2e_index_delete.json
curl -fsS "$BASE/api/v1/search/fields?q=index%3Dapp&limit=100" | python3 -m json.tool >/tmp/xdp_e2e_fields.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_e2e_fields.json'))
fields={item.get('name') for item in body.get('fields', [])}
assert 'service' in fields, body
print('fields api ok', sorted(fields))
PY
curl -fsS "$BASE/api/v1/search/timeline?q=index%3Dapp&interval=hour" | python3 -m json.tool >/tmp/xdp_e2e_timeline.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_e2e_timeline.json'))
assert body.get('buckets'), body
print('timeline api ok', body['buckets'])
PY

printf '== verify productized syslog parser -> index -> hot fields -> spl stats ==\n'
curl -fsS -X POST "$BASE/api/v1/indexes" -H 'Content-Type: application/json' -d '{"index_name":"audit_p0","ttl_days":30,"status":"active"}' | python3 -m json.tool >/tmp/xdp_e2e_audit_index.json
curl -fsS -X POST "$BASE/api/v1/parse-rules" -H 'Content-Type: application/json' -d '{
  "id":"pr_e2e_audit_regex",
  "name":"E2E Audit Regex",
  "status":"active",
  "parser_plugin":"regex",
  "data_source_name":"Firewall Syslog",
  "input_route":"xdp.raw.syslog",
  "output_index":"audit_p0",
  "sourcetype":"syslog",
  "sample_event":"src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048",
  "plugin_config":{"regex_pattern":"src=(?<src_ip>\\S+)\\s+dst=(?<dst_ip>\\S+)\\s+action=(?<action>\\S+)\\s+bytes=(?<bytes>\\d+)"},
  "props_conf":"[source::Firewall Syslog]\nEXTRACT-audit = src=(?<src_ip>\\S+)\\s+dst=(?<dst_ip>\\S+)\\s+action=(?<action>\\S+)\\s+bytes=(?<bytes>\\d+)"
}' | python3 -m json.tool >/tmp/xdp_e2e_parse_rule.json
python3 - <<'PY'
import json
rule=json.load(open('/tmp/xdp_e2e_parse_rule.json'))
fields={item.get('name'): item for item in rule.get('hot_fields', [])}
assert rule.get('output_index') == 'audit_p0', rule
assert {'src_ip','dst_ip','action','bytes'} <= set(fields), rule
assert fields['bytes'].get('type') == 'uint64', rule
assert 'src' in fields['src_ip'].get('aliases', []), rule
print('parse rule api ok', sorted(fields))
PY
python3 - <<'PY'
import json, urllib.request
body=json.load(urllib.request.urlopen('http://127.0.0.1:8080/api/v1/runtime/pipelines'))
for pipe in body.get('pipelines', []):
    if pipe.get('metadata', {}).get('id') == 'firewall-syslog-pipeline':
        stages=[stage.get('plugin') for stage in pipe.get('spec', {}).get('stages', [])]
        outputs=pipe.get('spec', {}).get('outputs', [])
        assert 'props-conf-parser' in stages, pipe
        assert outputs and outputs[0].get('config', {}).get('index') == 'audit_p0', pipe
        print('runtime parser pipeline ok', stages)
        break
else:
    raise SystemExit('firewall runtime pipeline not found')
PY
python3 - <<'PY'
import base64, json, urllib.request
url='http://127.0.0.1:8123/'
req=urllib.request.Request(url, data=b'DESCRIBE TABLE xdp.events_audit_p0 FORMAT JSONEachRow', method='POST')
req.add_header('Authorization', 'Basic ' + base64.b64encode(b'xdp:xdp').decode())
rows=[json.loads(line) for line in urllib.request.urlopen(req, timeout=5).read().decode().splitlines() if line.strip()]
columns={row.get('name'): row for row in rows}
assert {'src_ip','dst_ip','action','bytes'} <= set(columns), columns
assert columns['bytes'].get('type') == 'UInt64', columns['bytes']
assert columns['bytes'].get('default_type') == 'MATERIALIZED', columns['bytes']
print('clickhouse hot fields ok', sorted(name for name in columns if name in {'src_ip','dst_ip','action','bytes'}))
PY
sleep 4
python3 - <<'PY'
import socket
msg=b'src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048'
s=socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.sendto(msg, ('127.0.0.1', 5514))
s.close()
print('audit syslog sent')
PY
python3 - <<'PY'
import base64, json, time, urllib.request
url='http://127.0.0.1:8123/'
raw="src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048"
def q(sql):
    req=urllib.request.Request(url, data=sql.encode(), method='POST')
    req.add_header('Authorization', 'Basic ' + base64.b64encode(b'xdp:xdp').decode())
    return urllib.request.urlopen(req, timeout=5).read().decode().strip()
for _ in range(60):
    count=q("SELECT count() FROM xdp.events_audit_p0 WHERE raw = '" + raw + "'")
    if int(count or 0) >= 1:
        row=json.loads(q("SELECT raw, fields_json, src_ip, dst_ip, action, bytes FROM xdp.events_audit_p0 WHERE raw = '" + raw + "' ORDER BY ingest_time DESC LIMIT 1 FORMAT JSONEachRow"))
        assert row['src_ip'] == '10.0.1.8', row
        assert row['dst_ip'] == '172.16.0.4', row
        assert row['action'] == 'deny', row
        assert int(row['bytes']) == 2048, row
        print('audit clickhouse row ok', row['src_ip'], row['dst_ip'], row['action'], row['bytes'])
        break
    time.sleep(1)
else:
    raise SystemExit('audit_p0 row not found')
PY
curl -fsS --get "$BASE/api/v1/search" --data-urlencode 'q=index=audit_p0 src="10.0.1.8"' --data-urlencode 'earliest=-1h' --data-urlencode 'latest=now' --data-urlencode 'limit=10' | python3 -m json.tool >/tmp/xdp_e2e_audit_search.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_e2e_audit_search.json'))
assert body.get('mode') == 'events', body
assert body.get('pagination', {}).get('returned') >= 1, body
event=body['events'][0]
assert event.get('metadata', {}).get('index') == 'audit_p0', event
assert event.get('fields', {}).get('src_ip') == '10.0.1.8', event
print('spl field search ok', event['fields']['src_ip'])
PY
curl -fsS --get "$BASE/api/v1/search" --data-urlencode 'q=index=audit_p0 | stats count as total sum(bytes) as total_bytes avg(bytes) as avg_bytes by src action' --data-urlencode 'earliest=-1h' --data-urlencode 'latest=now' --data-urlencode 'limit=10' | python3 -m json.tool >/tmp/xdp_e2e_audit_stats.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_e2e_audit_stats.json'))
assert body.get('mode') == 'stats', body
rows=body.get('stats', {}).get('rows', [])
assert rows, body
row=rows[0]
assert row.get('src') == '10.0.1.8', row
assert row.get('action') == 'deny', row
assert int(row.get('total')) >= 1, row
assert int(row.get('total_bytes')) >= 2048, row
assert float(row.get('avg_bytes')) >= 2048, row
print('spl stats ok', row)
PY

printf '== verify datasource persistence and worker hot reload ==\n'
curl -fsS -X POST "$BASE/api/v1/datasources" -H 'Content-Type: application/json' -d '{"id":"http-json","type":"http","name":"HTTP JSON","status":"active","addr":":8081","path":"/ingest","default_index":"hotreload","time_field":"@timestamp","parser":"json","pipeline_id":"mvp-json-pipeline"}' | python3 -m json.tool >/tmp/xdp_e2e_datasource_save.json
sleep 4
HOT_TS=$(python3 - <<'PY'
from datetime import datetime, timezone
print(datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace('+00:00', 'Z'))
PY
)
curl -fsS -X POST "$AGENT/ingest" -H 'Content-Type: application/json' -d "{\"time_field\":\"@timestamp\",\"raw\":\"{\\\"@timestamp\\\":\\\"$HOT_TS\\\",\\\"level\\\":\\\"info\\\",\\\"msg\\\":\\\"hot reload\\\",\\\"service\\\":\\\"e2e-hot\\\",\\\"bytes\\\":64}\"}" | python3 -m json.tool
python3 - <<'PY'
import time, urllib.request, urllib.error, base64
url='http://127.0.0.1:8123/'
def q(sql):
    req=urllib.request.Request(url, data=sql.encode(), method='POST')
    req.add_header('Authorization', 'Basic ' + base64.b64encode(b'xdp:xdp').decode())
    try:
        return urllib.request.urlopen(req, timeout=5).read().decode().strip()
    except urllib.error.HTTPError:
        return '0'
for _ in range(60):
    count=q("SELECT count() FROM xdp.events_hotreload WHERE position(raw, 'e2e-hot') > 0")
    if int(count or 0) >= 1:
        print('hot reload ok', count)
        break
    time.sleep(1)
else:
    raise SystemExit('hot reload rows not found')
PY

printf '== api metrics ==\n'
curl -fsS "$BASE/metrics" | grep -E 'xdp_ingest_events_total|xdp_deadletter_events_total' || true
printf 'Real end-to-end acceptance passed.\n'
