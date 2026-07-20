# XDP

XDP（eXtensible Data Platform）是一个面向日志与事件数据的可扩展数据平台。项目目标是提供类似 Splunk 的最小可用体验：采集数据、解析字段、写入逻辑 index、使用 SPL 风格语句搜索和统计数据。

当前仓库已完成 MVP/P0 核心能力，并补齐 P1 关键增强能力：插件管理、Kafka 外部采集插件、JSON Parser 外部解析插件、搜索增强命令、Writer 批量入库和 Index 容量趋势采样。适合用于本地验收、原型验证、二次开发和产品化迭代。

## 核心能力

- Web 控制台：登录、采集配置、解析配置、索引配置、搜索页、插件管理。
- 基础权限：用户名密码登录、Bearer Token、受保护 API。
- Syslog 采集：宿主机 Agent 监听 UDP 端口，支持采集源启停和运行状态查看。
- 插件管理：支持 Web 上传插件包、启用、停用、引用保护和单版本覆盖升级。
- 采集插件：内置 Syslog；Kafka 通过外部插件包导入后可在采集页配置。
- 解析配置：内置正则解析，使用 `props.conf` 风格高级配置表达；JSON Parser 通过外部插件包导入。
- 同源多规则：同一采集源可配置多条解析规则，按优先级依次匹配。
- 逻辑 index：查询使用 `index=audit`，存储使用 ClickHouse 物理表 `events_audit`。
- ClickHouse 入库：事件表物理分表，支持字段 JSON、热字段列化、Writer 批量写入和失败可观测。
- 搜索页：支持 SPL 风格查询、事件视图、行展开详情、stats 聚合、table/sort/head/dedup 管道命令、时间柱状图、分页和保存搜索。
- Index 治理：支持 TTL 同步、容量趋势快照采样、趋势图和快照保留清理。
- 运行可观测：采集详情展示 Agent 心跳、监听状态、累计接收事件数、累计字节数、最近错误和链路拓扑。
- 配置持久化：采集、解析、索引等配置持久化到 MySQL，运行时通过 API 热加载。

## 当前阶段边界

已完成并可验收：

- 登录与基础权限。
- Syslog UDP 采集配置与端口监听联动。
- 正则解析配置和 `props.conf` 高级配置。
- 解析规则优先级匹配。
- Index 配置 CRUD 与 ClickHouse 物理分表创建。
- 搜索事件与 `stats count/sum/avg ... by ...`。
- 搜索增强命令：`table`、`sort`、`head`、`dedup`。
- 搜索结果分页、行展开、时间柱状图。
- 已保存搜索的查询、回填、删除。
- 插件管理：Web 上传 Kafka / JSON Parser 插件包、启用、停用、删除保护、引用保护。
- Kafka 采集插件：外部插件导入后可创建 Kafka 数据源并消费入库。
- JSON Parser 插件：外部插件导入后可创建 JSON 解析规则并展开嵌套字段。
- Writer 批量入库：支持批量 flush、失败重试、运行状态接口、Prometheus 指标和压测脚本。
- Index 增强：ClickHouse TTL 在线同步、容量趋势快照采样、快照默认保留 90 天并自动清理。
- 前端 Vue3 控制台和一键本地启动脚本。

仍属于后续阶段：

- Deadletter 页面和失败重投。
- 搜索历史、导出 JSON/CSV、异步搜索任务。
- SPL 扩展命令：`append`、`eval`、`top`。
- SPL Function Plugin。
- 多版本插件生命周期管理。目前插件采用单版本策略，同一插件新版本覆盖旧版本。
- 自定义时间选择器和高级时间表达式输入。

说明：采集和解析插件配置页由平台基于 `config_schema + ui_schema` 统一渲染，不执行插件包内前端代码；Search Command Plugin 支持执行型插件，导入时校验 zip、manifest 和 `entrypoint`，启用时解压到运行目录，搜索执行时只启动已准备好的包内脚本并通过 JSON stdin/stdout 交换数据。执行型插件 P1 仅允许 `python3` 解释器，平台会限制单次执行超时、最大输入行数、stdout/stderr 输出大小，并阻断插件脚本访问运行目录外的业务路径或发起网络连接。插件管理详情页会展示最终生效的执行限制和最近执行审计记录；每次外部搜索命令插件执行会写入审计记录，失败时返回插件级错误原因，主搜索服务继续可用。

