# XDP

> English version: [README.en.md](./README.en.md)

XDP（eXtensible Data Platform）是一个面向日志与事件数据的可扩展数据平台。项目目标是提供类似 Splunk 的最小可用体验：采集数据、解析字段、写入逻辑 index、使用 SPL 风格语句搜索和统计数据。

当前仓库处于 MVP/P0 阶段，已经具备本地完整链路验证能力，适合用于原型验证、二次开发和产品化迭代。

## 核心能力

- Web 控制台：登录、采集配置、解析配置、索引配置、搜索页。
- 基础权限：用户名密码登录、Bearer Token、受保护 API。
- Syslog 采集：宿主机 Agent 监听 UDP 端口，支持采集源启停和运行状态查看。
- 解析配置：支持正则、JSON、KV、分隔符解析方式，使用 `props.conf` 风格高级配置表达。
- 同源多规则：同一采集源可配置多条解析规则，按优先级依次匹配。
- 逻辑 index：查询使用 `index=audit`，存储使用 ClickHouse 物理表 `events_audit`。
- ClickHouse 入库：事件表物理分表，支持字段 JSON 和热字段列化。
- 搜索页：支持 SPL 风格查询、事件视图、行展开详情、stats 聚合、时间柱状图、分页和保存搜索。
- 运行可观测：采集详情展示 Agent 心跳、监听状态、累计接收事件数、累计字节数、最近错误和链路拓扑。
- 配置持久化：采集、解析、索引等配置持久化到 MySQL，运行时通过 API 热加载。

## 当前 MVP 边界

已完成并可验收：

- 登录与基础权限。
- Syslog UDP 采集配置与端口监听联动。
- 正则/JSON/KV/分隔符解析配置。
- 解析规则优先级匹配。
- Index 配置 CRUD 与 ClickHouse 物理分表创建。
- 搜索事件与 `stats count/sum/avg ... by ...`。
- 搜索结果分页、行展开、时间柱状图。
- 已保存搜索的查询、回填、删除。
- 前端 Vue3 控制台和一键本地启动脚本。

仍属于后续阶段：

- Kafka 采集完整产品化。
- Web 手动导入采集/解析插件包和热加载。
- Deadletter 页面和失败重投。
- 搜索历史、导出 JSON/CSV、异步搜索任务。
- SPL 扩展命令：`append`、`dedup`、`eval`、`head`、`sort`、`top`、`table`。
- 自定义时间选择器和高级时间表达式输入。
- Index TTL 在线同步到 ClickHouse 物理表。

说明：当前 `ttl_days` 是配置层 TTL，用于保存、校验和页面展示；ClickHouse 物理表 TTL 仍使用默认 30 天。按 index 动态建表 TTL 和 `ALTER TABLE ... MODIFY TTL` 在线同步属于 P1。

## 技术栈

- 后端：Go 1.24
- 前端：Vue 3 + Vite
- 消息队列：Kafka
- 热存储：ClickHouse
- 元数据：MySQL
- 缓存/扩展依赖：Redis、MinIO
- 本地编排：Docker Compose + 宿主机 Agent

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
| Agent Health | `http://127.0.0.1:8081/healthz` |
| ClickHouse HTTP | `http://127.0.0.1:8123` |
| MySQL | `127.0.0.1:3306` |
| Kafka | `127.0.0.1:9092` |
| MinIO Console | `http://127.0.0.1:9001` |

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

## 常用开发命令

### 后端测试

如果本机 Go 缓存目录不可写，可以把缓存放到项目目录：

```bash
mkdir -p .cache/go-build .cache/go-mod
GOCACHE="$PWD/.cache/go-build" \
GOMODCACHE="$PWD/.cache/go-mod" \
go test ./...
```

### 前端测试和构建

```bash
cd web/console
npm test
npm run build
```

### MVP 验收脚本

```bash
bash scripts/verify-mvp.sh
```

### 真实链路脚本

```bash
bash scripts/real-e2e.sh
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
services/api/internal/mvp    MVP API 与产品化配置接口
web/console                  Vue3 控制台
migrations/                  MySQL / ClickHouse 迁移
deployments/docker-compose   本地 Docker Compose 编排
scripts/                     启动、迁移、验收和演示脚本
docs/prototypes              HTML 原型图（本地原型目录，不随 GitHub 发布 Markdown 文档）
```

## 文档说明

除 `README.md` 外，其它产品需求、测试用例、数据库设计、编码规范和原型说明等 Markdown 文档均作为本地内部资料保留，不随 GitHub 发布。

## 当前状态

最近一次本地验证结果：

- 前端：`npm test` 通过，44 条测试通过。
- 前端构建：`npm run build` 通过。
- 后端：`go test ./...` 通过。

## License

当前仓库尚未声明开源许可证。正式发布到 GitHub 前建议补充 `LICENSE` 文件。
