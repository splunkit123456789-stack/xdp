#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

grep -F "CREATE DATABASE IF NOT EXISTS xdp" "$ROOT/migrations/clickhouse/000001_create_index_tables.sql" >/dev/null
grep -F "ADD COLUMN IF NOT EXISTS parse_status" "$ROOT/scripts/migrate-clickhouse.sh" >/dev/null
grep -F "ADD COLUMN IF NOT EXISTS parse_rule_id" "$ROOT/scripts/migrate-clickhouse.sh" >/dev/null
grep -F "ADD COLUMN IF NOT EXISTS parse_rule_name" "$ROOT/scripts/migrate-clickhouse.sh" >/dev/null
grep -F "ADD COLUMN IF NOT EXISTS parse_error" "$ROOT/scripts/migrate-clickhouse.sh" >/dev/null
grep -F "ADD COLUMN IF NOT EXISTS parsed_at" "$ROOT/scripts/migrate-clickhouse.sh" >/dev/null
grep -F "Asia/Shanghai" "$ROOT/scripts/migrate-clickhouse.sh" >/dev/null
grep -F "for table in tables:" "$ROOT/scripts/migrate-clickhouse.sh" >/dev/null

printf 'PASS TC-P0-DB-001 parse status migration definitions\n'
