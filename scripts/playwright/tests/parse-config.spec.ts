/**
 * 解析配置端到端验收脚本
 *
 * 覆盖用例：TC-P0-PARSE-001 ~ TC-P0-PARSE-006
 *
 * 验收链路：
 *   登录态复用 → API 准备采集源与 index → 进入解析页 → 新增正则解析规则
 *   → 预览解析结果 → 保存成功 → 修改规则 → 删除清理
 *
 * 运行：npx playwright test tests/parse-config.spec.ts --project=admin
 */
import dgram from 'node:dgram';
import type { AddressInfo } from 'node:net';
import { test, expect, type Page } from '@playwright/test';

const API_URL = process.env.XDP_API_URL || 'http://127.0.0.1:8080';
const RUN_ID = Date.now();
const RUN_SUFFIX = String(RUN_ID).slice(-10);
const DATA_SOURCE_ID = `pds-${RUN_SUFFIX}`;
const DATA_SOURCE_NAME = `accept_p0_parse_source_${RUN_ID}`;
const INDEX_NAME = `accept_parse_${RUN_ID}`;
const RULE_NAME = `accept_p0_regex_rule_${RUN_ID}`;
const UPDATED_RULE_NAME = `${RULE_NAME}_updated`;
const SAMPLE_LOG = 'src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048';
const UPDATED_SAMPLE_LOG = 'src=10.0.1.9 dst=172.16.0.5 action=allow bytes=4096';
const REGEX_PATTERN = String.raw`src=(?<src_ip>\S+)\s+dst=(?<dst_ip>\S+)\s+action=(?<action>\S+)\s+bytes=(?<bytes>\d+)`;

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

async function authHeaders(page: Page) {
  const token = await page.evaluate(() => localStorage.getItem('xdp_api_token') || '');
  return { Authorization: `Bearer ${token}` };
}

async function requestJSON(page: Page, method: 'GET' | 'POST' | 'DELETE', path: string, data?: unknown) {
  const headers = await authHeaders(page);
  const response = await page.request.fetch(`${API_URL}${path}`, {
    method,
    headers: { ...headers, ...(data ? { 'Content-Type': 'application/json' } : {}) },
    data,
  });
  if (!response.ok()) {
    throw new Error(`${method} ${path} failed: ${response.status()} ${await response.text()}`);
  }
  const text = await response.text();
  return text ? JSON.parse(text) : {};
}

async function cleanupParseRulesByName(page: Page, names: string[]) {
  const payload = await requestJSON(page, 'GET', '/api/v1/parse-rules?page=1&page_size=1000').catch(() => ({ parse_rules: [] }));
  const rules = Array.isArray(payload.parse_rules) ? payload.parse_rules : [];
  const targetNames = new Set(names.map((name) => name.trim()));
  for (const rule of rules) {
    if (!targetNames.has(String(rule.name || '').trim())) continue;
    const id = String(rule.id || rule.code || '').trim();
    if (!id) continue;
    await requestJSON(page, 'DELETE', `/api/v1/parse-rules/${encodeURIComponent(id)}`).catch(() => undefined);
  }
}

async function cleanupDataSourceByName(page: Page, name: string) {
  const payload = await requestJSON(page, 'GET', '/api/v1/datasources?page=1&page_size=1000').catch(() => ({ datasources: [] }));
  const sources = Array.isArray(payload.datasources) ? payload.datasources : [];
  for (const source of sources) {
    if (String(source.name || '').trim() !== name) continue;
    const id = String(source.id || source.code || '').trim();
    if (!id) continue;
    await requestJSON(page, 'DELETE', `/api/v1/datasources/${encodeURIComponent(id)}`).catch(() => undefined);
  }
}

async function cleanupIndex(page: Page, indexName: string) {
  await requestJSON(page, 'DELETE', `/api/v1/indexes?index=${encodeURIComponent(indexName)}&drop_storage=true`).catch(() => undefined);
}

async function preparePrerequisites(page: Page) {
  await cleanupParseRulesByName(page, [RULE_NAME, UPDATED_RULE_NAME]);
  await cleanupDataSourceByName(page, DATA_SOURCE_NAME);
  await cleanupIndex(page, INDEX_NAME);

  await requestJSON(page, 'POST', '/api/v1/indexes', {
    index_name: INDEX_NAME,
    ttl_days: 7,
    status: 'active',
  });

  await requestJSON(page, 'POST', '/api/v1/datasources', {
    id: DATA_SOURCE_ID,
    code: DATA_SOURCE_ID,
    name: DATA_SOURCE_NAME,
    plugin_code: 'syslog',
    status: 'disabled',
    plugin_config: {
      collector_port: dataSourcePort,
      transport_protocol: 'UDP',
      encoding: 'UTF-8',
      log_filter_enabled: false,
    },
  });
}

