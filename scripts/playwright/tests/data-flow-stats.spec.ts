/**
 * 数据链路端到端验收脚本
 *
 * 覆盖用例：TC-P0-E2E-001 ~ TC-P0-E2E-005
 *
 * 验收链路：
 *   登录态复用 → 页面新增 index → 页面新增 Syslog 采集源
 *   → 页面新增 Regex 解析规则 → 发送 UDP Syslog 测试数据
 *   → 搜索页执行 stats SPL 并验证统计结果
 *
 * 运行：npx playwright test tests/data-flow-stats.spec.ts --project=admin
 */
import dgram from 'node:dgram';
import type { AddressInfo } from 'node:net';
import { test, expect, type Page } from '@playwright/test';

const API_URL = process.env.XDP_API_URL || 'http://127.0.0.1:8080';
const RUN_ID = Date.now();
const INDEX_NAME = `accept_e2e_${RUN_ID}`;
const DATA_SOURCE_NAME = `accept_e2e_syslog_${RUN_ID}`;
const RULE_NAME = `accept_e2e_regex_${RUN_ID}`;
const SAMPLE_LOG = 'src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048';
const EXTRA_LOG = 'src=10.0.1.8 dst=172.16.0.5 action=deny bytes=1024';
const REGEX_PATTERN = String.raw`src=(?<src_ip>\S+)\s+dst=(?<dst_ip>\S+)\s+action=(?<action>\S+)\s+bytes=(?<bytes>\d+)`;
const STATS_SPL = `index=${INDEX_NAME} | stats count as total sum(bytes) as total_bytes by src_ip action`;

let dataSourcePort = 0;
const SYSLOG_PORT_MIN = Number(process.env.XDP_E2E_SYSLOG_PORT_MIN || 20000);
const SYSLOG_PORT_MAX = Number(process.env.XDP_E2E_SYSLOG_PORT_MAX || 40000);

type UDPReservation = {
  port: number;
  close: () => Promise<void>;
};

async function reserveUDPPort(): Promise<UDPReservation> {
  const socket = dgram.createSocket('udp4');
  await new Promise<void>((resolve, reject) => {
    socket.once('error', reject);
    socket.bind(0, '127.0.0.1', resolve);
  });
  const address = socket.address() as AddressInfo;
  return {
    port: address.port,
    close: () =>
      new Promise<void>((resolve) => {
        socket.close(() => resolve());
      }),
  };
}

async function tryReserveUDPPort(port: number): Promise<UDPReservation | null> {
  const socket = dgram.createSocket('udp4');
  try {
    await new Promise<void>((resolve, reject) => {
      socket.once('error', reject);
      socket.bind(port, '127.0.0.1', resolve);
    });
    return {
      port,
      close: () =>
        new Promise<void>((resolve) => {
          socket.close(() => resolve());
        }),
    };
  } catch {
    try {
      socket.close();
    } catch {
      // Socket may not be running when bind fails.
    }
    return null;
  }
}

async function findFreeUDPPort(): Promise<number> {
  for (let offset = 0; offset <= SYSLOG_PORT_MAX - SYSLOG_PORT_MIN; offset += 1) {
    const candidate = SYSLOG_PORT_MIN + ((Date.now() + offset) % (SYSLOG_PORT_MAX - SYSLOG_PORT_MIN + 1));
    const reservation = await tryReserveUDPPort(candidate);
    if (!reservation) continue;
    await reservation.close();
    return reservation.port;
  }
  const reservation = await reserveUDPPort();
  await reservation.close();
  return reservation.port;
}

async function sendUDP(port: number, messages: string[]) {
  const socket = dgram.createSocket('udp4');
  try {
    for (const message of messages) {
      await new Promise<void>((resolve, reject) => {
        socket.send(Buffer.from(message), port, '127.0.0.1', (error) => {
          if (error) reject(error);
          else resolve();
        });
      });
    }
  } finally {
    socket.close();
  }
}

async function sendUDPWithRetries(port: number, messages: string[], attempts = 6, intervalMs = 1000) {
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    await sendUDP(port, messages);
    if (attempt < attempts - 1) {
      await new Promise((resolve) => setTimeout(resolve, intervalMs));
    }
  }
}

async function authHeaders(page: Page) {
  const token = await page.evaluate(() => localStorage.getItem('xdp_api_token') || '');
  return { Authorization: `Bearer ${token}` };
}

