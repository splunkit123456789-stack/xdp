/**
 * 搜索增强命令管道端到端验收脚本
 *
 * 对应人工验收文档：docs/requirements/references/XDP_P1搜索增强命令人工验收测试用例.md
 * 覆盖用例：TC-P1-SEARCH-MANUAL-001 ~ TC-P1-SEARCH-MANUAL-011
 *
 * 验收链路：
 *   登录态复用 → API 预置 index → ClickHouse 直接写入 3 条确定性事件
 *   → table 字段投影 → sort 升降序 → head 截断 → dedup 单/多字段去重
 *   → stats 后继续管道 → 管道执行顺序 → 非法参数提示
 *
 * 运行：npx playwright test tests/search-command-pipe.spec.ts --project=admin
 *
 * 说明：table/sort/head/dedup 为外部 Search Command Plugin；本脚本在 beforeAll 中导入并启用插件包，
 *       不依赖插件管理页面用例的历史状态。搜索命令专项测试不再依赖 Syslog/Agent 热加载链路，
 *       采集解析入库闭环由 data-flow-stats.spec.ts 覆盖。
 */
import { Buffer } from 'node:buffer';
import { readFileSync } from 'node:fs';
import { basename, join } from 'node:path';
import { test, expect, type Page } from '@playwright/test';

const API_URL = process.env.XDP_API_URL || 'http://127.0.0.1:8080';
const CLICKHOUSE_URL = process.env.XDP_CLICKHOUSE_URL || process.env.CLICKHOUSE_URL || 'http://127.0.0.1:8123';
const CLICKHOUSE_DB = process.env.XDP_CLICKHOUSE_DB || process.env.CLICKHOUSE_DB || 'xdp';
const CLICKHOUSE_USER = process.env.XDP_CLICKHOUSE_USERNAME || process.env.CLICKHOUSE_USER || 'xdp';
const CLICKHOUSE_PASSWORD = process.env.XDP_CLICKHOUSE_PASSWORD || process.env.CLICKHOUSE_PASSWORD || 'xdp';
const RUN_ID = Date.now();
const INDEX_NAME = `accept_search_cmd_${RUN_ID}`;
const SEARCH_COMMAND_PLUGIN_PACKAGES = [
  { code: 'table', file: 'table-search-command-sample.zip' },
  { code: 'sort', file: 'sort-search-command-sample.zip' },
  { code: 'head', file: 'head-search-command-sample.zip' },
  { code: 'dedup', file: 'dedup-search-command-sample.zip' },
];

// 参考文档 3.4 节规定的三条测试日志
const TEST_LOGS = [
  'src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048',
  'src=10.0.1.8 dst=172.16.0.5 action=deny bytes=4096',
  'src=10.0.1.9 dst=172.16.0.6 action=allow bytes=1024',
];

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

async function importAndEnableSearchCommandPlugins(page: Page) {
  const headers = await authHeaders(page);
  for (const plugin of SEARCH_COMMAND_PLUGIN_PACKAGES) {
    const packagePath = join(process.cwd(), '..', '..', 'build', 'plugin-packages', plugin.file);
    const importResponse = await page.request.fetch(`${API_URL}/api/v1/plugins/import?overwrite=true`, {
      method: 'POST',
      headers,
      multipart: {
        file: {
          name: basename(packagePath),
          mimeType: 'application/zip',
          buffer: readFileSync(packagePath),
        },
      },
    });
    if (!importResponse.ok()) {
      throw new Error(`import search command plugin ${plugin.code} failed: ${importResponse.status()} ${await importResponse.text()}`);
    }
    await requestJSON(page, 'POST', `/api/v1/plugins/${encodeURIComponent(plugin.code)}/enable?plugin_type=search_command`);
  }
}

function clickHouseURL() {
  return `${CLICKHOUSE_URL.replace(/\/+$/, '')}/?database=${encodeURIComponent(CLICKHOUSE_DB)}`;
}

function clickHouseHeaders() {
  const headers: Record<string, string> = { 'Content-Type': 'text/plain' };
  if (CLICKHOUSE_USER || CLICKHOUSE_PASSWORD) {
    headers.Authorization = `Basic ${Buffer.from(`${CLICKHOUSE_USER}:${CLICKHOUSE_PASSWORD}`).toString('base64')}`;
  }
  return headers;
}

async function clickHouseExec(page: Page, sql: string) {
  const response = await page.request.post(clickHouseURL(), {
    headers: clickHouseHeaders(),
    data: sql,
  });
  if (!response.ok()) {
    throw new Error(`ClickHouse query failed: ${response.status()} ${await response.text()}`);
  }
}

async function cleanupIndex(page: Page) {
  await requestJSON(page, 'DELETE', `/api/v1/indexes?index=${encodeURIComponent(INDEX_NAME)}&drop_storage=true`).catch(() => undefined);
}

