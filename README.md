# XDP

> Chinese version: [README.zh.md](./README.zh.md)

XDP, short for eXtensible Data Platform, is an extensible data platform for log and event data. The project aims to provide a minimum viable Splunk-like experience: ingest data, parse fields, write to logical indexes, and search or aggregate data with SPL-style queries.

The repository is currently in the MVP/P0 phase. It already supports a complete local validation path and is suitable for prototyping, secondary development, and productization.

## Core Capabilities

- Web console: login, collection configuration, parser configuration, index configuration, and search.
- Basic authentication: username/password login, bearer token, and protected APIs.
- Syslog ingestion: a host-side agent listens on UDP ports and supports source enablement, disablement, and runtime status checks.
- Parser configuration: regex, JSON, KV, delimiter-based parsing, and advanced `props.conf`-style configuration.
- Multiple rules per source: a single data source can have multiple parser rules matched by priority.
- Logical indexes: queries use `index=audit`, while storage uses ClickHouse physical tables such as `events_audit`.
- ClickHouse writes: per-index event tables with JSON fields and hot field columns.
- Search page: SPL-style query input, event view, expandable rows, stats aggregation, timeline histogram, pagination, and saved searches.
- Runtime observability: collection details include agent heartbeat, listener status, received event count, received bytes, latest error, and link topology.
- Persistent configuration: collection, parser, and index settings are persisted in MySQL and hot-loaded through APIs at runtime.

## Current MVP Scope

Completed and ready for validation:

- Login and basic authentication.
- Syslog UDP collection configuration and listener lifecycle control.
- Regex, JSON, KV, and delimiter parser configuration.
- Priority-based parser rule matching.
- Index configuration CRUD and ClickHouse physical table creation.
- Event search and `stats count/sum/avg ... by ...`.
- Search pagination, expandable rows, and timeline histogram.
- Saved search query, backfill, and deletion.
- Vue 3 web console and one-click local startup script.

Planned for later phases:

- Productized Kafka ingestion.
- Manual web import and hot loading for collection/parser plugin packages.
- Deadletter page and failed event replay.
- Search history, JSON/CSV export, and asynchronous search jobs.
- Additional SPL commands: `append`, `dedup`, `eval`, `head`, `sort`, `top`, and `table`.
- Custom time picker and advanced time expression input.
- Online synchronization of index TTL to ClickHouse physical tables.

Note: `ttl_days` is currently a configuration-level TTL used for saving, validation, and UI display. ClickHouse physical tables still use the default 30-day TTL. Dynamic per-index table TTL creation and `ALTER TABLE ... MODIFY TTL` synchronization belong to P1.

## Technology Stack

- Backend: Go 1.24
- Frontend: Vue 3 + Vite
- Message queue: Kafka
- Hot storage: ClickHouse
- Metadata store: MySQL
- Cache and extension dependencies: Redis, MinIO
- Local orchestration: Docker Compose + host-side agent

## Quick Start

### Requirements

- Go 1.24+
- Node.js 18+
- npm
- Docker Desktop / Docker Engine
- Docker Compose v2
- curl

### Start the full local stack

```bash
bash scripts/start-oneclick.sh
```

Then open:

```text
http://127.0.0.1:5173
```

Default login:

```text
admin / xdp
```

Default service endpoints:

| Service | Address |
|---|---|
| Web Console | `http://127.0.0.1:5173` |
| API | `http://127.0.0.1:8080` |
| Agent Health | `http://127.0.0.1:8081/healthz` |
| ClickHouse HTTP | `http://127.0.0.1:8123` |
| MySQL | `127.0.0.1:3306` |
| Kafka | `127.0.0.1:9092` |
| MinIO Console | `http://127.0.0.1:9001` |

Stop the local stack:

```bash
bash scripts/start-oneclick.sh --stop
```

The one-click script performs these steps:

- Builds Linux backend binaries for containers and a host-side `xdp-agent`.
- Starts MySQL, ClickHouse, Kafka, MinIO, and Redis.
- Runs ClickHouse migrations.
- Starts `xdp-api`, `xdp-worker`, and `xdp-writer`.
- Starts `xdp-agent` on the host so it can listen on local UDP ports directly.
- Starts the Vue console development server.

## End-to-End UI Validation

1. Log in to `http://127.0.0.1:5173` with username `admin` and password `xdp`.
2. Open the index configuration page and create an index:

```text
Index name: audit
TTL days: 30
Status: active
```

3. Open the collection configuration page and create a Syslog source:

```text
Device name: Firewall Syslog
Collection plugin: Syslog
Listen port: 5514
Log filtering: disabled
Transport protocol: UDP
Character encoding: UTF-8
Status: active
```

