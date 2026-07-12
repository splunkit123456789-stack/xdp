#!/usr/bin/env bash
set -euo pipefail
BASE=${BASE:-http://127.0.0.1:8080}

printf '== health ==\n'
curl -fsS "$BASE/healthz"

printf '\n\n== productized plugins ==\n'
curl -fsS "$BASE/api/v1/plugins?plugin_type=input&page_size=100" | python3 -m json.tool >/tmp/xdp_plugins_input.json
curl -fsS "$BASE/api/v1/plugins?plugin_type=parser&page_size=100" | python3 -m json.tool >/tmp/xdp_plugins_parser.json
curl -fsS "$BASE/api/v1/plugins?plugin_type=search_command&page_size=100" | python3 -m json.tool >/tmp/xdp_plugins_search_command.json
python3 - <<'PY'
import json
items = []
for path in [
    '/tmp/xdp_plugins_input.json',
    '/tmp/xdp_plugins_parser.json',
    '/tmp/xdp_plugins_search_command.json',
]:
    body = json.load(open(path))
    items.extend(body.get('plugins', body if isinstance(body, list) else []))
codes = {i.get('plugin_code') or i.get('code') for i in items}
required = {'syslog', 'regex', 'stats'}
forbidden = {'http-input', 'json-parser'}
missing = required - codes
unexpected = forbidden & codes
assert not missing, missing
assert not unexpected, unexpected
print('plugins ok', sorted(codes))
PY

printf '\n== runtime pipelines ==\n'
curl -fsS "$BASE/api/v1/runtime/pipelines" | python3 -m json.tool >/tmp/xdp_pipelines.json
python3 - <<'PY'
import json
body = json.load(open('/tmp/xdp_pipelines.json'))
pipes = body.get('pipelines', body if isinstance(body, list) else [])
assert pipes, body
for pipe in pipes:
    source = pipe.get('spec', {}).get('source', {})
    assert source.get('plugin') != 'http-input', pipe
    for stage in pipe.get('spec', {}).get('stages') or []:
        assert stage.get('plugin') != 'json-parser', pipe
print('runtime pipelines ok', [p.get('metadata', {}).get('id') for p in pipes])
PY

printf '\n== datasource and parser config ==\n'
curl -fsS "$BASE/api/v1/datasources" | python3 -m json.tool >/tmp/xdp_datasources.json
python3 - <<'PY'
import json
body = json.load(open('/tmp/xdp_datasources.json'))
items = body.get('datasources', body if isinstance(body, list) else [])
assert any((item.get('plugin_code') == 'syslog' or item.get('type') == 'syslog') for item in items), body
assert not any(item.get('type') == 'http' or item.get('id') == 'http-json' for item in items), body
print('datasources ok', [item.get('name') or item.get('id') for item in items])
PY

curl -fsS "$BASE/api/v1/parser-plugins" | python3 -m json.tool >/tmp/xdp_parser_plugins.json
python3 - <<'PY'
import json
body = json.load(open('/tmp/xdp_parser_plugins.json'))
items = body.get('plugins', body if isinstance(body, list) else [])
regex = [item for item in items if item.get('plugin_code') == 'regex' or item.get('code') == 'regex']
assert regex and regex[0].get('status') == 'active', body
print('parser plugins ok')
PY

printf '\n== search api ==\n'
curl -fsS "$BASE/api/v1/search?q=index%3D_unparsed&limit=1&page=1" | python3 -m json.tool >/tmp/xdp_search.json
python3 - <<'PY'
import json
body = json.load(open('/tmp/xdp_search.json'))
assert 'pagination' in body or 'events' in body, body
print('search api ok')
PY

printf '\nMVP acceptance passed.\n'
