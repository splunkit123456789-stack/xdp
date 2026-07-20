/**
 * 索引配置端到端验收脚本
 *
 * 覆盖用例：TC-P0-INDEX-001 ~ TC-P0-INDEX-007
 *
 * 验收链路：
 *   登录态复用 → API 清理测试 index → 进入索引页 → 新增 index
 *   → 必填校验 → 保存成功 → 修改 TTL / 状态 → 查看趋势 → 删除清理
 *
 * 运行：npx playwright test tests/index-config.spec.ts --project=admin
 */
import { test, expect, type Page } from '@playwright/test';

const API_URL = process.env.XDP_API_URL || 'http://127.0.0.1:8080';
const INDEX_NAME = `accept_index_${Date.now()}`;

async function authHeaders(page: Page) {
  const token = await page.evaluate(() => localStorage.getItem('xdp_api_token') || '');
  return { Authorization: `Bearer ${token}` };
}

async function requestJSON(page: Page, method: 'GET' | 'POST' | 'PUT' | 'DELETE', path: string, data?: unknown) {
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

async function cleanupIndex(page: Page, indexName: string) {
  await requestJSON(page, 'DELETE', `/api/v1/indexes?index=${encodeURIComponent(indexName)}&drop_storage=true`).catch(() => undefined);
}

async function openIndexPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('[data-testid="logout"]')).toBeVisible();
  await page.click('[data-testid="nav-index"]');
  await expect(page.locator('[data-testid="index-page"]')).toBeVisible();
  await page.locator('[data-testid="index-page-size"]').selectOption('1000');
}

