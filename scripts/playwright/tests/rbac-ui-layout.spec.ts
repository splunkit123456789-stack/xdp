/**
 * P2 RBAC UI 布局回归验收脚本
 *
 * 覆盖近期 UI 调整：
 *   - 新建角色模态框切换继承 / 菜单 / 插件 / 索引时布局不跳动
 *   - 新建用户、角色取消 / 保存按钮位于表单内容区，不使用独立白底 footer
 *   - 采集列表和搜索结果表头左对齐
 *   - 用户 / 角色页面 CRUD 主按钮闭环
 *   - admin 用户和内置角色不展示删除按钮
 *   - 用户 / 角色右侧弹窗点击外部区域自动关闭
 *
 * 运行：npx playwright test tests/rbac-ui-layout.spec.ts --project=admin
 */
import { test, expect, type Locator, type Page } from '@playwright/test';

const API_URL = process.env.XDP_API_URL || 'http://127.0.0.1:8080';
const RUN_ID = Date.now();
const PREFIX = `accept_p2_rbac_ui_${RUN_ID}`;
const ROLE_CODE = `${PREFIX}_role`;
const ROLE_NAME = `按钮覆盖角色 ${RUN_ID}`;
const ROLE_NAME_UPDATED = `按钮覆盖角色已修改 ${RUN_ID}`;
const USERNAME = `${PREFIX}_user`;
const USER_DISPLAY_NAME = `按钮覆盖用户 ${RUN_ID}`;
const USER_DISPLAY_NAME_UPDATED = `按钮覆盖用户已修改 ${RUN_ID}`;
const PASSWORD = 'XdpButton_123';
const INDEX_NAME = `${PREFIX}_index`;

type HTTPMethod = 'GET' | 'POST' | 'PUT' | 'DELETE';

type Box = {
  x: number;
  y: number;
  width: number;
  height: number;
};

async function authHeaders(page: Page) {
  const token = await page.evaluate(() => localStorage.getItem('xdp_api_token') || '');
  return { Authorization: `Bearer ${token}` };
}

async function requestJSON<T = Record<string, unknown>>(page: Page, method: HTTPMethod, path: string, data?: unknown): Promise<T> {
  const headers = await authHeaders(page);
  const response = await page.request.fetch(`${API_URL}${path}`, {
    method,
    headers: { ...headers, ...(data ? { 'Content-Type': 'application/json' } : {}) },
    data,
  });
  const text = await response.text();
  if (!response.ok()) {
    throw new Error(`${method} ${path} failed: ${response.status()} ${text}`);
  }
  return (text ? JSON.parse(text) : {}) as T;
}

async function cleanupRBACButtonData(page: Page) {
  const usersPayload = await requestJSON<{ users?: Array<{ id?: string; username?: string }> }>(page, 'GET', '/api/v1/users?page=1&page_size=1000').catch(() => ({ users: [] }));
  for (const user of usersPayload.users || []) {
    if (!String(user.username || '').startsWith('accept_p2_rbac_ui_')) continue;
    const id = String(user.id || '').trim();
    if (id) await requestJSON(page, 'DELETE', `/api/v1/users/${encodeURIComponent(id)}`).catch(() => undefined);
  }

  const rolesPayload = await requestJSON<{ roles?: Array<{ id?: string; role_code?: string; builtin?: boolean }> }>(page, 'GET', '/api/v1/roles').catch(() => ({ roles: [] }));
  for (const role of rolesPayload.roles || []) {
    if (role.builtin || !String(role.role_code || '').startsWith('accept_p2_rbac_ui_')) continue;
    const id = String(role.id || '').trim();
    if (id) await requestJSON(page, 'DELETE', `/api/v1/roles/${encodeURIComponent(id)}`).catch(() => undefined);
  }

  await requestJSON(page, 'DELETE', `/api/v1/indexes?index=${encodeURIComponent(INDEX_NAME)}&drop_storage=true`).catch(() => undefined);
}

async function prepareRBACButtonData(page: Page) {
  await cleanupRBACButtonData(page);
  await requestJSON(page, 'POST', '/api/v1/indexes', {
    index_name: INDEX_NAME,
    ttl_days: 7,
    status: 'active',
  });
}

