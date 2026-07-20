/**
 * P2 RBAC 综合端到端验收脚本
 *
 * 对应人工验收文档：
 *   - docs/requirements/XDP_P2_人工验收.md
 *   - docs/requirements/references/XDP_P2_RBAC页面人工验收测试用例.md
 *
 * 覆盖用例：
 *   TC-P2-RBAC-PW-001 管理员创建角色/用户后受限用户 /api/v1/me 返回权限与 scope
 *   TC-P2-RBAC-PW-002 受限用户登录后顶部菜单按模块权限过滤
 *   TC-P2-RBAC-PW-003 index scope 允许授权 index 搜索并拒绝未授权 index
 *   TC-P2-RBAC-PW-004 plugin scope 允许 table 并拒绝 sort 外部搜索命令
 *   TC-P2-RBAC-PW-005 直接 API 越权返回 403
 *
 * 验收链路：
 *   登录态复用管理员 → API 创建测试 index、角色、用户 → Web 登录受限用户
 *   → 验证菜单可见性 → 验证 index scope 与 plugin scope → 验证直接 API 403
 *
 * 运行：npx playwright test tests/rbac-permissions.spec.ts --project=admin
 */
import { readFileSync } from 'node:fs';
import { basename, join } from 'node:path';
import { test, expect, type Browser, type Page } from '@playwright/test';

const API_URL = process.env.XDP_API_URL || 'http://127.0.0.1:8080';
const RUN_ID = Date.now();
const PREFIX = `accept_p2_rbac_${RUN_ID}`;
const ALLOWED_INDEX = `${PREFIX}_allowed`;
const DENIED_INDEX = `${PREFIX}_denied`;
const SEARCH_ROLE_CODE = `${PREFIX}_search`;
const COLLECT_ROLE_CODE = `${PREFIX}_collect`;
const SEARCH_USERNAME = `${PREFIX}_search_user`;
const COLLECT_USERNAME = `${PREFIX}_collect_user`;
const PASSWORD = 'xdpP2!23456';
const SEARCH_COMMAND_PLUGIN_PACKAGES = [
  { code: 'table', file: 'table-search-command-sample.zip' },
  { code: 'sort', file: 'sort-search-command-sample.zip' },
];

type HTTPMethod = 'GET' | 'POST' | 'PUT' | 'DELETE';

type APIResponsePayload = {
  error?: {
    code?: string;
    message?: string;
    required_permission?: string;
    required_scope?: string;
    resource_name?: string;
    plugin_type?: string;
    plugin_code?: string;
  };
  [key: string]: unknown;
};

let searchUserToken = '';
let collectUserToken = '';
let searchRoleID = '';
let collectRoleID = '';

async function authHeaders(page: Page) {
  const token = await page.evaluate(() => localStorage.getItem('xdp_api_token') || '');
  return { Authorization: `Bearer ${token}` };
}

async function apiFetchWithToken(
  page: Page,
  token: string,
  method: HTTPMethod,
  path: string,
  data?: unknown,
) {
  return page.request.fetch(`${API_URL}${path}`, {
    method,
    headers: {
      Authorization: `Bearer ${token}`,
      ...(data ? { 'Content-Type': 'application/json' } : {}),
    },
    data,
  });
}

async function requestJSONWithToken<T = APIResponsePayload>(
  page: Page,
  token: string,
  method: HTTPMethod,
  path: string,
  data?: unknown,
): Promise<T> {
  const response = await apiFetchWithToken(page, token, method, path, data);
  const text = await response.text();
  const payload = text ? JSON.parse(text) : {};
  if (!response.ok()) {
    throw new Error(`${method} ${path} failed: ${response.status()} ${text}`);
  }
  return payload as T;
}

async function requestJSON<T = APIResponsePayload>(
  page: Page,
  method: HTTPMethod,
  path: string,
  data?: unknown,
): Promise<T> {
  const headers = await authHeaders(page);
  return requestJSONWithToken<T>(page, headers.Authorization.replace(/^Bearer\s+/i, ''), method, path, data);
}

