/**
 * 插件管理端到端验收脚本
 *
 * 覆盖用例：TC-P1-PLUGIN-001 ~ TC-P1-PLUGIN-008
 *
 * 验收链路：
 *   登录态复用 → 生成临时插件包 → 进入插件管理页 → 类型切换
 *   → 内置插件保护展示 → 非法包错误提示 → 上传外部插件
 *   → 查看行内详情 → 启用 / 覆盖确认 / 停用 / 删除清理
 *
 * 运行：npx playwright test tests/plugin-management.spec.ts --project=admin
 */
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test, expect, type Page } from '@playwright/test';

const API_URL = process.env.XDP_API_URL || 'http://127.0.0.1:8080';
const RUN_ID = Date.now();
const PLUGIN_CODE = `accept-plugin-${RUN_ID}`;
const PLUGIN_NAME = `Accept Plugin ${RUN_ID}`;
const PAGINATION_PLUGIN_PREFIX = `accept-plugin-page-${RUN_ID}`;
const PAGINATION_PLUGIN_CODES = Array.from({ length: 12 }, (_, index) => `${PAGINATION_PLUGIN_PREFIX}-${index + 1}`);

let packageDir = '';
let pluginPackagePath = '';
let invalidPackagePath = '';
let paginationPluginPackagePaths: Array<{ code: string; path: string }> = [];

function requireZipCommand() {
  try {
    execFileSync('zip', ['-v'], { stdio: 'ignore' });
  } catch {
    throw new Error('missing required command: zip');
  }
}

async function authHeaders(page: Page) {
  const token = await page.evaluate(() => localStorage.getItem('xdp_api_token') || '');
  return { Authorization: `Bearer ${token}` };
}

async function requestJSON<T = Record<string, unknown>>(page: Page, method: 'GET' | 'POST' | 'DELETE', path: string): Promise<T> {
  const headers = await authHeaders(page);
  const response = await page.request.fetch(`${API_URL}${path}`, { method, headers });
  if (!response.ok()) {
    throw new Error(`${method} ${path} failed: ${response.status()} ${await response.text()}`);
  }
  const text = await response.text();
  return (text ? JSON.parse(text) : {}) as T;
}

async function cleanupPlugin(page: Page, pluginType: string, pluginCode: string) {
  const type = encodeURIComponent(pluginType);
  const code = encodeURIComponent(pluginCode);
  await requestJSON(page, 'POST', `/api/v1/plugins/${code}/disable?plugin_type=${type}`).catch(() => undefined);
  await requestJSON(page, 'DELETE', `/api/v1/plugins/${code}?plugin_type=${type}`).catch(() => undefined);
}

async function cleanupPluginPrefix(page: Page, pluginType: string, prefix: string) {
  const payload = await requestJSON<{ plugins?: Array<{ plugin_code?: string }> }>(page, 'GET', `/api/v1/plugins?plugin_type=${encodeURIComponent(pluginType)}&page=1&page_size=1000`).catch(() => ({ plugins: [] }));
  for (const plugin of payload.plugins || []) {
    const code = String(plugin.plugin_code || '').trim();
    if (code.startsWith(prefix)) {
      await cleanupPlugin(page, pluginType, code);
    }
  }
}

function createInputPluginPackageZip(code: string, name: string) {
  const pluginDir = join(packageDir, code);
  mkdirSync(pluginDir, { recursive: true });
  const manifest = {
    plugin_code: code,
    plugin_type: 'input',
    plugin_version: '1.0.0',
    name,
    description: 'Playwright generated pagination plugin.',
    runtime: 'go_builtin',
    entrypoint: `builtin://plugins/input/${code}`,
    config_schema: {
      type: 'object',
      required: ['endpoint'],
      properties: {
        endpoint: { type: 'string', minLength: 1, title: 'Endpoint' },
      },
    },
    ui_schema: { order: ['endpoint'] },
  };
  writeFileSync(join(pluginDir, 'manifest.json'), `${JSON.stringify(manifest, null, 2)}\n`);
  const output = join(packageDir, `${code}.zip`);
  execFileSync('zip', ['-qr', output, 'manifest.json'], { cwd: pluginDir });
  return output;
}

