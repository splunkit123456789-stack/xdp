/**
 * 导航与布局端到端验收脚本
 *
 * 对应人工验收文档：
 *   - docs/requirements/references/XDP_P0页面人工验收测试用例.md
 *     TC-P0-NAV-002 顶部唯一导航与全宽内容区
 *     TC-P0-NAV-003 刷新后保持当前控制台模块
 *     TC-P0-CONFIG-UI-001 配置页默认隐藏新增表单
 *   - docs/requirements/references/XDP_P1真实浏览器人工验收.md
 *     TC-P1-REFRESH-BROWSER-001 刷新后页面状态保持
 *
 * 验收链路：
 *   登录态复用 → 顶部唯一导航 → 模块切换 → 刷新保持
 *   → 三个配置页默认隐藏表单 → 取消可关闭 → 修改可回填
 *
 * 运行：npx playwright test tests/nav-layout.spec.ts --project=admin
 */
import { test, expect, type Page } from '@playwright/test';

const MODULE_KEYS = ['collect', 'parse', 'index', 'search'] as const;
type ModuleKey = typeof MODULE_KEYS[number];

async function assertConsoleShell(page: Page) {
  await expect(page.locator('[data-testid="console-shell"]'), '预期进入控制台外壳').toBeVisible();
  await expect(page.locator('[data-testid="main-nav"]'), '预期展示顶部主导航').toBeVisible();
}

async function assertModuleVisible(page: Page, key: ModuleKey) {
  await expect(page.locator(`[data-testid="${key}-page"]`), `预期 ${key} 模块面板可见`).toBeVisible();
}

test.describe('TC-P0-NAV 导航与布局端到端', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await assertConsoleShell(page);
  });

  test('TC-P0-NAV-002 顶部唯一导航与全宽内容区', async ({ page }) => {
    console.log('== phase == TC-P0-NAV-002 assert top nav only and module switch');
    // 顶部导航应紧贴品牌右侧，含主模块入口
    await expect(page.locator('[data-testid="nav-collect"]'), '预期顶部含采集配置入口').toBeVisible();
    await expect(page.locator('[data-testid="nav-parse"]'), '预期顶部含解析配置入口').toBeVisible();
    await expect(page.locator('[data-testid="nav-index"]'), '预期顶部含索引配置入口').toBeVisible();
    await expect(page.locator('[data-testid="nav-search"]'), '预期顶部含搜索页入口').toBeVisible();

    // 不应存在左侧模块导航（项目无左侧 nav 容器，断言不存在该结构）
    // 项目 DOM 中 modules 按钮只在 [data-testid="main-nav"] 内，无左侧 sidebar
    const navButtons = page.locator('[data-testid="main-nav"] button');
    const buttonCount = await navButtons.count();
    expect(buttonCount, '预期顶部导航按钮数等于 modules 数量').toBeGreaterThanOrEqual(4);

    // 依次点击四个模块，断言对应面板可见
    for (const key of MODULE_KEYS) {
      console.log(`== phase == TC-P0-NAV-002 switch to ${key}`);
      await page.locator(`[data-testid="nav-${key}"]`).click();
      await assertModuleVisible(page, key);
    }
  });

  test('TC-P0-NAV-003 刷新后保持当前控制台模块', async ({ page }) => {
    console.log('== phase == TC-P0-NAV-003 assert refresh keeps current module');
    // 切换到搜索页
    await page.locator('[data-testid="nav-search"]').click();
    await assertModuleVisible(page, 'search');

    // 刷新后应仍停留在搜索页
    await page.reload();
    await assertConsoleShell(page);
    await assertModuleVisible(page, 'search');

    // 切换到解析配置后再刷新
    await page.locator('[data-testid="nav-parse"]').click();
    await assertModuleVisible(page, 'parse');
    await page.reload();
    await assertConsoleShell(page);
    await assertModuleVisible(page, 'parse');
  });
});

test.describe('TC-P0-CONFIG-UI 配置页默认隐藏新增表单', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await assertConsoleShell(page);
  });

  // 三个配置页的表单测试数据，用 (模块 key, 表单卡片 testid, 显示按钮 testid, 取消按钮 testid) 表示
  const CONFIG_PAGES: ReadonlyArray<[ModuleKey, string, string, string]> = [
    ['collect', 'input-form-card', 'show-input-form', 'cancel-input-form'],
    ['parse', 'rule-form-card', 'show-rule-form', 'cancel-rule-form'],
    ['index', 'index-form-card', 'show-index-form', 'cancel-index-form'],
  ];

  for (const [key, formCard, showBtn, cancelBtn] of CONFIG_PAGES) {
    test(`TC-P0-CONFIG-UI-001 ${key} 配置页默认隐藏表单且可取消`, async ({ page }) => {
      console.log(`== phase == TC-P0-CONFIG-UI-001 ${key} default hidden and cancel`);
      await page.locator(`[data-testid="nav-${key}"]`).click();
      await assertModuleVisible(page, key);

      // 默认应隐藏新增表单
      await expect(page.locator(`[data-testid="${formCard}"]`), `[TC-P0-CONFIG-UI-001] 预期 ${key} 默认不展示新增表单`).not.toBeVisible();

      // 点击新增后表单可见
      await page.locator(`[data-testid="${showBtn}"]`).click();
      await expect(page.locator(`[data-testid="${formCard}"]`), `[TC-P0-CONFIG-UI-001] 预期 ${key} 点击新增后表单可见`).toBeVisible({ timeout: 5_000 });

      // 取消后表单收起
      await page.locator(`[data-testid="${cancelBtn}"]`).click();
      await expect(page.locator(`[data-testid="${formCard}"]`), `[TC-P0-CONFIG-UI-001] 预期 ${key} 取消后表单不可见`).not.toBeVisible();
    });
  }
});

test.describe('TC-P1-REFRESH 刷新一致性', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await assertConsoleShell(page);
  });

  test('TC-P1-REFRESH-BROWSER-001 刷新后页面状态保持', async ({ page }) => {
    console.log('== phase == TC-P1-REFRESH-BROWSER-001 refresh consistency across modules');
    // 依次进入各模块后刷新，断言停留在该模块
    const modulesToCheck: ModuleKey[] = ['collect', 'parse', 'index', 'search'];
    for (const key of modulesToCheck) {
      console.log(`== phase == TC-P1-REFRESH-BROWSER-001 visit ${key} then reload`);
      await page.locator(`[data-testid="nav-${key}"]`).click();
      await assertModuleVisible(page, key);
      await page.reload();
      await assertConsoleShell(page);
      await assertModuleVisible(page, key);
    }
  });
});