async function assertConsoleShell(page: Page) {
  await expect(page.locator('[data-testid="console-shell"]'), '预期进入控制台外壳').toBeVisible({ timeout: 20_000 });
  await expect(page.locator('[data-testid="main-nav"]'), '预期展示顶部主导航').toBeVisible();
}

async function visibleBox(locator: Locator, label: string): Promise<Box> {
  await expect(locator, label).toBeVisible({ timeout: 10_000 });
  const box = await locator.boundingBox();
  expect(box, `${label} 预期可计算布局位置`).not.toBeNull();
  return box as Box;
}

function expectBoxStable(label: string, actual: Box, expected: Box, tolerance = 2) {
  for (const key of ['x', 'y', 'width', 'height'] as const) {
    expect(
      Math.abs(actual[key] - expected[key]),
      `${label}.${key} 预期保持稳定，baseline=${expected[key]}, actual=${actual[key]}`,
    ).toBeLessThanOrEqual(tolerance);
  }
}

async function roleModalBoxes(page: Page) {
  const modal = page.locator('[data-testid="role-modal"]');
  return {
    modal: await visibleBox(modal, '预期角色模态框可见'),
    roleCode: await visibleBox(page.locator('[data-testid="role-code"]'), '预期角色编码输入框可见'),
    roleName: await visibleBox(page.locator('[data-testid="role-name"]'), '预期角色名称输入框可见'),
    roleStatus: await visibleBox(page.locator('[data-testid="role-status"]'), '预期角色状态选择框可见'),
    roleDescription: await visibleBox(page.locator('[data-testid="role-description"]'), '预期角色描述输入框可见'),
    tabFrame: await visibleBox(page.locator('[data-testid="role-tab-frame"]'), '预期角色页签内容框可见'),
    footer: await visibleBox(page.locator('[data-testid="role-modal"] .rbac-modal-body > .rbac-modal-footer'), '预期角色按钮区位于表单内容区'),
  };
}

async function assertInlineActions(page: Page, modalTestId: string, label: string) {
  const modal = page.locator(`[data-testid="${modalTestId}"]`);
  const body = modal.locator('.rbac-modal-body');
  const footer = modal.locator('.rbac-modal-body > .rbac-modal-footer');
  await expect(footer, `${label} 预期取消 / 保存按钮在表单内容区内`).toBeVisible({ timeout: 10_000 });

  const bodyBox = await visibleBox(body, `${label} 预期表单内容区可见`);
  const footerBox = await visibleBox(footer, `${label} 预期按钮区可见`);
  expect(footerBox.y, `${label} 按钮区应位于表单内容区内`).toBeGreaterThanOrEqual(bodyBox.y - 1);
  expect(footerBox.y + footerBox.height, `${label} 按钮区不应脱离表单内容区形成独立底栏`).toBeLessThanOrEqual(bodyBox.y + bodyBox.height + 1);

  const background = await footer.evaluate((node) => getComputedStyle(node).backgroundColor);
  expect(background, `${label} 按钮区不应是独立白底 footer`).not.toBe('rgb(255, 255, 255)');
}

async function assertHeadersLeftAligned(headers: Locator, label: string) {
  await expect(headers.first(), `${label} 预期至少存在一个表头`).toBeVisible({ timeout: 10_000 });
  const aligns = await headers.evaluateAll((nodes) => nodes.map((node) => getComputedStyle(node).textAlign));
  expect(aligns.length, `${label} 预期表头数量大于 0`).toBeGreaterThan(0);
  for (const align of aligns) {
    expect(align, `${label} 表头应左对齐`).toBe('left');
  }
}

function rbacUserRow(page: Page, username = USERNAME) {
  return page.locator('[data-testid="rbac-users-table"] tbody tr', { hasText: username }).first();
}

function rbacRoleRow(page: Page, roleCode = ROLE_CODE) {
  return page.locator('[data-testid="rbac-roles-table"] tbody tr', { hasText: roleCode }).first();
}

async function openRBACPage(page: Page) {
  await page.goto('/rbac');
  await assertConsoleShell(page);
  await expect(page.locator('[data-testid="rbac-page"]'), '预期进入用户与权限页面').toBeVisible({ timeout: 10_000 });
}