function createPluginPackage() {
  requireZipCommand();
  packageDir = mkdtempSync(join(tmpdir(), 'xdp-plugin-e2e-'));
  const validDir = join(packageDir, 'valid');
  const invalidDir = join(packageDir, 'invalid');
  mkdirSync(validDir, { recursive: true });
  mkdirSync(invalidDir, { recursive: true });

  const manifest = {
    plugin_code: PLUGIN_CODE,
    plugin_type: 'input',
    plugin_version: '1.0.0',
    name: PLUGIN_NAME,
    description: 'Playwright generated input plugin for plugin management acceptance.',
    runtime: 'go_builtin',
    entrypoint: 'builtin://plugins/input/acceptance',
    config_schema: {
      type: 'object',
      required: ['endpoint'],
      properties: {
        endpoint: {
          type: 'string',
          minLength: 1,
          title: 'Endpoint',
        },
      },
    },
    ui_schema: {
      order: ['endpoint'],
    },
  };

  writeFileSync(join(validDir, 'manifest.json'), `${JSON.stringify(manifest, null, 2)}\n`);
  writeFileSync(join(invalidDir, 'README.txt'), 'invalid plugin package without manifest\n');

  pluginPackagePath = join(packageDir, `${PLUGIN_CODE}.zip`);
  invalidPackagePath = join(packageDir, `${PLUGIN_CODE}-invalid.zip`);
  execFileSync('zip', ['-qr', pluginPackagePath, 'manifest.json'], { cwd: validDir });
  execFileSync('zip', ['-qr', invalidPackagePath, 'README.txt'], { cwd: invalidDir });
  paginationPluginPackagePaths = PAGINATION_PLUGIN_CODES.map((code, index) => ({
    code,
    path: createInputPluginPackageZip(code, `Pagination Plugin ${RUN_ID}-${index + 1}`),
  }));
}

function removePluginPackage() {
  if (packageDir) rmSync(packageDir, { recursive: true, force: true });
}

async function openPluginPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('[data-testid="logout"]')).toBeVisible();
  await page.click('[data-testid="nav-plugins"]');
  await expect(page.locator('[data-testid="plugins-page"]')).toBeVisible();
  await page.locator('[data-testid="plugin-page-size"]').selectOption('1000');
}

async function selectPluginTab(page: Page, pluginType: 'input' | 'parser' | 'search_command') {
  await page.click(`[data-testid="plugin-tab-${pluginType}"]`);
  await expect(page.locator(`[data-testid="plugin-tab-${pluginType}"]`)).toHaveClass(/active/);
  await page.locator('[data-testid="plugin-page-size"]').selectOption('1000');
}

function pluginRow(page: Page, pluginCode = PLUGIN_CODE, version = '1.0.0') {
  return page.locator(`[data-testid="plugin-row-${pluginCode}-${version}"]`).first();
}

async function uploadPluginPackage(page: Page, filePath: string) {
  await page.locator('[data-testid="plugin-upload-file"]').setInputFiles(filePath);
  await expect(page.locator('[data-testid="plugin-upload-filename"]')).toContainText(filePath.split('/').pop() || '');
  await Promise.all([
    page.waitForResponse((res) => {
      const url = new URL(res.url());
      return url.pathname === '/api/v1/plugins/import' && res.request().method() === 'POST';
    }),
    page.click('[data-testid="plugin-upload-button"]'),
  ]);
}

async function importPluginPackageByAPI(page: Page, filePath: string) {
  const headers = await authHeaders(page);
  const response = await page.request.fetch(`${API_URL}/api/v1/plugins/import?overwrite=true`, {
    method: 'POST',
    headers,
    multipart: {
      file: {
        name: filePath.split('/').pop() || 'plugin.zip',
        mimeType: 'application/zip',
        buffer: readFileSync(filePath),
      },
    },
  });
  if (!response.ok()) {
    throw new Error(`import plugin package failed: ${response.status()} ${await response.text()}`);
  }
}

