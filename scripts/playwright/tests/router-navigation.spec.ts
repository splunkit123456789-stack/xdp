/**
 * P3 vue-router 页面验收脚本
 *
 * 对应文档：
 *   - docs/requirements/XDP_P3_需求设计.md
 *   - docs/requirements/XDP_P3_前端设计.md
 *
 * 验收链路：
 *   登录态复用 → URL 直达模块 → 顶部导航写入浏览器历史
 *   → 浏览器前进/后退切换模块 → 刷新保持当前 URL 和模块 → 未知路径展示 404
 *   → 未登录 URL 保持 → router.beforeEach 菜单级准入
 *
 * 运行：npx playwright test tests/router-navigation.spec.ts --project=admin
 */
import { test, expect, type Browser, type Page } from '@playwright/test';

type RouteModule = 'collect' | 'parse' | 'index' | 'search' | 'plugins' | 'rbac';

const panelRoutes: Array<{ module: RouteModule; path: string; title: string }> = [
  { module: 'collect', path: '/collect', title: '采集配置' },
  { module: 'parse', path: '/parse', title: '解析配置' },
  { module: 'index', path: '/index', title: '索引配置' },
  { module: 'search', path: '/search', title: '搜索页' },
  { module: 'plugins', path: '/plugins', title: '插件管理' },
  { module: 'rbac', path: '/rbac', title: '用户与权限' },
];

const API_URL = process.env.XDP_API_URL || 'http://127.0.0.1:8080';
const RUN_ID = Date.now();
const ROUTER_ROLE_CODE = `accept_p3_router_${RUN_ID}_search`;
const ROUTER_USERNAME = `accept_p3_router_${RUN_ID}_user`;
const ROUTER_PASSWORD = 'xdpP3!23456';

type HTTPMethod = 'GET' | 'POST' | 'DELETE';

async function authHeaders(page: Page) {
  const token = await page.evaluate(() => localStorage.getItem('xdp_api_token') || '');
  return { Authorization: `Bearer ${token}` };
}