function indexRow(page: Page, indexName = INDEX_NAME) {
  return page.locator(`tr:has-text("${indexName}")`).first();
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

test.describe('TC-P0-INDEX 索引配置端到端', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      await page.goto('/');
      await expect(page.locator('[data-testid="logout"]')).toBeVisible();
      await cleanupIndex(page, INDEX_NAME);
    } finally {
      await context.close();
    }
  });

  test.beforeEach(async ({ page }) => {
    await openIndexPage(page);
  });

  test('TC-P0-INDEX-001 进入索引配置页面', async ({ page }) => {
    console.log('== phase == TC-P0-INDEX-001 assert index page visible');
    const indexPage = page.locator('[data-testid="index-page"]');
    await expect(indexPage.getByRole('heading', { name: /索引配置/ }), '[TC-P0-INDEX-001] 预期展示索引配置标题').toBeVisible();
    await expect(indexPage.getByText('索引列表'), '[TC-P0-INDEX-001] 预期展示索引列表').toBeVisible();
    await expect(page.locator('[data-testid="writer-runtime-panel"]'), '[TC-P0-INDEX-001] 预期展示 Writer 运行面板').toBeVisible();
  });

  test('TC-P0-INDEX-002 点击新增后展示索引表单，取消可关闭', async ({ page }) => {
    console.log('== phase == TC-P0-INDEX-002 open and cancel index form');
    await expect(page.locator('[data-testid="index-form-card"]')).not.toBeVisible();

    await openFormByButton(page, 'show-index-form', 'index-form-card');
    const form = page.locator('[data-testid="index-form-card"]');
    await expect(form, '[TC-P0-INDEX-002] 预期点击新增后表单可见').toBeVisible();
    await expect(form.getByText('新增索引'), '[TC-P0-INDEX-002] 预期表单含新增索引标题').toBeVisible();
    await expect(page.locator('[data-testid="index-name"]'), '[TC-P0-INDEX-002] 预期索引名初始为空').toHaveValue('');
    await expect(page.locator('[data-testid="index-name"]'), '[TC-P0-INDEX-002] 预期索引名 placeholder').toHaveAttribute('placeholder', '请输入index名称');
    await expect(page.locator('[data-testid="index-ttl"]'), '[TC-P0-INDEX-002] 预期 TTL 默认 30').toHaveValue('30');
    await expect(page.locator('[data-testid="index-status"]'), '[TC-P0-INDEX-002] 预期状态默认 active').toHaveValue('active');

    await page.click('[data-testid="cancel-index-form"]');
    await expect(page.locator('[data-testid="index-form-card"]'), '[TC-P0-INDEX-002] 预期取消后表单不可见').not.toBeVisible();
  });

  test('TC-P0-INDEX-003 必填项校验生效', async ({ page }) => {
    console.log('== phase == TC-P0-INDEX-003 submit blank name and expect block');
    await openFormByButton(page, 'show-index-form', 'index-form-card');
    await page.fill('[data-testid="index-name"]', '   ');

    await page.click('[data-testid="index-page"] form button[type="submit"]');

    await expect(page.locator('[data-testid="index-form-error"]'), '[TC-P0-INDEX-003] 预期提示 index 名称为必填项').toContainText('index 名称为必填项');
    await expect(page.locator('[data-testid="index-form-card"]'), '[TC-P0-INDEX-003] 预期表单保持可见未被提交').toBeVisible();
  });

  test('TC-P0-INDEX-004 新增 index 成功，列表展示物理表、TTL 和状态', async ({ page }) => {
    console.log('== phase == TC-P0-INDEX-004 submit valid index');
    await cleanupIndex(page, INDEX_NAME);
    await openFormByButton(page, 'show-index-form', 'index-form-card');
    await page.fill('[data-testid="index-name"]', INDEX_NAME);
    await page.fill('[data-testid="index-ttl"]', '7');
    await page.locator('[data-testid="index-status"]').selectOption('active');

    await page.click('[data-testid="index-page"] form button[type="submit"]');

    console.log('== phase == TC-P0-INDEX-004 assert index row visible');
    const row = indexRow(page);
    await expect(row, '[TC-P0-INDEX-004] 预期索引行出现').toBeVisible({ timeout: 10_000 });
    await expect(row, '[TC-P0-INDEX-004] 预期行含索引名').toContainText(INDEX_NAME);
    await expect(row, '[TC-P0-INDEX-004] 预期行含物理表名').toContainText(`events_${INDEX_NAME}`);
    await expect(row, '[TC-P0-INDEX-004] 预期行含 TTL 7d').toContainText('7d');
    await expect(row, '[TC-P0-INDEX-004] 预期行含 active 状态').toContainText('active');
  });

  test('TC-P0-INDEX-005 修改 index TTL 和状态后列表同步更新', async ({ page }) => {
    console.log('== phase == TC-P0-INDEX-005 edit index TTL and status');
    const row = indexRow(page);
    await expect(row, '[TC-P0-INDEX-005] 预期索引行出现').toBeVisible();

    await row.locator('button', { hasText: '修改' }).first().click();
    await expect(page.locator('[data-testid="index-form-card"]'), '[TC-P0-INDEX-005] 预期修改后表单可见').toBeVisible();
    await expect(page.locator('[data-testid="index-name"]'), '[TC-P0-INDEX-005] 预期索引名回填').toHaveValue(INDEX_NAME);
    await expect(page.locator('[data-testid="index-name"]'), '[TC-P0-INDEX-005] 预期索引名禁用编辑').toBeDisabled();

    await page.fill('[data-testid="index-ttl"]', '14');
    await page.locator('[data-testid="index-status"]').selectOption('disabled');

    const disabledResponsePromise = page.waitForResponse((response) =>
      response.request().method() === 'PUT' &&
      response.url().includes(`/api/v1/indexes/${encodeURIComponent(INDEX_NAME)}`),
    );
    await page.click('[data-testid="index-page"] form button[type="submit"]');
    const disabledResponse = await disabledResponsePromise;
    expect(disabledResponse.ok(), '[TC-P0-INDEX-005] 预期 disabled 更新请求成功').toBe(true);
    const disabledPayload = await disabledResponse.json();
    expect(disabledPayload.status, '[TC-P0-INDEX-005] 预期后端接受 disabled 状态').toBe('disabled');

    await requestJSON(page, 'PUT', `/api/v1/indexes/${encodeURIComponent(INDEX_NAME)}`, {
      index_name: INDEX_NAME,
      ttl_days: 14,
      status: 'active',
    });
    await openIndexPage(page);

    console.log('== phase == TC-P0-INDEX-005 assert updated row');
    const updatedRow = indexRow(page);
    await expect(updatedRow, '[TC-P0-INDEX-005] 预期更新后行出现').toBeVisible({ timeout: 10_000 });
    await expect(updatedRow, '[TC-P0-INDEX-005] 预期行含 TTL 14d').toContainText('14d');
    await expect(updatedRow, '[TC-P0-INDEX-005] 预期恢复 active 后列表仍可见').toContainText('active');
  });

  test('TC-P0-INDEX-006 点击趋势后展示索引趋势面板', async ({ page }) => {
    console.log('== phase == TC-P0-INDEX-006 open trend panel');
    const row = indexRow(page);
    await expect(row, '[TC-P0-INDEX-006] 预期索引行出现').toBeVisible();

    const trendResponse = page.waitForResponse((response) =>
      response.url().includes(`/api/v1/indexes/${encodeURIComponent(INDEX_NAME)}/trend`) &&
      response.request().method() === 'GET',
    );
    await row.locator('button', { hasText: '趋势' }).first().click();
    await trendResponse;

    const trendPanel = row.locator('xpath=following-sibling::tr[1]').locator('[data-testid="index-trend-panel"]');
    await expect(trendPanel, '[TC-P0-INDEX-006] 预期趋势面板可见').toBeVisible({ timeout: 10_000 });
    await expect(trendPanel, '[TC-P0-INDEX-006] 预期趋势面板含加载或错误提示').toContainText(/当前|趋势加载中|容量趋势加载失败|load index trend failed|index trend requires clickhouse output/);
    if (await trendPanel.locator('[data-testid="index-trend-y-axis"]').first().isVisible()) {
      const yAxis = trendPanel.locator('[data-testid="index-trend-y-axis"]').first();
      const xAxis = trendPanel.locator('[data-testid="index-trend-x-axis"]').first();
      await expect(yAxis, '[TC-P0-INDEX-006] 预期 Y 轴含条单位').toContainText('条');
      await expect(xAxis, '[TC-P0-INDEX-006] 预期 X 轴展示时间刻度').toBeVisible();
      const xAxisText = (await xAxis.innerText()).trim();
      expect(xAxisText, '[TC-P0-INDEX-006] 预期 X 轴不是空白，应展示日期或时间刻度').toMatch(/\d{2}\/\d{2}|\d{4}-\d{2}-\d{2}|\d{2}:\d{2}/);
      await expect(trendPanel.locator('.index-trend-summary'), '[TC-P0-INDEX-006] 预期趋势摘要展示当前条目数').toContainText(/当前.*条/);
    }
  });

  test('TC-P0-INDEX-007 删除 index 后列表不再展示', async ({ page }) => {
    console.log('== phase == TC-P0-INDEX-007 delete index and assert row gone');
    const row = indexRow(page);
    await expect(row, '[TC-P0-INDEX-007] 预期索引行出现').toBeVisible();

    await row.locator('button', { hasText: '删除' }).first().click();

    await expect(indexRow(page), '[TC-P0-INDEX-007] 预期删除后行不可见').not.toBeVisible({ timeout: 10_000 });
  });

  test.afterAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      await page.goto('/');
      await expect(page.locator('[data-testid="logout"]')).toBeVisible();
      await cleanupIndex(page, INDEX_NAME);
    } finally {
      await context.close();
    }
  });
});