async function requestJSON(page: Page, method: 'GET' | 'POST' | 'DELETE', path: string) {
  const headers = await authHeaders(page);
  const response = await page.request.fetch(`${API_URL}${path}`, { method, headers });
  if (!response.ok()) {
    throw new Error(`${method} ${path} failed: ${response.status()} ${await response.text()}`);
  }
  const text = await response.text();
  return text ? JSON.parse(text) : {};
}

async function cleanupParseRuleByName(page: Page, name: string) {
  const payload = await requestJSON(page, 'GET', '/api/v1/parse-rules?page=1&page_size=1000').catch(() => ({ parse_rules: [] }));
  const rules = Array.isArray(payload.parse_rules) ? payload.parse_rules : [];
  for (const rule of rules) {
    if (String(rule.name || '').trim() !== name) continue;
    const id = String(rule.id || rule.code || '').trim();
    if (id) await requestJSON(page, 'DELETE', `/api/v1/parse-rules/${encodeURIComponent(id)}`).catch(() => undefined);
  }
}

async function cleanupDataSourceByName(page: Page, name: string) {
  const payload = await requestJSON(page, 'GET', '/api/v1/datasources?page=1&page_size=1000').catch(() => ({ datasources: [] }));
  const sources = Array.isArray(payload.datasources) ? payload.datasources : [];
  for (const source of sources) {
    if (String(source.name || '').trim() !== name) continue;
    const id = String(source.id || source.code || '').trim();
    if (id) await requestJSON(page, 'DELETE', `/api/v1/datasources/${encodeURIComponent(id)}`).catch(() => undefined);
  }
}

async function cleanupIndex(page: Page, indexName: string) {
  await requestJSON(page, 'DELETE', `/api/v1/indexes?index=${encodeURIComponent(indexName)}&drop_storage=true`).catch(() => undefined);
}

async function cleanupE2E(page: Page) {
  await cleanupParseRuleByName(page, RULE_NAME);
  await cleanupDataSourceByName(page, DATA_SOURCE_NAME);
  await cleanupIndex(page, INDEX_NAME);
}

async function openModule(page: Page, module: 'index' | 'collect' | 'parse' | 'search') {
  await page.goto('/');
  await expect(page.locator('[data-testid="logout"]')).toBeVisible();
  await page.click(`[data-testid="nav-${module}"]`);
  await expect(page.locator(`[data-testid="${module}-page"]`)).toBeVisible();
}

function indexRow(page: Page) {
  return page.locator(`tr:has-text("${INDEX_NAME}")`).first();
}

function dataSourceRow(page: Page) {
  return page.locator(`tr:has-text("${DATA_SOURCE_NAME}")`).first();
}

function parseRuleRow(page: Page) {
  return page.locator(`tr:has-text("${RULE_NAME}")`).first();
}

async function openFormByButton(page: Page, buttonTestID: string, formTestID: string) {
  const button = page.locator(`[data-testid="${buttonTestID}"]`);
  const form = page.locator(`[data-testid="${formTestID}"]`);

  await expect(button).toBeVisible();
  await expect(button).toBeEnabled();
  await expect(async () => {
    if (!(await form.isVisible().catch(() => false))) {
      await button.click();
    }
    await expect(form).toBeVisible({ timeout: 1_000 });
  }).toPass({ timeout: 10_000, intervals: [250, 500, 1000] });
}

async function createIndexByPage(page: Page) {
  console.log('== phase == open index module');
  await openModule(page, 'index');
  await page.locator('[data-testid="index-page-size"]').selectOption('1000');
  await openFormByButton(page, 'show-index-form', 'index-form-card');
  console.log('== phase == fill index form');
  await page.fill('[data-testid="index-name"]', INDEX_NAME);
  await page.fill('[data-testid="index-ttl"]', '7');
  await page.locator('[data-testid="index-status"]').selectOption('active');
  console.log('== phase == submit index form');
  await page.click('[data-testid="index-page"] form button[type="submit"]');
}

async function createSyslogDataSourceByPage(page: Page) {
  await openModule(page, 'collect');
  await page.locator('[data-testid="collect-page-size"]').selectOption('1000');
  await openFormByButton(page, 'show-input-form', 'input-form-card');
  const form = page.locator('[data-testid="input-form-card"]');
  const syslogPlugin = form.locator('[data-testid="input-plugin-syslog"]');
  if (await syslogPlugin.isVisible().catch(() => false)) {
    await syslogPlugin.click();
  }
  await expect(form.locator('input[placeholder="5514"]')).toBeVisible();
  await form.locator('input[placeholder="请输入设备名称"]').fill(DATA_SOURCE_NAME);
  await form.locator('input[placeholder="5514"]').fill(String(dataSourcePort));
  await page.click('[data-testid="collect-page"] form button[type="submit"]');
}

