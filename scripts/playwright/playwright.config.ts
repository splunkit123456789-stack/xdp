import { defineConfig, devices } from '@playwright/test';

const browserChannel = process.env.XDP_PLAYWRIGHT_CHANNEL || undefined;

/**
 * XDP 页面验收 Playwright 配置
 *
 * 约定见 scripts/README.md「页面验收约定（Playwright）」：
 * - 只安装 chromium，不装 firefox/webkit
 * - 登录态通过 storageState 复用，不每个用例重新登录
 * - 报告输出到 scripts/playwright/reports/
 */
export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  forbidRetry: false,
  retries: 0,
  workers: 1,
  reporter: [
    ['list'],
    ['html', { outputDir: 'reports/html' }],
    ['json', { outputFile: 'reports/result.json' }],
    ['junit', { outputFile: 'reports/junit.xml' }],
  ],
  use: {
    baseURL: process.env.XDP_WEB_URL || 'http://127.0.0.1:5173',
    ...(browserChannel ? { channel: browserChannel } : {}),
    trace: 'retain-on-failure',      // 失败即录 trace.zip，供 show-trace 回放
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    console: 'retain-on-failure',    // 保留浏览器 console 日志
    network: 'retain-on-failure',    // 保留网络请求日志
    actionTimeout: 10_000,
    navigationTimeout: 15_000,
  },
  projects: [
    {
      name: 'setup',
      testMatch: /.*\.setup\.ts/,
    },
    {
      name: 'admin',
      dependencies: ['setup'],
      testIgnore: /env-clean\.spec\.ts/,
      use: {
        ...devices['Desktop Chrome'],
        storageState: 'fixtures/admin.storageState',
      },
    },
    {
      name: 'clean-env',
      testMatch: /env-clean\.spec\.ts/,
      use: {
        ...devices['Desktop Chrome'],
      },
    },
  ],
});