4. Open the parser configuration page and create a regex parser rule:

```text
Rule name: Firewall Regex
Related data source name: Firewall Syslog
Output index: audit
Parser type: regex
Sample log: src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048
Regex: src=(?<src_ip>\S+)\s+dst=(?<dst_ip>\S+)\s+action=(?<action>\S+)\s+bytes=(?<bytes>\d+)
```

5. Send a simulated Syslog event from the terminal:

```bash
printf 'src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048\n' | nc -u -w1 127.0.0.1 5514
```

6. Open the search page and query:

```spl
index=audit src_ip=10.0.1.8
```

7. Or run a stats aggregation:

```spl
index=audit | stats count as total sum(bytes) as total_bytes avg(bytes) as avg_bytes by src_ip action
```

Expected results:

- The search page shows the event or aggregation result.
- Event rows can be expanded to inspect raw data and parsed fields.
- ClickHouse contains ingested rows in `xdp.events_audit`.

## ClickHouse Validation

```bash
curl -sS 'http://127.0.0.1:8123/?database=xdp&user=xdp&password=xdp' \
  --data-binary "SELECT event_time, raw, fields_json, source_name, sourcetype, parse_status FROM events_audit ORDER BY event_time DESC LIMIT 5 FORMAT Vertical"
```

Show table DDL:

```bash
curl -sS 'http://127.0.0.1:8123/?database=xdp&user=xdp&password=xdp' \
  --data-binary "SHOW CREATE TABLE events_audit"
```

## Development Commands

### Backend tests

If the local Go cache is not writable, place Go caches inside the project directory:

```bash
mkdir -p .cache/go-build .cache/go-mod
GOCACHE="$PWD/.cache/go-build" \
GOMODCACHE="$PWD/.cache/go-mod" \
go test ./...
```

### Frontend tests and build

```bash
cd web/console
npm test
npm run build
```

### MVP verification

```bash
bash scripts/verify-mvp.sh
```

### Real end-to-end verification

```bash
bash scripts/real-e2e.sh
```

## API Examples

Check authentication status:

```bash
curl -sS http://127.0.0.1:8080/api/v1/auth | python3 -m json.tool
```

Log in:

```bash
curl -sS -X POST http://127.0.0.1:8080/api/v1/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"xdp"}' | python3 -m json.tool
```

List indexes with the default development token:

```bash
curl -sS http://127.0.0.1:8080/api/v1/indexes \
  -H 'Authorization: Bearer xdp-dev-token' | python3 -m json.tool
```

Search:

```bash
curl -sS 'http://127.0.0.1:8080/api/v1/search?q=index%3Daudit%20%7C%20stats%20count%20by%20action' \
  -H 'Authorization: Bearer xdp-dev-token' | python3 -m json.tool
```

## Configuration and Authentication

The one-click script enables basic authentication by default:

```text
XDP_AUTH_ENABLED=true
XDP_AUTH_USERNAME=admin
XDP_AUTH_PASSWORD=xdp
XDP_API_TOKEN=xdp-dev-token
```

To start the API manually with authentication:

```bash
XDP_AUTH_ENABLED=true \
XDP_AUTH_USERNAME=admin \
XDP_AUTH_PASSWORD=xdp \
XDP_API_TOKEN=change-me \
go run ./cmd/xdp-api
```

When authentication is enabled, all APIs except the following public paths require `Authorization: Bearer <token>` or `X-API-Token`:

- `/`
- `/healthz`
- `/readyz`
- `/api/v1/auth`
- `/api/v1/login`

## Project Layout

```text
cmd/                         Service entry points
pkg/                         Core packages for events, pipelines, runtime, storage, and search
plugins/                     Input, parser, transform, router, enrichment, and output plugins
services/api/internal/mvp    MVP APIs and productized configuration interfaces
web/console                  Vue 3 console
migrations/                  MySQL and ClickHouse migrations
deployments/docker-compose   Local Docker Compose orchestration
scripts/                     Startup, migration, validation, and demo scripts
docs/prototypes              Local HTML/SVG prototypes
```

## Documentation Notes

Besides `README.md` and `README.zh.md`, additional product requirements, test cases, database design notes, coding standards, and prototype documentation are kept as local internal materials and are not required for the GitHub release package.

## Current Status

Most recent local validation recorded in the Chinese README:

- Frontend: `npm test` passed with 44 tests.
- Frontend build: `npm run build` passed.
- Backend: `go test ./...` passed.

## License

This repository does not currently declare an open-source license. Add a `LICENSE` file before publishing if the project should be released under a specific license.
