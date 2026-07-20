# XDP 脚本规范

本文档定义 XDP 仓库中启动、验证、验收和运维脚本的编写约束。以下场景应先阅读本文档：

- 新增 `scripts/*.sh` 脚本
- 修改现有脚本的结构、输入输出或清理逻辑
- 编写 `verify-*.sh` 验证脚本
- 编写 `acceptance/*.sh` 验收脚本

## 基本约定

- Bash 脚本统一使用 `#!/usr/bin/env bash` 和 `set -euo pipefail`。
- 脚本必须可独立执行，不依赖调用者当前工作目录。
- 脚本应自行定位仓库根目录。位于 `scripts/*.sh` 的脚本推荐：

```bash
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
```

- 位于 `scripts/acceptance/*.sh` 等更深层目录的脚本，需要按实际层级调整 `ROOT`，或使用 `git rev-parse --show-toplevel` 作为 fallback。
- 脚本内部引用文件时使用 `$ROOT/path/to/file`，不要依赖相对路径。

## 输入约定

- 可配置项优先使用环境变量，并提供默认值，例如：

```bash
ADDR=${XDP_API_ADDR:-127.0.0.1:8080}
```

- 不使用位置参数承载业务配置；位置参数只用于子命令或开关，例如 `--clean`、`--help`。
- 验收脚本建议支持：
  - `--help`
  - `--clean`
  - `--report-dir`
- 敏感信息通过环境变量传入，例如 `XDP_AUTH_PASSWORD`、`XDP_API_TOKEN`。
- 不在 stdout 打印明文密码、完整 Token 或其它敏感凭据；需要输出时做脱敏。

## 依赖检查

- 脚本开始执行前应检查必要命令是否存在。
- 常见依赖包括 `curl`、`jq`、`docker`、`mysql`、`clickhouse-client`、`python3`。
- 缺少依赖时应输出明确错误并以非零退出码退出。

示例：

```bash
require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 4
  }
}
```

## 输出约定

- 成功退出码为 `0`，失败退出码为非零。
- stdout 输出结构化结果，使用 `== phase ==` 分段。
- stderr 输出诊断信息、依赖缺失、环境异常和失败原因。
- 验证脚本用例输出统一格式：

```text
PASS <用例ID> <说明>
FAIL <用例ID> <说明>
SKIP <用例ID> <说明>
```

示例：

```text
== P2 RBAC API acceptance ==
PASS TC-P2-RBAC-001 login and /api/v1/me ok
FAIL TC-P2-RBAC-004 expected 403, got 200
== summary passed=8 failed=1 skipped=0 duration=12s ==
```

## 退出码约定

| 退出码 | 含义 |
|---|---|
| `0` | 全部通过 |
| `1` | 有用例失败 |
| `2` | 环境未就绪 |
| `3` | 参数错误 |
| `4` | 依赖缺失 |

区分原则：退出码 2 表示需要启动或等待服务（端口未监听、健康检查未通过）；退出码 4 表示需要安装系统依赖（缺少 curl/jq/docker 等）。两者处理策略不同，不应混用。

## 报告约定

- 验收脚本除控制台输出外，可选写入报告目录。
- 默认报告目录：`reports/acceptance/<timestamp>/`
- 报告文件建议包括：`summary.md`、`result.json`、`junit.xml`、`logs/`
- `result.json` 结构在首个实现该约定的脚本中定义，本文档不预先固化 schema。
- Playwright HTML 报告查看：`npx playwright show-report reports/html`（在 `scripts/playwright/` 下执行）。

## 清理约定

- 涉及临时文件、后台进程或临时端口监听时，必须使用 `trap cleanup EXIT`。
- `cleanup` 只清理当前脚本创建并记录的资源。
- 不允许无差别 kill 进程，避免误杀用户手动启动的服务或其它验收脚本。
- 临时文件应使用 `mktemp` 创建，并在退出时删除。
- 验收脚本不应残留监听端口、临时 topic、临时文件或临时进程。

## 编排约定

- 单职责：一个脚本只验证一个模块或一条明确链路。
- `verify-all.sh` 只做编排和汇总，不实现具体业务验收逻辑。
- 脚本之间通过环境变量传参，例如 `BASE`、`AUTH_TOKEN`、`PORT`。
- 子脚本失败时，编排脚本应继续收集可执行结果，并最终以非零退出码结束。

## 幂等约定

- 脚本必须支持重复运行。
- 重复运行不应因为已存在数据、topic、index、用户或端口而异常失败。
- 启动类脚本应先检测已有实例或健康检查状态，再决定启动或复用。
- 验收数据统一使用可识别前缀，例如 `accept_p2_`，便于清理和排查。

## 验收脚本用例约定

- 用例 ID 必须与需求或验收文档一致，例如：
  - `TC-P2-RBAC-001`
  - `TC-P1-PLUGIN-001`
  - `TC-P0-SEARCH-001`