test.describe('TC-P2-RBAC-UI RBAC 与基础列表布局回归', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      await page.goto('/');
      await assertConsoleShell(page);
      await prepareRBACButtonData(page);
    } finally {
      await context.close();
    }
  });

  test('TC-P2-RBAC-UI-001 新建角色四个页签切换时布局位置和大小保持稳定', async ({ page }) => {
    console.log('== phase == TC-P2-RBAC-UI-001 role modal tab layout stability');
    await openRBACPage(page);
    await page.locator('[data-testid="rbac-tab-roles"]').click();
    await expect(page.locator('[data-testid="rbac-roles-table"]'), '预期角色列表可见').toBeVisible({ timeout: 10_000 });
    await page.locator('[data-testid="show-role-modal"]').click();
    await expect(page.locator('[data-testid="role-modal"]'), '预期新建角色模态框可见').toBeVisible({ timeout: 10_000 });

    const baseline = await roleModalBoxes(page);
    const tabs = [
      { tab: 'role-modal-tab-inherit', panel: 'role-inheritance', label: '继承' },
      { tab: 'role-modal-tab-menu', panel: 'role-permissions', label: '菜单' },
      { tab: 'role-modal-tab-plugin', panel: 'role-plugin-scopes', label: '插件' },
      { tab: 'role-modal-tab-index', panel: 'role-index-list', label: '索引' },
    ];

    for (const item of tabs) {
      await page.locator(`[data-testid="${item.tab}"]`).click();
      await expect(page.locator(`[data-testid="${item.panel}"]`), `预期 ${item.label} 页签内容可见`).toBeVisible({ timeout: 10_000 });
      const current = await roleModalBoxes(page);
      expectBoxStable(`${item.label} 页签 modal`, current.modal, baseline.modal);
      expectBoxStable(`${item.label} 页签 roleCode`, current.roleCode, baseline.roleCode);
      expectBoxStable(`${item.label} 页签 roleName`, current.roleName, baseline.roleName);
      expectBoxStable(`${item.label} 页签 roleStatus`, current.roleStatus, baseline.roleStatus);
      expectBoxStable(`${item.label} 页签 roleDescription`, current.roleDescription, baseline.roleDescription);
      expectBoxStable(`${item.label} 页签 tabFrame`, current.tabFrame, baseline.tabFrame);
      expectBoxStable(`${item.label} 页签 footer`, current.footer, baseline.footer);
    }
  });

  test('TC-P2-RBAC-UI-002 新建用户和角色取消保存按钮位于表单内容区', async ({ page }) => {
    console.log('== phase == TC-P2-RBAC-UI-002 modal actions inline');
    await openRBACPage(page);

    await page.locator('[data-testid="show-user-modal"]').click();
    await assertInlineActions(page, 'user-modal', '新建用户');
    await page.locator('[data-testid="cancel-user-modal"]').click();
    await expect(page.locator('[data-testid="user-modal"]'), '预期取消后用户模态框关闭').toHaveCount(0);

    await page.locator('[data-testid="rbac-tab-roles"]').click();
    await expect(page.locator('[data-testid="rbac-roles-table"]'), '预期角色列表可见').toBeVisible({ timeout: 10_000 });
    await page.locator('[data-testid="show-role-modal"]').click();
    await assertInlineActions(page, 'role-modal', '新建角色');
    await page.locator('[data-testid="cancel-role-modal"]').click();
    await expect(page.locator('[data-testid="role-modal"]'), '预期取消后角色模态框关闭').toHaveCount(0);
  });

  test('TC-P2-RBAC-UI-003 采集列表和搜索结果表头左对齐', async ({ page }) => {
    console.log('== phase == TC-P2-RBAC-UI-003 collect and search table headers align left');
    await page.goto('/collect');
    await assertConsoleShell(page);
    await assertHeadersLeftAligned(page.locator('[data-testid="collect-page"] .collect-table thead th'), '采集列表');

    await page.goto('/search');
    await assertConsoleShell(page);
    await assertHeadersLeftAligned(page.locator('[data-testid="search-results"] .result-table thead th'), '搜索结果');
  });

  test('TC-P2-RBAC-UI-004 admin 用户和内置角色不展示删除按钮', async ({ page }) => {
    console.log('== phase == TC-P2-RBAC-UI-004 protected delete buttons hidden');
    await openRBACPage(page);

    const adminRow = rbacUserRow(page, 'admin');
    await expect(adminRow, '[TC-P2-RBAC-UI-004] 预期 admin 用户行可见').toBeVisible({ timeout: 10_000 });
    await expect(adminRow.locator('button', { hasText: '删除' }), '[TC-P2-RBAC-UI-004] admin 用户行不应展示删除按钮').toHaveCount(0);

    await page.locator('[data-testid="rbac-tab-roles"]').click();
    const builtinRoleRow = rbacRoleRow(page, 'platform_admin');
    await expect(builtinRoleRow, '[TC-P2-RBAC-UI-004] 预期平台管理员内置角色行可见').toBeVisible({ timeout: 10_000 });
    await expect(builtinRoleRow.locator('button', { hasText: '删除' }), '[TC-P2-RBAC-UI-004] 内置角色行不应展示删除按钮').toHaveCount(0);
    await expect(builtinRoleRow, '[TC-P2-RBAC-UI-004] 平台管理员角色索引列应展示全部索引权限').toContainText('*');
  });

  test('TC-P2-RBAC-UI-005 页面创建并修改角色', async ({ page }) => {
    console.log('== phase == TC-P2-RBAC-UI-005 create and edit role through page');
    await openRBACPage(page);
    await page.locator('[data-testid="rbac-tab-roles"]').click();
    await expect(page.locator('[data-testid="rbac-roles-table"]'), '预期角色列表可见').toBeVisible({ timeout: 10_000 });

    await page.locator('[data-testid="show-role-modal"]').click();
    await expect(page.locator('[data-testid="role-modal"]'), '预期角色弹窗可见').toBeVisible({ timeout: 10_000 });
    await page.locator('[data-testid="role-code"]').fill(ROLE_CODE);
    await page.locator('[data-testid="role-name"]').fill(ROLE_NAME);
    await page.locator('[data-testid="role-description"]').fill('Playwright 按钮覆盖矩阵角色');

    await page.locator('[data-testid="role-modal-tab-menu"]').click();
    await page.locator('[data-testid="role-permission-search-execute"]').check();

    await page.locator('[data-testid="role-modal-tab-index"]').click();
    await page.locator(`[data-testid="role-index-item-${INDEX_NAME}"]`).check();

    await page.locator('[data-testid="role-modal-tab-plugin"]').click();
    await page.locator('[data-testid="role-plugin-scopes"]').fill('use:search_command/table');

    await page.locator('[data-testid="create-role"] button[type="submit"]').click();
    const row = rbacRoleRow(page);
    await expect(row, '[TC-P2-RBAC-UI-005] 预期新角色行出现').toBeVisible({ timeout: 10_000 });
    await expect(row, '[TC-P2-RBAC-UI-005] 预期角色行含角色名称').toContainText(ROLE_NAME);
    await expect(row, '[TC-P2-RBAC-UI-005] 预期角色行含菜单权限').toContainText('搜索页');
    await expect(row, '[TC-P2-RBAC-UI-005] 预期角色行含授权索引').toContainText(INDEX_NAME);
    await expect(row, '[TC-P2-RBAC-UI-005] 预期角色行含插件 scope').toContainText('use:search_command/table');

    await row.locator('button', { hasText: '修改' }).click();
    await expect(page.locator('[data-testid="role-modal"]'), '[TC-P2-RBAC-UI-005] 预期修改角色弹窗可见').toBeVisible({ timeout: 10_000 });
    await page.locator('[data-testid="role-name"]').fill(ROLE_NAME_UPDATED);
    await page.locator('[data-testid="create-role"] button[type="submit"]').click();
    await expect(rbacRoleRow(page), '[TC-P2-RBAC-UI-005] 预期修改后角色行仍可见').toContainText(ROLE_NAME_UPDATED, { timeout: 10_000 });
  });

  test('TC-P2-RBAC-UI-006 页面创建、修改并删除用户', async ({ page }) => {
    console.log('== phase == TC-P2-RBAC-UI-006 create edit delete user through page');
    await openRBACPage(page);
    await page.locator('[data-testid="show-user-modal"]').click();
    await expect(page.locator('[data-testid="user-modal"]'), '预期用户弹窗可见').toBeVisible({ timeout: 10_000 });
    await page.locator('[data-testid="user-username"]').fill(USERNAME);
    await page.locator('[data-testid="user-display-name"]').fill(USER_DISPLAY_NAME);
    await page.locator('[data-testid="user-password"]').fill(PASSWORD);
    await page.locator('[data-testid="user-confirm-password"]').fill(PASSWORD);
    await page.locator('[data-testid="user-role-transfer"] button', { hasText: ROLE_CODE }).click();
    await page.locator('[data-testid="create-user"] button[type="submit"]').click();

    const row = rbacUserRow(page);
    await expect(row, '[TC-P2-RBAC-UI-006] 预期新用户行出现').toBeVisible({ timeout: 10_000 });
    await expect(row, '[TC-P2-RBAC-UI-006] 预期用户行含显示名').toContainText(USER_DISPLAY_NAME);
    await expect(row, '[TC-P2-RBAC-UI-006] 预期用户行含分配角色').toContainText(ROLE_NAME_UPDATED);

    await row.locator('button', { hasText: '修改' }).click();
    await expect(page.locator('[data-testid="user-modal"]'), '[TC-P2-RBAC-UI-006] 预期修改用户弹窗可见').toBeVisible({ timeout: 10_000 });
    await page.locator('[data-testid="user-display-name"]').fill(USER_DISPLAY_NAME_UPDATED);
    await page.locator('[data-testid="create-user"] button[type="submit"]').click();
    await expect(rbacUserRow(page), '[TC-P2-RBAC-UI-006] 预期修改后用户行显示新全称').toContainText(USER_DISPLAY_NAME_UPDATED, { timeout: 10_000 });

    await rbacUserRow(page).locator('button', { hasText: '删除' }).click();
    await expect(rbacUserRow(page), '[TC-P2-RBAC-UI-006] 预期删除后用户行不可见').not.toBeVisible({ timeout: 10_000 });
  });

  test('TC-P2-RBAC-UI-007 页面删除非内置角色', async ({ page }) => {
    console.log('== phase == TC-P2-RBAC-UI-007 delete non-builtin role through page');
    await openRBACPage(page);
    await page.locator('[data-testid="rbac-tab-roles"]').click();
    const row = rbacRoleRow(page);
    await expect(row, '[TC-P2-RBAC-UI-007] 预期待删除角色行出现').toBeVisible({ timeout: 10_000 });

    await row.locator('button', { hasText: '删除' }).click();
    await expect(rbacRoleRow(page), '[TC-P2-RBAC-UI-007] 预期删除后角色行不可见').not.toBeVisible({ timeout: 10_000 });
  });

  test('TC-P2-RBAC-UI-008 用户和角色弹窗点击外部区域自动关闭', async ({ page }) => {
    console.log('== phase == TC-P2-RBAC-UI-008 click outside closes user and role drawers');
    await openRBACPage(page);

    await page.locator('[data-testid="show-user-modal"]').click();
    await expect(page.locator('[data-testid="user-modal"]'), '[TC-P2-RBAC-UI-008] 预期用户弹窗可见').toBeVisible({ timeout: 10_000 });
    await page.mouse.click(24, 220);
    await expect(page.locator('[data-testid="user-modal"]'), '[TC-P2-RBAC-UI-008] 预期点击弹窗外部后用户弹窗关闭').toHaveCount(0, { timeout: 10_000 });

    await page.locator('[data-testid="rbac-tab-roles"]').click();
    await expect(page.locator('[data-testid="rbac-roles-table"]'), '[TC-P2-RBAC-UI-008] 预期角色列表可见').toBeVisible({ timeout: 10_000 });
    await page.locator('[data-testid="show-role-modal"]').click();
    await expect(page.locator('[data-testid="role-modal"]'), '[TC-P2-RBAC-UI-008] 预期角色弹窗可见').toBeVisible({ timeout: 10_000 });
    await page.mouse.click(24, 220);
    await expect(page.locator('[data-testid="role-modal"]'), '[TC-P2-RBAC-UI-008] 预期点击弹窗外部后角色弹窗关闭').toHaveCount(0, { timeout: 10_000 });
  });

  test.afterAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      await page.goto('/');
      await assertConsoleShell(page);
      await cleanupRBACButtonData(page);
    } finally {
      await context.close();
    }
  });
});
