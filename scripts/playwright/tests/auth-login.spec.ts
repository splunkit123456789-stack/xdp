/**
 * 登录与登出端到端验收脚本
 *
 * 对应人工验收文档：docs/requirements/references/XDP_P0页面人工验收测试用例.md
 * 覆盖用例：TC-P0-LOGIN-001、TC-P0-LOGIN-002、TC-P0-LOGIN-003、TC-P0-AUTH-001
 *
 * 验收链路：
 *   打开登录页 → 必填校验 → 错误密码失败提示
 *   → 正确账号登录进入控制台 → 退出回到登录页 → 受保护数据不可直访
 *
 * 运行：npx playwright test tests/auth-login.spec.ts --project=admin
 *
 * 说明：本脚本不依赖 storageState，每个用例从登录页起步，验证完整鉴权链路。
 */
import { test, expect } from '@playwright/test';

const API_URL = process.env.XDP_API_URL || 'http://127.0.0.1:8080';

test.describe('TC-P0-LOGIN/AUTH 登录与登出端到端', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeEach(async ({ page }) => {
    // 每个用例从空白登录页起步，避免 storageState 污染
    console.log('== phase == open login page from clean context');
    await page.context().clearCookies();
    await page.addInitScript(() => {
      window.localStorage.clear();
      window.sessionStorage.clear();
    });
    await page.goto('/');
    await expect(page.locator('[data-testid="login-page"]'), '[setup] 预期展示登录页').toBeVisible();
  });

  test('TC-P0-LOGIN-001 登录必填校验', async ({ page }) => {
    console.log('== phase == TC-P0-LOGIN-001 submit empty credentials and expect block');
    // 前端默认预填 admin，需清空用户名和密码以触发必填校验
    await page.locator('input[placeholder="请输入用户名"]').fill('');
    await page.locator('input[placeholder="请输入密码"]').fill('');

    // 点击登录按钮，断言前端拦截而非调用后端
    const loginRequest = page.waitForRequest((req) => req.url().includes('/api/v1/login'), { timeout: 3_000 }).catch(() => null);
    await page.locator('form.login-form button[type="submit"]').click();

    const usernameMissing = await page.locator('input[placeholder="请输入用户名"]').evaluate((el: HTMLInputElement) => el.validity.valueMissing);
    const passwordMissing = await page.locator('input[placeholder="请输入密码"]').evaluate((el: HTMLInputElement) => el.validity.valueMissing);

    // 预期：浏览器 required 校验直接拦截，不发送登录请求
    expect(usernameMissing, '[TC-P0-LOGIN-001] 预期用户名触发必填校验').toBe(true);
    expect(passwordMissing, '[TC-P0-LOGIN-001] 预期密码触发必填校验').toBe(true);
    await expect(page.locator('[data-testid="login-page"]'), '[TC-P0-LOGIN-001] 预期不跳转仍停留在登录页').toBeVisible();
    expect(await loginRequest, '[TC-P0-LOGIN-001] 预期不发送登录请求').toBeNull();
  });

  test('TC-P0-LOGIN-002 错误密码登录失败', async ({ page }) => {
    console.log('== phase == TC-P0-LOGIN-002 submit wrong password and expect failure');
    await page.locator('input[placeholder="请输入用户名"]').fill('admin');
    await page.locator('input[placeholder="请输入密码"]').fill('wrong');

    // 拦截登录请求，断言确实调用了后端
    const [response] = await Promise.all([
      page.waitForResponse((res) => res.url().includes('/api/v1/login') && res.request().method() === 'POST'),
      page.locator('form.login-form button[type="submit"]').click(),
    ]);

    // 后端应拒绝错误凭据
    expect(response.status(), '[TC-P0-LOGIN-002] 预期后端返回非 2xx 拒绝错误凭据').toBeGreaterThanOrEqual(400);

    // 前端应展示登录失败提示
    await expect(page.locator('[data-testid="login-error"]'), '[TC-P0-LOGIN-002] 预期前端展示登录失败提示').toBeVisible();
    await expect(page.locator('[data-testid="login-page"]'), '[TC-P0-LOGIN-002] 预期不进入控制台仍停留在登录页').toBeVisible();

    // 本地不应保存有效 Token
    const token = await page.evaluate(() => localStorage.getItem('xdp_api_token') || '');
    expect(token, '[TC-P0-LOGIN-002] 预期本地不保存有效 Token').toBe('');
  });

  test('TC-P0-LOGIN-003 正确账号登录成功', async ({ page }) => {
    console.log('== phase == TC-P0-LOGIN-003 submit valid credentials and assert console');
    await page.locator('input[placeholder="请输入用户名"]').fill('admin');
    await page.locator('input[placeholder="请输入密码"]').fill('xdp');

    const [response] = await Promise.all([
      page.waitForResponse((res) => res.url().includes('/api/v1/login') && res.request().method() === 'POST'),
      page.locator('form.login-form button[type="submit"]').click(),
    ]);

    // 后端应接受正确凭据
    expect(response.ok(), '[TC-P0-LOGIN-003] 预期后端返回 2xx 接受正确凭据').toBe(true);

    // 应进入控制台外壳
    await expect(page.locator('[data-testid="console-shell"]'), '[TC-P0-LOGIN-003] 预期进入控制台外壳').toBeVisible({ timeout: 10_000 });

    // 顶部导航应展示主模块入口（采集/解析/索引/搜索至少存在）
    await expect(page.locator('[data-testid="nav-collect"]'), '[TC-P0-LOGIN-003] 预期顶部导航含采集配置').toBeVisible();
    await expect(page.locator('[data-testid="nav-parse"]'), '[TC-P0-LOGIN-003] 预期顶部导航含解析配置').toBeVisible();
    await expect(page.locator('[data-testid="nav-index"]'), '[TC-P0-LOGIN-003] 预期顶部导航含索引配置').toBeVisible();
    await expect(page.locator('[data-testid="nav-search"]'), '[TC-P0-LOGIN-003] 预期顶部导航含搜索页').toBeVisible();

    // 登出按钮应可见
    await expect(page.locator('[data-testid="logout"]'), '[TC-P0-LOGIN-003] 预期展示退出入口').toBeVisible();

    // 本地应保存有效 Token
    const token = await page.evaluate(() => localStorage.getItem('xdp_api_token') || '');
    expect(token.length, '[TC-P0-LOGIN-003] 预期本地保存有效 Token').toBeGreaterThan(0);
  });

  test('TC-P0-AUTH-001 登出后回到登录页且受保护数据不可直访', async ({ page }) => {
    console.log('== phase == TC-P0-AUTH-001 login, logout and assert protected data inaccessible');
    // 先登录以获得登出操作入口
    await page.locator('input[placeholder="请输入用户名"]').fill('admin');
    await page.locator('input[placeholder="请输入密码"]').fill('xdp');
    await page.locator('form.login-form button[type="submit"]').click();
    await expect(page.locator('[data-testid="console-shell"]'), '[TC-P0-AUTH-001] setup: 预期登录后进入控制台').toBeVisible({ timeout: 10_000 });
    await expect(page.locator('[data-testid="logout"]'), '[TC-P0-AUTH-001] setup: 预期展示退出入口').toBeVisible();

    // 点击退出
    console.log('== phase == TC-P0-AUTH-001 click logout');
    await page.locator('[data-testid="logout"]').click();

    // 应回到登录页
    await expect(page.locator('[data-testid="login-page"]'), '[TC-P0-AUTH-001] 预期退出后回到登录页').toBeVisible({ timeout: 10_000 });

    // 本地 Token 应被清除
    const token = await page.evaluate(() => localStorage.getItem('xdp_api_token') || '');
    expect(token, '[TC-P0-AUTH-001] 预期退出后本地 Token 被清除').toBe('');

    // 受保护 API 不应可直访：直接请求应返回 401
    console.log('== phase == TC-P0-AUTH-001 assert protected API rejects');
    const protectedResponse = await page.request.get(`${API_URL}/api/v1/datasources?page=1&page_size=1`);
    expect(protectedResponse.status(), '[TC-P0-AUTH-001] 预期受保护 API 返回 401 拒绝直访').toBe(401);

    // 刷新页面后仍应停留在登录页，不会自动回控制台
    await page.reload();
    await expect(page.locator('[data-testid="login-page"]'), '[TC-P0-AUTH-001] 预期刷新后仍停留在登录页').toBeVisible();
  });
});
