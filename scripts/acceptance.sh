#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
BASE=${BASE:-${XDP_API_BASE:-http://127.0.0.1:8080}}
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/xdp-acceptance.XXXXXX")"
PASSED=0
FAILED=0
SKIPPED=0

cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    printf 'missing required command: %s\n' "$1" >&2
    exit 4
  }
}

run_case() {
  local case_id="$1"
  local description="$2"
  shift 2

  printf '== %s %s ==\n' "$case_id" "$description"
  set +e
  "$@"
  local status=$?
  set -e

  if [ "$status" -eq 0 ]; then
    printf 'PASS %s %s\n' "$case_id" "$description"
    PASSED=$((PASSED + 1))
  else
    printf 'FAIL %s %s\n' "$case_id" "$description"
    FAILED=$((FAILED + 1))
  fi
}

health_case() {
  curl -fsS "$BASE/healthz" >/dev/null
}

plugins_case() {
  curl -fsS "$BASE/api/v1/plugins?plugin_type=input&page_size=100" | python3 -m json.tool >"$TMP_DIR/plugins_input.json"
  curl -fsS "$BASE/api/v1/plugins?plugin_type=parser&page_size=100" | python3 -m json.tool >"$TMP_DIR/plugins_parser.json"
  curl -fsS "$BASE/api/v1/plugins?plugin_type=search_command&page_size=100" | python3 -m json.tool >"$TMP_DIR/plugins_search_command.json"
  python3 - "$TMP_DIR" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
items = []
for path in [
    root / "plugins_input.json",
    root / "plugins_parser.json",
    root / "plugins_search_command.json",
]:
    body = json.load(path.open())
    items.extend(body.get("plugins", body if isinstance(body, list) else []))

codes = {i.get("plugin_code") or i.get("code") for i in items}
required = {"syslog", "regex", "stats"}
forbidden = {"http-input", "json-parser"}
missing = required - codes
unexpected = forbidden & codes
if missing:
    raise SystemExit(f"missing required plugins: {sorted(missing)}")
if unexpected:
    raise SystemExit(f"unexpected built-in plugins: {sorted(unexpected)}")
PY
}

runtime_pipelines_case() {
  curl -fsS "$BASE/api/v1/runtime/pipelines" | python3 -m json.tool >"$TMP_DIR/pipelines.json"
  python3 - "$TMP_DIR/pipelines.json" <<'PY'
import json
import sys

body = json.load(open(sys.argv[1]))
pipes = body.get("pipelines", body if isinstance(body, list) else [])
if not pipes:
    raise SystemExit(f"empty runtime pipelines: {body}")
for pipe in pipes:
    source = pipe.get("spec", {}).get("source", {})
    if source.get("plugin") == "http-input":
        raise SystemExit(f"unexpected http-input source: {pipe}")
    for stage in pipe.get("spec", {}).get("stages") or []:
        if stage.get("plugin") == "json-parser":
            raise SystemExit(f"unexpected json-parser stage: {pipe}")
PY
}

datasources_case() {
  curl -fsS "$BASE/api/v1/datasources" | python3 -m json.tool >"$TMP_DIR/datasources.json"
  python3 - "$TMP_DIR/datasources.json" <<'PY'
import json
import sys

body = json.load(open(sys.argv[1]))
items = body.get("datasources", body if isinstance(body, list) else [])
if not any((item.get("plugin_code") == "syslog" or item.get("type") == "syslog") for item in items):
    raise SystemExit(f"missing syslog datasource: {body}")
if any(item.get("type") == "http" or item.get("id") == "http-json" for item in items):
    raise SystemExit(f"unexpected http datasource: {body}")
PY
}

parser_plugins_case() {
  curl -fsS "$BASE/api/v1/parser-plugins" | python3 -m json.tool >"$TMP_DIR/parser_plugins.json"
  python3 - "$TMP_DIR/parser_plugins.json" <<'PY'
import json
import sys

body = json.load(open(sys.argv[1]))
items = body.get("plugins", body if isinstance(body, list) else [])
regex = [item for item in items if item.get("plugin_code") == "regex" or item.get("code") == "regex"]
if not regex or regex[0].get("status") != "active":
    raise SystemExit(f"regex parser plugin not active: {body}")
PY
}

search_case() {
  curl -fsS "$BASE/api/v1/search?q=index%3D_unparsed&limit=1&page=1" | python3 -m json.tool >"$TMP_DIR/search.json"
  python3 - "$TMP_DIR/search.json" <<'PY'
import json
import sys

body = json.load(open(sys.argv[1]))
if "pagination" not in body and "events" not in body:
    raise SystemExit(f"unexpected search response: {body}")
PY
}

require_cmd curl
require_cmd python3

cd "$ROOT"

run_case "TC-MVP-ACCEPT-001" "health endpoint" health_case
run_case "TC-MVP-ACCEPT-002" "built-in plugin baseline" plugins_case
run_case "TC-MVP-ACCEPT-003" "runtime pipelines baseline" runtime_pipelines_case
run_case "TC-MVP-ACCEPT-004" "datasource baseline" datasources_case
run_case "TC-MVP-ACCEPT-005" "parser plugin baseline" parser_plugins_case
run_case "TC-MVP-ACCEPT-006" "search API baseline" search_case

if [ "$FAILED" -ne 0 ]; then
  printf '== summary passed=%s failed=%s skipped=%s ==\n' "$PASSED" "$FAILED" "$SKIPPED"
  exit 1
fi

printf '== summary passed=%s failed=%s skipped=%s ==\n' "$PASSED" "$FAILED" "$SKIPPED"
