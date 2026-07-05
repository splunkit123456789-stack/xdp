#!/usr/bin/env bash
set -euo pipefail

grep -F "parse_status LowCardinality(String) DEFAULT 'unparsed'" migrations/clickhouse/000001_create_index_tables.sql
grep -F "parse_rule_id String DEFAULT ''" migrations/clickhouse/000001_create_index_tables.sql
grep -F "parse_rule_name String DEFAULT ''" migrations/clickhouse/000001_create_index_tables.sql
grep -F "parse_error String DEFAULT ''" migrations/clickhouse/000001_create_index_tables.sql
grep -F "parsed_at DateTime64(3, 'Asia/Shanghai')" migrations/clickhouse/000001_create_index_tables.sql

grep -F "ADD COLUMN IF NOT EXISTS parse_status" migrations/clickhouse/000003_add_parse_status_columns.sql
grep -F "ADD COLUMN IF NOT EXISTS parse_status" scripts/migrate-clickhouse.sh
grep -F "for table in tables:" scripts/migrate-clickhouse.sh