async function expectForbidden(
  page: Page,
  token: string,
  method: HTTPMethod,
  path: string,
  data: unknown | undefined,
  expected: Partial<NonNullable<APIResponsePayload['error']>>,
) {
  const response = await apiFetchWithToken(page, token, method, path, data);
  const text = await response.text();
  const payload = text ? JSON.parse(text) as APIResponsePayload : {};
  expect(response.status(), `${method} ${path} 预期返回 403，实际响应：${text}`).toBe(403);
  expect(payload.error?.code, `${method} ${path} 预期错误码为 FORBIDDEN`).toBe('FORBIDDEN');
  for (const [key, value] of Object.entries(expected)) {
    expect(payload.error?.[key as keyof NonNullable<APIResponsePayload['error']>], `${method} ${path} 预期 ${key}=${value}`).toBe(value);
  }
  return payload;
}

async function cleanupTestRBACData(page: Page) {
  const usersPayload = await requestJSON<{ users?: Array<{ id: string; username: string }> }>(page, 'GET', '/api/v1/users?page=1&page_size=1000').catch(() => ({ users: [] }));
  for (const user of usersPayload.users || []) {
    if (String(user.username || '').startsWith('accept_p2_rbac_')) {
      await requestJSON(page, 'DELETE', `/api/v1/users/${encodeURIComponent(user.id)}`).catch(() => undefined);
    }
  }

  const rolesPayload = await requestJSON<{ roles?: Array<{ id: string; role_code: string; builtin?: boolean }> }>(page, 'GET', '/api/v1/roles').catch(() => ({ roles: [] }));
  for (const role of rolesPayload.roles || []) {
    if (!role.builtin && String(role.role_code || '').startsWith('accept_p2_rbac_')) {
      await requestJSON(page, 'DELETE', `/api/v1/roles/${encodeURIComponent(role.id)}`).catch(() => undefined);
    }
  }

  for (const indexName of [ALLOWED_INDEX, DENIED_INDEX]) {
    await requestJSON(page, 'DELETE', `/api/v1/indexes?index=${encodeURIComponent(indexName)}&drop_storage=true`).catch(() => undefined);
  }
}

