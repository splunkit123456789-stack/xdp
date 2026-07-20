#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

COMPOSE=${COMPOSE:-deployments/docker-compose/docker-compose.yaml}
BASE=${BASE:-http://127.0.0.1:8080}
AUTH_TOKEN=${AUTH_TOKEN:-xdp-e2e-token-$(date +%s)}
AUTH_HEADER=(-H "Authorization: Bearer $AUTH_TOKEN")
export AUTH_TOKEN
export NO_PROXY="127.0.0.1,localhost,::1,${NO_PROXY:-}"
export no_proxy="127.0.0.1,localhost,::1,${no_proxy:-}"
AGENT_ADDR=${AGENT_ADDR:-127.0.0.1:18081}
AGENT=${AGENT:-http://$AGENT_ADDR}
AGENT_ADDR_EXPLICIT=${AGENT_ADDR_EXPLICIT:-}
AGENT_EXPLICIT=${AGENT_EXPLICIT:-}
AUDIT_SYSLOG_PORT=${AUDIT_SYSLOG_PORT:-15514}
HOT_SYSLOG_PORT=${HOT_SYSLOG_PORT:-15515}
CLICKHOUSE_URL=${CLICKHOUSE_URL:-http://127.0.0.1:8123}
GO_CACHE_DIR="${GOCACHE:-$ROOT_DIR/.cache/go-build}"
GO_MOD_CACHE_DIR="${GOMODCACHE:-$ROOT_DIR/.cache/go-mod}"
GO_PATH_DIR="${GOPATH:-$ROOT_DIR/.cache/go-path}"
HOST_AGENT_BIN="$ROOT_DIR/build/host-bin/xdp-agent"
HOST_AGENT_LOG="$ROOT_DIR/.cache/e2e/agent.log"
E2E_COMPOSE_OVERRIDE="$ROOT_DIR/.cache/e2e/docker-compose.auth.yaml"
E2E_TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/xdp-real-e2e.XXXXXX")"
HOST_AGENT_PID=""
TEST_CASE_ID=TC-P1-E2E-001
export BUILDX_CONFIG="${BUILDX_CONFIG:-$ROOT_DIR/.cache/docker-buildx}"
export E2E_TMP_DIR
mkdir -p "$BUILDX_CONFIG"

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || {
        printf 'missing required command: %s\n' "$1" >&2
        exit 4
    }
}

require_cmd curl
require_cmd docker
require_cmd go
require_cmd lsof
require_cmd python3
require_cmd zip

api_curl() {
    curl --noproxy '*' -fsS "${AUTH_HEADER[@]}" "$@"
}

cleanup() {
    if [ -n "$HOST_AGENT_PID" ] && kill -0 "$HOST_AGENT_PID" >/dev/null 2>&1; then
        kill "$HOST_AGENT_PID" >/dev/null 2>&1 || true
    fi
    rm -rf "$E2E_TMP_DIR"
}
on_error() {
    printf 'FAIL %s real end-to-end acceptance failed\n' "$TEST_CASE_ID" >&2
}
trap cleanup EXIT
trap on_error ERR

find_free_tcp_port() {
    python3 - <<'PY'
import socket
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
}

stop_stale_host_agent() {
    local agent_port pids pid pidfile i
    agent_port="${AGENT_ADDR##*:}"
    pids="$(
        {
            for pidfile in "$ROOT_DIR/.cache/xdp-oneclick/agent.pid" "$ROOT_DIR/.cache/e2e/agent.pid"; do
                if [ -f "$pidfile" ]; then
                    cat "$pidfile" 2>/dev/null || true
                fi
            done
            lsof -nP -iTCP -sTCP:LISTEN 2>/dev/null | awk '$1 == "xdp-agent" {print $2}' || true
            lsof -nP -iUDP 2>/dev/null | awk '$1 == "xdp-agent" {print $2}' || true
            lsof -tiTCP:"$agent_port" -sTCP:LISTEN 2>/dev/null || true
            lsof -tiUDP:"$AUDIT_SYSLOG_PORT" 2>/dev/null || true
            lsof -tiUDP:"$HOT_SYSLOG_PORT" 2>/dev/null || true
        } | sort -u
    )"
    for pid in $pids; do
        kill "$pid" >/dev/null 2>&1 || true
    done
    rm -f "$ROOT_DIR/.cache/xdp-oneclick/agent.pid" "$ROOT_DIR/.cache/e2e/agent.pid"
    for i in $(seq 1 20); do
        if ! lsof -tiTCP:"$agent_port" -sTCP:LISTEN >/dev/null 2>&1 && \
           ! lsof -nP -iTCP -sTCP:LISTEN 2>/dev/null | awk '$1 == "xdp-agent" {found=1} END {exit found ? 0 : 1}'; then
            break
        fi
        sleep 0.2
    done
    pids="$(
        {
            lsof -nP -iTCP -sTCP:LISTEN 2>/dev/null | awk '$1 == "xdp-agent" {print $2}' || true
            lsof -nP -iUDP 2>/dev/null | awk '$1 == "xdp-agent" {print $2}' || true
            lsof -tiTCP:"$agent_port" -sTCP:LISTEN 2>/dev/null || true
        } | sort -u
    )"
    if [ -n "$pids" ]; then
        # shellcheck disable=SC2086
        kill -9 $pids >/dev/null 2>&1 || true
        sleep 0.5
    fi
    if lsof -tiTCP:"$agent_port" -sTCP:LISTEN >/dev/null 2>&1 || \
       lsof -nP -iTCP -sTCP:LISTEN 2>/dev/null | awk '$1 == "xdp-agent" {found=1} END {exit found ? 0 : 1}'; then
        if [ -n "$AGENT_ADDR_EXPLICIT" ] || [ -n "$AGENT_EXPLICIT" ]; then
            printf 'host agent port still in use: %s\n' "$agent_port" >&2
            lsof -nP -iTCP:"$agent_port" -sTCP:LISTEN >&2 || true
            exit 1
        fi
        agent_port="$(find_free_tcp_port)"
        AGENT_ADDR="127.0.0.1:$agent_port"
        AGENT="http://$AGENT_ADDR"
        printf 'host agent port in use; switched to %s\n' "$AGENT_ADDR"
    fi
}

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
for topic in xdp.raw.syslog xdp.output.default; do
    docker compose -f "$COMPOSE" exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --create --if-not-exists --topic "$topic" --partitions 1 --replication-factor 1 >/dev/null