## 技术栈

- 后端：Go 1.24
- 前端：Vue 3 + Vite
- 消息队列：Kafka
- 热存储：ClickHouse
- 元数据：MySQL
- 缓存/扩展依赖：Redis、MinIO
- 本地编排：Docker Compose + 宿主机 Agent

## 架构概览

```text
Syslog UDP
   │
   ▼
xdp-agent              Web Console
   │                         │
   │ raw events              │ REST API
   ▼                         ▼
Kafka  ─────────────►  xdp-api / config center
   │                         │
   ▼                         │ runtime pipelines
xdp-worker ◄─────────────────┘
   │ parsed events
   ▼
xdp-writer
   │
   ▼
ClickHouse events_<index>
```

核心服务：

- `xdp-api`：API、认证、配置中心、搜索接口。
- `xdp-agent`：宿主机采集器，负责 Syslog UDP 监听和采集配置热加载。
- `xdp-worker`：从 Kafka 拉取原始事件，执行 Pipeline、解析和路由。
- `xdp-writer`：将处理后的事件写入 ClickHouse。
- `web/console`：Vue3 产品控制台。

## 快速启动

### 环境要求

- Go 1.24+
- Node.js 18+
- npm
- Docker Desktop / Docker Engine
- Docker Compose v2
- curl

### 一键启动完整本地环境

```bash
bash scripts/start-oneclick.sh
```

启动后访问：

```text
http://127.0.0.1:5173
```

默认登录：

```text
admin / xdp
```

默认服务地址：

| 服务 | 地址 |
|---|---|
| Web 控制台 | `http://127.0.0.1:5173` |
| API | `http://127.0.0.1:8080` |
| Agent Health | 以启动摘要中的 `Agent:` 为准，默认 `http://127.0.0.1:8081/healthz` |
| ClickHouse HTTP | `http://127.0.0.1:8123` |
| MySQL | `127.0.0.1:3306` |
| Kafka | `127.0.0.1:9092` |
| MinIO Console | `http://127.0.0.1:9001` |

如果本机已有旧的 `xdp-agent` 占用 `127.0.0.1:8081`，一键脚本会优先尝试清理旧进程；若当前用户无权限清理且未显式指定 `XDP_AGENT_ADDR`，脚本会自动切换到空闲端口，并把实际 Agent 地址写入 API 的 `XDP_AGENT_BASE_URL`。启动摘要中的 `Agent:` 为本次有效地址，后续排障请以该地址为准。

停止环境：

```bash
bash scripts/start-oneclick.sh --stop
```

一键脚本会完成以下工作：

- 构建后端 Linux 容器二进制和宿主机 `xdp-agent`。
- 启动 MySQL、ClickHouse、Kafka、MinIO、Redis。
- 执行 ClickHouse 迁移。
- 启动 `xdp-api`、`xdp-worker`、`xdp-writer`。
- 在宿主机启动 `xdp-agent`，用于直接监听本机 UDP 端口。
- 启动 Vue 控制台开发服务器。

## 页面端到端验证

1. 登录 `http://127.0.0.1:5173`，账号 `admin`，密码 `xdp`。
2. 进入「索引配置」，新增 index，例如：

```text
index 名称：audit
TTL 天数：30
状态：active
```

3. 进入「采集配置」，新增 Syslog 采集源，例如：

```text
设备名称：Firewall Syslog
采集插件：Syslog
监听端口：5514
日志筛选：关闭
传输层协议：UDP
字符编码：UTF-8
状态：active
```

4. 进入「解析配置」，新增正则解析规则：

```text
规则名称：Firewall Regex
关联采集数据源名称：Firewall Syslog
写入 index：audit
解析方式：正则
日志样例：src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048
正则表达式：src=(?<src_ip>\S+)\s+dst=(?<dst_ip>\S+)\s+action=(?<action>\S+)\s+bytes=(?<bytes>\d+)
```

5. 在终端发送一条模拟 Syslog：

```bash
printf 'src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048\n' | nc -u -w1 127.0.0.1 5514
```

6. 进入「搜索页」，输入：

```spl
index=audit src_ip=10.0.1.8
```

7. 或执行 stats 聚合：

```spl
index=audit | stats count as total sum(bytes) as total_bytes avg(bytes) as avg_bytes by src_ip action
```

