#!/usr/bin/env bash
set -euo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-deployments/docker-compose/docker-compose.yaml}"
CLICKHOUSE_URL="${CLICKHOUSE_URL:-http://127.0.0.1:8123}"
CLICKHOUSE_USER="${CLICKHOUSE_USER:-xdp}"
CLICKHOUSE_PASSWORD="${CLICKHOUSE_PASSWORD:-xdp}"
MYSQL_USER="${MYSQL_USER:-xdp}"
MYSQL_PASSWORD="${MYSQL_PASSWORD:-xdp}"
MYSQL_DATABASE="${MYSQL_DATABASE:-xdp}"

wait_mysql() {
  printf '== wait mysql ==\n'
  local i
  for i in $(seq 1 90); do
    if docker compose -f "$COMPOSE_FILE" exec -T mysql mysqladmin ping -h 127.0.0.1 -u"$MYSQL_USER" -p"$MYSQL_PASSWORD" >/dev/null 2>&1; then
      printf 'mysql ready\n'
      return 0
    fi
    sleep 1
  done
  printf 'mysql not ready\n' >&2
  docker compose -f "$COMPOSE_FILE" logs --tail=80 mysql >&2 || true
  return 1
}

reset_clickhouse() {
  printf '== reset clickhouse event tables ==\n'
  python3 - <<'PY'
import base64
import os
import urllib.request

url = os.environ.get("CLICKHOUSE_URL", "http://127.0.0.1:8123") + "/"
user = os.environ.get("CLICKHOUSE_USER", "")
password = os.environ.get("CLICKHOUSE_PASSWORD", "")

def execute(sql):
    req = urllib.request.Request(url, data=sql.encode(), method="POST")
    if user or password:
        req.add_header("Authorization", "Basic " + base64.b64encode(f"{user}:{password}".encode()).decode())
    return urllib.request.urlopen(req, timeout=10).read().decode()

execute("CREATE DATABASE IF NOT EXISTS xdp")
tables = execute(
    "SELECT name FROM system.tables "
    "WHERE database = 'xdp' AND startsWith(name, 'events_') FORMAT TSV"
).splitlines()
for table in tables:
    if table and all(ch.isalnum() or ch == "_" for ch in table):
        execute(f"DROP TABLE IF EXISTS xdp.{table}")
print("clickhouse reset ok", len(tables))
PY
}

reset_mysql() {
  printf '== reset mysql business tables ==\n'
  local tables=(
    data_source_runtime_states
    data_sources
    parse_rules
    saved_searches
    search_command_execution_audits
    indexes
    parser_plugins
    pipeline_versions
    pipelines
    plugin_versions
    plugins
    deadletter_records
    auth_audit_logs
  )
  local table_names
  table_names="$(printf "'%s'," "${tables[@]}")"
  table_names="${table_names%,}"
  local existing_tables
  existing_tables="$(docker compose -f "$COMPOSE_FILE" exec -T mysql mysql -N -B -u"$MYSQL_USER" -p"$MYSQL_PASSWORD" "$MYSQL_DATABASE" \
    -e "SELECT table_name FROM information_schema.tables WHERE table_schema = '$MYSQL_DATABASE' AND table_name IN ($table_names)")"

  local reset_sql="SET FOREIGN_KEY_CHECKS=0;" table
  for table in "${tables[@]}"; do
    if printf '%s\n' "$existing_tables" | grep -Fxq "$table"; then
      reset_sql="${reset_sql}
TRUNCATE TABLE \`$table\`;"
    fi
  done
  reset_sql="${reset_sql}
SET FOREIGN_KEY_CHECKS=1;"
  docker compose -f "$COMPOSE_FILE" exec -T mysql mysql -u"$MYSQL_USER" -p"$MYSQL_PASSWORD" "$MYSQL_DATABASE" <<<"$reset_sql"
  printf 'mysql reset ok\n'
}

wait_topic_deleted() {
  local topic="$1"
  local i
  for i in $(seq 1 30); do
    if ! docker compose -f "$COMPOSE_FILE" exec -T kafka /opt/kafka/bin/kafka-topics.sh \
      --bootstrap-server localhost:9092 --list 2>/dev/null | grep -Fxq "$topic"; then
      return 0
    fi
    sleep 1
  done
  printf 'warn: kafka topic still exists after delete timeout: %s\n' "$topic" >&2
}

reset_kafka() {
  printf '== reset kafka topics ==\n'
  local topic
  for topic in xdp.raw.syslog xdp.output.default xdp.deadletter.writer raw.ds_e2e_syslog_source; do
    docker compose -f "$COMPOSE_FILE" exec -T kafka /opt/kafka/bin/kafka-topics.sh \
      --bootstrap-server localhost:9092 --delete --if-exists --topic "$topic" >/dev/null 2>&1 || true
  done
  for topic in xdp.raw.syslog xdp.output.default xdp.deadletter.writer raw.ds_e2e_syslog_source; do
    wait_topic_deleted "$topic"
  done
  for topic in xdp.raw.syslog xdp.output.default xdp.deadletter.writer; do
    docker compose -f "$COMPOSE_FILE" exec -T kafka /opt/kafka/bin/kafka-topics.sh \
      --bootstrap-server localhost:9092 --create --if-not-exists --topic "$topic" --partitions 1 --replication-factor 1 >/dev/null
  done
}

reset_clickhouse
wait_mysql
reset_mysql
reset_kafka
printf 'test environment reset complete.\n'