async function ensureIndexes(page: Page) {
  for (const indexName of [ALLOWED_INDEX, DENIED_INDEX]) {
    await requestJSON(page, 'POST', '/api/v1/indexes', {
      index_name: indexName,
      ttl_days: 7,
      status: 'active',
    });
  }
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

async function createRolesAndUsers(page: Page) {
  const searchRole = await requestJSON<{ id: string }>(page, 'POST', '/api/v1/roles', {
    role_code: SEARCH_ROLE_CODE,
    role_name: `验收受限搜索 ${RUN_ID}`,
    description: 'Playwright P2 RBAC index scope + plugin scope acceptance role.',
    status: 'active',
    permission_codes: ['index:read', 'search:execute'],
    index_scopes: {
      read: [ALLOWED_INDEX],
      search: [ALLOWED_INDEX],
    },
    plugin_scopes: {
      use: [
        { plugin_type: 'search_command', plugin_code: 'table' },
      ],
    },
  });
  searchRoleID = searchRole.id;

  const collectRole = await requestJSON<{ id: string }>(page, 'POST', '/api/v1/roles', {
    role_code: COLLECT_ROLE_CODE,
    role_name: `验收采集只读 ${RUN_ID}`,
    description: 'Playwright P2 RBAC menu visibility acceptance role.',
    status: 'active',
    permission_codes: ['datasource:read'],
    index_scopes: {},
    plugin_scopes: {},
  });
  collectRoleID = collectRole.id;

  await requestJSON<{ id: string }>(page, 'POST', '/api/v1/users', {
    username: SEARCH_USERNAME,
    display_name: `验收搜索受限用户 ${RUN_ID}`,
    password: PASSWORD,
    status: 'active',
    role_ids: [searchRoleID],
  });

  await requestJSON<{ id: string }>(page, 'POST', '/api/v1/users', {
    username: COLLECT_USERNAME,
    display_name: `验收采集只读用户 ${RUN_ID}`,
    password: PASSWORD,
    status: 'active',
    role_ids: [collectRoleID],
  });
}

async function loginAs(browser: Browser, username: string) {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.goto('/');
  await expect(page.locator('[data-testid="login-page"]'), `[${username}] 预期进入登录页`).toBeVisible();
  await page.locator('input[placeholder="请输入用户名"]').fill(username);
  await page.locator('input[placeholder="请输入密码"]').fill(PASSWORD);
  const [response] = await Promise.all([
    page.waitForResponse((res) => res.url().includes('/api/v1/login') && res.request().method() === 'POST'),
    page.locator('form.login-form button[type="submit"]').click(),
  ]);
  expect(response.ok(), `[${username}] 预期用户名密码登录成功`).toBe(true);
  await expect(page.locator('[data-testid="console-shell"]'), `[${username}] 预期进入控制台`).toBeVisible({ timeout: 10_000 });
  const token = await page.evaluate(() => localStorage.getItem('xdp_api_token') || '');
  expect(token.length, `[${username}] 预期登录后写入本地 Token`).toBeGreaterThan(0);
  return { context, page, token };
}

async function assertRestrictedPrincipal(page: Page, token: string, username: string) {
  const me = await requestJSONWithToken<{
    user?: { username?: string };
    roles?: Array<{ role_code?: string }>;
    permissions?: string[];
    scopes?: {
      indexes?: Record<string, { restricted?: boolean; patterns?: string[] }>;
      plugins?: Record<string, Array<{ plugin_type?: string; plugin_code?: string }>>;
    };
  }>(page, token, 'GET', '/api/v1/me');

  expect(
    me.user?.username,
    `受限用户登录后 /api/v1/me 应返回 ${username}；如果返回 admin/platform_admin，说明登录仍复用默认 Token，P2-RBAC 登录链路未闭环。`,
  ).toBe(username);
  return me;
}

function nav(page: Page, key: string) {
  return page.locator(`[data-testid="nav-${key}"]`);
}

test.describe('TC-P2-RBAC-PW RBAC 权限综合端到端', () => {
  test.describe.configure({ mode: 'serial' });
  test.setTimeout(120_000);

  test.beforeAll(async ({ browser }, testInfo) => {
    testInfo.setTimeout(Math.max(testInfo.timeout, 120_000));
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      console.log('== phase == TC-P2-RBAC-PW beforeAll open admin console');
      await page.goto('/');
      await expect(page.locator('[data-testid="logout"]')).toBeVisible();
      console.log('== phase == TC-P2-RBAC-PW beforeAll cleanup previous acceptance data');
      await cleanupTestRBACData(page);
      console.log('== phase == TC-P2-RBAC-PW beforeAll prepare indexes');
      await ensureIndexes(page);
      console.log('== phase == TC-P2-RBAC-PW beforeAll import and enable table/sort plugins');
      await importAndEnableSearchCommandPlugins(page);
      console.log('== phase == TC-P2-RBAC-PW beforeAll create roles and users');
      await createRolesAndUsers(page);
    } finally {
      await context.close();
    }
  });

  test('TC-P2-RBAC-PW-001 管理员创建角色/用户后受限用户 /api/v1/me 返回权限与 scope', async ({ browser }) => {
    console.log('== phase == TC-P2-RBAC-PW-001 login search user and assert /me scopes');
    const { context, page, token } = await loginAs(browser, SEARCH_USERNAME);
    try {
      searchUserToken = token;
      const me = await assertRestrictedPrincipal(page, token, SEARCH_USERNAME);
      expect(me.roles?.some((role) => role.role_code === SEARCH_ROLE_CODE), '[TC-P2-RBAC-PW-001] 预期绑定受限搜索角色').toBe(true);
      expect(me.permissions || [], '[TC-P2-RBAC-PW-001] 预期包含 search:execute').toContain('search:execute');
      expect(me.permissions || [], '[TC-P2-RBAC-PW-001] 预期包含 index:read').toContain('index:read');
      expect(me.permissions || [], '[TC-P2-RBAC-PW-001] 预期不包含 rbac:manage').not.toContain('rbac:manage');
      expect(me.scopes?.indexes?.search?.patterns || [], '[TC-P2-RBAC-PW-001] 预期 search scope 只包含授权 index').toContain(ALLOWED_INDEX);
      expect(me.scopes?.indexes?.search?.patterns || [], '[TC-P2-RBAC-PW-001] 预期 search scope 不包含未授权 index').not.toContain(DENIED_INDEX);
      expect(me.scopes?.plugins?.use || [], '[TC-P2-RBAC-PW-001] 预期 plugin use scope 包含 table').toEqual(
        expect.arrayContaining([expect.objectContaining({ plugin_type: 'search_command', plugin_code: 'table' })]),
      );
    } finally {
      await context.close();
    }
  });

  test('TC-P2-RBAC-PW-002 受限用户登录后顶部菜单按模块权限过滤', async ({ browser }) => {
    console.log('== phase == TC-P2-RBAC-PW-002 login collect user and assert menu visibility');
    const collectLogin = await loginAs(browser, COLLECT_USERNAME);
    try {
      collectUserToken = collectLogin.token;
      await assertRestrictedPrincipal(collectLogin.page, collectLogin.token, COLLECT_USERNAME);
      await expect(nav(collectLogin.page, 'collect'), '[TC-P2-RBAC-PW-002] 采集只读用户应看到采集配置菜单').toBeVisible();
      await expect(nav(collectLogin.page, 'parse'), '[TC-P2-RBAC-PW-002] 采集只读用户不应看到解析菜单').toHaveCount(0);
      await expect(nav(collectLogin.page, 'index'), '[TC-P2-RBAC-PW-002] 采集只读用户不应看到索引菜单').toHaveCount(0);
      await expect(nav(collectLogin.page, 'search'), '[TC-P2-RBAC-PW-002] 采集只读用户不应看到搜索菜单').toHaveCount(0);
      await expect(nav(collectLogin.page, 'plugins'), '[TC-P2-RBAC-PW-002] 采集只读用户不应看到插件管理菜单').toHaveCount(0);
      await expect(nav(collectLogin.page, 'rbac'), '[TC-P2-RBAC-PW-002] 采集只读用户不应看到用户与权限菜单').toHaveCount(0);
    } finally {
      await collectLogin.context.close();
    }

    console.log('== phase == TC-P2-RBAC-PW-002 login search user and assert menu visibility');
    const searchLogin = await loginAs(browser, SEARCH_USERNAME);
    try {
      searchUserToken = searchLogin.token;
      await assertRestrictedPrincipal(searchLogin.page, searchLogin.token, SEARCH_USERNAME);
      await expect(nav(searchLogin.page, 'index'), '[TC-P2-RBAC-PW-002] 搜索用户应看到索引配置菜单').toBeVisible();
      await expect(nav(searchLogin.page, 'search'), '[TC-P2-RBAC-PW-002] 搜索用户应看到搜索菜单').toBeVisible();
      await expect(nav(searchLogin.page, 'collect'), '[TC-P2-RBAC-PW-002] 搜索用户不应看到采集菜单').toHaveCount(0);
      await expect(nav(searchLogin.page, 'parse'), '[TC-P2-RBAC-PW-002] 搜索用户不应看到解析菜单').toHaveCount(0);
      await expect(nav(searchLogin.page, 'plugins'), '[TC-P2-RBAC-PW-002] 搜索用户不应看到插件管理菜单').toHaveCount(0);
      await expect(nav(searchLogin.page, 'rbac'), '[TC-P2-RBAC-PW-002] 搜索用户不应看到用户与权限菜单').toHaveCount(0);
    } finally {
      await searchLogin.context.close();
    }
  });

  test('TC-P2-RBAC-PW-003 index scope 允许授权 index 搜索并拒绝未授权 index', async ({ page }) => {
    console.log('== phase == TC-P2-RBAC-PW-003 assert index scope allow and deny');
    expect(searchUserToken, '[TC-P2-RBAC-PW-003] 预期已有受限搜索用户 token').not.toBe('');

    const allowed = await apiFetchWithToken(page, searchUserToken, 'GET', `/api/v1/search?q=${encodeURIComponent(`index=${ALLOWED_INDEX}`)}&limit=1&page=1`);
    expect(allowed.ok(), `[TC-P2-RBAC-PW-003] 授权 index=${ALLOWED_INDEX} 搜索应成功，实际 ${allowed.status()} ${await allowed.text()}`).toBe(true);

    await expectForbidden(
      page,
      searchUserToken,
      'GET',
      `/api/v1/search?q=${encodeURIComponent(`index=${DENIED_INDEX}`)}&limit=1&page=1`,
      undefined,
      { required_scope: 'index:search', resource_name: DENIED_INDEX },
    );

    const indexes = await requestJSONWithToken<{ indexes?: Array<{ index_name?: string; name?: string }> }>(page, searchUserToken, 'GET', '/api/v1/indexes?page=1&page_size=1000');
    const visibleNames = (indexes.indexes || []).map((item) => item.index_name || item.name || '');
    expect(visibleNames, '[TC-P2-RBAC-PW-003] 受限用户 index 列表应包含授权 index').toContain(ALLOWED_INDEX);
    expect(visibleNames, '[TC-P2-RBAC-PW-003] 受限用户 index 列表不应包含未授权 index').not.toContain(DENIED_INDEX);
  });

  test('TC-P2-RBAC-PW-004 plugin scope 允许 table 并拒绝 sort 外部搜索命令', async ({ page }) => {
    console.log('== phase == TC-P2-RBAC-PW-004 assert plugin scope allow table and deny sort');
    expect(searchUserToken, '[TC-P2-RBAC-PW-004] 预期已有受限搜索用户 token').not.toBe('');

    const tableResponse = await apiFetchWithToken(page, searchUserToken, 'GET', `/api/v1/search?q=${encodeURIComponent(`index=${ALLOWED_INDEX} | table raw`)}&limit=10&page=1`);
    expect(tableResponse.ok(), `[TC-P2-RBAC-PW-004] 已授权 table 搜索命令应成功，实际 ${tableResponse.status()} ${await tableResponse.text()}`).toBe(true);

    await expectForbidden(
      page,
      searchUserToken,
      'GET',
      `/api/v1/search?q=${encodeURIComponent(`index=${ALLOWED_INDEX} | sort - _time`)}&limit=10&page=1`,
      undefined,
      { required_scope: 'plugin:use', plugin_type: 'search_command', plugin_code: 'sort' },
    );
  });

  test('TC-P2-RBAC-PW-005 直接 API 越权返回 403', async ({ page }) => {
    console.log('== phase == TC-P2-RBAC-PW-005 assert direct unauthorized APIs return 403');
    expect(searchUserToken, '[TC-P2-RBAC-PW-005] 预期已有搜索用户 token').not.toBe('');
    expect(collectUserToken, '[TC-P2-RBAC-PW-005] 预期已有采集只读用户 token').not.toBe('');

    await expectForbidden(page, collectUserToken, 'POST', '/api/v1/indexes', {
      index_name: `${PREFIX}_collect_forbidden`,
      ttl_days: 7,
      status: 'active',
    }, { required_permission: 'index:manage' });

    await expectForbidden(page, collectUserToken, 'POST', '/api/v1/datasources', {
      name: `${PREFIX}_forbidden_syslog`,
      plugin_code: 'syslog',
      status: 'active',
      plugin_config: {
        collector_port: 5514,
        transport_protocol: 'UDP',
        encoding: 'UTF-8',
        log_filter_enabled: false,
      },
    }, { required_permission: 'datasource:create' });

    await expectForbidden(page, searchUserToken, 'POST', `/api/v1/plugins/${encodeURIComponent('table')}/disable?plugin_type=search_command`, undefined, {
      required_scope: 'plugin:manage',
      plugin_type: 'search_command',
    });
  });

  test.afterAll(async ({ browser }, testInfo) => {
    testInfo.setTimeout(Math.max(testInfo.timeout, 60_000));
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      console.log('== phase == TC-P2-RBAC-PW afterAll cleanup users roles indexes');
      await page.goto('/');
      await expect(page.locator('[data-testid="logout"]')).toBeVisible();
      await cleanupTestRBACData(page);
    } finally {
      await context.close();
    }
  });
});