预期结果：

- 搜索页能看到事件或聚合结果。
- 事件行可展开查看 raw 和解析字段。
- ClickHouse 中可查询到 `xdp.events_audit` 的入库数据。

## ClickHouse 验证

```bash
curl -sS 'http://127.0.0.1:8123/?database=xdp&user=xdp&password=xdp' \
  --data-binary "SELECT event_time, raw, fields_json, source_name, sourcetype, parse_status FROM events_audit ORDER BY event_time DESC LIMIT 5 FORMAT Vertical"
```

查看表结构：

```bash
curl -sS 'http://127.0.0.1:8123/?database=xdp&user=xdp&password=xdp' \
  --data-binary "SHOW CREATE TABLE events_audit"
```

## 发布 Runbook

### 启动

```bash
bash scripts/start-oneclick.sh
```

启动完成后访问 `http://127.0.0.1:5173`，使用 `admin / xdp` 登录。

### 停止

```bash
bash scripts/start-oneclick.sh --stop
```

### 干净环境启动

用于验收或排查历史脏数据影响：

```bash
bash scripts/start-oneclick.sh --clean
```

`--clean` 会清理 ClickHouse 事件表、MySQL 业务配置表和 Kafka 测试 topic，然后重新启动服务。清理后不会自动恢复 `app`、`firewall` 等演示业务 index，业务 index 需要在页面或 API 中显式创建。

默认访问与账号：

- Web 控制台：`http://127.0.0.1:5173`
- API：`http://127.0.0.1:8080`
- 默认账号：`admin`
- 默认密码：`xdp`
- 默认开发 Token：`xdp-dev-token`

### P1 发布准入验证

建议在发布前按顺序执行：

```bash
bash scripts/start-oneclick.sh --clean
bash scripts/real-e2e.sh
TOTAL_EVENTS=2000 BATCH_SIZES="50 100 500 1000" OUTPUT_CSV=.cache/writer-benchmark/results.csv bash scripts/writer-benchmark.sh
bash scripts/writer-recovery-sample.sh
npm --prefix web/console test -- --run
npm --prefix web/console run build
```

关键通过标准：

- `scripts/real-e2e.sh` 输出 `Real end-to-end acceptance passed.`。
- Kafka 外部采集插件、JSON Parser 外部解析插件可导入、启用并完成端到端入库。
- 搜索增强命令 `table/sort/head/dedup` 与 `stats` 管道组合执行正常。
- 搜索命令插件执行审计可查：`/api/v1/plugins/{plugin_code}/execution-audits?plugin_type=search_command` 对 `table/sort/head/dedup` 返回执行记录，MySQL `search_command_execution_audits` 存在对应审计行。
- Writer benchmark 写入 `writer_bench`，失败数和 Deadletter 数为 0。
- `writer-recovery-sample.sh` 生成 `writer_recovery` 页面验收数据。
- `POST /api/v1/indexes/snapshots` 可生成 Index 快照；趋势接口返回 `source=snapshot`。

### Index 快照与趋势

Index 快照是每个逻辑 index 在某个时间点的容量指标采样，不是原始日志副本。采样数据存储在 MySQL `index_storage_snapshots`，用于趋势图和容量分析。

默认行为：

- API 服务启动后默认每 300 秒采样一次，可通过 `XDP_INDEX_SNAPSHOT_INTERVAL_SECONDS` 调整。
- 快照默认保留 90 天，可通过 `XDP_INDEX_SNAPSHOT_RETENTION_DAYS` 调整。
- 每次手动或定时采样成功后自动清理过期快照。
- 物理列使用 `row_count` 保存行数，API 对外仍返回 `rows`。

手动触发采样：

```bash
curl -sS -X POST http://127.0.0.1:8080/api/v1/indexes/snapshots \
  -H 'Authorization: Bearer xdp-dev-token' | python3 -m json.tool
```

查看趋势：

```bash
curl -sS http://127.0.0.1:8080/api/v1/indexes/audit/trend?days=7 \
  -H 'Authorization: Bearer xdp-dev-token' | python3 -m json.tool
```

### 插件包上传

样例插件包位于：

```text
build/plugin-packages/kafka-input-sample.zip
build/plugin-packages/json-parser-sample.zip
build/plugin-packages/json-parser-sample-1.1.0.zip
build/plugin-packages/table-search-command-sample.zip
build/plugin-packages/sort-search-command-sample.zip
build/plugin-packages/head-search-command-sample.zip
build/plugin-packages/dedup-search-command-sample.zip
```

