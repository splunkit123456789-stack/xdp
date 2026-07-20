/**
 * Writer 入库页面端到端验收脚本
 *
 * 对应人工验收文档：docs/requirements/references/XDP_P1_Writer入库页面人工验收.md
 * 覆盖用例：TC-P1-WEB-WRITER-001~003、TC-P1-WEB-SEARCH-001~006（9 个）
 *
 * 验收链路：
 *   登录态复用 → Bash 预置 writer_bench + writer_recovery 数据
 *   → 索引页查看 Writer 入库状态卡片 + 刷新
 *   → 列表断言 writer_bench/writer_recovery 行存在
 *   → 搜索页查询 bench 原始事件 + 展开明细 + 字段过滤 + stats 聚合
 *   → 搜索页查询 recovery 原始事件 + stats 聚合
 *
 * 运行：npx playwright test tests/writer-page.spec.ts --project=admin
 *
 * 说明：本脚本依赖 Bash 压测脚本预置数据，实跑需 Writer 服务可访问（端口 8082）。
 */
import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { test, expect, type Page } from '@playwright/test';

const API_URL = process.env.XDP_API_URL || 'http://127.0.0.1:8080';
const REPO_ROOT = findRepoRoot();

function findRepoRoot() {
  let dir = process.cwd();
  for (let i = 0; i < 6; i += 1) {
    if (existsSync(join(dir, 'scripts', 'writer-benchmark.sh'))) {
      return dir;
    }
    dir = resolve(dir, '..');
  }
  return resolve(process.cwd(), '..', '..');
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

// 调 Bash 压测脚本预置 writer_bench / writer_recovery 数据
async function spawnScript(cmd: string, args: string[], env: Record<string, string> = {}, timeoutMs = 600_000) {
  let stdout = '';
  let stderr = '';
  const exitCode = await new Promise<number>((resolve, reject) => {
    const proc = spawn(cmd, args, { cwd: REPO_ROOT, env: { ...process.env, ...env } });
    const timeout = setTimeout(() => {
      proc.kill('SIGTERM');
      reject(new Error(`${cmd} ${args.join(' ')} 超过 ${timeoutMs}ms 未退出`));
    }, timeoutMs);
    proc.stdout.on('data', (chunk) => {
      stdout += String(chunk);
    });
    proc.stderr.on('data', (chunk) => {
      stderr += String(chunk);
    });
    proc.on('exit', (code) => {
      clearTimeout(timeout);
      resolve(code ?? -1);
    });
    proc.on('error', reject);
  });
  if (exitCode !== 0) {
    throw new Error(`${cmd} ${args.join(' ')} 退出码 ${exitCode}\nstdout:\n${stdout.slice(-2000)}\nstderr:\n${stderr.slice(-2000)}`);
  }
}

async function prepareWriterData() {
  console.log('== phase == prepare writer_bench data via writer-benchmark.sh');
  await spawnScript('bash', ['scripts/writer-benchmark.sh'], {
    TOTAL_EVENTS: '2000',
    BATCH_SIZES: '50 100 500 1000',
    OUTPUT_CSV: '.cache/writer-benchmark/results.csv',
    WRITER_RUNTIME_TOKEN: process.env.XDP_API_TOKEN || 'xdp-dev-token',
  }, 600_000);
  console.log('== phase == prepare writer_recovery data via writer-recovery-sample.sh');
  await spawnScript('bash', ['scripts/writer-recovery-sample.sh'], {
    WRITER_RUNTIME_TOKEN: process.env.XDP_API_TOKEN || 'xdp-dev-token',
  }, 180_000);
}

async function openIndexPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('[data-testid="logout"]')).toBeVisible();
  await page.click('[data-testid="nav-index"]');
  await expect(page.locator('[data-testid="index-page"]')).toBeVisible();
  await page.locator('[data-testid="index-page-size"]').selectOption('1000');
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

async function waitForResultMode(page: Page, mode: '事件视图' | '统计视图', timeout = 15_000) {
  await expect(page.locator('[data-testid="result-mode"]'), `预期结果模式为 ${mode}`).toContainText(mode, { timeout });
}

// 等待 index 数据量达标，轮询 API
async function waitForIndexCount(page: Page, indexName: string, minCount: number, timeoutMs = 90_000) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    try {
      const payload = await requestJSON(page, 'GET', `/api/v1/search?q=${encodeURIComponent(`index=${indexName}`)}&limit=1&page=1`);
      const total = Number(payload.pagination?.total || payload.total || payload.total_events || 0);
      if (total >= minCount) return;
    } catch {
      // 索引可能尚未创建，继续轮询
    }
    await new Promise((resolve) => setTimeout(resolve, 2000));
  }
  throw new Error(`等待 index=${indexName} 数据量 >= ${minCount} 超时`);
}

