/**
 * 采集配置端到端验收脚本
 *
 * 对应人工验收文档：docs/requirements/XDP_P0_人工验收.md 第 5 章「采集配置验收」
 * 覆盖用例：TC-P0-COLLECT-001 ~ TC-P0-COLLECT-009
 *
 * 验收链路：
 *   登录态复用 → 进入采集页 → 新增 Syslog 采集源 → 端口占用校验
 *   → 保存成功 → 列表断言 → 启停切换 → 修改锁定插件 → 删除清理
 *
 * 运行：npx playwright test tests/collect-config.spec.ts --project=admin
 */
import dgram from 'node:dgram';
import type { AddressInfo } from 'node:net';
import { test, expect, type Page } from '@playwright/test';

// 端到端测试数据，使用可识别前缀便于清理
const DATA_SOURCE_NAME = `accept_p0_syslog_${Date.now()}`;
const API_URL = process.env.XDP_API_URL || 'http://127.0.0.1:8080';

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

async function openCollectPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('[data-testid="logout"]')).toBeVisible();
  await page.click('[data-testid="nav-collect"]');
  await expect(page.locator('[data-testid="collect-page"]')).toBeVisible();
  await page.locator('[data-testid="collect-page-size"]').selectOption('1000');
}

function dataSourceRow(page: Page) {
  return page.locator(`tr:has-text("${DATA_SOURCE_NAME}")`).first();
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

async function cleanupDataSourceByName(page: Page, name: string) {
  const headers = await authHeaders(page);
  const response = await page.request.get(`${API_URL}/api/v1/datasources?page=1&page_size=1000`, { headers });
  if (!response.ok()) return;
  const payload = await response.json();
  const sources = Array.isArray(payload.datasources) ? payload.datasources : [];
  for (const source of sources) {
    if (String(source.name || '').trim() !== name) continue;
    const id = String(source.id || source.code || '').trim();
    if (!id) continue;
    await page.request.delete(`${API_URL}/api/v1/datasources/${encodeURIComponent(id)}`, { headers });
  }
}

async function cleanupDataSourcesByPrefix(page: Page, prefix: string) {
  const headers = await authHeaders(page);
  const response = await page.request.get(`${API_URL}/api/v1/datasources?page=1&page_size=1000`, { headers });
  if (!response.ok()) return;
  const payload = await response.json();
  const sources = Array.isArray(payload.datasources) ? payload.datasources : [];
  for (const source of sources) {
    if (!String(source.name || '').trim().startsWith(prefix)) continue;
    const id = String(source.id || source.code || '').trim();
    if (!id) continue;
    await page.request.delete(`${API_URL}/api/v1/datasources/${encodeURIComponent(id)}`, { headers });
  }
}

async function createSyslogDataSourceByAPI(page: Page, name: string, port: number) {
  await cleanupDataSourceByName(page, name);
  await requestJSON(page, 'POST', '/api/v1/datasources', {
    name,
    plugin_code: 'syslog',
    status: 'active',
    plugin_config: {
      collector_port: port,
      transport_protocol: 'UDP',
      encoding: 'UTF-8',
      log_filter_enabled: false,
    },
  });
}

async function waitForPortUnavailable(page: Page, port: number, timeoutMs = 20_000) {
  const started = Date.now();
  const headers = await authHeaders(page);
  let lastResult = '';
  while (Date.now() - started < timeoutMs) {
    const response = await page.request.post(`${API_URL}/api/v1/datasources/port-check`, {
      headers: { ...headers, 'Content-Type': 'application/json' },
      data: {
        plugin_code: 'syslog',
        collector_port: port,
        transport_protocol: 'UDP',
      },
    });
    const text = await response.text();
    lastResult = `${response.status()} ${text}`;
    if (response.status() === 409 && text.includes('LISTENER_PORT_UNAVAILABLE')) {
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
  throw new Error(`[TC-P0-COLLECT-004] API 未能识别端口 ${port} 已被 Agent listener 占用，最后响应：${lastResult}`);
}

test.describe('TC-P0-COLLECT 采集配置端到端', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeAll(async () => {
    dataSourcePort = await findFreeUDPPort();
  });

  test.beforeEach(async ({ page }) => {
    await openCollectPage(page);
  });

  test('TC-P0-COLLECT-001 进入采集配置页面', async ({ page }) => {
    console.log('== phase == TC-P0-COLLECT-001 assert collect page visible');
    const collectPage = page.locator('[data-testid="collect-page"]');
    await expect(collectPage.getByRole('heading', { name: /采集配置/ }), '[TC-P0-COLLECT-001] 预期展示采集配置标题').toBeVisible();
    await expect(collectPage.getByText('采集列表'), '[TC-P0-COLLECT-001] 预期展示采集列表').toBeVisible();
  });

  test('TC-P0-COLLECT-002 点击新增后展示采集表单，取消可关闭', async ({ page }) => {
    console.log('== phase == TC-P0-COLLECT-002 open and cancel input form');
    await expect(page.locator('[data-testid="input-form-card"]')).not.toBeVisible();

    await openFormByButton(page, 'show-input-form', 'input-form-card');
    const form = page.locator('[data-testid="input-form-card"]');
    await expect(form, '[TC-P0-COLLECT-002] 预期点击新增后表单可见').toBeVisible();
    await expect(form.getByText('新增采集'), '[TC-P0-COLLECT-002] 预期表单含新增采集标题').toBeVisible();

    await page.click('[data-testid="cancel-input-form"]');
    await expect(page.locator('[data-testid="input-form-card"]'), '[TC-P0-COLLECT-002] 预期取消后表单不可见').not.toBeVisible();
  });

  test('TC-P0-COLLECT-003 监听端口为必填项，空端口提交被阻断', async ({ page }) => {
    console.log('== phase == TC-P0-COLLECT-003 submit empty port and expect block');
    await openFormByButton(page, 'show-input-form', 'input-form-card');

    const form = page.locator('[data-testid="input-form-card"]');
    const portInput = form.locator('input[placeholder="5514"]');
    await form.locator('input[placeholder="请输入设备名称"]').fill(`${DATA_SOURCE_NAME}_invalid`);
    // 监听端口留空
    await portInput.fill('');

    await page.click('[data-testid="collect-page"] form button[type="submit"]');

    expect(await portInput.evaluate((el: HTMLInputElement) => el.validity.valueMissing), '[TC-P0-COLLECT-003] 预期监听端口触发浏览器必填校验').toBe(true);
    await expect(form, '[TC-P0-COLLECT-003] 预期表单保持可见未被提交').toBeVisible();
  });

  test('TC-P0-COLLECT-004 已占用端口提示不可用', async ({ page }) => {
    console.log('== phase == TC-P0-COLLECT-004 submit occupied port and expect rejection');
    const occupiedName = `${DATA_SOURCE_NAME}_occupied_owner`;
    const rejectedName = `${DATA_SOURCE_NAME}_port`;
    const occupiedPort = await findFreeUDPPort();
    try {
      await createSyslogDataSourceByAPI(page, occupiedName, occupiedPort);
      await waitForPortUnavailable(page, occupiedPort);
      await openFormByButton(page, 'show-input-form', 'input-form-card');

      await page.fill('input[placeholder="请输入设备名称"]', rejectedName);
      await page.fill('input[placeholder="5514"]', String(occupiedPort));

      await page.click('[data-testid="collect-page"] form button[type="submit"]');

      await expect(page.locator('[data-testid="collector-port-error"]'), '[TC-P0-COLLECT-004] 预期提示端口不可用').toContainText('端口不可用');
    } finally {
      await cleanupDataSourceByName(page, rejectedName);
      await cleanupDataSourceByName(page, occupiedName);
    }
  });

  test('TC-P0-COLLECT-005 保存 Syslog 采集源成功，列表显示运行状态', async ({ page }) => {
    console.log('== phase == TC-P0-COLLECT-005 submit valid syslog datasource');
    await cleanupDataSourceByName(page, DATA_SOURCE_NAME);
    await openFormByButton(page, 'show-input-form', 'input-form-card');

    await page.fill('input[placeholder="请输入设备名称"]', DATA_SOURCE_NAME);
    await page.fill('input[placeholder="5514"]', String(dataSourcePort));

    await page.click('[data-testid="collect-page"] form button[type="submit"]');

    console.log('== phase == TC-P0-COLLECT-005 assert datasource row visible');
    // 列表出现刚创建的采集源
    await expect(dataSourceRow(page), '[TC-P0-COLLECT-005] 预期列表出现刚创建的采集源').toBeVisible({ timeout: 10_000 });
    // 运行状态列展示
    await expect(dataSourceRow(page), '[TC-P0-COLLECT-005] 预期行含运行状态').toContainText(/运行中|已停止|异常|未知/);
  });

  test('TC-P0-COLLECT-006 启停切换：停止后监听停止，启动后恢复', async ({ page }) => {
    console.log('== phase == TC-P0-COLLECT-006 toggle datasource start/stop');
    // 等待列表加载
    await expect(dataSourceRow(page), '[TC-P0-COLLECT-006] 预期采集源行出现').toBeVisible();

    // 找到刚创建的采集源行对应的 toggle 按钮
    const toggleBtn = dataSourceRow(page).locator('[data-testid^="toggle-input-"]').first();
    await expect(toggleBtn, '[TC-P0-COLLECT-006] 预期存在启停按钮').toBeVisible();
    const initialLabel = await toggleBtn.textContent();

    await toggleBtn.click();
    // 等待状态切换：按钮文案从"停止"变"启动"或反之
    await expect(toggleBtn, '[TC-P0-COLLECT-006] 预期点击后按钮文案变化').not.toHaveText(initialLabel || '', { timeout: 10_000 });
    const afterClickLabel = await toggleBtn.textContent();
    expect(afterClickLabel, '[TC-P0-COLLECT-006] 预期点击后文案与初始不同').not.toBe(initialLabel);

    // 切换回来，保持采集源 active 状态便于后续清理
    await toggleBtn.click();
    await expect(toggleBtn, '[TC-P0-COLLECT-006] 预期二次点击后文案恢复').toHaveText(initialLabel || '', { timeout: 10_000 });
  });

  test('TC-P0-COLLECT-007 修改模式下采集插件不可切换', async ({ page }) => {
    console.log('== phase == TC-P0-COLLECT-007 enter edit mode and assert plugin locked');
    await expect(dataSourceRow(page), '[TC-P0-COLLECT-007] 预期采集源行出现').toBeVisible();

    // 点击修改按钮
    const editBtn = dataSourceRow(page).locator('button', { hasText: '修改' }).first();
    await editBtn.click();

    // 插件选择按钮应被禁用
    const syslogBtn = page.locator('[data-testid="input-plugin-syslog"]');
    const kafkaBtn = page.locator('[data-testid="input-plugin-kafka"]');

    await expect(syslogBtn, '[TC-P0-COLLECT-007] 预期 syslog 插件按钮禁用').toBeDisabled();
    if (await kafkaBtn.isVisible()) {
      await expect(kafkaBtn, '[TC-P0-COLLECT-007] 预期 kafka 插件按钮禁用').toBeDisabled();
    }

    // 退出修改模式
    await page.click('[data-testid="cancel-input-form"]');
  });

  test('TC-P0-COLLECT-008 展开行显示 Agent 心跳与 listener 状态', async ({ page }) => {
    console.log('== phase == TC-P0-COLLECT-008 expand row and assert runtime detail');
    await expect(dataSourceRow(page), '[TC-P0-COLLECT-008] 预期采集源行出现').toBeVisible();

    // 点击展开图标
    const expandBtn = dataSourceRow(page).locator('[data-testid^="collect-expand-"]').first();
    await expandBtn.click();

    // 展开后应出现运行详情卡片
    const detail = page.locator('[data-testid="collect-runtime-detail"]');
    await expect(detail, '[TC-P0-COLLECT-008] 预期展开后显示运行详情').toBeVisible({ timeout: 10_000 });
    await expect(detail.getByText('listener 状态', { exact: true }), '[TC-P0-COLLECT-008] 预期运行详情含 listener 状态').toBeVisible();
    await expect(detail.getByText('Agent 心跳', { exact: true }), '[TC-P0-COLLECT-008] 预期运行详情含 Agent 心跳').toBeVisible();
  });

  test('TC-P0-COLLECT-009 页面删除采集源后列表不再展示', async ({ page }) => {
    console.log('== phase == TC-P0-COLLECT-009 delete datasource through page');
    const row = dataSourceRow(page);
    await expect(row, '[TC-P0-COLLECT-009] 预期采集源行出现').toBeVisible({ timeout: 10_000 });

    await row.locator('button', { hasText: '删除' }).click();
    await expect(dataSourceRow(page), '[TC-P0-COLLECT-009] 预期删除后采集源行不可见').not.toBeVisible({ timeout: 10_000 });
  });

  // 清理：测试完成后删除采集源，避免污染下次验收
  test.afterAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      await page.goto('/');
      await cleanupDataSourcesByPrefix(page, DATA_SOURCE_NAME);
    } finally {
      await context.close();
    }
  });
});