插件管理页面上传流程：

1. 打开「插件管理」。
2. 上传对应 zip 插件包，系统按 `manifest.json` 自动识别插件类型。
3. 搜索命令插件包必须包含 `runtime=executable_search_command`、`entrypoint` 和包内可执行脚本；导入阶段只做包校验和持久化，不参与搜索执行。
4. 启用插件；启用阶段会把执行型搜索命令插件解压到 `XDP_PLUGIN_RUNTIME_DIR` 指定的运行目录，未设置时使用系统临时目录下的 `xdp-plugin-runtime`。
5. 回到采集配置、解析配置或搜索页，确认插件出现在可选目录中并可执行；搜索执行阶段不再解压插件包，只启动启用阶段已准备好的脚本。
6. 执行型搜索命令插件安全边界：只允许 `runtime_config.interpreter=python3`；默认单次超时 5 秒，最大 30 秒；默认最大输入 10000 行；默认 stdout/stderr 各限制 4MB；插件详情展示最终生效值和最近执行审计记录；插件失败会返回插件级错误原因，不会中断主搜索服务。
7. 执行审计：外部搜索命令插件每次执行记录插件编码、版本、命令名、耗时、输入行数、输出行数、成功状态、错误码和错误摘要，便于定位慢插件、资源超限和脚本异常。

### 常见问题

- 如果页面无法访问，先确认 `npm` 前端进程是否启动，并检查 `http://127.0.0.1:8080/healthz`。
- 如果本机设置了 HTTP 代理，建议确保 `127.0.0.1,localhost,::1` 在 `NO_PROXY/no_proxy` 中，避免本地健康检查返回 `502`。
- 如果 Syslog 端口不可用，执行 `lsof -nP -iUDP:<端口>` 查看是否已有进程占用。
- 如果新增采集源后没有监听端口，检查 `http://127.0.0.1:8081/healthz` 和采集配置运行状态。
- 如果搜索无结果，先确认解析配置里的写入 index 与搜索 SPL 中的 `index=...` 一致。
- 如果 ClickHouse 查询不到热字段列，确认解析规则保存后发送的是新数据；P0 不回填历史数据。
- 如果 Index 趋势为空，先触发 `POST /api/v1/indexes/snapshots`，再打开索引趋势。

## 常用开发命令

### 后端测试

如果本机 Go 缓存目录不可写，可以把缓存放到项目目录：

```bash
mkdir -p .cache/go-build .cache/go-mod .cache/go-path
GOCACHE="$PWD/.cache/go-build" \
GOMODCACHE="$PWD/.cache/go-mod" \
GOPATH="$PWD/.cache/go-path" \
go test ./...
```

### 前端测试和构建

```bash
cd web/console
npm test
npm run build
```

### 基础回归脚本

```bash
bash scripts/verify-mvp.sh
```

`verify-mvp.sh` 用于快速检查基础 MVP 链路。P1 发布准入请以“发布 Runbook”中的真实链路、Writer 压测、恢复样例和前端构建命令为准。

### 真实链路脚本

```bash
bash scripts/real-e2e.sh
```

### Writer 压测与恢复样例

```bash
TOTAL_EVENTS=2000 BATCH_SIZES="50 100 500 1000" OUTPUT_CSV=.cache/writer-benchmark/results.csv bash scripts/writer-benchmark.sh
bash scripts/writer-recovery-sample.sh
```

## API 示例

查询认证状态：

```bash
curl -sS http://127.0.0.1:8080/api/v1/auth | python3 -m json.tool
```

登录：

```bash
curl -sS -X POST http://127.0.0.1:8080/api/v1/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"xdp"}' | python3 -m json.tool
```

使用默认开发 Token 查询 index：

```bash
curl -sS http://127.0.0.1:8080/api/v1/indexes \
  -H 'Authorization: Bearer xdp-dev-token' | python3 -m json.tool
```

搜索：

```bash
curl -sS 'http://127.0.0.1:8080/api/v1/search?q=index%3Daudit%20%7C%20stats%20count%20by%20action' \
  -H 'Authorization: Bearer xdp-dev-token' | python3 -m json.tool
```

## 配置与认证