done

printf '== reset test data ==\n'
COMPOSE_FILE="$COMPOSE" CLICKHOUSE_USER=xdp CLICKHOUSE_PASSWORD=xdp CLICKHOUSE_URL="$CLICKHOUSE_URL" bash scripts/reset-test-env.sh

mkdir -p "$(dirname "$E2E_COMPOSE_OVERRIDE")"
cat >"$E2E_COMPOSE_OVERRIDE" <<EOF
services:
  xdp-api:
    environment:
      XDP_AUTH_ENABLED: "true"
      XDP_AUTH_USERNAME: "admin"
      XDP_AUTH_PASSWORD: "xdp"
      XDP_API_TOKEN: "$AUTH_TOKEN"
  xdp-worker:
    environment:
      XDP_API_TOKEN: "$AUTH_TOKEN"
EOF
docker compose -f "$COMPOSE" -f "$E2E_COMPOSE_OVERRIDE" up -d --build xdp-api xdp-worker xdp-writer

printf '== wait api ==\n'
python3 - <<'PY'
import time, urllib.request
for _ in range(90):
    try:
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        with opener.open('http://127.0.0.1:8080/healthz', timeout=2) as r:
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
stop_stale_host_agent
env \
  XDP_AGENT_ADDR="$AGENT_ADDR" \
  XDP_KAFKA_BROKERS=127.0.0.1:9092 \
  XDP_CONFIG_API="$BASE" \
  XDP_CONFIG_API_TOKEN="$AUTH_TOKEN" \
  XDP_API_TOKEN="$AUTH_TOKEN" \
  XDP_CONFIG_RELOAD_INTERVAL=2s \
  "$HOST_AGENT_BIN" >"$HOST_AGENT_LOG" 2>&1 &
HOST_AGENT_PID="$!"
sleep 1
if ! kill -0 "$HOST_AGENT_PID" >/dev/null 2>&1; then
    cat "$HOST_AGENT_LOG" >&2 || true
    exit 1
fi

printf '== wait host agent ==\n'
AGENT="$AGENT" python3 - <<'PY'
import os, time, urllib.request
url=os.environ["AGENT"].rstrip("/") + "/healthz"
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
for _ in range(90):
    try:
        with opener.open(url, timeout=2) as r:
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
import os, urllib.request, json, time
def api_request(url, data=None, method=None):
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header('Authorization', 'Bearer ' + os.environ['AUTH_TOKEN'])
    return req
def plugin_codes(plugin_type):
    body=json.load(urllib.request.urlopen(api_request(f'http://127.0.0.1:8080/api/v1/plugins?plugin_type={plugin_type}&page_size=100'), timeout=10))
    items = body.get('plugins', body if isinstance(body, list) else [])
    return {item.get('plugin_code') or item.get('code') for item in items}
for _ in range(30):
    codes = plugin_codes('input') | plugin_codes('parser') | plugin_codes('search_command')
    if {'syslog', 'regex', 'stats'} <= codes and not ({'http-input', 'json-parser'} & codes):
        print('mysql/plugin api ok', sorted(codes))
        break
    time.sleep(1)
else:
    raise SystemExit('plugins not seeded')
PY

printf '== verify P1 external plugin management lifecycle ==\n'
PLUGIN_PACKAGE_DIR="$ROOT_DIR/build/plugin-packages"
mkdir -p "$PLUGIN_PACKAGE_DIR"
rm -f "$PLUGIN_PACKAGE_DIR/kafka-input-sample.zip" "$PLUGIN_PACKAGE_DIR/json-parser-sample.zip" "$PLUGIN_PACKAGE_DIR/json-parser-sample-1.1.0.zip" "$PLUGIN_PACKAGE_DIR/invalid-plugin.zip"
(cd "$ROOT_DIR/docs/plugins/kafka-input-sample" && zip -qr "$PLUGIN_PACKAGE_DIR/kafka-input-sample.zip" manifest.json README.md)
(cd "$ROOT_DIR/docs/plugins/json-parser-sample" && zip -qr "$PLUGIN_PACKAGE_DIR/json-parser-sample.zip" manifest.json README.md examples)
(cd "$ROOT_DIR/docs/plugins/table-search-command-sample" && zip -qr "$PLUGIN_PACKAGE_DIR/table-search-command-sample.zip" manifest.json README.md bin)
(cd "$ROOT_DIR/docs/plugins/sort-search-command-sample" && zip -qr "$PLUGIN_PACKAGE_DIR/sort-search-command-sample.zip" manifest.json README.md bin)
(cd "$ROOT_DIR/docs/plugins/head-search-command-sample" && zip -qr "$PLUGIN_PACKAGE_DIR/head-search-command-sample.zip" manifest.json README.md bin)
(cd "$ROOT_DIR/docs/plugins/dedup-search-command-sample" && zip -qr "$PLUGIN_PACKAGE_DIR/dedup-search-command-sample.zip" manifest.json README.md bin)
python3 - <<'PY'
import json, zipfile
manifest = json.load(open('docs/plugins/json-parser-sample/manifest.json'))
manifest['plugin_version'] = '1.1.0'
manifest['description'] = manifest.get('description', '') + ' Upgraded package for single-version lifecycle acceptance.'
with zipfile.ZipFile('build/plugin-packages/json-parser-sample-1.1.0.zip', 'w', zipfile.ZIP_DEFLATED) as zf:
    zf.writestr('manifest.json', json.dumps(manifest, ensure_ascii=False, separators=(',', ':')))
    zf.writestr('README.md', '# JSON Parser 1.1.0\n')