test.describe('TC-P1-WEB Writer 入库页面端到端', () => {
  test.describe.configure({ mode: 'serial' });
  test.setTimeout(180_000);

  test.beforeAll(async ({}, testInfo) => {
    testInfo.setTimeout(780_000);
    await prepareWriterData();
  });

  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('[data-testid="logout"]')).toBeVisible();
  });

  test('TC-P1-WEB-WRITER-001 查看 Writer 入库状态卡片', async ({ page }) => {
    console.log('== phase == TC-P1-WEB-WRITER-001 assert writer runtime panel');
    await openIndexPage(page);

    const panel = page.locator('[data-testid="writer-runtime-panel"]');
    await expect(panel, '[TC-P1-WEB-WRITER-001] 预期 Writer 入库状态卡片可见').toBeVisible({ timeout: 10_000 });

    // 状态应为 idle 或 running
    await expect(panel, '[TC-P1-WEB-WRITER-001] 预期状态为 idle 或 running').toContainText(/idle|running/);
    // 指标项可见：EPS、P95、失败率、Deadletter、批量策略
    await expect(panel, '[TC-P1-WEB-WRITER-001] 预期含 EPS 吞吐').toContainText(/EPS/);
    await expect(panel, '[TC-P1-WEB-WRITER-001] 预期含 P95 入库延迟').toContainText(/P95/);
    await expect(panel, '[TC-P1-WEB-WRITER-001] 预期含失败/Deadletter 指标').toContainText(/Deadletter|DLQ/);
    await expect(panel, '[TC-P1-WEB-WRITER-001] 预期含批量策略').toContainText(/批量|批/);

    // 点击刷新按钮，不报错
    console.log('== phase == TC-P1-WEB-WRITER-001 click refresh');
    await panel.locator('button', { hasText: '刷新' }).click();
    await expect(panel, '[TC-P1-WEB-WRITER-001] 预期刷新后仍可见且不报错').toBeVisible();
    await expect(panel, '[TC-P1-WEB-WRITER-001] 预期刷新后无错误提示').not.toContainText(/加载失败|error/i);
  });

  test('TC-P1-WEB-WRITER-002 查看 writer_bench index', async ({ page }) => {
    console.log('== phase == TC-P1-WEB-WRITER-002 assert writer_bench row');
    await waitForIndexCount(page, 'writer_bench', 2000);
    await openIndexPage(page);
    const row = page.locator('tr:has-text("writer_bench")').first();
    await expect(row, '[TC-P1-WEB-WRITER-002] 预期 writer_bench 行可见').toBeVisible({ timeout: 10_000 });
    await expect(row, '[TC-P1-WEB-WRITER-002] 预期含物理表名 events_writer_bench').toContainText('events_writer_bench');
    await expect(row, '[TC-P1-WEB-WRITER-002] 预期状态为 active').toContainText('active');
  });

  test('TC-P1-WEB-WRITER-003 查看 writer_recovery index', async ({ page }) => {
    console.log('== phase == TC-P1-WEB-WRITER-003 assert writer_recovery row');
    await openIndexPage(page);
    const row = page.locator('tr:has-text("writer_recovery")').first();
    await expect(row, '[TC-P1-WEB-WRITER-003] 预期 writer_recovery 行可见').toBeVisible({ timeout: 10_000 });
    await expect(row, '[TC-P1-WEB-WRITER-003] 预期含物理表名 events_writer_recovery').toContainText('events_writer_recovery');
    await expect(row, '[TC-P1-WEB-WRITER-003] 预期状态为 active').toContainText('active');
  });

  test('TC-P1-WEB-SEARCH-001 查询 writer_bench 原始事件', async ({ page }) => {
    console.log('== phase == TC-P1-WEB-SEARCH-001 search writer_bench events');
    await openSearchPage(page);
    await runSearch(page, 'index=writer_bench');
    await waitForResultMode(page, '事件视图');

    const results = page.locator('[data-testid="search-results"]');
    await expect(results, '[TC-P1-WEB-SEARCH-001] 预期命中 bench 数据').toContainText('writer benchmark');
    await expect(page.locator('[data-testid="search-pagination"]'), '[TC-P1-WEB-SEARCH-001] 预期分页控件可见').toBeVisible();
    await expect(page.locator('[data-testid="search-page-size"]'), '[TC-P1-WEB-SEARCH-001] 预期默认 20 条/页').toHaveValue('20');
  });

  test('TC-P1-WEB-SEARCH-002 展开 writer_bench 事件明细', async ({ page }) => {
    console.log('== phase == TC-P1-WEB-SEARCH-002 expand bench event detail');
    await openSearchPage(page);
    await runSearch(page, 'index=writer_bench');
    await waitForResultMode(page, '事件视图');

    const expandBtn = page.locator('[data-testid^="expand-event-"]').first();
    await expect(expandBtn, '[TC-P1-WEB-SEARCH-002] 预期存在展开按钮').toBeVisible();
    await expandBtn.click();

    const results = page.locator('[data-testid="search-results"]');
    await expect(results, '[TC-P1-WEB-SEARCH-002] 预期展开后含 raw').toContainText('writer benchmark');
    await expect(results, '[TC-P1-WEB-SEARCH-002] 预期展开后含 seq 字段').toContainText('seq');
    await expect(results, '[TC-P1-WEB-SEARCH-002] 预期展开后含 bytes 字段').toContainText('bytes');
    await expect(results, '[TC-P1-WEB-SEARCH-002] 预期展开后含 batch_size 字段').toContainText('batch_size');
    await expect(results, '[TC-P1-WEB-SEARCH-002] 预期展开后含 service 字段').toContainText('service');
    // 不展示分类/类型列
    await expect(results, '[TC-P1-WEB-SEARCH-002] 预期不含分类列').not.toContainText('分类');
    await expect(results, '[TC-P1-WEB-SEARCH-002] 预期不含类型列').not.toContainText(/\b类型\b/);
  });

  test('TC-P1-WEB-SEARCH-003 writer_bench 字段过滤', async ({ page }) => {
    console.log('== phase == TC-P1-WEB-SEARCH-003 field filter service=writer');
    await openSearchPage(page);
    await runSearch(page, 'index=writer_bench service=writer');
    await waitForResultMode(page, '事件视图');

    const results = page.locator('[data-testid="search-results"]');
    await expect(results, '[TC-P1-WEB-SEARCH-003] 预期命中 writer 服务数据').toContainText('writer benchmark');
  });

  test('TC-P1-WEB-SEARCH-004 writer_bench stats 聚合', async ({ page }) => {
    console.log('== phase == TC-P1-WEB-SEARCH-004 stats aggregate');
    await openSearchPage(page);
    await runSearch(page, 'index=writer_bench | stats count as total sum(bytes) as total_bytes avg(bytes) as avg_bytes by service batch_size');
    await waitForResultMode(page, '统计视图');

    const results = page.locator('[data-testid="search-results"]');
    await expect(results, '[TC-P1-WEB-SEARCH-004] 预期含 service 列').toContainText('service');
    await expect(results, '[TC-P1-WEB-SEARCH-004] 预期含 batch_size 列').toContainText('batch_size');
    await expect(results, '[TC-P1-WEB-SEARCH-004] 预期含 total 列').toContainText('total');
    await expect(results, '[TC-P1-WEB-SEARCH-004] 预期含 total_bytes 列').toContainText('total_bytes');
    await expect(results, '[TC-P1-WEB-SEARCH-004] 预期含 avg_bytes 列').toContainText('avg_bytes');
    await expect(results, '[TC-P1-WEB-SEARCH-004] 预期含 service=writer 行').toContainText('writer');
  });

  test('TC-P1-WEB-SEARCH-005 查询 writer_recovery 原始事件', async ({ page }) => {
    console.log('== phase == TC-P1-WEB-SEARCH-005 search writer_recovery events');
    await openSearchPage(page);
    await runSearch(page, 'index=writer_recovery');
    await waitForResultMode(page, '事件视图');

    const results = page.locator('[data-testid="search-results"]');
    await expect(results, '[TC-P1-WEB-SEARCH-005] 预期命中 recovery 数据').toContainText('writer recovery');
  });

  test('TC-P1-WEB-SEARCH-006 writer_recovery stats 聚合', async ({ page }) => {
    console.log('== phase == TC-P1-WEB-SEARCH-006 recovery stats by phase');
    await openSearchPage(page);
    await runSearch(page, 'index=writer_recovery | stats count as total by phase');
    await waitForResultMode(page, '统计视图');

    const results = page.locator('[data-testid="search-results"]');
    await expect(results, '[TC-P1-WEB-SEARCH-006] 预期含 phase 列').toContainText('phase');
    await expect(results, '[TC-P1-WEB-SEARCH-006] 预期含 total 列').toContainText('total');
    await expect(results, '[TC-P1-WEB-SEARCH-006] 预期含 phase=recovery 行').toContainText('recovery');
  });
});