一键脚本默认启用基础认证：

```text
XDP_AUTH_ENABLED=true
XDP_AUTH_USERNAME=admin
XDP_AUTH_PASSWORD=xdp
XDP_API_TOKEN=xdp-dev-token
```

手动启动 API 时可使用：

```bash
XDP_AUTH_ENABLED=true \
XDP_AUTH_USERNAME=admin \
XDP_AUTH_PASSWORD=xdp \
XDP_API_TOKEN=change-me \
go run ./cmd/xdp-api
```

启用认证后，除以下公开路径外，其它 API 需要 `Authorization: Bearer <token>` 或 `X-API-Token`：

- `/`
- `/healthz`
- `/readyz`
- `/api/v1/auth`
- `/api/v1/login`

## 目录结构

```text
cmd/                         服务入口
pkg/                         事件模型、Pipeline、Runtime、存储、搜索核心包
plugins/                     输入、解析、转换、路由、输出插件
docs/plugins/                插件规范、开发说明和样例源码目录
build/plugin-packages/       构建生成的可上传插件包 zip 产物
services/api/internal/mvp    MVP API 与产品化配置接口
web/console                  Vue3 控制台
migrations/                  MySQL / ClickHouse 迁移
deployments/docker-compose   本地 Docker Compose 编排
scripts/                     启动、迁移、验收和演示脚本
```

插件目录约定：

- `docs/plugins/` 只保存插件规范、样例源码、manifest 示例和开发说明，不作为 Web 上传产物目录。
- `build/plugin-packages/` 保存构建生成的插件包，例如 `kafka-input-sample.zip`、`json-parser-sample.zip`、`table-search-command-sample.zip`，插件管理页面手动上传时优先选择该目录下的 zip。
- `plugins/` 保存平台内置或运行时插件代码，例如内置 `syslog`、`regex`、`stats` 以及已编译进平台的运行时适配器。

## GitHub 发布包说明

对外发布目录通过以下命令生成：

```bash
bash scripts/sync-github-source.sh
```

生成目录为 `xdp-github-release/`。发布包只包含源码、配置、迁移、脚本、`README.md` 和 `CHANGELOG.md`；其它本地规划、验收、设计和视觉稿文件不进入发布目录。

发布目录会额外保留 `build/plugin-packages/` 下可直接 Web 上传的样例插件包：

```text
build/plugin-packages/kafka-input-sample.zip
build/plugin-packages/json-parser-sample.zip
build/plugin-packages/json-parser-sample-1.1.0.zip
build/plugin-packages/table-search-command-sample.zip
build/plugin-packages/sort-search-command-sample.zip
build/plugin-packages/head-search-command-sample.zip
build/plugin-packages/dedup-search-command-sample.zip
```

说明：`invalid-plugin.zip` 仅用于本地自动化错误路径验证，不进入 GitHub 发布目录。

## 当前状态

最近一次 P1 发布准入检查结果：

- 后端：`go test ./pkg/storage/mysql ./pkg/storage/clickhouse ./services/api/internal/mvp` 通过。
- 前端：`npm --prefix web/console test -- --run App.mvp.test.js App.parse.test.js App.index.test.js App.plugins.test.js App.collect.test.js` 通过，54 个用例通过。
- 前端构建：`npm --prefix web/console run build` 通过。
- 真实端到端：2026-07-12 复核 `Syslog + Regex`、`Kafka + JSON Parser`、`stats`、`table/sort/head/dedup` 均通过；`events_audit_p1_manual=1`，`events_json_p1_manual=8`。
- 插件执行治理：`table/sort/head/dedup` 审计 API 与 MySQL `search_command_execution_audits` 均有执行记录，包含输入/输出事件行数、耗时、解释器和限制参数。
- 一键启动：修复旧宿主机 `xdp-agent` 占用 8081 时误判 Agent ready 的问题；启动摘要会展示本次 API 实际使用的 Agent 地址。
- Index 趋势增强：`POST /api/v1/indexes/snapshots` 采样通过，`audit_p0/json_p1` 趋势接口返回 `source=snapshot`。
- Writer 页面数据：`scripts/writer-recovery-sample.sh` 可稳定生成 `writer_recovery` 验收数据。

## License

当前仓库尚未声明开源许可证。正式发布到 GitHub 前建议补充 `LICENSE` 文件。