function formatClickHouseTime(date: Date) {
  const pad = (value: number, size = 2) => String(value).padStart(size, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}.${pad(date.getMilliseconds(), 3)}`;
}

function parseRawLog(raw: string) {
  const fields: Record<string, string> = {};
  for (const part of raw.split(/\s+/)) {
    const [key, value] = part.split('=');
    if (key && value !== undefined) {
      fields[key] = value;
    }
  }
  return {
    src: fields.src || '',
    src_ip: fields.src || '',
    dst: fields.dst || '',
    dst_ip: fields.dst || '',
    action: fields.action || '',
    bytes: fields.bytes || '',
  };
}

async function seedClickHouseEvents(page: Page) {
  const now = new Date();
  const rows = TEST_LOGS.map((raw, index) => {
    const eventTime = formatClickHouseTime(new Date(now.getTime() + index * 1000));
    return {
      index_name: INDEX_NAME,
      event_id: `${RUN_ID}-${index + 1}`,
      event_time: eventTime,
      ingest_time: eventTime,
      pipeline_id: 'playwright-search-command',
      pipeline_version: '1',
      source_type: 'playwright',
      source_name: 'playwright-search-command',
      source_host: 'localhost',
      source_ip: '127.0.0.1',
      sourcetype: 'playwright-search-command',
      parse_status: 'parsed',
      parse_rule_id: 'playwright-search-command',
      parse_rule_name: 'playwright-search-command',
      parse_error: '',
      parsed_at: eventTime,
      vendor: '',
      product: '',
      raw,
      fields_json: JSON.stringify(parseRawLog(raw)),
      labels_json: '{}',
      tags: [],
      errors_json: '[]',
    };
  });
  await clickHouseExec(page, `INSERT INTO ${CLICKHOUSE_DB}.events_${INDEX_NAME} FORMAT JSONEachRow\n${rows.map((row) => JSON.stringify(row)).join('\n')}\n`);
}

async function prepareTestData(page: Page) {
  await cleanupIndex(page);
  await requestJSON(page, 'POST', '/api/v1/indexes', { index_name: INDEX_NAME, ttl_days: 30, status: 'active' });
  await seedClickHouseEvents(page);
}

async function waitForData(page: Page, expectedSubstring: string, timeoutMs = 90_000) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    const payload = await requestJSON(page, 'GET', `/api/v1/search?q=${encodeURIComponent(`index=${INDEX_NAME}`)}&limit=20&page=1`).catch(() => null);
    if (JSON.stringify(payload || {}).includes(expectedSubstring)) return;
    await new Promise((resolve) => setTimeout(resolve, 1500));
  }
  throw new Error(`等待数据入库超时：预期含 ${expectedSubstring}`);
}

async function openSearchPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('[data-testid="logout"]')).toBeVisible();
  await page.click('[data-testid="nav-search"]');
  await expect(page.locator('[data-testid="search-page"]')).toBeVisible();
}

async function runSearch(page: Page, spl: string) {
  await page.locator('[data-testid="search-query"]').fill(spl);
  await page.locator('[data-testid="search-time"]').selectOption('所有时间');
  await page.locator('[data-testid="search-button"]').click();
}

async function waitForResultMode(page: Page, mode: '事件视图' | '统计视图' | '表格视图', timeout = 15_000) {
  await expect(page.locator('[data-testid="result-mode"]'), `预期结果模式为 ${mode}`).toContainText(mode, { timeout });
}

// 检测命令未注册时给出明确上下文；正常路径由 beforeAll 负责导入并启用插件。
async function checkCommandAvailable(page: Page): Promise<boolean> {
  const text = await page.locator('[data-testid="search-results"]').innerText();
  const unavailablePatterns = ['插件未启用', '命令不可用', 'SPL_COMMAND_NOT_FOUND', 'search command not found'];
  const matched = unavailablePatterns.find((p) => text.includes(p));
  if (matched) {
    throw new Error(`搜索命令插件不可用：命中错误 ${matched}，请检查 table/sort/head/dedup 插件包导入和启用状态`);
  }
  return true;
}

test.describe('TC-P1-SEARCH-MANUAL 搜索增强命令管道', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeAll(async ({ browser }) => {
    test.setTimeout(120_000);
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      console.log('== phase == TC-P1-SEARCH-MANUAL beforeAll open console');
      await page.goto('/');
      await expect(page.locator('[data-testid="logout"]')).toBeVisible();
      console.log('== phase == TC-P1-SEARCH-MANUAL beforeAll import and enable search command plugins');
      await importAndEnableSearchCommandPlugins(page);
      console.log('== phase == TC-P1-SEARCH-MANUAL beforeAll prepare index and seed ClickHouse events');
      await prepareTestData(page);
      console.log('== phase == TC-P1-SEARCH-MANUAL beforeAll wait searchable data');
      await waitForData(page, '10.0.1.8');
    } finally {
      await context.close();
    }
  });

  test.afterAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      await page.goto('/');
      await expect(page.locator('[data-testid="logout"]')).toBeVisible();
      await cleanupIndex(page);
    } finally {
      await context.close();
    }
  });

  test.beforeEach(async ({ page }) => {
    await openSearchPage(page);
  });

  test('TC-P1-SEARCH-MANUAL-001 table 字段投影', async ({ page }) => {
    console.log('== phase == TC-P1-SEARCH-MANUAL-001 table field projection');
    await runSearch(page, `index=${INDEX_NAME} | table _time src_ip action bytes`);
    await waitForResultMode(page, '表格视图');
    await checkCommandAvailable(page);
    const results = page.locator('[data-testid="search-results"]');
    await expect(results, '[TC-P1-SEARCH-MANUAL-001] 预期表头含 src_ip').toContainText('src_ip');
    await expect(results, '[TC-P1-SEARCH-MANUAL-001] 预期表头含 action').toContainText('action');
    await expect(results, '[TC-P1-SEARCH-MANUAL-001] 预期含 10.0.1.8 值').toContainText('10.0.1.8');
    await expect(results, '[TC-P1-SEARCH-MANUAL-001] 预期含 4096 值').toContainText('4096');
  });

  test('TC-P1-SEARCH-MANUAL-002 sort 降序排序', async ({ page }) => {
    console.log('== phase == TC-P1-SEARCH-MANUAL-002 sort desc');
    await runSearch(page, `index=${INDEX_NAME} | table src_ip action bytes | sort - bytes`);
    await waitForResultMode(page, '表格视图');
    await checkCommandAvailable(page);
    const rows = page.locator('[data-testid="search-results"] tbody tr');
    const count = await rows.count();
    expect(count, '[TC-P1-SEARCH-MANUAL-002] 预期至少 3 行').toBeGreaterThanOrEqual(3);
    // 降序：首行 bytes 应为 4096
    const firstRowText = await rows.first().innerText();
    await expect(rows.first(), '[TC-P1-SEARCH-MANUAL-002] 预期降序首行含 4096').toContainText('4096');
  });

  test('TC-P1-SEARCH-MANUAL-003 sort 升序排序', async ({ page }) => {
    console.log('== phase == TC-P1-SEARCH-MANUAL-003 sort asc');
    await runSearch(page, `index=${INDEX_NAME} | table src_ip action bytes | sort bytes`);
    await waitForResultMode(page, '表格视图');
    await checkCommandAvailable(page);
    const rows = page.locator('[data-testid="search-results"] tbody tr');
    await expect(rows.first(), '[TC-P1-SEARCH-MANUAL-003] 预期升序首行含 1024').toContainText('1024');
  });

  test('TC-P1-SEARCH-MANUAL-004 head 截断结果', async ({ page }) => {
    console.log('== phase == TC-P1-SEARCH-MANUAL-004 head truncate');
    await runSearch(page, `index=${INDEX_NAME} | table _time src_ip action bytes | sort - bytes | head 2`);
    await waitForResultMode(page, '表格视图');
    await checkCommandAvailable(page);
    const rows = page.locator('[data-testid="search-results"] tbody tr');
    const count = await rows.count();
    expect(count, '[TC-P1-SEARCH-MANUAL-004] 预期 head 2 后只返回 2 行').toBe(2);
    await expect(rows.first(), '[TC-P1-SEARCH-MANUAL-004] 预期首行 bytes=4096').toContainText('4096');
    await expect(rows.nth(1), '[TC-P1-SEARCH-MANUAL-004] 预期次行 bytes=2048').toContainText('2048');
  });

  test('TC-P1-SEARCH-MANUAL-005 dedup 单字段去重', async ({ page }) => {
    console.log('== phase == TC-P1-SEARCH-MANUAL-005 dedup single field');
    await runSearch(page, `index=${INDEX_NAME} | sort - bytes | dedup src_ip | table src_ip action bytes`);
    await waitForResultMode(page, '表格视图');
    await checkCommandAvailable(page);
    const rows = page.locator('[data-testid="search-results"] tbody tr');
    const count = await rows.count();
    expect(count, '[TC-P1-SEARCH-MANUAL-005] 预期 dedup src_ip 后共 2 行').toBe(2);
    await expect(rows.first(), '[TC-P1-SEARCH-MANUAL-005] 预期 10.0.1.8 保留 bytes=4096').toContainText('4096');
  });

  test('TC-P1-SEARCH-MANUAL-006 dedup 多字段组合去重', async ({ page }) => {
    console.log('== phase == TC-P1-SEARCH-MANUAL-006 dedup multi field');
    await runSearch(page, `index=${INDEX_NAME} | sort - bytes | dedup src_ip action | table src_ip action bytes`);
    await waitForResultMode(page, '表格视图');
    await checkCommandAvailable(page);
    const rows = page.locator('[data-testid="search-results"] tbody tr');
    const count = await rows.count();
    expect(count, '[TC-P1-SEARCH-MANUAL-006] 预期 dedup src_ip+action 后共 2 行').toBe(2);
  });

  test('TC-P1-SEARCH-MANUAL-007 stats 后继续执行搜索命令', async ({ page }) => {
    console.log('== phase == TC-P1-SEARCH-MANUAL-007 stats then pipe');
    await runSearch(page, `index=${INDEX_NAME} | stats count as total sum(bytes) as total_bytes by src action | sort - total_bytes | head 1 | table src action total_bytes`);
    await waitForResultMode(page, '表格视图');
    await checkCommandAvailable(page);
    const rows = page.locator('[data-testid="search-results"] tbody tr');
    const count = await rows.count();
    expect(count, '[TC-P1-SEARCH-MANUAL-007] 预期 head 1 后只返回 1 行').toBe(1);
    await expect(rows.first(), '[TC-P1-SEARCH-MANUAL-007] 预期首行含 10.0.1.8').toContainText('10.0.1.8');
    await expect(rows.first(), '[TC-P1-SEARCH-MANUAL-007] 预期首行含 6144').toContainText('6144');
  });

  test('TC-P1-SEARCH-MANUAL-008 管道执行顺序', async ({ page }) => {
    console.log('== phase == TC-P1-SEARCH-MANUAL-008 pipe order: sort then head');
    await runSearch(page, `index=${INDEX_NAME} | sort - bytes | head 1 | table src_ip bytes`);
    await waitForResultMode(page, '表格视图');
    await checkCommandAvailable(page);
    const rows = page.locator('[data-testid="search-results"] tbody tr');
    await expect(rows.first(), '[TC-P1-SEARCH-MANUAL-008] 预期 sort|head 顺序返回 4096').toContainText('4096');

    console.log('== phase == TC-P1-SEARCH-MANUAL-008 pipe order: head then sort');
    await runSearch(page, `index=${INDEX_NAME} | head 1 | sort - bytes | table src_ip bytes`);
    await waitForResultMode(page, '表格视图');
    // head 1 先取一条，sort 在单条结果中无效——不要求返回 4096，只断言返回 1 行
    const count = await rows.count();
    expect(count, '[TC-P1-SEARCH-MANUAL-008] 预期 head 1 后只返回 1 行').toBe(1);
  });

  test('TC-P1-SEARCH-MANUAL-009 非法 head 参数', async ({ page }) => {
    console.log('== phase == TC-P1-SEARCH-MANUAL-009 invalid head 0');
    await runSearch(page, `index=${INDEX_NAME} | head 0`);
    const results = page.locator('[data-testid="search-results"]');
    await expect(results, '[TC-P1-SEARCH-MANUAL-009] 预期不白屏').toBeVisible();
    const text = await results.innerText();
    expect(text, '[TC-P1-SEARCH-MANUAL-009] 预期展示可读错误或暂无结果').toMatch(/搜索失败|暂无匹配结果|VALIDATION|参数|invalid|head/i);
  });

  test('TC-P1-SEARCH-MANUAL-010 sort 缺少字段', async ({ page }) => {
    console.log('== phase == TC-P1-SEARCH-MANUAL-010 sort missing field');
    await runSearch(page, `index=${INDEX_NAME} | sort`);
    const results = page.locator('[data-testid="search-results"]');
    await expect(results, '[TC-P1-SEARCH-MANUAL-010] 预期不白屏').toBeVisible();
    const text = await results.innerText();
    expect(text, '[TC-P1-SEARCH-MANUAL-010] 预期展示 sort 缺少字段或可读错误').toMatch(/搜索失败|暂无匹配结果|VALIDATION|sort|缺少|字段|invalid/i);
  });

  test('TC-P1-SEARCH-MANUAL-011 head 非数字参数', async ({ page }) => {
    console.log('== phase == TC-P1-SEARCH-MANUAL-011 head non-numeric');
    await runSearch(page, `index=${INDEX_NAME} | head abc`);
    const results = page.locator('[data-testid="search-results"]');
    await expect(results, '[TC-P1-SEARCH-MANUAL-011] 预期不白屏').toBeVisible();
    const text = await results.innerText();
    expect(text, '[TC-P1-SEARCH-MANUAL-011] 预期展示可读参数错误').toMatch(/搜索失败|暂无匹配结果|VALIDATION|参数|invalid|head|abc/i);
  });
});