async function openParsePage(page: Page) {
  await page.goto('/');
  await expect(page.locator('[data-testid="logout"]')).toBeVisible();
  await page.click('[data-testid="nav-collect"]');
  await expect(page.locator('[data-testid="collect-page"]')).toBeVisible();
  await page.click('[data-testid="nav-parse"]');
  await expect(page.locator('[data-testid="parse-page"]')).toBeVisible();
  await page.locator('[data-testid="parse-page-size"]').selectOption('1000');
}

function parseRuleRow(page: Page, name: string) {
  return page.locator(`tr:has-text("${name}")`).first();
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

async function fillRegexRuleForm(page: Page, name: string, sampleLog = SAMPLE_LOG) {
  await page.fill('[data-testid="rule-name"]', name);
  await page.locator('[data-testid="rule-source"]').selectOption(DATA_SOURCE_NAME);
  await page.locator('[data-testid="rule-output-index"]').selectOption(INDEX_NAME);
  await page.fill('[data-testid="rule-priority"]', '10');
  await page.click('[data-testid="parser-regex"]');
  await page.fill('[data-testid="sample-log"]', sampleLog);
  await page.fill('[data-testid="regex-pattern"]', REGEX_PATTERN);
  await expect(page.locator('[data-testid="props-conf"]')).toHaveValue(/EXTRACT-custom/);
}

test.describe('TC-P0-PARSE 解析配置端到端', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeAll(async ({ browser }) => {
    dataSourcePort = await findFreeUDPPort();
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      await page.goto('/');
      await expect(page.locator('[data-testid="logout"]')).toBeVisible();
      await preparePrerequisites(page);
    } finally {
      await context.close();
    }
  });

  test.beforeEach(async ({ page }) => {
    await openParsePage(page);
  });

  test('TC-P0-PARSE-001 进入解析配置页面', async ({ page }) => {
    console.log('== phase == TC-P0-PARSE-001 assert parse page visible');
    const parsePage = page.locator('[data-testid="parse-page"]');
    await expect(parsePage.getByRole('heading', { name: /解析配置/ }), '[TC-P0-PARSE-001] 预期展示解析配置标题').toBeVisible();
    await expect(parsePage.getByText('规则列表'), '[TC-P0-PARSE-001] 预期展示规则列表').toBeVisible();
  });

  test('TC-P0-PARSE-002 点击新增后展示解析表单，必填项校验生效', async ({ page }) => {
    console.log('== phase == TC-P0-PARSE-002 open form and assert required validation');
    await expect(page.locator('[data-testid="rule-form-card"]')).not.toBeVisible();

    await openFormByButton(page, 'show-rule-form', 'rule-form-card');
    const form = page.locator('[data-testid="rule-form-card"]');
    await expect(form, '[TC-P0-PARSE-002] 预期点击新增后表单可见').toBeVisible();
    await expect(form.getByText('新增规则', { exact: true }), '[TC-P0-PARSE-002] 预期表单含新增规则标题').toBeVisible();
    await expect(page.locator('[data-testid="parser-regex"]'), '[TC-P0-PARSE-002] 预期正则解析默认选中').toHaveClass(/active/);

    await page.click('[data-testid="parse-page"] form button[type="submit"]');
    const ruleNameMissing = await page.locator('[data-testid="rule-name"]').evaluate((el: HTMLInputElement) => el.validity.valueMissing);
    await expect(page.locator('[data-testid="rule-form-card"]'), '[TC-P0-PARSE-002] 预期原生必填校验拦截后表单仍可见').toBeVisible();
    expect(ruleNameMissing, '[TC-P0-PARSE-002] 预期规则名称触发必填校验').toBe(true);

    await page.click('[data-testid="cancel-rule-form"]');
    await expect(page.locator('[data-testid="rule-form-card"]'), '[TC-P0-PARSE-002] 预期取消后表单不可见').not.toBeVisible();
  });

  test('TC-P0-PARSE-003 正则解析预览展示提取字段', async ({ page }) => {
    console.log('== phase == TC-P0-PARSE-003 fill regex rule and preview parse');
    await openFormByButton(page, 'show-rule-form', 'rule-form-card');
    await fillRegexRuleForm(page, RULE_NAME);

    await page.click('[data-testid="preview-parse"]');

    const preview = page.locator('[data-testid="parse-preview"]');
    await expect(preview, '[TC-P0-PARSE-003] 预期预览含 src_ip').toContainText('src_ip');
    await expect(preview, '[TC-P0-PARSE-003] 预期预览含 10.0.1.8').toContainText('10.0.1.8');
    await expect(preview, '[TC-P0-PARSE-003] 预期预览含 dst_ip').toContainText('dst_ip');
    await expect(preview, '[TC-P0-PARSE-003] 预期预览含 172.16.0.4').toContainText('172.16.0.4');
    await expect(preview, '[TC-P0-PARSE-003] 预期预览含 action').toContainText('action');
    await expect(preview, '[TC-P0-PARSE-003] 预期预览含 deny').toContainText('deny');
    await expect(preview, '[TC-P0-PARSE-003] 预期预览含 bytes').toContainText('bytes');
    await expect(preview, '[TC-P0-PARSE-003] 预期预览含 2048').toContainText('2048');
  });

  test('TC-P0-PARSE-004 保存正则解析规则成功，规则列表展示绑定关系', async ({ page }) => {
    console.log('== phase == TC-P0-PARSE-004 submit regex parse rule');
    await cleanupParseRulesByName(page, [RULE_NAME, UPDATED_RULE_NAME]);
    await openFormByButton(page, 'show-rule-form', 'rule-form-card');
    await fillRegexRuleForm(page, RULE_NAME);

    await page.click('[data-testid="parse-page"] form button[type="submit"]');

    console.log('== phase == TC-P0-PARSE-004 assert parse rule row visible');
    const row = parseRuleRow(page, RULE_NAME);
    await expect(row, '[TC-P0-PARSE-004] 预期规则行出现').toBeVisible({ timeout: 10_000 });
    await expect(row, '[TC-P0-PARSE-004] 预期行含正则解析插件').toContainText('正则解析插件');
    await expect(row, '[TC-P0-PARSE-004] 预期行关联采集源').toContainText(DATA_SOURCE_NAME);
    await expect(row, '[TC-P0-PARSE-004] 预期行关联 index').toContainText(INDEX_NAME);
    await expect(row, '[TC-P0-PARSE-004] 预期行含 EXTRACT-custom').toContainText('EXTRACT-custom');
  });

  test('TC-P0-PARSE-005 修改解析规则后列表同步更新', async ({ page }) => {
    console.log('== phase == TC-P0-PARSE-005 edit parse rule and assert sync');
    const row = parseRuleRow(page, RULE_NAME);
    await expect(row, '[TC-P0-PARSE-005] 预期规则行出现').toBeVisible();

    await row.locator('button', { hasText: '修改' }).first().click();
    await expect(page.locator('[data-testid="rule-form-card"]'), '[TC-P0-PARSE-005] 预期修改后表单可见').toBeVisible();
    await expect(page.locator('[data-testid="rule-name"]'), '[TC-P0-PARSE-005] 预期规则名回填').toHaveValue(RULE_NAME);

    await page.fill('[data-testid="rule-name"]', UPDATED_RULE_NAME);
    await page.fill('[data-testid="sample-log"]', UPDATED_SAMPLE_LOG);
    await page.fill('[data-testid="rule-priority"]', '20');
    await page.click('[data-testid="preview-parse"]');
    await expect(page.locator('[data-testid="parse-preview"]'), '[TC-P0-PARSE-005] 预期修改后预览含 4096').toContainText('4096');

    await page.click('[data-testid="parse-page"] form button[type="submit"]');

    console.log('== phase == TC-P0-PARSE-005 assert updated row visible');
    const updatedRow = parseRuleRow(page, UPDATED_RULE_NAME);
    await expect(updatedRow, '[TC-P0-PARSE-005] 预期更新后行出现').toBeVisible({ timeout: 10_000 });
    await expect(updatedRow, '[TC-P0-PARSE-005] 预期行含优先级 20').toContainText('20');
  });

  test('TC-P0-PARSE-006 删除解析规则后列表不再展示', async ({ page }) => {
    console.log('== phase == TC-P0-PARSE-006 delete parse rule and assert row gone');
    const row = parseRuleRow(page, UPDATED_RULE_NAME);
    await expect(row, '[TC-P0-PARSE-006] 预期规则行出现').toBeVisible();

    await row.locator('button', { hasText: '删除' }).first().click();

    await expect(parseRuleRow(page, UPDATED_RULE_NAME), '[TC-P0-PARSE-006] 预期删除后行不可见').not.toBeVisible({ timeout: 10_000 });
  });

  test.afterAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      await page.goto('/');
      await expect(page.locator('[data-testid="logout"]')).toBeVisible();
      await cleanupParseRulesByName(page, [RULE_NAME, UPDATED_RULE_NAME]);
      await cleanupDataSourceByName(page, DATA_SOURCE_NAME);
      await cleanupIndex(page, INDEX_NAME);
    } finally {
      await context.close();
    }
  });
});
