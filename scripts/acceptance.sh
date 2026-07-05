#!/usr/bin/env bash
set -euo pipefail
BASE=${BASE:-http://127.0.0.1:8080}

printf '== health ==\n'
curl -fsS "$BASE/healthz"
printf '\n\n== plugins ==\n'
curl -fsS "$BASE/api/v1/plugins" | python3 -m json.tool >/tmp/xdp_plugins.json
python3 - <<'PY'
import json
items=json.load(open('/tmp/xdp_plugins.json'))
required={'json-parser','regex-parser','field-mapping','type-convert','index-router','geoip','clickhouse-output','s3-output'}
found={i['code'] for i in items}
missing=required-found
assert not missing, missing
print('plugins ok', len(items))
PY
printf '\n== ingest/search ==\n'
curl -fsS -X POST "$BASE/api/v1/ingest/json" -H 'Content-Type: application/json' -d '{"time_field":"@timestamp","raw":"{\"@timestamp\":\"2026-01-02T03:04:05Z\",\"level\":\"info\",\"msg\":\"ok\",\"service\":\"demo\",\"bytes\":1024}"}' | python3 -m json.tool >/tmp/xdp_ingest.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_ingest.json'))
assert body['status']=='indexed', body
assert body['event']['event_time'].startswith('2026-01-02T03:04:05'), body
print('ingest ok', body['event']['event_id'])
PY
curl -fsS "$BASE/api/v1/search?field=service&value=demo" | python3 -m json.tool >/tmp/xdp_search.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_search.json'))
assert len(body['events']) >= 1, body
print('search ok', len(body['events']))
PY
curl -fsS "$BASE/api/v1/search?field=service&value=demo&start_time=2026-01-02T00%3A00%3A00Z&end_time=2026-01-03T00%3A00%3A00Z" | python3 -m json.tool >/tmp/xdp_time_search.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_time_search.json'))
assert len(body['events']) >= 1, body
assert all(event['event_time'].startswith('2026-01-02') for event in body['events']), body
print('time search ok', len(body['events']))
PY
curl -fsS "$BASE/api/v1/search?q=stats%20count%20as%20total%20by%20service" | python3 -m json.tool >/tmp/xdp_stats.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_stats.json'))
stats=body.get('stats') or {}
rows=stats.get('rows') or []
assert any(row.get('service') == 'demo' and row.get('total', 0) >= 1 for row in rows), body
print('stats ok', rows)
PY
curl -fsS "$BASE/api/v1/search?q=index%3Dapp%20service%3Ddemo" | python3 -m json.tool >/tmp/xdp_spl_search.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_spl_search.json'))
assert len(body['events']) >= 1, body
assert any((event.get('metadata') or {}).get('index') == 'app' and (event.get('fields') or {}).get('service') == 'demo' for event in body['events']), body
print('spl search ok', len(body['events']))
PY
curl -fsS "$BASE/api/v1/search?q=index%3Dapp%20%7C%20stats%20count%20as%20total%20by%20service" | python3 -m json.tool >/tmp/xdp_spl_stats.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_spl_stats.json'))
stats=body.get('stats') or {}
rows=stats.get('rows') or []
assert any(row.get('service') == 'demo' and row.get('total', 0) >= 1 for row in rows), body
print('spl stats ok', rows)
PY
curl -fsS "$BASE/api/v1/search?q=index%3Dapp&limit=1&page=1" | python3 -m json.tool >/tmp/xdp_page_search.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_page_search.json'))
page=body.get('pagination') or {}
assert page.get('limit') == 1 and page.get('returned', 0) >= 1, body
print('pagination ok', page)
PY
curl -fsS "$BASE/api/v1/search/fields?q=index%3Dapp&limit=100" | python3 -m json.tool >/tmp/xdp_fields.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_fields.json'))
fields={item.get('name') for item in body.get('fields', [])}
assert 'service' in fields, body
print('fields ok', sorted(fields))
PY
curl -fsS "$BASE/api/v1/search/timeline?q=index%3Dapp&start_time=2026-01-02T00%3A00%3A00Z&end_time=2026-01-03T00%3A00%3A00Z" | python3 -m json.tool >/tmp/xdp_timeline.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_timeline.json'))
assert body.get('buckets'), body
print('timeline ok', body['buckets'])
PY
curl -fsS "$BASE/api/v1/indexes" | python3 -m json.tool >/tmp/xdp_indexes.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_indexes.json'))
indexes={item.get('index_name') for item in body.get('indexes', [])}
assert 'app' in indexes, body
print('indexes ok', sorted(indexes))
PY
curl -fsS "$BASE/api/v1/datasources" | python3 -m json.tool >/tmp/xdp_datasources.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_datasources.json'))
ids={item.get('id') for item in body.get('datasources', [])}
assert 'http-json' in ids and 'syslog-firewall' in ids, body
print('datasources ok', sorted(ids))
PY
printf '\n== deadletter ==\n'
curl -fsS -X POST "$BASE/api/v1/ingest/json" -H 'Content-Type: application/json' -d '{"raw":"not-json"}' | python3 -m json.tool >/tmp/xdp_bad.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_bad.json'))
assert body['status']=='dead_letter', body
print('deadletter ingest ok')
PY
curl -fsS "$BASE/api/v1/deadletters" | python3 -m json.tool >/tmp/xdp_deadletters.json
python3 - <<'PY'
import json
body=json.load(open('/tmp/xdp_deadletters.json'))
assert len(body['events']) >= 1, body
assert len(body.get('deadletters', [])) >= 1, body
assert body['deadletters'][0].get('error_code'), body
print('deadletters ok', len(body['events']), body['deadletters'][0].get('error_code'))
PY
printf '\n== metrics ==\n'
curl -fsS "$BASE/metrics" | grep -E 'xdp_ingest_events_total|xdp_deadletter_events_total'
printf '\nMVP acceptance passed.\n'