with zipfile.ZipFile('build/plugin-packages/invalid-plugin.zip', 'w', zipfile.ZIP_DEFLATED) as zf:
    zf.writestr('README.md', 'missing manifest')
PY
python3 - <<'PY'
import json, os, urllib.error, urllib.request
def post_zip(path, plugin_type):
    data=open(path,'rb').read()
    req=urllib.request.Request('http://127.0.0.1:8080/api/v1/plugins/import?plugin_type='+plugin_type, data=data, method='POST')
    req.add_header('Authorization', 'Bearer ' + os.environ['AUTH_TOKEN'])
    req.add_header('Content-Type','application/zip')
    return urllib.request.urlopen(req, timeout=10)
def expect_error(path, plugin_type, status, code):
    try:
        post_zip(path, plugin_type)
    except urllib.error.HTTPError as exc:
        body=json.loads(exc.read().decode())
        assert exc.code == status, (exc.code, body)
        assert body.get('error', {}).get('code') == code, body
        print('plugin error ok', code)
        return body
    raise SystemExit(f'{code} unexpectedly succeeded')
expect_error('build/plugin-packages/invalid-plugin.zip', 'input', 400, 'PLUGIN_MANIFEST_MISSING')
for path, ptype, code in [
    ('build/plugin-packages/kafka-input-sample.zip', 'input', 'kafka'),
    ('build/plugin-packages/json-parser-sample.zip', 'parser', 'json-parser'),
    ('build/plugin-packages/table-search-command-sample.zip', 'search_command', 'table'),
    ('build/plugin-packages/sort-search-command-sample.zip', 'search_command', 'sort'),
    ('build/plugin-packages/head-search-command-sample.zip', 'search_command', 'head'),
    ('build/plugin-packages/dedup-search-command-sample.zip', 'search_command', 'dedup'),
]:
    body=json.load(post_zip(path, ptype))
    assert body['plugin_code'] == code and body['plugin_type'] == ptype and body['status'] == 'disabled', body
    print('plugin import ok', code, body['plugin_version'])
expect_error('build/plugin-packages/json-parser-sample.zip', 'parser', 409, 'PLUGIN_ALREADY_EXISTS')
body=json.load(post_zip('build/plugin-packages/json-parser-sample-1.1.0.zip', 'parser'))
assert body['plugin_code'] == 'json-parser' and body['plugin_version'] == '1.1.0' and body['status'] == 'disabled', body
print('plugin upgrade ok', body['plugin_code'], body['plugin_version'])
for ptype, code in [('input','kafka'), ('parser','json-parser'), ('search_command','table'), ('search_command','sort'), ('search_command','head'), ('search_command','dedup')]:
    req=urllib.request.Request(f'http://127.0.0.1:8080/api/v1/plugins/{code}/enable?plugin_type={ptype}', data=b'', method='POST')
    req.add_header('Authorization', 'Bearer ' + os.environ['AUTH_TOKEN'])
    body=json.load(urllib.request.urlopen(req, timeout=10))
    assert body['plugin_code'] == code and body['status'] == 'enabled', body
    print('plugin enable ok', code)
for ptype, code, version in [('input','kafka','1.0.0'), ('parser','json-parser','1.1.0'), ('search_command','table','1.0.0'), ('search_command','sort','1.0.0'), ('search_command','head','1.0.0'), ('search_command','dedup','1.0.0')]:
    req=urllib.request.Request(f'http://127.0.0.1:8080/api/v1/plugins/{code}?plugin_type={ptype}')
    req.add_header('Authorization', 'Bearer ' + os.environ['AUTH_TOKEN'])
    body=json.load(urllib.request.urlopen(req, timeout=10))
    assert body['plugin_code'] == code and body['plugin_version'] == version and body['status'] == 'enabled', body
    if ptype == 'search_command':
        assert body.get('runtime') == 'executable_search_command', body
        assert body.get('entrypoint', '').startswith('bin/'), body
    assert body.get('references', {}).get('count') == 0, body
    print('plugin detail ok', code, version)
req=urllib.request.Request('http://127.0.0.1:8080/api/v1/plugins/catalog?plugin_type=input&status=enabled')
req.add_header('Authorization', 'Bearer ' + os.environ['AUTH_TOKEN'])
body=json.load(urllib.request.urlopen(req, timeout=10))
assert any(item.get('plugin_code') == 'kafka' for item in body.get('plugins', [])), body
req=urllib.request.Request('http://127.0.0.1:8080/api/v1/plugins/catalog?plugin_type=parser&status=enabled')
req.add_header('Authorization', 'Bearer ' + os.environ['AUTH_TOKEN'])
body=json.load(urllib.request.urlopen(req, timeout=10))
assert any(item.get('plugin_code') == 'json-parser' and item.get('plugin_version') == '1.1.0' for item in body.get('plugins', [])), body
req=urllib.request.Request('http://127.0.0.1:8080/api/v1/plugins/catalog?plugin_type=search_command&status=enabled')
req.add_header('Authorization', 'Bearer ' + os.environ['AUTH_TOKEN'])
body=json.load(urllib.request.urlopen(req, timeout=10))
codes={item.get('plugin_code') for item in body.get('plugins', [])}
assert {'table','sort','head','dedup'}.issubset(codes), body
print('plugin catalog ok')
PY

printf '== verify product api on clickhouse ==\n'
api_curl "$BASE/api/v1/indexes" | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_indexes.json"
python3 - <<'PY'
import json, os
body=json.load(open(os.path.join(os.environ['E2E_TMP_DIR'], 'xdp_e2e_indexes.json')))
indexes={item.get('index_name') for item in body.get('indexes', [])}
assert 'app' not in indexes, body
assert 'firewall' not in indexes, body
print('indexes api ok', sorted(indexes))
PY
api_curl -X POST "$BASE/api/v1/indexes" -H 'Content-Type: application/json' -d '{"index_name":"audit_e2e","name":"Audit E2E","ttl_days":7,"status":"active"}' | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_index_save.json"
api_curl "$BASE/api/v1/indexes" | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_indexes_after_save.json"
python3 - <<'PY'
import json, os
body=json.load(open(os.path.join(os.environ['E2E_TMP_DIR'], 'xdp_e2e_indexes_after_save.json')))
indexes={item.get('index_name') for item in body.get('indexes', [])}
assert 'audit_e2e' in indexes, body
print('index save api ok', sorted(indexes))
PY
api_curl -X DELETE "$BASE/api/v1/indexes?index=audit_e2e&drop_storage=true" | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_index_delete.json"

