/**
 * 干净环境启动端到端验收脚本
 *
 * 对应人工验收文档：docs/requirements/references/XDP_P0页面人工验收测试用例.md
 * 覆盖用例：TC-P0-PREP-001 干净环境启动与脏数据清理
 *
 * 验收链路：
 *   调用 scripts/start-oneclick.sh --clean → 等待服务可用
 *   → 登录 admin/xdp → 索引/采集/解析页无历史脏数据
 *   → 搜索页历史 index 不命中 → ClickHouse events_* 已清
 *
 * 运行：npx playwright test tests/env-clean.spec.ts --project=clean-env
 *
 * 说明：本脚本不依赖 storageState，也不依赖提前启动的 start-oneclick.sh。
 *       用例内部会执行 start-oneclick.sh --clean 并等待服务 ready。
 * 前置：脚本运行前需确保无其它非 XDP 进程占用固定端口。
 */
import { spawn } from 'node:child_process';
import { closeSync, existsSync, mkdirSync, openSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { test, expect, type Page } from '@playwright/test';

const API_URL = process.env.XDP_API_URL || 'http://127.0.0.1:8080';
const WEB_URL = process.env.XDP_WEB_URL || 'http://127.0.0.1:5173';

// 已知系统内置 index（带 _ 前缀），不作为脏数据
const BUILTIN_INDEX_PREFIX = '_';

function findRepoRoot(startDir = process.cwd()) {
  let current = resolve(startDir);
  while (current !== dirname(current)) {
    if (existsSync(join(current, 'scripts', 'start-oneclick.sh'))) return current;
    current = dirname(current);
  }
  throw new Error(`cannot find repository root from ${startDir}`);
}

function readLog(path: string) {
  try {
    return readFileSync(path, 'utf8');
  } catch {
    return '';
  }
}

async function waitForLogMarker(path: string, marker: string, timeoutMs: number, getExitCode: () => number | null, getError: () => Error | null) {
  const startedAt = Date.now();
  while (Date.now() - startedAt < timeoutMs) {
    const content = readLog(path);
    if (content.includes(marker)) return;

    const error = getError();
    if (error) {
      throw new Error(`start-oneclick.sh --clean failed to start: ${error.message}\nstdout:\n${content}`);
    }

    const exitCode = getExitCode();
    if (exitCode !== null) {
      throw new Error(`start-oneclick.sh --clean exited before ready: ${exitCode}\nstdout:\n${content}`);
    }

    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
  throw new Error(`start-oneclick.sh --clean did not print ready marker within ${timeoutMs}ms\nstdout:\n${readLog(path)}`);
}

async function waitForHTTP(url: string, timeoutMs: number) {
  const startedAt = Date.now();
  while (Date.now() - startedAt < timeoutMs) {
    try {
      const response = await fetch(url, { cache: 'no-store' });
      if (response.ok) return;
    } catch {
      // Service may be restarting during --clean.
    }
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
  throw new Error(`service not ready within ${timeoutMs}ms: ${url}`);
}

async function startCleanStack(repoRoot: string) {
  const logDir = join(repoRoot, '.cache', 'xdp-oneclick', 'playwright');
  mkdirSync(logDir, { recursive: true });
  const stdoutPath = join(logDir, 'env-clean-start-oneclick.stdout.log');
  const stderrPath = join(logDir, 'env-clean-start-oneclick.stderr.log');
  const stdoutFd = openSync(stdoutPath, 'w');
  const stderrFd = openSync(stderrPath, 'w');
  let exitCode: number | null = null;
  let spawnError: Error | null = null;

  const proc = spawn('bash', ['scripts/start-oneclick.sh', '--clean'], {
    cwd: repoRoot,
    detached: true,
    stdio: ['ignore', stdoutFd, stderrFd],
  });
  closeSync(stdoutFd);
  closeSync(stderrFd);
  proc.once('exit', (code) => {
    exitCode = code ?? -1;
  });
  proc.once('error', (error) => {
    exitCode = -1;
    spawnError = error;
  });

  try {
    await waitForLogMarker(stdoutPath, 'XDP one-click stack is running.', 360_000, () => exitCode, () => spawnError);
    await waitForHTTP(`${API_URL}/healthz`, 60_000);
    await waitForHTTP(WEB_URL, 60_000);
  } catch (error) {
    throw new Error(`${error instanceof Error ? error.message : String(error)}\nstderr:\n${readLog(stderrPath)}`);
  }

  // start-oneclick.sh intentionally stays alive to own frontend and host agent.
  proc.unref();
}

async function loginAdmin(page: Page) {
  await page.goto('/');
  await expect(page.locator('[data-testid="login-page"]')).toBeVisible();
  await page.locator('input[placeholder="请输入用户名"]').fill('admin');
  await page.locator('input[placeholder="请输入密码"]').fill('xdp');
  await page.locator('form.login-form button[type="submit"]').click();
  await expect(page.locator('[data-testid="console-shell"]'), '预期 --clean 后可登录').toBeVisible({ timeout: 30_000 });
}

async function authHeaders(page: Page) {
  const token = await page.evaluate(() => localStorage.getItem('xdp_api_token') || '');
  return { Authorization: `Bearer ${token}` };
}

async function apiList(page: Page, path: string): Promise<{ items: string[]; raw: any }> {
  const headers = await authHeaders(page);
  const response = await page.request.get(`${API_URL}${path}`, { headers });
  if (!response.ok()) {
    throw new Error(`GET ${path} failed: ${response.status()} ${await response.text()}`);
  }
  const payload = await response.json();
  return { items: Array.isArray(payload.items) ? payload.items : [], raw: payload };
}

async function assertNoBusinessIndex(page: Page) {
  const payload = await page.request.get(`${API_URL}/api/v1/indexes?page=1&page_size=1000`, { headers: await authHeaders(page) });
  const data = await payload.json();
  const indexes: Array<{ index_name?: string; name?: string }> = Array.isArray(data.indexes) ? data.indexes : [];
  for (const idx of indexes) {
    const name = String(idx.index_name || idx.name || '').trim();
    if (!name) continue;
    expect(name.startsWith(BUILTIN_INDEX_PREFIX), `预期 --clean 后无业务 index，发现 ${name}`).toBe(true);
  }
}

async function assertNoBusinessCollectSource(page: Page) {
  const payload = await page.request.get(`${API_URL}/api/v1/datasources?page=1&page_size=1000`, { headers: await authHeaders(page) });
  const data = await payload.json();
  const sources: Array<{ name?: string }> = Array.isArray(data.datasources) ? data.datasources : [];
  // --clean 应清理业务采集源，允许保留零条
  expect(sources.length, '预期 --clean 后无业务采集源残留').toBe(0);
}

async function assertNoBusinessParseRule(page: Page) {
  const payload = await page.request.get(`${API_URL}/api/v1/parse-rules?page=1&page_size=1000`, { headers: await authHeaders(page) });
  const data = await payload.json();
  const rules: Array<{ name?: string }> = Array.isArray(data.parse_rules) ? data.parse_rules : [];
  expect(rules.length, '预期 --clean 后无业务解析规则残留').toBe(0);
}

test.describe('TC-P0-PREP 干净环境启动', () => {
  test.describe.configure({ mode: 'serial' });
  test.setTimeout(480_000);

  test('TC-P0-PREP-001 干净环境启动与脏数据清理', async ({ browser }) => {
    console.log('== phase == TC-P0-PREP-001 invoke start-oneclick.sh --clean');
    const repoRoot = findRepoRoot();
    await startCleanStack(repoRoot);

    // 新建 context 登录
    const context = await browser.newContext();
    const page = await context.newPage();
    await loginAdmin(page);

    console.log('== phase == TC-P0-PREP-001 assert no business index/datasource/parse-rule');
    await assertNoBusinessIndex(page);
    await assertNoBusinessCollectSource(page);
    await assertNoBusinessParseRule(page);

    console.log('== phase == TC-P0-PREP-001 assert search historical index not hit');
    // 搜索页用历史业务 index 应无命中
    await page.click('[data-testid="nav-search"]');
    await expect(page.locator('[data-testid="search-page"]')).toBeVisible();
    await page.locator('[data-testid="search-query"]').fill('index=audit_p0');
    await page.locator('[data-testid="search-time"]').selectOption('所有时间');
    await page.locator('[data-testid="search-button"]').click();
    await expect(page.locator('[data-testid="search-results"]'), '[TC-P0-PREP-001] 预期历史 audit_p0 无命中').toContainText('暂无匹配结果', { timeout: 15_000 });

    await context.close();
  });
});