async function createRegexParseRuleByPage(page: Page) {
  await openModule(page, 'collect');
  await expect(dataSourceRow(page)).toBeVisible();
  await page.click('[data-testid="nav-parse"]');
  await expect(page.locator('[data-testid="parse-page"]')).toBeVisible();
  await page.locator('[data-testid="parse-page-size"]').selectOption('1000');
  await openFormByButton(page, 'show-rule-form', 'rule-form-card');
  await page.fill('[data-testid="rule-name"]', RULE_NAME);
  await page.locator('[data-testid="rule-source"]').selectOption(DATA_SOURCE_NAME);
  await page.locator('[data-testid="rule-output-index"]').selectOption(INDEX_NAME);
  await page.fill('[data-testid="rule-priority"]', '10');
  await page.click('[data-testid="parser-regex"]');
  await page.fill('[data-testid="sample-log"]', SAMPLE_LOG);
  await page.fill('[data-testid="regex-pattern"]', REGEX_PATTERN);
  await expect(page.locator('[data-testid="props-conf"]')).toHaveValue(/EXTRACT-custom/);
  await page.click('[data-testid="preview-parse"]');
  await expect(page.locator('[data-testid="parse-preview"]')).toContainText('src_ip');
  await expect(page.locator('[data-testid="parse-preview"]')).toContainText('bytes');
  const [response] = await Promise.all([
    page.waitForResponse((res) => {
      const url = new URL(res.url());
      return url.pathname === '/api/v1/parse-rules' && res.request().method() === 'POST';
    }),
    page.locator('[data-testid="rule-form-card"] button[type="submit"]').click(),
  ]);
  if (!response.ok()) {
    throw new Error(`POST /api/v1/parse-rules failed: ${response.status()} ${await response.text()}`);
  }
}

async function runStatsSearchByPage(page: Page) {
  await openModule(page, 'search');
  await page.locator('[data-testid="search-page-size"]').selectOption('20');
  await page.fill('[data-testid="search-query"]', STATS_SPL);
  await page.locator('[data-testid="search-time"]').selectOption('所有时间');
  await page.click('[data-testid="search-button"]');
  await expect(page.locator('[data-testid="result-mode"]')).toContainText('统计视图', { timeout: 15_000 });
  return (await page.locator('[data-testid="search-results"]').innerText()).trim();
}

async function runStatsSearchAfterFreshSyslogSend(page: Page) {
  // Agent/worker 配置热加载存在短暂窗口；搜索轮询期间持续补发样例，避免前置发送全部落在 pipeline 生效前。
  await sendUDP(dataSourcePort, [SAMPLE_LOG, EXTRA_LOG]);
  return runStatsSearchByPage(page);
}