printf '== verify productized syslog parser -> index -> hot fields -> spl stats ==\n'
api_curl -X POST "$BASE/api/v1/indexes" -H 'Content-Type: application/json' -d '{"index_name":"audit_p0","ttl_days":30,"status":"active"}' | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_audit_index.json"
cat >"$E2E_TMP_DIR/xdp_e2e_syslog_datasource_payload.json" <<JSON
{
  "id":"e2e-syslog-source",
  "name":"E2E Syslog Source",
  "plugin_code":"syslog",
  "status":"active",
  "plugin_config":{
    "collector_port":${AUDIT_SYSLOG_PORT},
    "transport_protocol":"UDP",
    "encoding":"UTF-8",
    "log_filter_enabled":false
  }
}
JSON
api_curl -X POST "$BASE/api/v1/datasources" -H 'Content-Type: application/json' -d @"$E2E_TMP_DIR/xdp_e2e_syslog_datasource_payload.json" | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_syslog_datasource.json"
api_curl -X POST "$BASE/api/v1/parse-rules" -H 'Content-Type: application/json' -d '{
  "id":"pr_e2e_audit_regex",
  "name":"E2E Audit Regex",
  "status":"active",
  "parser_plugin":"regex",
  "data_source_name":"E2E Syslog Source",
  "input_route":"raw.ds_e2e_syslog_source",
  "output_index":"audit_p0",
  "sample_event":"src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048",
  "plugin_config":{"regex_pattern":"src=(?<src_ip>\\S+)\\s+dst=(?<dst_ip>\\S+)\\s+action=(?<action>\\S+)\\s+bytes=(?<bytes>\\d+)"},
  "props_conf":"[source::E2E Syslog Source]\nEXTRACT-audit = src=(?<src_ip>\\S+)\\s+dst=(?<dst_ip>\\S+)\\s+action=(?<action>\\S+)\\s+bytes=(?<bytes>\\d+)"
}' | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_parse_rule.json"
python3 - <<'PY'
import json, os
rule=json.load(open(os.path.join(os.environ['E2E_TMP_DIR'], 'xdp_e2e_parse_rule.json')))
fields={item.get('name'): item for item in rule.get('hot_fields', [])}
assert rule.get('output_index') == 'audit_p0', rule
assert {'src_ip','dst_ip','action','bytes'} <= set(fields), rule
assert fields['bytes'].get('type') == 'uint64', rule
assert 'src' in fields['src_ip'].get('aliases', []), rule
print('parse rule api ok', sorted(fields))
PY
python3 - <<'PY'
import json, os, urllib.request
req=urllib.request.Request('http://127.0.0.1:8080/api/v1/runtime/pipelines')
req.add_header('Authorization', 'Bearer ' + os.environ['AUTH_TOKEN'])
body=json.load(urllib.request.urlopen(req, timeout=10))
for pipe in body.get('pipelines', []):
    if pipe.get('metadata', {}).get('id') == 'pipe_e2e_syslog_source':
        stages=[stage.get('plugin') for stage in pipe.get('spec', {}).get('stages', [])]
        child_plugins=[]
        for stage in pipe.get('spec', {}).get('stages', []):
            child_plugins.extend(child.get('plugin') for child in stage.get('stages', []))
        outputs=pipe.get('spec', {}).get('outputs', [])
        assert 'regex' in child_plugins, pipe
        assert outputs and outputs[0].get('config', {}).get('index') == '${metadata.index}', pipe
        print('runtime parser pipeline ok', stages, child_plugins)
        break
else:
    raise SystemExit('syslog runtime pipeline not found')
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
for name in ['src_ip', 'dst_ip', 'action', 'bytes']:
    assert columns[name].get('default_type') != 'MATERIALIZED', columns[name]
print('clickhouse hot fields ok', sorted(name for name in columns if name in {'src_ip','dst_ip','action','bytes'}))
PY
sleep 4
AUDIT_SYSLOG_PORT="$AUDIT_SYSLOG_PORT" python3 - <<'PY'
import os, socket
messages=[
    b'src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048',
    b'src=10.0.1.8 dst=172.16.0.5 action=deny bytes=4096',
    b'src=10.0.1.9 dst=172.16.0.6 action=allow bytes=1024',
]
s=socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
try:
    for msg in messages:
        s.sendto(msg, ('127.0.0.1', int(os.environ["AUDIT_SYSLOG_PORT"])))
finally:
    s.close()
print('audit syslog sent', len(messages))
PY
AUDIT_SYSLOG_PORT="$AUDIT_SYSLOG_PORT" python3 - <<'PY'
import base64, json, os, socket, time, urllib.request
url='http://127.0.0.1:8123/'
raw="src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048"
messages=[
    b'src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048',
    b'src=10.0.1.8 dst=172.16.0.5 action=deny bytes=4096',
    b'src=10.0.1.9 dst=172.16.0.6 action=allow bytes=1024',
]
def q(sql):
    req=urllib.request.Request(url, data=sql.encode(), method='POST')
    req.add_header('Authorization', 'Basic ' + base64.b64encode(b'xdp:xdp').decode())
    return urllib.request.urlopen(req, timeout=5).read().decode().strip()
def resend():
    s=socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        for msg in messages:
            s.sendto(msg, ('127.0.0.1', int(os.environ["AUDIT_SYSLOG_PORT"])))
    finally:
        s.close()
