#!/usr/bin/env bash
set -euo pipefail
BASE=${BASE:-http://127.0.0.1:8080}
curl -sS -X POST "$BASE/api/v1/ingest/json" \
  -H 'Content-Type: application/json' \
  -d '{"raw":"{\"level\":\"info\",\"msg\":\"ok\",\"service\":\"demo\",\"bytes\":1024}"}' | python3 -m json.tool
curl -sS "$BASE/api/v1/search?field=service&value=demo" | python3 -m json.tool