- 每个断言失败必须输出：
  - 用例 ID
  - 请求或操作摘要
  - 预期结果
  - 实际结果
- API 验收优先使用 `curl + jq`。
- 复杂流程可使用 Python，但脚本入口仍应保持 Bash 编排。
- 页面验收使用 Playwright，不用 Shell 模拟浏览器行为。

## 页面验收约定（Playwright）

- 项目根目录：`scripts/playwright/`
- 只安装 chromium，不装 firefox/webkit
- 登录态通过 storageState 复用，不每个用例重新登录
- 用例 ID 与人工验收文档一致（TC-P2-RBAC-001）
- selector 优先用 data-testid，不用 CSS 类名或 XPath
- 报告输出到 `scripts/playwright/reports/`
- 如本机 Playwright 下载的 Chromium 无法启动，可临时指定系统浏览器 channel，例如 `XDP_PLAYWRIGHT_CHANNEL=chrome npm test -- --project=admin tests/parse-config.spec.ts --reporter=list`。
- `env-clean.spec.ts` 是特殊干净环境用例，会自行执行 `scripts/start-oneclick.sh --clean` 并重启本地服务；运行时使用 `--project=clean-env`，不要使用 `--project=admin`。
- `admin` project 依赖登录态 setup，适用于服务已启动后的页面验收；`clean-env` project 不依赖登录态 setup，适用于从空环境启动并检查清理语义。

## 脚本清单

> 完整脚本列表见 `ls scripts/*.sh`。本表只列出验证和验收相关脚本。

| 脚本 | 用途 |
|---|---|
| `start-oneclick.sh` | 一键启动完整环境 |
| `verify-mvp.sh` | MVP 闭环验证 |
| `acceptance.sh` | 验收入口或通用验收编排 |
| `real-e2e.sh` | 真实端到端链路验证 |
| `reset-test-env.sh` | 重置测试环境 |
| `writer-benchmark.sh` | Writer 入库压测 |
| `writer-recovery-sample.sh` | Writer 恢复样例验证 |
| `migrate-clickhouse.sh` | ClickHouse 迁移执行 |
| `sync-github-source.sh` | GitHub 发布源同步 |
| `test-clickhouse-migration-parse-status.sh` | parse_status 迁移测试 |
| `test-docker-timezone.sh` | Docker 时区测试 |
| `test-start-oneclick.sh` | start-oneclick.sh --dry-run 输出测试 |
| `demo-syslog-collection.sh` | Syslog 采集演示 |
| `verify-rbac.sh` | RBAC 越权测试，待补 |
| `verify-all.sh` | 全量验收编排，待补 |

### Playwright 页面级 E2E 脚本

> 完整脚本列表见 `ls scripts/playwright/tests/*.spec.ts`。运行前置：`npx playwright install chromium` + 服务已启动。配置和可观测性约定见本文件「页面验收约定（Playwright）」节。

| 脚本 | 用途 | 用例数 |
|---|---|---|
| `playwright/tests/auth.setup.ts` | 登录态 setup，复用 storageState | setup |
| `playwright/tests/auth-login.spec.ts` | 登录 / 登出端到端 | 4 |
| `playwright/tests/nav-layout.spec.ts` | 导航 / 布局 / 刷新一致性 | 6 |
| `playwright/tests/env-clean.spec.ts` | 干净环境启动与脏数据清理 | 1 |
| `playwright/tests/index-config.spec.ts` | 索引配置端到端 | 7 |
| `playwright/tests/collect-config.spec.ts` | 采集配置端到端 | 9 |
| `playwright/tests/parse-config.spec.ts` | 解析配置端到端 | 6 |
| `playwright/tests/data-flow-stats.spec.ts` | 索引采集解析搜索 stats 端到端 | 5 |
| `playwright/tests/search-page.spec.ts` | 搜索页端到端 | 12 |
| `playwright/tests/search-command-pipe.spec.ts` | 搜索增强命令管道端到端 | 11 |
| `playwright/tests/plugin-management.spec.ts` | 插件管理端到端 | 8 |
| `playwright/tests/writer-page.spec.ts` | Writer 入库页面端到端 | 9 |
| `playwright/tests/kafka-collect.spec.ts` | Kafka 采集 + JSON Parser 端到端 | 6 |
| `playwright/tests/router-navigation.spec.ts` | P3 vue-router 导航、权限守卫和面板拆分回归 | 11 |
| `playwright/tests/rbac-permissions.spec.ts` | P2 RBAC 菜单、index scope、plugin scope 和 API 403 综合验收 | 5 |
| `playwright/tests/rbac-ui-layout.spec.ts` | P2 RBAC 新建用户 / 角色模态框、用户 / 角色 CRUD 按钮和基础表格对齐 UI 回归 | 8 |

运行示例：`cd scripts/playwright && npx playwright test tests/auth-login.spec.ts --project=admin`