for attempt in range(120):
    if attempt < 20:
        resend()
    count=q("SELECT count() FROM xdp.events_audit_p0 WHERE raw = '" + raw + "'")
    if int(count or 0) >= 1:
        row=json.loads(q("SELECT raw, fields_json, src_ip, dst_ip, action, bytes FROM xdp.events_audit_p0 WHERE raw = '" + raw + "' ORDER BY ingest_time DESC LIMIT 1 FORMAT JSONEachRow"))
        assert row['src_ip'] == '10.0.1.8', row
        assert row['dst_ip'] == '172.16.0.4', row
        assert row['action'] == 'deny', row
        assert int(row['bytes']) == 2048, row
        print('audit clickhouse row ok', row['src_ip'], row['dst_ip'], row['action'], row['bytes'])
        break
    time.sleep(2)
else:
    raise SystemExit('audit_p0 row not found')
for attempt in range(120):
    total=q("SELECT count() FROM xdp.events_audit_p0 WHERE raw IN ('src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048','src=10.0.1.8 dst=172.16.0.5 action=deny bytes=4096','src=10.0.1.9 dst=172.16.0.6 action=allow bytes=1024')")
    if int(total or 0) >= 3:
        print('audit clickhouse test rows ok', total)
        break
    if attempt < 20:
        resend()
    time.sleep(2)
else:
    raise SystemExit('audit_p0 test rows not complete')
PY
api_curl --get "$BASE/api/v1/search" --data-urlencode 'q=index=audit_p0 src="10.0.1.8"' --data-urlencode 'earliest=-1h' --data-urlencode 'latest=now' --data-urlencode 'limit=10' | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_audit_search.json"
python3 - <<'PY'
import json, os
body=json.load(open(os.path.join(os.environ['E2E_TMP_DIR'], 'xdp_e2e_audit_search.json')))
assert body.get('mode') == 'events', body
assert body.get('pagination', {}).get('returned') >= 1, body
event=body['events'][0]
assert event.get('metadata', {}).get('index') == 'audit_p0', event
assert event.get('fields', {}).get('src_ip') == '10.0.1.8', event
print('spl field search ok', event['fields']['src_ip'])
PY
api_curl "$BASE/api/v1/search/fields?q=index%3Daudit_p0&limit=100" | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_fields.json"
python3 - <<'PY'
import json, os
body=json.load(open(os.path.join(os.environ['E2E_TMP_DIR'], 'xdp_e2e_fields.json')))
fields={item.get('name') for item in body.get('fields', [])}
assert {'src_ip','dst_ip','action','bytes'} <= fields, body
print('fields api ok', sorted(fields))
PY
api_curl --get "$BASE/api/v1/search/timeline" --data-urlencode 'q=index=audit_p0' --data-urlencode 'earliest=-1h' --data-urlencode 'latest=now' --data-urlencode 'interval=minute' | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_timeline.json"
python3 - <<'PY'
import json, os
body=json.load(open(os.path.join(os.environ['E2E_TMP_DIR'], 'xdp_e2e_timeline.json')))
assert body.get('buckets'), body
assert sum(int(item.get('count') or 0) for item in body['buckets']) >= 1, body
print('timeline api ok', body['interval'], len(body['buckets']))
PY
api_curl --get "$BASE/api/v1/search" --data-urlencode 'q=index=audit_p0 | stats count as total sum(bytes) as total_bytes avg(bytes) as avg_bytes by src action' --data-urlencode 'earliest=-1h' --data-urlencode 'latest=now' --data-urlencode 'limit=10' | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_audit_stats.json"
python3 - <<'PY'
import json, os
body=json.load(open(os.path.join(os.environ['E2E_TMP_DIR'], 'xdp_e2e_audit_stats.json')))
assert body.get('mode') == 'stats', body
rows=body.get('stats', {}).get('rows', [])
assert rows, body
row=next((item for item in rows if item.get('src') == '10.0.1.8' and item.get('action') == 'deny'), None)
assert row, rows
assert row.get('src') == '10.0.1.8', row
assert row.get('action') == 'deny', row
assert int(row.get('total')) >= 1, row
assert int(row.get('total_bytes')) >= 2048, row
assert float(row.get('avg_bytes')) >= 2048, row
print('spl stats ok', row)
PY

printf '== verify P1 search command plugins table/sort/head/dedup ==\n'
api_curl --get "$BASE/api/v1/search" --data-urlencode 'q=index=audit_p0 | table _time src_ip action bytes | sort - bytes | head 2' --data-urlencode 'earliest=-1h' --data-urlencode 'latest=now' --data-urlencode 'limit=10' | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_p1_table_sort_head.json"
python3 - <<'PY'
import json, os
body=json.load(open(os.path.join(os.environ['E2E_TMP_DIR'], 'xdp_e2e_p1_table_sort_head.json')))
assert body.get('mode') == 'table', body
assert body.get('search_command', {}).get('plugin_code') == 'table', body
assert body.get('search_command', {}).get('plugin_type') == 'search_command', body
table=body.get('table', {})
assert table.get('fields') == ['_time','src_ip','action','bytes'], table
rows=table.get('rows', [])
assert len(rows) >= 2, body
assert int(rows[0].get('bytes')) >= int(rows[1].get('bytes')), rows
assert rows[0].get('src_ip') == '10.0.1.8', rows
print('p1 table/sort/head ok', rows[:2])
PY
api_curl --get "$BASE/api/v1/search" --data-urlencode 'q=index=audit_p0 | sort - bytes | dedup src_ip action | table src_ip action bytes' --data-urlencode 'earliest=-1h' --data-urlencode 'latest=now' --data-urlencode 'limit=10' | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_p1_dedup.json"
python3 - <<'PY'
import json, os
body=json.load(open(os.path.join(os.environ['E2E_TMP_DIR'], 'xdp_e2e_p1_dedup.json')))
assert body.get('mode') == 'table', body
table=body.get('table', {})
assert table.get('fields') == ['src_ip','action','bytes'], table
rows=table.get('rows', [])
keys={(row.get('src_ip'), row.get('action')) for row in rows}
assert ('10.0.1.8','deny') in keys, rows
row_108=next(row for row in rows if row.get('src_ip') == '10.0.1.8' and row.get('action') == 'deny')
assert int(row_108.get('bytes')) == 4096, rows
print('p1 dedup ok', rows)
PY
api_curl --get "$BASE/api/v1/search" --data-urlencode 'q=index=audit_p0 | stats count as total sum(bytes) as total_bytes by src action | sort - total_bytes | head 1 | table src action total_bytes' --data-urlencode 'earliest=-1h' --data-urlencode 'latest=now' --data-urlencode 'limit=10' | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_p1_stats_pipe.json"
python3 - <<'PY'
import json, os
body=json.load(open(os.path.join(os.environ['E2E_TMP_DIR'], 'xdp_e2e_p1_stats_pipe.json')))
assert body.get('mode') == 'table', body
table=body.get('table', {})
assert table.get('fields') == ['src','action','total_bytes'], table
rows=table.get('rows', [])
assert len(rows) == 1, body
assert rows[0].get('src') == '10.0.1.8', rows
assert rows[0].get('action') == 'deny', rows
assert int(rows[0].get('total_bytes')) >= 6144, rows
print('p1 stats pipeline ok', rows[0])
PY
python3 - <<'PY'
import json, os, urllib.error, urllib.parse, urllib.request
params=urllib.parse.urlencode({'q':'index=audit_p0 | head 0'})
try:
    req=urllib.request.Request('http://127.0.0.1:8080/api/v1/search?' + params)
    req.add_header('Authorization', 'Bearer ' + os.environ['AUTH_TOKEN'])
    urllib.request.urlopen(req, timeout=5)