test.describe('TC-P0-E2E 索引采集解析搜索 stats 页面端到端', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeAll(async ({ browser }) => {
    dataSourcePort = await findFreeUDPPort();
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      await page.goto('/');
      await expect(page.locator('[data-testid="logout"]')).toBeVisible();
      await cleanupE2E(page);
    } finally {
      await context.close();
    }
  });

  test('TC-P0-E2E-001 页面新增 index 并展示物理表', async ({ page }) => {
    console.log('== phase == TC-P0-E2E-001 create index via page');
    await createIndexByPage(page);

    console.log('== phase == TC-P0-E2E-001 assert index row visible');
    const row = indexRow(page);
    await expect(row, '[TC-P0-E2E-001] 预期索引行出现').toBeVisible({ timeout: 10_000 });
    await expect(row, '[TC-P0-E2E-001] 预期行含索引名').toContainText(INDEX_NAME);
    await expect(row, '[TC-P0-E2E-001] 预期行含物理表名').toContainText(`events_${INDEX_NAME}`);
    await expect(row, '[TC-P0-E2E-001] 预期行含 TTL 7d').toContainText('7d');
    await expect(row, '[TC-P0-E2E-001] 预期行含 active 状态').toContainText('active');
  });

  test('TC-P0-E2E-002 页面新增 Syslog 采集配置并启动监听', async ({ page }) => {
    console.log('== phase == TC-P0-E2E-002 create syslog datasource via page');
    await createSyslogDataSourceByPage(page);

    console.log('== phase == TC-P0-E2E-002 assert datasource row visible');
    const row = dataSourceRow(page);
    await expect(row, '[TC-P0-E2E-002] 预期采集源行出现').toBeVisible({ timeout: 10_000 });
    await expect(row, '[TC-P0-E2E-002] 预期行含 Syslog 插件').toContainText('Syslog');
    await expect(row, '[TC-P0-E2E-002] 预期行含监听端口').toContainText(String(dataSourcePort));
    await expect(row, '[TC-P0-E2E-002] 预期行含运行状态').toContainText(/运行中|已停止|异常|未知/);
    await expect(row.locator('[data-testid^="toggle-input-"]').first(), '[TC-P0-E2E-002] 预期存在启停按钮').toBeVisible();
  });

  test('TC-P0-E2E-003 页面新增 Regex 解析规则并绑定采集源与 index', async ({ page }) => {
    console.log('== phase == TC-P0-E2E-003 create regex parse rule via page');
    await createRegexParseRuleByPage(page);

    console.log('== phase == TC-P0-E2E-003 assert parse rule row visible');
    const row = parseRuleRow(page);
    await expect(row, '[TC-P0-E2E-003] 预期解析规则行出现').toBeVisible({ timeout: 10_000 });
    await expect(row, '[TC-P0-E2E-003] 预期行含正则解析插件').toContainText('正则解析插件');
    await expect(row, '[TC-P0-E2E-003] 预期行关联采集源').toContainText(DATA_SOURCE_NAME);
    await expect(row, '[TC-P0-E2E-003] 预期行关联 index').toContainText(INDEX_NAME);
    await expect(row, '[TC-P0-E2E-003] 预期行含 EXTRACT-custom').toContainText('EXTRACT-custom');
  });

  test('TC-P0-E2E-004 发送 Syslog 日志到页面配置的监听端口', async ({ page }) => {
    console.log('== phase == TC-P0-E2E-004 send syslog with retries during agent hot reload window');
    await sendUDPWithRetries(dataSourcePort, [SAMPLE_LOG, EXTRA_LOG]);
    console.log('== phase == TC-P0-E2E-004 assert runtime detail expand');
    await openModule(page, 'collect');
    const row = dataSourceRow(page);
    await expect(row, '[TC-P0-E2E-004] 预期采集源行出现').toBeVisible();
    await row.locator('[data-testid^="collect-expand-"]').first().click();
    await expect(page.locator('[data-testid="collect-runtime-detail"]'), '[TC-P0-E2E-004] 预期展开后显示运行详情').toBeVisible({ timeout: 10_000 });
  });

  test('TC-P0-E2E-005 搜索页执行 stats 并展示解析字段统计结果', async ({ page }) => {
    test.setTimeout(120_000);
    console.log('== phase == TC-P0-E2E-005 poll stats search until src_ip=10.0.1.8 appears');
    let statsText = '';
    await expect
      .poll(
        async () => {
          statsText = await runStatsSearchAfterFreshSyslogSend(page);
          return statsText;
        },
        {
          timeout: 90_000,
          intervals: [1000, 2000, 3000, 5000],
          message: '[TC-P0-E2E-005] 等待 Syslog 数据完成解析、入库并能被 stats 查询命中；预期 src_ip=10.0.1.8 出现在统计结果',
        },
      )
      .toContain('10.0.1.8');

    console.log(`== phase == TC-P0-E2E-005 stats returned:\n${statsText}`);

    const results = page.locator('[data-testid="search-results"]');
    await expect(results, '[TC-P0-E2E-005] 预期统计结果含 src_ip 字段').toContainText('src_ip');
    await expect(results, '[TC-P0-E2E-005] 预期统计结果含 action 字段').toContainText('action');
    await expect(results, '[TC-P0-E2E-005] 预期统计结果含 total 聚合列').toContainText('total');
    await expect(results, '[TC-P0-E2E-005] 预期统计结果含 total_bytes 聚合列').toContainText('total_bytes');
    await expect(results, '[TC-P0-E2E-005] 预期统计结果含 deny 行').toContainText('deny');
  });

  test.afterAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      await page.goto('/');
      await expect(page.locator('[data-testid="logout"]')).toBeVisible();
      await cleanupE2E(page);
    } finally {
      await context.close();
    }
  });
});
