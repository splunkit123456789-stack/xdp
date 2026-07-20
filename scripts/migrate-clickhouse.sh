#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
CLICKHOUSE_URL=${CLICKHOUSE_URL:-http://127.0.0.1:8123}
CLICKHOUSE_USER=${CLICKHOUSE_USER:-}
CLICKHOUSE_PASSWORD=${CLICKHOUSE_PASSWORD:-}
MIGRATIONS_DIR=${MIGRATIONS_DIR:-$ROOT/migrations/clickhouse}

if ! command -v python3 >/dev/null 2>&1; then
    printf 'missing required command: python3\n' >&2
    exit 4
fi

export CLICKHOUSE_URL CLICKHOUSE_USER CLICKHOUSE_PASSWORD MIGRATIONS_DIR

python3 - <<'PY'
from pathlib import Path
import os
import urllib.request
import base64
url = os.environ.get('CLICKHOUSE_URL', 'http://127.0.0.1:8123') + '/'
user = os.environ.get('CLICKHOUSE_USER', '')
password = os.environ.get('CLICKHOUSE_PASSWORD', '')
migrations_dir = Path(os.environ['MIGRATIONS_DIR'])

def execute(stmt):
    req = urllib.request.Request(url, data=stmt.encode(), method='POST')
    if user or password:
        token = base64.b64encode(f'{user}:{password}'.encode()).decode()
        req.add_header('Authorization', 'Basic ' + token)
    with urllib.request.urlopen(req, timeout=10) as resp:
        return resp.read().decode()

for path in sorted(migrations_dir.glob('*.sql')):
    sql = path.read_text()
    for stmt in [s.strip() for s in sql.split(';') if s.strip()]:
        execute(stmt)

tables = execute(
    "SELECT name FROM system.tables "
    "WHERE database = 'xdp' AND startsWith(name, 'events_') FORMAT TSV"
).splitlines()
for table in tables:
    safe_table = ''.join(ch for ch in table if ch.isalnum() or ch == '_')
    if safe_table != table:
        continue
    execute(
        f"ALTER TABLE xdp.{safe_table} "
        "ADD COLUMN IF NOT EXISTS parse_status LowCardinality(String) DEFAULT 'unparsed' AFTER sourcetype, "
        "ADD COLUMN IF NOT EXISTS parse_rule_id String DEFAULT '' AFTER parse_status, "
        "ADD COLUMN IF NOT EXISTS parse_rule_name String DEFAULT '' AFTER parse_rule_id, "
        "ADD COLUMN IF NOT EXISTS parse_error String DEFAULT '' AFTER parse_rule_name, "
        "ADD COLUMN IF NOT EXISTS parsed_at DateTime64(3, 'Asia/Shanghai') DEFAULT toDateTime64('1970-01-01 00:00:00', 3, 'Asia/Shanghai') AFTER parse_error"
    )
    execute(
        f"ALTER TABLE xdp.{safe_table} "
        "MODIFY COLUMN event_time DateTime64(3, 'Asia/Shanghai'), "
        "MODIFY COLUMN ingest_time DateTime64(3, 'Asia/Shanghai'), "
        "MODIFY COLUMN created_at DateTime64(3, 'Asia/Shanghai') DEFAULT now64(3, 'Asia/Shanghai')"
    )
print('clickhouse migrations applied')
PY