except urllib.error.HTTPError as exc:
    body=json.loads(exc.read().decode())
    assert exc.code == 400, exc.code
    assert body.get('error', {}).get('code') == 'SPL_COMMAND_VALIDATION_ERROR', body
    print('p1 command validation ok', body['error']['code'])
else:
    raise SystemExit('invalid head command unexpectedly succeeded')
PY

printf '== verify P1 search command runtime recovery after api restart ==\n'
docker compose -f "$COMPOSE" -f "$E2E_COMPOSE_OVERRIDE" restart xdp-api >/dev/null
python3 - <<'PY'
import time, urllib.request
for _ in range(90):
    try:
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        with opener.open('http://127.0.0.1:8080/healthz', timeout=2) as r:
            if r.status == 200:
                print('api ready after restart')
                break
    except Exception:
        time.sleep(1)
else:
    raise SystemExit('api not ready after restart')
PY
api_curl --get "$BASE/api/v1/search" --data-urlencode 'q=index=audit_p0 | table src action bytes | sort - bytes | head 10' --data-urlencode 'earliest=-1h' --data-urlencode 'latest=now' --data-urlencode 'limit=10' | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_p1_restart_recovery.json"
python3 - <<'PY'
import json, os
body=json.load(open(os.path.join(os.environ['E2E_TMP_DIR'], 'xdp_e2e_p1_restart_recovery.json')))
assert body.get('mode') == 'table', body
assert body.get('search_command', {}).get('plugin_code') == 'table', body
table=body.get('table', {})
assert table.get('fields') == ['src','action','bytes'], table
rows=table.get('rows', [])
assert rows, body
assert rows[0].get('src') == '10.0.1.8', rows
assert rows[0].get('action') == 'deny', rows
assert int(rows[0].get('bytes')) >= 2048, rows
print('p1 runtime recovery after api restart ok', rows[0])
PY

printf '== verify P1 kafka input plugin + json parser plugin end-to-end ==\n'
docker compose -f "$COMPOSE" exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --create --if-not-exists --topic xdp.e2e.json --partitions 1 --replication-factor 1 >/dev/null
api_curl -X POST "$BASE/api/v1/indexes" -H 'Content-Type: application/json' -d '{"index_name":"json_p1","ttl_days":30,"status":"active"}' | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_json_index.json"
cat >"$E2E_TMP_DIR/xdp_e2e_kafka_datasource_payload.json" <<JSON
{
  "id":"e2e-kafka-json-source",
  "name":"E2E Kafka JSON Source",
  "plugin_code":"kafka",
  "status":"active",
  "plugin_config":{
    "brokers":["127.0.0.1:9092"],
    "topic":"xdp.e2e.json",
    "consumer_group":"xdp-e2e-json",
    "start_offset":"earliest",
    "security_protocol":"PLAINTEXT",
    "encoding":"UTF-8",
    "log_filter_enabled":false
  }
}
JSON
api_curl -X POST "$BASE/api/v1/datasources" -H 'Content-Type: application/json' -d @"$E2E_TMP_DIR/xdp_e2e_kafka_datasource_payload.json" | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_kafka_datasource.json"
python3 - <<'PY'
import json, os
source=json.load(open(os.path.join(os.environ['E2E_TMP_DIR'], 'xdp_e2e_kafka_datasource.json')))
assert source.get('plugin_code') == 'kafka', source
assert source.get('internal_raw_topic') == 'raw.ds_e2e_kafka_json_source', source
assert source.get('runtime_status') == 'running', source
print('kafka datasource ok', source.get('internal_raw_topic'), source.get('listener_endpoint'))
PY
api_curl -X POST "$BASE/api/v1/parse-rules" -H 'Content-Type: application/json' -d '{
  "id":"pr_e2e_json_parser",
  "name":"E2E JSON Parser",
  "status":"active",
  "parser_plugin":"json-parser",
  "data_source_name":"E2E Kafka JSON Source",
  "input_route":"raw.ds_e2e_kafka_json_source",
  "output_index":"json_p1",
  "priority":10,
  "sample_event":"{\"level\":\"warn\",\"service\":\"checkout\",\"user\":{\"id\":\"u-1\",\"geo\":{\"country\":\"CN\"}},\"latency\":128}",
  "plugin_config":{"source_field":"raw","target":"fields","flatten_nested":true,"flatten_separator":".","array_mode":"json_string","on_invalid_json":"continue"},
  "props_conf":"[source::E2E Kafka JSON Source]\nINDEXED_EXTRACTIONS = json\nKV_MODE = none"
}' | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_json_parse_rule.json"
python3 - <<'PY'
import json, os
rule=json.load(open(os.path.join(os.environ['E2E_TMP_DIR'], 'xdp_e2e_json_parse_rule.json')))
assert rule.get('parser_plugin') == 'json-parser', rule
assert rule.get('parser_plugin_version') == '1.1.0', rule
assert rule.get('output_index') == 'json_p1', rule
fields={item.get('name') for item in rule.get('hot_fields', [])}
assert {'level','service','latency'} <= fields, rule
assert 'user.id' not in fields and 'user.geo.country' not in fields, rule
print('json parse rule ok', rule.get('parser_plugin_version'), sorted(fields))
PY
python3 - <<'PY'
import json, os, urllib.request
req=urllib.request.Request('http://127.0.0.1:8080/api/v1/runtime/pipelines')
req.add_header('Authorization', 'Bearer ' + os.environ['AUTH_TOKEN'])
body=json.load(urllib.request.urlopen(req, timeout=10))
for pipe in body.get('pipelines', []):
    if pipe.get('metadata', {}).get('id') == 'pipe_e2e_kafka_json_source':
        source=pipe.get('spec', {}).get('source', {})
        children=[]
        for stage in pipe.get('spec', {}).get('stages', []):
            children.extend(child.get('plugin') for child in stage.get('stages', []))
        assert source.get('plugin') == 'kafka', pipe
        assert source.get('config', {}).get('internal_raw_topic') == 'raw.ds_e2e_kafka_json_source', pipe
        assert 'json-parser' in children, pipe
        print('kafka json runtime pipeline ok', source.get('plugin'), children)
        break
