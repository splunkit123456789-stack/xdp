/**
 * 登录态 setup：生成 fixtures/admin.storageState
 *
 * 约定：登录态通过 storageState 复用，不每个用例重新登录。
 * 该脚本在 playwright.config.ts 的 setup project 中自动执行。
 */
import { test as setup, expect } from '@playwright/test';

const WEB_URL = process.env.XDP_WEB_URL || 'http://127.0.0.1:5173';
const ADMIN_USER = process.env.XDP_ADMIN_USER || 'admin';
const ADMIN_PASSWORD = process.env.XDP_ADMIN_PASSWORD || 'xdp';

setup('login as admin and save storageState', async ({ page }) => {
  await page.goto(`${WEB_URL}/`);
  // 等待登录页渲染
  await expect(page.locator('[data-testid="login-page"]')).toBeVisible({ timeout: 20_000 });

  await page.fill('input[name="username"]', ADMIN_USER);
  await page.fill('input[name="password"]', ADMIN_PASSWORD);
  await page.click('button[type="submit"]');

  const logout = page.locator('[data-testid="logout"]');
  const loginError = page.locator('[data-testid="login-error"]');
  await Promise.race([
    logout.waitFor({ state: 'visible', timeout: 20_000 }),
    loginError.waitFor({ state: 'visible', timeout: 20_000 }).then(async () => {
      throw new Error(`login failed: ${(await loginError.innerText()).trim()}`);
    }),
  ]);

  await page.context().storageState({ path: 'fixtures/admin.storageState' });
});