test.describe('TC-P1-PLUGIN 插件管理端到端', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeAll(async ({ browser }) => {
    createPluginPackage();
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      await page.goto('/');
      await expect(page.locator('[data-testid="logout"]')).toBeVisible();
      await cleanupPlugin(page, 'input', PLUGIN_CODE);
      await cleanupPluginPrefix(page, 'input', PAGINATION_PLUGIN_PREFIX);
    } finally {
      await context.close();
    }
  });

  test.beforeEach(async ({ page }) => {
    await openPluginPage(page);
  });

  test('TC-P1-PLUGIN-001 进入插件管理页面并展示三类插件页签', async ({ page }) => {
    console.log('== phase == TC-P1-PLUGIN-001 assert plugins page visible');
    const pluginsPage = page.locator('[data-testid="plugins-page"]');
    await expect(pluginsPage.getByRole('heading', { name: /插件管理/ }), '[TC-P1-PLUGIN-001] 预期展示插件管理标题').toBeVisible();
    await expect(page.locator('[data-testid="plugin-tab-input"]'), '[TC-P1-PLUGIN-001] 预期展示 input 页签').toBeVisible();
    await expect(page.locator('[data-testid="plugin-tab-parser"]'), '[TC-P1-PLUGIN-001] 预期展示 parser 页签').toBeVisible();
    await expect(page.locator('[data-testid="plugin-tab-search_command"]'), '[TC-P1-PLUGIN-001] 预期展示 search_command 页签').toBeVisible();
    await expect(page.locator('[data-testid="plugin-pagination"]'), '[TC-P1-PLUGIN-001] 预期展示分页控件').toBeVisible();
  });

  test('TC-P1-PLUGIN-002 内置基础插件仅展示保护状态，不展示详情和启停删除操作', async ({ page }) => {
    console.log('== phase == TC-P1-PLUGIN-002 assert builtin plugins protected');
    await selectPluginTab(page, 'input');
    await expect(pluginRow(page, 'syslog'), '[TC-P1-PLUGIN-002] 预期 syslog 展示内置基础能力').toContainText('内置基础能力');
    await expect(page.locator('[data-testid="plugin-detail-syslog"]'), '[TC-P1-PLUGIN-002] 预期 syslog 无详情按钮').not.toBeVisible();
    await expect(page.locator('[data-testid="plugin-enable-syslog"]'), '[TC-P1-PLUGIN-002] 预期 syslog 无启用按钮').not.toBeVisible();
    await expect(page.locator('[data-testid="plugin-delete-syslog"]'), '[TC-P1-PLUGIN-002] 预期 syslog 无删除按钮').not.toBeVisible();

    await selectPluginTab(page, 'parser');
    await expect(pluginRow(page, 'regex'), '[TC-P1-PLUGIN-002] 预期 regex 展示内置基础能力').toContainText('内置基础能力');
    await expect(page.locator('[data-testid="plugin-detail-regex"]'), '[TC-P1-PLUGIN-002] 预期 regex 无详情按钮').not.toBeVisible();

    await selectPluginTab(page, 'search_command');
    await expect(pluginRow(page, 'stats'), '[TC-P1-PLUGIN-002] 预期 stats 展示内置基础能力').toContainText('内置基础能力');
    await expect(page.locator('[data-testid="plugin-detail-stats"]'), '[TC-P1-PLUGIN-002] 预期 stats 无详情按钮').not.toBeVisible();
  });

  test('TC-P1-PLUGIN-003 上传非法插件包时展示明确错误', async ({ page }) => {
    console.log('== phase == TC-P1-PLUGIN-003 upload invalid package and expect error');
    await selectPluginTab(page, 'input');

    await uploadPluginPackage(page, invalidPackagePath);

    await expect(page.locator('[data-testid="plugin-upload-error"]'), '[TC-P1-PLUGIN-003] 预期提示 manifest 缺失错误').toContainText(/PLUGIN_MANIFEST_MISSING|manifest/i);
  });

  test('TC-P1-PLUGIN-004 上传外部插件包后自动识别类型并展示禁用状态', async ({ page }) => {
    console.log('== phase == TC-P1-PLUGIN-004 upload valid package and assert row');
    await selectPluginTab(page, 'parser');

    await uploadPluginPackage(page, pluginPackagePath);

    await expect(page.locator('[data-testid="plugin-tab-input"]'), '[TC-P1-PLUGIN-004] 预期自动切换到 input 页签').toHaveClass(/active/);
    await expect(page.locator('[data-testid="plugin-upload-status"]'), '[TC-P1-PLUGIN-004] 预期展示导入成功').toContainText('导入成功');
    const row = pluginRow(page);
    await expect(row, '[TC-P1-PLUGIN-004] 预期插件行出现').toBeVisible({ timeout: 10_000 });
    await expect(row, '[TC-P1-PLUGIN-004] 预期行含插件名').toContainText(PLUGIN_NAME);
    await expect(row, '[TC-P1-PLUGIN-004] 预期行含插件 code').toContainText(PLUGIN_CODE);
    await expect(row, '[TC-P1-PLUGIN-004] 预期行含版本号').toContainText('1.0.0');
    await expect(row, '[TC-P1-PLUGIN-004] 预期行含未启用状态').toContainText('未启用');
    await expect(page.locator(`[data-testid="plugin-enable-${PLUGIN_CODE}"]`), '[TC-P1-PLUGIN-004] 预期存在启用按钮').toBeVisible();
    await expect(page.locator(`[data-testid="plugin-delete-${PLUGIN_CODE}"]`), '[TC-P1-PLUGIN-004] 预期存在删除按钮').toBeVisible();
  });

  test('TC-P1-PLUGIN-005 查看外部插件行内详情并展示 schema', async ({ page }) => {
    console.log('== phase == TC-P1-PLUGIN-005 open detail and assert schema');
    await selectPluginTab(page, 'input');
    const row = pluginRow(page);
    await expect(row, '[TC-P1-PLUGIN-005] 预期插件行出现').toBeVisible();

    await page.click(`[data-testid="plugin-detail-${PLUGIN_CODE}"]`);

    const detail = page.locator(`[data-testid="plugin-detail-row-${PLUGIN_CODE}"]`);
    await expect(detail, '[TC-P1-PLUGIN-005] 预期详情行展开').toBeVisible({ timeout: 10_000 });
    await expect(detail, '[TC-P1-PLUGIN-005] 预期详情含插件 code').toContainText(PLUGIN_CODE);
    await expect(detail, '[TC-P1-PLUGIN-005] 预期详情含 input 类型').toContainText('input');
    await expect(detail, '[TC-P1-PLUGIN-005] 预期详情含 Config Schema').toContainText('Config Schema');
    await expect(detail, '[TC-P1-PLUGIN-005] 预期详情含 endpoint 字段').toContainText('endpoint');
    await expect(detail, '[TC-P1-PLUGIN-005] 预期详情含 UI Schema').toContainText('UI Schema');
  });

  test('TC-P1-PLUGIN-006 启用插件后状态和操作按钮同步切换', async ({ page }) => {
    console.log('== phase == TC-P1-PLUGIN-006 enable plugin and assert state switch');
    await selectPluginTab(page, 'input');
    await expect(pluginRow(page), '[TC-P1-PLUGIN-006] 预期插件行出现').toBeVisible();

    await page.click(`[data-testid="plugin-enable-${PLUGIN_CODE}"]`);

    const row = pluginRow(page);
    await expect(row, '[TC-P1-PLUGIN-006] 预期行含已启用状态').toContainText('已启用', { timeout: 10_000 });
    await expect(page.locator(`[data-testid="plugin-disable-${PLUGIN_CODE}"]`), '[TC-P1-PLUGIN-006] 预期出现停用按钮').toBeVisible();
    await expect(page.locator(`[data-testid="plugin-delete-${PLUGIN_CODE}"]`), '[TC-P1-PLUGIN-006] 预期启用后删除按钮不可见').not.toBeVisible();
  });

  test('TC-P1-PLUGIN-007 重复上传触发覆盖确认，停用后可删除', async ({ page }) => {
    console.log('== phase == TC-P1-PLUGIN-007 reupload, confirm override, disable and delete');
    await selectPluginTab(page, 'input');

    page.once('dialog', async (dialog) => {
      expect(dialog.message(), '[TC-P1-PLUGIN-007] 预期覆盖确认对话框含插件已存在').toContain('插件已存在');
      await dialog.accept();
    });
    await uploadPluginPackage(page, pluginPackagePath);
    await expect(page.locator('[data-testid="plugin-upload-status"]'), '[TC-P1-PLUGIN-007] 预期展示已覆盖').toContainText('已覆盖', { timeout: 10_000 });

    await page.click(`[data-testid="plugin-disable-${PLUGIN_CODE}"]`);
    await expect(pluginRow(page), '[TC-P1-PLUGIN-007] 预期停用后行含未启用').toContainText('未启用', { timeout: 10_000 });

    await page.click(`[data-testid="plugin-delete-${PLUGIN_CODE}"]`);
    await expect(pluginRow(page), '[TC-P1-PLUGIN-007] 预期删除后行不可见').not.toBeVisible({ timeout: 10_000 });
  });

  test('TC-P1-PLUGIN-008 插件列表分页上一页下一页和每页条数边界', async ({ page }) => {
    console.log('== phase == TC-P1-PLUGIN-008 pagination next prev and page size boundary');
    await selectPluginTab(page, 'input');
    await cleanupPluginPrefix(page, 'input', PAGINATION_PLUGIN_PREFIX);
    for (const plugin of paginationPluginPackagePaths) {
      await importPluginPackageByAPI(page, plugin.path);
    }

    await openPluginPage(page);
    await selectPluginTab(page, 'input');
    await page.locator('[data-testid="plugin-page-size"]').selectOption('10');

    await expect(page.locator('[data-testid="plugin-pagination"]'), '[TC-P1-PLUGIN-008] 预期分页控件可见').toBeVisible({ timeout: 10_000 });
    await expect(page.locator('[data-testid="plugin-page-1"]'), '[TC-P1-PLUGIN-008] 预期第 1 页可见').toBeVisible();
    await expect(page.locator('[data-testid="plugin-page-1"]'), '[TC-P1-PLUGIN-008] 预期当前位于第 1 页').toHaveClass(/active/);
    await expect(page.locator('[data-testid="plugin-page-2"]'), '[TC-P1-PLUGIN-008] 多插件数据下应展示第 2 页').toBeVisible({ timeout: 10_000 });
    await expect(page.locator('[data-testid="plugin-prev"]'), '[TC-P1-PLUGIN-008] 首页上一页应禁用').toBeDisabled();
    await expect(page.locator('[data-testid="plugin-next"]'), '[TC-P1-PLUGIN-008] 首页下一页应可点击').toBeEnabled();

    await page.locator('[data-testid="plugin-next"]').click();
    await expect(page.locator('[data-testid="plugin-page-2"]'), '[TC-P1-PLUGIN-008] 点击下一页后第 2 页 active').toHaveClass(/active/, { timeout: 10_000 });
    await expect(page.locator('[data-testid="plugin-prev"]'), '[TC-P1-PLUGIN-008] 第 2 页上一页应可点击').toBeEnabled();

    await page.locator('[data-testid="plugin-page-size"]').selectOption('1000');
    await expect(page.locator('[data-testid="plugin-page-1"]'), '[TC-P1-PLUGIN-008] 切换 1000 条/页后回到第 1 页').toHaveClass(/active/, { timeout: 10_000 });
    await expect(page.locator('[data-testid="plugin-page-2"]'), '[TC-P1-PLUGIN-008] 切换 1000 条/页后不再展示第 2 页').toHaveCount(0);
  });

  test.afterAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      await page.goto('/');
      await expect(page.locator('[data-testid="logout"]')).toBeVisible();
      await cleanupPlugin(page, 'input', PLUGIN_CODE);
      await cleanupPluginPrefix(page, 'input', PAGINATION_PLUGIN_PREFIX);
    } finally {
      await context.close();
      removePluginPackage();
    }
  });
});