else:
    raise SystemExit('kafka json runtime pipeline not found')
PY
sleep 5
COMPOSE_FILE="$COMPOSE" python3 - <<'PY'
import json, os, subprocess
messages=[
    {"level":"warn","service":"checkout","user":{"id":"u-1","geo":{"country":"CN"}},"latency":128},
    {"level":"info","service":"checkout","user":{"id":"u-2","geo":{"country":"US"}},"latency":64},
    {"level":"warn","service":"billing","user":{"id":"u-3","geo":{"country":"CN"}},"latency":256},
]
payload='\n'.join(json.dumps(item, separators=(',', ':')) for item in messages) + '\n'
subprocess.run(
    ['docker','compose','-f',os.environ['COMPOSE_FILE'],'exec','-T','kafka','/opt/kafka/bin/kafka-console-producer.sh','--bootstrap-server','localhost:9092','--topic','xdp.e2e.json'],
    input=payload.encode(),
    check=True,
)
print('kafka json messages sent', len(messages))
PY
COMPOSE_FILE="$COMPOSE" python3 - <<'PY'
import base64, json, os, subprocess, time, urllib.request
url='http://127.0.0.1:8123/'
messages=[
    {"level":"warn","service":"checkout","user":{"id":"u-1","geo":{"country":"CN"}},"latency":128},
    {"level":"info","service":"checkout","user":{"id":"u-2","geo":{"country":"US"}},"latency":64},
    {"level":"warn","service":"billing","user":{"id":"u-3","geo":{"country":"CN"}},"latency":256},
]
payload='\n'.join(json.dumps(item, separators=(',', ':')) for item in messages) + '\n'
def q(sql):
    req=urllib.request.Request(url, data=sql.encode(), method='POST')
    req.add_header('Authorization', 'Basic ' + base64.b64encode(b'xdp:xdp').decode())
    return urllib.request.urlopen(req, timeout=5).read().decode().strip()
def resend():
    subprocess.run(
        ['docker','compose','-f',os.environ['COMPOSE_FILE'],'exec','-T','kafka','/opt/kafka/bin/kafka-console-producer.sh','--bootstrap-server','localhost:9092','--topic','xdp.e2e.json'],
        input=payload.encode(),
        check=True,
        stdout=subprocess.DEVNULL,
    )
for _ in range(90):
    count=q("SELECT count() FROM xdp.events_json_p1 WHERE service = 'checkout' AND parse_status = 'parsed'")
    if int(count or 0) >= 2:
        row=json.loads(q("SELECT raw, fields_json, parse_status, parse_rule_name, service, level, latency FROM xdp.events_json_p1 WHERE service = 'checkout' AND level = 'warn' ORDER BY ingest_time DESC LIMIT 1 FORMAT JSONEachRow"))
        fields=json.loads(row['fields_json'])
        assert row['parse_status'] == 'parsed', row
        assert row['parse_rule_name'] == 'E2E JSON Parser', row
        assert row['service'] == 'checkout', row
        assert row['level'] == 'warn', row
        assert int(row['latency']) == 128, row
        assert fields['user.id'] == 'u-1' and fields['user.geo.country'] == 'CN', fields
        print('json clickhouse row ok', row['parse_status'], fields['user.id'], fields['user.geo.country'])
        break
    resend()
    time.sleep(1)
else:
    raise SystemExit('json_p1 parsed rows not found')