async function requestJSON<T = Record<string, unknown>>(
  page: Page,
  method: HTTPMethod,
  path: string,
  data?: unknown,
): Promise<T> {
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

async function assertConsoleShell(page: Page) {
  await expect(page.locator('[data-testid="console-shell"]'), '预期进入控制台外壳').toBeVisible({ timeout: 20_000 });
  await expect(page.locator('[data-testid="main-nav"]'), '预期展示顶部主导航').toBeVisible();
}

async function assertModuleVisible(page: Page, module: RouteModule) {
  await expect(page.locator(`[data-testid="${module}-page"]`), `预期 ${module} 模块面板可见`).toBeVisible({ timeout: 15_000 });
}

async function cleanupRouterGuardData(page: Page) {
  const usersPayload = await requestJSON<{ users?: Array<{ id?: string; username?: string }> }>(page, 'GET', '/api/v1/users?page=1&page_size=1000').catch(() => ({ users: [] }));
  for (const user of usersPayload.users || []) {
    if (!String(user.username || '').startsWith('accept_p3_router_')) continue;
    const id = String(user.id || '').trim();
    if (id) await requestJSON(page, 'DELETE', `/api/v1/users/${encodeURIComponent(id)}`).catch(() => undefined);
  }

  const rolesPayload = await requestJSON<{ roles?: Array<{ id?: string; role_code?: string; builtin?: boolean }> }>(page, 'GET', '/api/v1/roles').catch(() => ({ roles: [] }));
  for (const role of rolesPayload.roles || []) {
    if (role.builtin || !String(role.role_code || '').startsWith('accept_p3_router_')) continue;
    const id = String(role.id || '').trim();
    if (id) await requestJSON(page, 'DELETE', `/api/v1/roles/${encodeURIComponent(id)}`).catch(() => undefined);
  }
}

async function prepareRouterGuardUser(page: Page) {
  await cleanupRouterGuardData(page);
  const role = await requestJSON<{ id: string }>(page, 'POST', '/api/v1/roles', {
    role_code: ROUTER_ROLE_CODE,
    role_name: `P3 路由受限搜索 ${RUN_ID}`,
    description: 'Playwright P3 router guard restricted search role.',
    status: 'active',
    permission_codes: ['search:execute'],
    index_scopes: {},
    plugin_scopes: {},
  });
  await requestJSON(page, 'POST', '/api/v1/users', {
    username: ROUTER_USERNAME,
    display_name: `P3 路由受限用户 ${RUN_ID}`,
    password: ROUTER_PASSWORD,
    status: 'active',
    role_ids: [role.id],
  });
}

async function loginRestrictedAt(browser: Browser, path: string) {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.goto(path);
  await expect(page.locator('[data-testid="login-page"]'), `[${path}] 预期先展示登录页`).toBeVisible({ timeout: 20_000 });
  await page.locator('input[placeholder="请输入用户名"]').fill(ROUTER_USERNAME);
  await page.locator('input[placeholder="请输入密码"]').fill(ROUTER_PASSWORD);
  const [response] = await Promise.all([
    page.waitForResponse((res) => res.url().includes('/api/v1/login') && res.request().method() === 'POST'),
    page.locator('form.login-form button[type="submit"]').click(),
  ]);
  expect(response.ok(), `[${path}] 预期受限用户登录成功`).toBe(true);
  await assertConsoleShell(page);
  return { context, page };
}

test.describe('TC-P3-ROUTER vue-router 导航端到端', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      console.log('== phase == TC-P3-ROUTER beforeAll prepare restricted route guard user');
      await page.goto('/');
      await assertConsoleShell(page);
      await prepareRouterGuardUser(page);
    } finally {
      await context.close();
    }
  });

  test('TC-P3-AUTH-001 未登录直达 /search 后登录保持目标 URL', async ({ browser }) => {
    console.log('== phase == TC-P3-AUTH-001 unauthenticated direct /search keeps target after login');
    const context = await browser.newContext();
    const page = await context.newPage();
    try {
      await page.goto('/search');
      await expect(page.locator('[data-testid="login-page"]'), '预期未登录直达业务 URL 时展示登录页').toBeVisible({ timeout: 20_000 });
      await expect(page, '预期登录前保持目标 URL').toHaveURL(/\/search$/);

      await page.fill('input[name="username"]', process.env.XDP_ADMIN_USER || 'admin');
      await page.fill('input[name="password"]', process.env.XDP_ADMIN_PASSWORD || 'xdp');
      await page.click('button[type="submit"]');

      await assertConsoleShell(page);
      await assertModuleVisible(page, 'search');
      await expect(page, '预期登录后仍停留 /search').toHaveURL(/\/search$/);
    } finally {
      await context.close();
    }
  });

  test('TC-P3-NAV-001 URL 直跳 /search 后展示搜索页', async ({ page }) => {
    console.log('== phase == TC-P3-NAV-001 direct /search');
    await page.goto('/search');
    await assertConsoleShell(page);
    await assertModuleVisible(page, 'search');
    await expect(page).toHaveURL(/\/search$/);
  });

  test('TC-P3-NAV-002 浏览器后退按钮可切换上一模块', async ({ page }) => {
    console.log('== phase == TC-P3-NAV-002 browser back switches modules');
    await page.goto('/collect');
    await assertConsoleShell(page);
    await assertModuleVisible(page, 'collect');

    await page.locator('[data-testid="nav-parse"]').click();
    await assertModuleVisible(page, 'parse');
    await expect(page).toHaveURL(/\/parse$/);

    await page.locator('[data-testid="nav-search"]').click();
    await assertModuleVisible(page, 'search');
    await expect(page).toHaveURL(/\/search$/);

    await page.goBack();
    await assertModuleVisible(page, 'parse');
    await expect(page).toHaveURL(/\/parse$/);

    await page.goBack();
    await assertModuleVisible(page, 'collect');
    await expect(page).toHaveURL(/\/collect$/);
  });

  test('TC-P3-NAV-003 刷新后保持当前 URL 与模块', async ({ page }) => {
    console.log('== phase == TC-P3-NAV-003 reload keeps route module');
    await page.goto('/search');
    await assertConsoleShell(page);
    await assertModuleVisible(page, 'search');

    await page.reload();
    await assertConsoleShell(page);
    await assertModuleVisible(page, 'search');
    await expect(page).toHaveURL(/\/search$/);
  });

  test('TC-P3-NAV-004 直接访问未知路径展示 404 面板', async ({ page }) => {
    console.log('== phase == TC-P3-NAV-004 unknown route shows not found panel');
    await page.goto('/nonexistent');
    await assertConsoleShell(page);
    await expect(page.locator('[data-testid="not-found-page"]'), '预期未知路径展示 404 面板').toBeVisible({ timeout: 15_000 });
    await expect(page.locator('[data-testid="not-found-page"]')).toContainText('404');
    await expect(page).toHaveURL(/\/nonexistent$/);
  });

  test('TC-P3-GUARD-004 未知路径 404 不覆盖最近模块', async ({ page }) => {
    console.log('== phase == TC-P3-GUARD-004 unknown route does not overwrite last module');
    await page.goto('/search');
    await assertConsoleShell(page);
    await assertModuleVisible(page, 'search');
    await page.evaluate(() => localStorage.setItem('xdp_current_module', 'search'));

    await page.goto('/nonexistent');
    await expect(page.locator('[data-testid="not-found-page"]'), '预期未知路径展示 404 面板').toBeVisible({ timeout: 15_000 });
    const stored = await page.evaluate(() => localStorage.getItem('xdp_current_module'));
    expect(stored).toBe('search');
  });

  test('TC-P3-GUARD-001 受限用户直达 /rbac 时重定向到可访问模块并展示 403', async ({ browser }) => {
    console.log('== phase == TC-P3-GUARD-001 restricted user direct /rbac redirects with 403');
    const { context, page } = await loginRestrictedAt(browser, '/rbac');
    try {
      await assertModuleVisible(page, 'search');
      await expect(page, '[TC-P3-GUARD-001] 预期越权访问 /rbac 后落到 /search').toHaveURL(/\/search(\?forbidden=rbac)?$/);
      await expect(page.locator('[data-testid="module-forbidden"]'), '[TC-P3-GUARD-001] 预期展示 403 提示').toContainText('403');
      await expect(page.locator('[data-testid="module-forbidden"]'), '[TC-P3-GUARD-001] 预期提示原目标用户与权限').toContainText('用户与权限');
      await expect(page.locator('[data-testid="nav-rbac"]'), '[TC-P3-GUARD-001] 受限用户不应看到 RBAC 菜单').toHaveCount(0);
    } finally {
      await context.close();
    }
  });

  test('TC-P3-GUARD-002 无插件 manage scope 用户直达 /plugins 时重定向并展示 403', async ({ browser }) => {
    console.log('== phase == TC-P3-GUARD-002 restricted user direct /plugins redirects with 403');
    const { context, page } = await loginRestrictedAt(browser, '/plugins');
    try {
      await assertModuleVisible(page, 'search');
      await expect(page, '[TC-P3-GUARD-002] 预期越权访问 /plugins 后落到 /search').toHaveURL(/\/search(\?forbidden=plugins)?$/);
      await expect(page.locator('[data-testid="module-forbidden"]'), '[TC-P3-GUARD-002] 预期展示 403 提示').toContainText('403');
      await expect(page.locator('[data-testid="module-forbidden"]'), '[TC-P3-GUARD-002] 预期提示原目标插件管理').toContainText('插件管理');
      await expect(page.locator('[data-testid="nav-plugins"]'), '[TC-P3-GUARD-002] 受限用户不应看到插件管理菜单').toHaveCount(0);
    } finally {
      await context.close();
    }
  });

  test('TC-P3-GUARD-003 管理员可直接访问全部业务模块', async ({ page }) => {
    console.log('== phase == TC-P3-GUARD-003 admin can access all modules');
    for (const item of panelRoutes) {
      await page.goto(item.path);
      await assertConsoleShell(page);
      await assertModuleVisible(page, item.module);
      await expect(page.locator('[data-testid="module-forbidden"]'), `[TC-P3-GUARD-003] 管理员访问 ${item.path} 不应展示 403`).toHaveCount(0);
      await expect(page, `[TC-P3-GUARD-003] 管理员访问 ${item.path} 不应被重定向`).toHaveURL(new RegExp(`${item.path}$`));
    }
  });

  test('TC-P3-PANEL-001 面板拆分后业务 URL 仍可直达原页面', async ({ page }) => {
    console.log('== phase == TC-P3-PANEL-001 extracted panels keep route pages reachable');
    for (const item of panelRoutes) {
      await page.goto(item.path);
      await assertConsoleShell(page);
      await assertModuleVisible(page, item.module);
      await expect(page.locator(`[data-testid="${item.module}-page"]`), `预期 ${item.title} 标题仍可见`).toContainText(item.title);
      await expect(page, `预期 ${item.title} 地址栏保持 ${item.path}`).toHaveURL(new RegExp(`${item.path}$`));
    }
  });

  test('TC-P3-PANEL-002 面板拆分后核心交互入口仍可用', async ({ page }) => {
    console.log('== phase == TC-P3-PANEL-002 extracted panels keep primary interactions');

    await page.goto('/collect');
    await assertConsoleShell(page);
    await page.locator('[data-testid="show-input-form"]').click();
    await expect(page.locator('[data-testid="input-form-card"]'), '预期采集新增表单可打开').toBeVisible({ timeout: 10_000 });
    await expect(page.locator('[data-testid="input-plugin-syslog"]'), '预期 Syslog 内置采集插件可见').toBeVisible({ timeout: 15_000 });
    await page.locator('[data-testid="cancel-input-form"]').click();

    await page.goto('/parse');
    await assertConsoleShell(page);
    await page.locator('[data-testid="show-rule-form"]').click();
    await expect(page.locator('[data-testid="rule-form-card"]'), '预期解析新增表单可打开').toBeVisible({ timeout: 10_000 });
    await expect(page.locator('[data-testid="parser-regex"]'), '预期 Regex 内置解析插件可见').toBeVisible({ timeout: 15_000 });
    await page.locator('[data-testid="cancel-rule-form"]').click();

    await page.goto('/index');
    await assertConsoleShell(page);
    await page.locator('[data-testid="show-index-form"]').click();
    await expect(page.locator('[data-testid="index-form-card"]'), '预期索引新增表单可打开').toBeVisible({ timeout: 10_000 });
    await page.locator('[data-testid="cancel-index-form"]').click();

    await page.goto('/search');
    await assertConsoleShell(page);
    await expect(page.locator('[data-testid="search-query"]'), '预期搜索 SPL 输入框可见').toBeVisible();
    await expect(page.locator('[data-testid="search-time"]'), '预期搜索时间筛选器可见').toBeVisible();
    await expect(page.locator('[data-testid="search-results"]'), '预期搜索结果区域可见').toBeVisible();

    await page.goto('/plugins');
    await assertConsoleShell(page);
    await expect(page.locator('[data-testid="plugin-tab-input"]'), '预期采集插件 tab 可见').toBeVisible({ timeout: 10_000 });
    await expect(page.locator('[data-testid="plugin-tab-parser"]'), '预期解析插件 tab 可见').toBeVisible();
    await expect(page.locator('[data-testid="plugin-tab-search_command"]'), '预期搜索命令插件 tab 可见').toBeVisible();

    await page.goto('/rbac');
    await assertConsoleShell(page);
    await expect(page.locator('[data-testid="rbac-users-table"]'), '预期 RBAC 用户列表可见').toBeVisible({ timeout: 10_000 });
    await expect(page.locator('[data-testid="rbac-roles-table"]'), '预期默认不展示 RBAC 角色列表').toHaveCount(0);
    await page.locator('[data-testid="rbac-tab-roles"]').click();
    await expect(page.locator('[data-testid="rbac-roles-table"]'), '预期 RBAC 角色列表可见').toBeVisible();
  });

  test.afterAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      console.log('== phase == TC-P3-ROUTER afterAll cleanup restricted route guard user');
      await page.goto('/');
      await assertConsoleShell(page);
      await cleanupRouterGuardData(page);
    } finally {
      await context.close();
    }
  });
});