PY
api_curl --get "$BASE/api/v1/search" --data-urlencode 'q=index=json_p1 service=checkout parse_status=parsed' --data-urlencode 'earliest=-1h' --data-urlencode 'latest=now' --data-urlencode 'limit=10' | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_json_search.json"
python3 - <<'PY'
import json, os
body=json.load(open(os.path.join(os.environ['E2E_TMP_DIR'], 'xdp_e2e_json_search.json')))
assert body.get('mode') == 'events', body
assert body.get('pagination', {}).get('total') >= 2, body
event=body['events'][0]
assert event.get('metadata', {}).get('parse_status') == 'parsed', event
assert event.get('fields', {}).get('service') == 'checkout', event
print('json field search ok', body.get('pagination', {}).get('total'))
PY
api_curl --get "$BASE/api/v1/search" --data-urlencode 'q=index=json_p1 | stats count by level service | sort level service | table level service count' --data-urlencode 'earliest=-1h' --data-urlencode 'latest=now' --data-urlencode 'limit=10' | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_json_stats.json"
python3 - <<'PY'
import json, os
body=json.load(open(os.path.join(os.environ['E2E_TMP_DIR'], 'xdp_e2e_json_stats.json')))
assert body.get('mode') == 'table', body
rows=body.get('table', {}).get('rows', [])
assert rows, body
pairs={(row.get('level'), row.get('service')): int(row.get('count')) for row in rows}
assert pairs.get(('warn','checkout'), 0) >= 1, rows
assert pairs.get(('info','checkout'), 0) >= 1, rows
assert pairs.get(('warn','billing'), 0) >= 1, rows
print('json stats pipeline ok', pairs)
PY
python3 - <<'PY'
import json, os, urllib.error, urllib.request
for ptype, code in [('input','kafka'), ('parser','json-parser')]:
    try:
        req=urllib.request.Request(f'http://127.0.0.1:8080/api/v1/plugins/{code}/disable?plugin_type={ptype}', data=b'', method='POST')
        req.add_header('Authorization', 'Bearer ' + os.environ['AUTH_TOKEN'])
        urllib.request.urlopen(req, timeout=10)
    except urllib.error.HTTPError as exc:
        body=json.loads(exc.read().decode())
        assert exc.code == 409, (ptype, code, exc.code, body)
        assert body.get('error', {}).get('code') == 'PLUGIN_IN_USE', body
        print('plugin reference protection ok', code)
        continue
    raise SystemExit(f'{code} disable unexpectedly succeeded')
for ptype, code in [('input','syslog'), ('parser','regex'), ('search_command','stats')]:
    try:
        req=urllib.request.Request(f'http://127.0.0.1:8080/api/v1/plugins/{code}/disable?plugin_type={ptype}', data=b'', method='POST')
        req.add_header('Authorization', 'Bearer ' + os.environ['AUTH_TOKEN'])
        urllib.request.urlopen(req, timeout=10)
    except urllib.error.HTTPError as exc:
        body=json.loads(exc.read().decode())
        assert exc.code == 409, (ptype, code, exc.code, body)
        assert body.get('error', {}).get('code') == 'BUILTIN_PLUGIN_PROTECTED', body
        print('builtin protection ok', code)
        continue
    raise SystemExit(f'builtin {code} disable unexpectedly succeeded')
PY

printf '== verify datasource persistence and worker hot reload ==\n'
api_curl -X POST "$BASE/api/v1/indexes" -H 'Content-Type: application/json' -d '{"index_name":"hotreload","ttl_days":30,"status":"active"}' | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_hotreload_index.json"
cat >"$E2E_TMP_DIR/xdp_e2e_hot_datasource_payload.json" <<JSON
{
  "id":"e2e-hot-syslog-source",
  "name":"E2E Hot Syslog Source",
  "plugin_code":"syslog",
  "status":"active",
  "plugin_config":{
    "collector_port":${HOT_SYSLOG_PORT},
    "transport_protocol":"UDP",
    "encoding":"UTF-8",
    "log_filter_enabled":false
  }
}
JSON
api_curl -X POST "$BASE/api/v1/datasources" -H 'Content-Type: application/json' -d @"$E2E_TMP_DIR/xdp_e2e_hot_datasource_payload.json" | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_datasource_save.json"
api_curl -X POST "$BASE/api/v1/parse-rules" -H 'Content-Type: application/json' -d '{
  "id":"pr_e2e_hot_regex",
  "name":"E2E Hot Regex",
  "status":"active",
  "parser_plugin":"regex",
  "data_source_name":"E2E Hot Syslog Source",
  "input_route":"raw.ds_e2e_hot_syslog_source",
  "output_index":"hotreload",
  "sample_event":"service=e2e-hot bytes=64",
  "plugin_config":{"regex_pattern":"service=(?<service>\\S+)\\s+bytes=(?<bytes>\\d+)"},
  "props_conf":"[source::E2E Hot Syslog Source]\nEXTRACT-hot = service=(?<service>\\S+)\\s+bytes=(?<bytes>\\d+)"
}' | python3 -m json.tool >"$E2E_TMP_DIR/xdp_e2e_hot_parse_rule.json"
sleep 4
HOT_SYSLOG_PORT="$HOT_SYSLOG_PORT" python3 - <<'PY'
import base64, os, socket, time, urllib.error, urllib.request
url='http://127.0.0.1:8123/'
def q(sql):
    req=urllib.request.Request(url, data=sql.encode(), method='POST')
    req.add_header('Authorization', 'Basic ' + base64.b64encode(b'xdp:xdp').decode())
    try:
        return urllib.request.urlopen(req, timeout=5).read().decode().strip()
    except urllib.error.HTTPError:
        return '0'
def send_hot():
    msg=b'service=e2e-hot bytes=64'
    s=socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        s.sendto(msg, ('127.0.0.1', int(os.environ["HOT_SYSLOG_PORT"])))
    finally:
        s.close()
deadline = time.time() + 120
attempt = 0
last_count = '0'
while time.time() < deadline:
    attempt += 1
    for _ in range(3):
        send_hot()
        time.sleep(0.2)
    # Writer consumes in batches from Kafka; give it enough time to flush the first batch.
    if attempt < 3:
        time.sleep(2)
    count=q("SELECT count() FROM xdp.events_hotreload WHERE position(raw, 'e2e-hot') > 0")
    last_count = count or '0'
    if int(count or 0) >= 1:
        print('hot reload ok', count)
        break
    time.sleep(1)
else:
    raise SystemExit(f'hot reload rows not found, last_count={last_count}')
PY

printf '== api metrics ==\n'
api_curl "$BASE/metrics" | grep -E 'xdp_ingest_events_total|xdp_deadletter_events_total' || true
printf 'PASS %s real end-to-end acceptance passed\n' "$TEST_CASE_ID"
